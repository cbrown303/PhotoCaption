// exifdump prints every EXIF tag found in a JPEG or PNG file.
// Usage: go run ./cmd/exifdump <image-file>
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	exif "github.com/dsoprea/go-exif/v3"
	jpegstructure "github.com/dsoprea/go-jpeg-image-structure/v2"
	pngstructure "github.com/dsoprea/go-png-image-structure/v2"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: exifdump <image-file>")
		os.Exit(1)
	}

	filePath := os.Args[1]

	rawExif, err := getRawExif(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading EXIF: %v\n", err)
		os.Exit(1)
	}

	tags, _, err := exif.GetFlatExifData(rawExif, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing EXIF: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("EXIF tags in %s\n\n", filePath)
	fmt.Printf("  %-6s  %-35s  %-25s  %s\n", "ID", "Tag Name", "IFD Path", "Value")
	fmt.Printf("  %-6s  %-35s  %-25s  %s\n",
		"------", strings.Repeat("-", 35), strings.Repeat("-", 25), strings.Repeat("-", 40))

	for _, tag := range tags {
		fmt.Printf("  0x%04x  %-35s  %-25s  %v\n",
			tag.TagId, tag.TagName, tag.IfdPath, tag.Value)
	}

	fmt.Printf("\nTotal: %d tags\n", len(tags))
}

func getRawExif(filePath string) ([]byte, error) {
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
