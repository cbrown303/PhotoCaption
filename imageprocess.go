package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
)

// Caption rendering constants — must mirror the values in frontend/index.html.
const (
	fontSizePercent = 0.03  // 3% of image width
	fontSizeMin     = 28.0  // minimum px
	paddingPercent  = 0.02  // 2% of image width
	lineGapPercent  = 0.008 // 0.8% of image width
)

var (
	captionBG     = color.RGBA{10, 10, 10, 255}
	captionText   = color.RGBA{240, 240, 240, 255}
	captionBorder = color.RGBA{60, 60, 60, 255}
)

// loadImage decodes a JPEG or PNG from disk.
func loadImage(filePath string) (image.Image, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// AppendCaptionToImage crops img to origHeight (removing any prior caption),
// renders description as a styled caption block, and saves atomically.
func AppendCaptionToImage(filePath string, origHeight int, description string) error {
	img, err := loadImage(filePath)
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

	// Derive caption metrics proportional to image width.
	fontSize := math.Max(fontSizeMin, float64(imgW)*fontSizePercent)
	padding := int(math.Round(float64(imgW) * paddingPercent))
	lineGap := max(2, int(math.Round(float64(imgW)*lineGapPercent)))
	lineHeight := int(math.Ceil(fontSize * 1.2))

	// Parse the TrueType font and create a face for measuring.
	ttf, err := truetype.Parse(goregular.TTF)
	if err != nil {
		return err
	}
	face := truetype.NewFace(ttf, &truetype.Options{Size: fontSize, DPI: 72})
	defer face.Close()

	// Word-wrap the description.
	lines := wrapText(description, face, imgW-2*padding)

	// Calculate total caption height.
	nLines := len(lines)
	captionH := padding + nLines*lineHeight + max(0, nLines-1)*lineGap + padding
	if nLines == 0 {
		captionH = padding * 2
	}

	// Compose output image: original + 1px border + caption.
	totalH := cropH + 1 + captionH
	out := image.NewRGBA(image.Rect(0, 0, imgW, totalH))

	// Draw original portion.
	imgBounds := img.Bounds()
	draw.Draw(out, image.Rect(0, 0, imgW, cropH), img, imgBounds.Min, draw.Src)

	// Draw 2px-style border (1px line at cropH, fill with border colour).
	for x := range imgW {
		out.Set(x, cropH, captionBorder)
	}

	// Fill caption background.
	draw.Draw(out,
		image.Rect(0, cropH+1, imgW, totalH),
		&image.Uniform{captionBG},
		image.Point{},
		draw.Src,
	)

	// Render text with freetype.
	fc := freetype.NewContext()
	fc.SetDPI(72)
	fc.SetFont(ttf)
	fc.SetFontSize(fontSize)
	fc.SetClip(out.Bounds())
	fc.SetDst(out)
	fc.SetSrc(image.NewUniform(captionText))

	y := cropH + 1 + padding + lineHeight
	for _, line := range lines {
		if line != "" {
			pt := freetype.Pt(padding, y)
			if _, err := fc.DrawString(line, pt); err != nil {
				return err
			}
		}
		y += lineHeight + lineGap
	}

	// Save atomically.
	return saveImage(filePath, out)
}

// wrapText splits text into lines that fit within maxWidth, honouring hard
// line breaks (newline characters) and soft word-wrapping.
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
