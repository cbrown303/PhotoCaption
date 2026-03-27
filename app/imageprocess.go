// Copyright (c) 2026 Christopher Brown
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	exif "github.com/dsoprea/go-exif/v3"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
)

// Caption rendering constants — must mirror the values in frontend/index.html.
const (
	fontSizePercent   = 0.03  // 3% of image width
	fontSizeMin       = 28.0  // minimum px
	paddingPercent    = 0.02  // background padding outside the label (2% of image width)
	lineGapPercent    = 0.008 // 0.8% of image width
	labelVPadRatio    = 0.50  // label vertical padding as fraction of fontSize
	labelHPadRatio    = 1.00  // label horizontal padding as fraction of fontSize
	cornerRadiusRatio = 0.40  // corner radius as fraction of fontSize
)

// parseHexColor converts a "#rrggbb" string to color.RGBA (alpha=255).
// Returns opaque black on any parse error.
func parseHexColor(s string) color.RGBA {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return color.RGBA{A: 255}
	}
	val, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.RGBA{A: 255}
	}
	return color.RGBA{R: uint8(val >> 16), G: uint8(val >> 8), B: uint8(val), A: 255}
}

// drawRoundedRect fills a rounded rectangle on img.
func drawRoundedRect(img *image.RGBA, x0, y0, x1, y1, radius int, col color.Color) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			var cx, cy int
			inCorner := false
			switch {
			case x < x0+radius && y < y0+radius:
				cx, cy, inCorner = x0+radius, y0+radius, true
			case x >= x1-radius && y < y0+radius:
				cx, cy, inCorner = x1-radius, y0+radius, true
			case x < x0+radius && y >= y1-radius:
				cx, cy, inCorner = x0+radius, y1-radius, true
			case x >= x1-radius && y >= y1-radius:
				cx, cy, inCorner = x1-radius, y1-radius, true
			}
			if inCorner {
				dx, dy := x-cx, y-cy
				if dx*dx+dy*dy > radius*radius {
					continue
				}
			}
			img.Set(x, y, col)
		}
	}
}

