package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	exif "github.com/dsoprea/go-exif/v3"
	exifcommon "github.com/dsoprea/go-exif/v3/common"
	jpegstructure "github.com/dsoprea/go-jpeg-image-structure/v2"
	pngstructure "github.com/dsoprea/go-png-image-structure/v2"
)

const (
	heightMarkerPrefix = "PHOTOCAPTION_H="
	heightMarkerSep    = " | "
)

// parseImageDescription splits a raw ImageDescription EXIF value into the
// user-visible description and the stored original height (0 if absent).
func parseImageDescription(raw string) (userDesc string, origHeight int) {
	idx := strings.Index(raw, heightMarkerPrefix)
	if idx < 0 {
		return strings.TrimSpace(raw), 0
	}

	// Everything before the marker is the user description.
	prefix := strings.TrimSpace(raw[:idx])
	userDesc = strings.TrimSuffix(prefix, "|")
	userDesc = strings.TrimSpace(userDesc)

	// Parse the height integer that follows the marker prefix.
	heightPart := raw[idx+len(heightMarkerPrefix):]
	if end := strings.IndexAny(heightPart, " \t\n\r|"); end >= 0 {
		heightPart = heightPart[:end]
	}
	h, err := strconv.Atoi(strings.TrimSpace(heightPart))
	if err == nil {
		origHeight = h
	}
	return
}

// buildImageDescription combines a user description and original height into
// the value stored in EXIF ImageDescription.
func buildImageDescription(userDesc string, origHeight int) string {
	if origHeight <= 0 {
		return userDesc
	}
	marker := fmt.Sprintf("%s%d", heightMarkerPrefix, origHeight)
	if userDesc == "" {
		return marker
	}
	return userDesc + heightMarkerSep + marker
}

// ReadMetadata returns the user-visible description and original height from
// EXIF ImageDescription. Non-fatal: returns zero values on any error.
// Also prints all discovered EXIF tags to stdout for debugging.
func ReadMetadata(filePath string) (userDesc string, origHeight int, err error) {
	printAllExifTags(filePath)
	raw, readErr := getRawImageDescription(filePath)
	if readErr != nil {
		return "", 0, nil
	}
	userDesc, origHeight = parseImageDescription(raw)
	return
}

// printAllExifTags reads every EXIF tag in filePath and prints them to stdout.
func printAllExifTags(filePath string) {
	rawExif, err := getRawExifBytes(filePath)
	if err != nil {
		fmt.Printf("[EXIF] No EXIF data found in %s: %v\n", filePath, err)
		return
	}
	tags, _, err := exif.GetFlatExifData(rawExif, nil)
	if err != nil {
		fmt.Printf("[EXIF] Failed to parse EXIF from %s: %v\n", filePath, err)
		return
	}
	fmt.Printf("[EXIF] %d tags found in %s:\n", len(tags), filePath)
	for _, tag := range tags {
		fmt.Printf("  %-40s %v\n", tag.TagName, tag.Value)
	}
}

// getRawExifBytes extracts raw EXIF bytes from a JPEG or PNG file.
func getRawExifBytes(filePath string) ([]byte, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg":
		jmp := jpegstructure.NewJpegMediaParser()
		intfc, err := jmp.ParseFile(filePath)
		if err != nil {
			return nil, err
		}
		_, rawExif, err := intfc.(*jpegstructure.SegmentList).Exif()
		return rawExif, err
	case ".png":
		pmp := pngstructure.NewPngMediaParser()
		intfc, err := pmp.ParseFile(filePath)
		if err != nil {
			return nil, err
		}
		_, rawExif, err := intfc.(*pngstructure.ChunkSlice).Exif()
		return rawExif, err
	default:
		return nil, fmt.Errorf("unsupported format: %s", ext)
	}
}

// ReadOriginalHeight returns just the stored original pixel height (0 if none).
func ReadOriginalHeight(filePath string) (int, error) {
	_, h, err := ReadMetadata(filePath)
	return h, err
}

// WriteDescription writes userDesc + origHeight marker to EXIF ImageDescription,
// preserving all existing EXIF tags.
func WriteDescription(filePath, userDesc string, origHeight int) error {
	value := buildImageDescription(userDesc, origHeight)
	return writeExifImageDescription(filePath, value)
}

// ─── internal helpers ───────────────────────────────────────────────────────

func getRawImageDescription(filePath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg":
		return getJpegImageDescription(filePath)
	case ".png":
		return getPngImageDescription(filePath)
	default:
		return "", fmt.Errorf("unsupported format: %s", ext)
	}
}

func getJpegImageDescription(filePath string) (string, error) {
	jmp := jpegstructure.NewJpegMediaParser()
	intfc, err := jmp.ParseFile(filePath)
	if err != nil {
		return "", err
	}
	sl := intfc.(*jpegstructure.SegmentList)
	_, rawExif, err := sl.Exif()
	if err != nil {
		return "", err
	}
	return extractImageDescriptionFromExifBytes(rawExif)
}

func getPngImageDescription(filePath string) (string, error) {
	pmp := pngstructure.NewPngMediaParser()
	intfc, err := pmp.ParseFile(filePath)
	if err != nil {
		return "", err
	}
	cs := intfc.(*pngstructure.ChunkSlice)
	_, rawExif, err := cs.Exif()
	if err != nil {
		return "", err
	}
	return extractImageDescriptionFromExifBytes(rawExif)
}

func extractImageDescriptionFromExifBytes(rawExif []byte) (string, error) {
	tags, _, err := exif.GetFlatExifData(rawExif, nil)
	if err != nil {
		return "", err
	}
	for _, tag := range tags {
		if tag.TagName == "ImageDescription" {
			return fmt.Sprintf("%v", tag.Value), nil
		}
	}
	return "", fmt.Errorf("ImageDescription tag not found")
}

// writeExifImageDescription writes value into EXIF ImageDescription for the
// given file, seeding the IfdBuilder from the full existing EXIF so that
// all original tags (Make, Model, GPS, DateTimeOriginal, …) are preserved.
func writeExifImageDescription(filePath, value string) error {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg":
		return writeJpegExifDescription(filePath, value)
	case ".png":
		return writePngExifDescription(filePath, value)
	default:
		return fmt.Errorf("unsupported format: %s", ext)
	}
}

func newFreshIfdBuilder() (*exif.IfdBuilder, error) {
	im, err := exifcommon.NewIfdMappingWithStandard()
	if err != nil {
		return nil, err
	}
	ti := exif.NewTagIndex()
	if err := exif.LoadStandardTags(ti); err != nil {
		return nil, err
	}
	return exif.NewIfdBuilder(im, ti, exifcommon.IfdStandardIfdIdentity, binary.BigEndian), nil
}

func writeJpegExifDescription(filePath, value string) error {
	jmp := jpegstructure.NewJpegMediaParser()
	intfc, err := jmp.ParseFile(filePath)
	if err != nil {
		return fmt.Errorf("parse jpeg: %w", err)
	}
	sl := intfc.(*jpegstructure.SegmentList)

	rootIb, err := sl.ConstructExifBuilder()
	if err != nil {
		rootIb, err = newFreshIfdBuilder()
		if err != nil {
			return fmt.Errorf("create ifd builder: %w", err)
		}
	}

	if err = rootIb.SetStandardWithName("ImageDescription", value); err != nil {
		return fmt.Errorf("set ImageDescription: %w", err)
	}

	if err = sl.SetExif(rootIb); err != nil {
		return fmt.Errorf("set exif in segment list: %w", err)
	}

	return atomicWrite(filePath, func(f *os.File) error {
		return sl.Write(f)
	})
}

func writePngExifDescription(filePath, value string) error {
	pmp := pngstructure.NewPngMediaParser()
	intfc, err := pmp.ParseFile(filePath)
	if err != nil {
		return fmt.Errorf("parse png: %w", err)
	}
	cs := intfc.(*pngstructure.ChunkSlice)

	rootIb, err := cs.ConstructExifBuilder()
	if err != nil {
		rootIb, err = newFreshIfdBuilder()
		if err != nil {
			return fmt.Errorf("create ifd builder: %w", err)
		}
	}

	if err = rootIb.SetStandardWithName("ImageDescription", value); err != nil {
		return fmt.Errorf("set ImageDescription: %w", err)
	}

	if err = cs.SetExif(rootIb); err != nil {
		return fmt.Errorf("set exif in chunk slice: %w", err)
	}

	return atomicWrite(filePath, func(f *os.File) error {
		return cs.WriteTo(f)
	})
}

// atomicWrite writes via a temp file in the same directory, then renames.
func atomicWrite(filePath string, writeFn func(*os.File) error) error {
	dir := filepath.Dir(filePath)
	tmp, err := os.CreateTemp(dir, "photocaption-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if err = writeFn(tmp); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Close()
	return os.Rename(tmpPath, filePath)
}