// LoadImage decodes a JPEG or PNG from disk.
func LoadImage(filePath string) (image.Image, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// AppendCaptionToImage crops img to origHeight (removing any prior caption),
// renders description as a label-style caption, and saves atomically.
// origExif is the raw APP1 EXIF segment bytes to restore after the pixel rewrite.
func AppendCaptionToImage(filePath string, origHeight int, description string, origExif []byte, s Settings) error {
	fmt.Printf("[DEBUG] AppendCaptionToImage called — file=%q origHeight=%d exifBytes=%d\n", filePath, origHeight, len(origExif))

	img, err := LoadImage(filePath)
	if err != nil {
		return err
	}

	bounds := img.Bounds()
	imgW := bounds.Dx()

	// Crop to original height when a prior caption exists.
	cropH := bounds.Dy()
	if origHeight > 0 && origHeight < bounds.Dy() {
		cropH = origHeight
		if si, ok := img.(interface {
			SubImage(image.Rectangle) image.Image
		}); ok {
			img = si.SubImage(image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Min.X+imgW, bounds.Min.Y+cropH))
		}
	}

	// Parse caption colors from settings.
	textColor := parseHexColor(s.CaptionTextColor)
	labelBg   := parseHexColor(s.CaptionLabelBg)
	bgColor   := parseHexColor(s.CaptionBackground)

	// Derive caption metrics proportional to image width.
	fontSize := math.Max(fontSizeMin, float64(imgW)*fontSizePercent)
	if s.CaptionFontSize > 0 {
		fontSize = float64(s.CaptionFontSize)
	}
	bgPad      := int(math.Round(float64(imgW) * paddingPercent))
	lineGap    := max(2, int(math.Round(float64(imgW)*lineGapPercent)))
	lineHeight := int(math.Ceil(fontSize * 1.2))
	labelVPad  := int(math.Round(fontSize * labelVPadRatio))
	labelHPad  := int(math.Round(fontSize * labelHPadRatio))
	cornerR    := int(math.Round(fontSize * cornerRadiusRatio))

	// Parse font and create face for text measurement.
	ttf, err := truetype.Parse(goregular.TTF)
	if err != nil {
		return err
	}
	face := truetype.NewFace(ttf, &truetype.Options{Size: fontSize, DPI: 72})
	defer face.Close()

	// Word-wrap within the label's inner content width.
	maxTextW := imgW - 2*bgPad - 2*labelHPad
	if maxTextW < 1 {
		maxTextW = imgW / 2
	}
	lines  := wrapText(description, face, maxTextW)
	nLines := len(lines)

	// Find the rendered pixel width of the widest line to size the label tightly.
	maxLineW := 0
	for _, line := range lines {
		if w := measureText(line, face); w > maxLineW {
			maxLineW = w
		}
	}
	labelW := maxLineW + 2*labelHPad
	if maxLabelW := imgW - 2*bgPad; labelW > maxLabelW {
		labelW = maxLabelW
	}

	// Calculate heights.
	textBlockH := nLines*lineHeight + max(0, nLines-1)*lineGap
	labelH     := labelVPad + textBlockH + labelVPad
	captionH   := bgPad + labelH + bgPad
	if nLines == 0 {
		captionH = bgPad * 2
	}
	totalH := cropH + 1 + captionH

	// Compose output image.
	out := image.NewRGBA(image.Rect(0, 0, imgW, totalH))

	// Draw original portion.
	imgBounds := img.Bounds()
	draw.Draw(out, image.Rect(0, 0, imgW, cropH), img, imgBounds.Min, draw.Src)

	// 1px separator (background colour for a seamless edge).
	for x := range imgW {
		out.Set(x, cropH, bgColor)
	}

	// Fill caption background.
	draw.Draw(out,
		image.Rect(0, cropH+1, imgW, totalH),
		&image.Uniform{bgColor},
		image.Point{},
		draw.Src,
	)

	// Draw the rounded label box, centred horizontally.
	labelX0 := (imgW - labelW) / 2
	labelY0 := cropH + 1 + bgPad
	drawRoundedRect(out, labelX0, labelY0, labelX0+labelW, labelY0+labelH, cornerR, labelBg)

	// Render text inside the label.
	fc := freetype.NewContext()
	fc.SetDPI(72)
	fc.SetFont(ttf)
	fc.SetFontSize(fontSize)
	fc.SetClip(out.Bounds())
	fc.SetDst(out)
	fc.SetSrc(image.NewUniform(textColor))

	y := labelY0 + labelVPad + lineHeight
	for _, line := range lines {
		if line != "" {
			pt := freetype.Pt(labelX0+labelHPad, y)
			if _, err := fc.DrawString(line, pt); err != nil {
				return err
			}
		}
		y += lineHeight + lineGap
	}

	fmt.Println("[AppendCaptionToImage] Saving Image")
	if err := saveImage(filePath, out); err != nil {
		fmt.Printf("[AppendCaptionToImage] Error: %v\n", err)
		return err
	}

	fmt.Println("[AppendCaptionToImage] Injecting original EXIF snapshot....")
	if len(origExif) >= 6 {
		if tags, _, err := exif.GetFlatExifData(origExif[6:], nil); err == nil {
			fmt.Printf("[AppendCaptionToImage] origExif snapshot contains %d tags\n", len(tags))
		} else {
			fmt.Printf("[AppendCaptionToImage] origExif snapshot parse warning: %v\n", err)
		}
		if err := InjectExifSegment(filePath, origExif); err != nil {
			return fmt.Errorf("reinject exif after pixel write: %w", err)
		}
	} else {
		fmt.Printf("[AppendCaptionToImage] origExif snapshot is empty — skipping inject\n")
	}
	fmt.Println("[AppendCaptionToImage] EXIF inject done")
	return nil
}

// wrapText splits text into lines that fit within maxWidth, honouring hard
// line breaks and soft word-wrapping.
func wrapText(text string, face font.Face, maxWidth int) []string {
	var result []string
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	hardLines := strings.SplitSeq(normalized, "\n")

	for hardLine := range hardLines {
		if strings.TrimSpace(hardLine) == "" {
			result = append(result, "")
			continue
		}
		words := strings.Fields(hardLine)
		current := ""
		for _, word := range words {
			candidate := word
			if current != "" {
				candidate = current + " " + word
			}
			if measureText(candidate, face) > maxWidth && current != "" {
				result = append(result, current)
				current = word
			} else {
				current = candidate
			}
		}
		if current != "" {
			result = append(result, current)
		}
	}
	return result
}

// measureText returns the pixel advance width of text rendered in face.
func measureText(text string, face font.Face) int {
	w := font.MeasureString(face, text)
	return int(w >> 6) // fixed.Int26_6 → pixels
}

// saveImage writes img to filePath (JPEG or PNG) via an atomic temp-file rename.
func saveImage(filePath string, img image.Image) error {
	ext := strings.ToLower(filepath.Ext(filePath))
	dir := filepath.Dir(filePath)

	tmp, err := os.CreateTemp(dir, "photocaption-img-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	var writeErr error
	switch ext {
	case ".jpg", ".jpeg":
		writeErr = jpeg.Encode(tmp, img, &jpeg.Options{Quality: 95})
	case ".png":
		writeErr = png.Encode(tmp, img)
	default:
		writeErr = jpeg.Encode(tmp, img, &jpeg.Options{Quality: 95})
	}
	tmp.Close()

	if writeErr != nil {
		os.Remove(tmpPath)
		return writeErr
	}
	return os.Rename(tmpPath, filePath)
}
