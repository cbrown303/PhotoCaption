// Copyright (c) 2026 Christopher Brown
// SPDX-License-Identifier: AGPL-3.0-only

package app

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

// UpdateExifDescription sets the ImageDescription tag inside origExif (raw APP1
// bytes including the "Exif\x00\x00" prefix) to a value built from userDesc and
// origHeight. If the tag already exists it is patched in-place; if it is absent a
// new IFD0 containing the tag is appended. All other EXIF tags are preserved
// bit-for-bit. Returns an error only if the TIFF header itself is unreadable.
func UpdateExifDescription(origExif []byte, userDesc string, origHeight int) ([]byte, error) {
	if len(origExif) < 8 {
		return nil, fmt.Errorf("origExif too short (%d bytes)", len(origExif))
	}
	value := buildImageDescription(userDesc, origHeight)

	// Try updating the existing tag first.
	patchedTiff, err := patchImageDescriptionInTiff(origExif[6:], value)
	if err != nil {
		fmt.Printf("[DEBUG] UpdateExifDescription: patch failed (%v) — adding tag\n", err)
		// Tag absent: append a new IFD0 that includes it.
		patchedTiff, err = addImageDescriptionToTiff(origExif[6:], value)
		if err != nil {
			return nil, fmt.Errorf("could not add ImageDescription: %w", err)
		}
	}

	updated := make([]byte, 6+len(patchedTiff))
	copy(updated[:6], origExif[:6]) // preserve "Exif\x00\x00"
	copy(updated[6:], patchedTiff)
	return updated, nil
}

// WriteDescription writes userDesc + origHeight marker to EXIF ImageDescription,
// preserving all existing EXIF tags.
func WriteDescription(filePath, userDesc string, origHeight int) error {
	value := buildImageDescription(userDesc, origHeight)
	return writeExifImageDescription(filePath, value)
}

// SnapshotExifSegment reads the raw APP1 EXIF segment bytes from filePath.
// These bytes can be injected verbatim into another JPEG, completely bypassing
// the IfdBuilder / exif.Collect code paths that fail on minimal EXIF data.
// Returns nil if the file has no EXIF segment (not an error).
func SnapshotExifSegment(filePath string) ([]byte, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg":
		jmp := jpegstructure.NewJpegMediaParser()
		intfc, err := jmp.ParseFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("parse jpeg for exif snapshot: %w", err)
		}
		for _, seg := range intfc.(*jpegstructure.SegmentList).Segments() {
			if seg.MarkerId == jpegstructure.MARKER_APP1 &&
				len(seg.Data) >= 6 &&
				string(seg.Data[:6]) == "Exif\x00\x00" {
				cp := make([]byte, len(seg.Data))
				copy(cp, seg.Data)
				fmt.Printf("[DEBUG] SnapshotExifSegment: captured %d raw bytes from %q\n", len(cp), filePath)
				return cp, nil
			}
		}
		fmt.Printf("[DEBUG] SnapshotExifSegment: no EXIF APP1 segment found in %q\n", filePath)
		return nil, nil
	case ".png":
		// PNG EXIF: fall back to IfdBuilder path for now.
		pmp := pngstructure.NewPngMediaParser()
		intfc, err := pmp.ParseFile(filePath)
		if err != nil {
			return nil, nil
		}
		_, rawExif, err := intfc.(*pngstructure.ChunkSlice).Exif()
		if err != nil {
			return nil, nil
		}
		cp := make([]byte, len(rawExif))
		copy(cp, rawExif)
		return cp, nil
	default:
		return nil, nil
	}
}

// InjectExifSegment writes previously-snapshotted raw EXIF bytes back into
// filePath after a pixel-only rewrite has stripped the EXIF segment.
// For JPEG, the bytes are re-inserted as the APP1 segment immediately after SOI.
// For PNG, the bytes are written via the IfdBuilder path.
func InjectExifSegment(filePath string, app1Data []byte) error {
	if len(app1Data) == 0 {
		fmt.Printf("[DEBUG] InjectExifSegment: nothing to inject into %q\n", filePath)
		return nil
	}

	// Debug: show what tags are inside the snapshot.
	if len(app1Data) >= 6 {
		if tags, _, err := exif.GetFlatExifData(app1Data[6:], nil); err == nil {
			fmt.Printf("[EXIF] InjectExifSegment: injecting %d tags into %q:\n", len(tags), filePath)
			for _, tag := range tags {
				fmt.Printf("  %-40s %v\n", tag.TagName, tag.Value)
			}
		}
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg":
		jmp := jpegstructure.NewJpegMediaParser()
		intfc, err := jmp.ParseFile(filePath)
		if err != nil {
			return fmt.Errorf("parse jpeg for exif inject: %w", err)
		}
		sl := intfc.(*jpegstructure.SegmentList)
		segs := sl.Segments()

		// Replace an existing APP1/EXIF segment if present.
		for _, seg := range segs {
			if seg.MarkerId == jpegstructure.MARKER_APP1 &&
				len(seg.Data) >= 6 &&
				string(seg.Data[:6]) == "Exif\x00\x00" {
				seg.Data = app1Data
				return atomicWrite(filePath, func(f *os.File) error { return sl.Write(f) })
			}
		}

		// No existing EXIF segment — build a new SegmentList with the
		// EXIF APP1 inserted right after SOI (position 1).
		newSeg := &jpegstructure.Segment{MarkerId: jpegstructure.MARKER_APP1, Data: app1Data}
		rebuilt := make([]*jpegstructure.Segment, 0, len(segs)+1)
		if len(segs) > 0 {
			rebuilt = append(rebuilt, segs[0]) // SOI
		}
		rebuilt = append(rebuilt, newSeg)
		if len(segs) > 1 {
			rebuilt = append(rebuilt, segs[1:]...)
		}
		newSl := jpegstructure.NewSegmentList(rebuilt)
		return atomicWrite(filePath, func(f *os.File) error { return newSl.Write(f) })

	case ".png":
		// PNG: re-parse raw bytes into an IfdBuilder and use SetExif.
		im, err := exifcommon.NewIfdMappingWithStandard()
		if err != nil {
			return err
		}
		ti := exif.NewTagIndex()
		if err = exif.LoadStandardTags(ti); err != nil {
			return err
		}
		_, index, err := exif.Collect(im, ti, app1Data)
		if err != nil {
			return fmt.Errorf("collect png exif for inject: %w", err)
		}
		rootIb := exif.NewIfdBuilderFromExistingChain(index.RootIfd)
		pmp := pngstructure.NewPngMediaParser()
		intfc, err := pmp.ParseFile(filePath)
		if err != nil {
			return err
		}
		cs := intfc.(*pngstructure.ChunkSlice)
		if err = cs.SetExif(rootIb); err != nil {
			return fmt.Errorf("inject exif into png: %w", err)
		}
		return atomicWrite(filePath, func(f *os.File) error { return cs.WriteTo(f) })
	default:
		return nil
	}
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
	// Strategy 1: patch the ImageDescription tag directly in the raw EXIF bytes.
	// This preserves every other tag (Make, Model, GPS, MakerNote, …) bit-for-bit
	// because we never go through ConstructExifBuilder / exif.Collect, which fail
	// on complex EXIF data (e.g. vendor MakerNote blobs) and drop all other tags
	// when the fallback empty IfdBuilder is used.
	origExif, err := SnapshotExifSegment(filePath)
	if err == nil && len(origExif) >= 8 {
		patchedTiff, patchErr := patchImageDescriptionInTiff(origExif[6:], value)
		if patchErr == nil {
			patched := make([]byte, 6+len(patchedTiff))
			copy(patched[:6], origExif[:6]) // preserve "Exif\x00\x00"
			copy(patched[6:], patchedTiff)
			fmt.Printf("[WriteDescription] raw TIFF patch succeeded — injecting %d bytes\n", len(patched))
			return InjectExifSegment(filePath, patched)
		}
		fmt.Printf("[WriteDescription] raw patch failed (%v), falling back to IfdBuilder\n", patchErr)
	}

	// Strategy 2 (fallback): IfdBuilder — used when the file has no EXIF at all,
	// or ImageDescription has never been written (so patchImageDescriptionInTiff
	// could not find the existing tag to update).
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

// exifPrefix is the 6-byte marker that starts every JPEG APP1 EXIF segment.
var exifSegPrefix = []byte("Exif\x00\x00")

// patchImageDescriptionInTiff locates the ImageDescription tag (0x010E) in
// rawTiff (raw TIFF bytes, NOT including the "Exif\x00\x00" JPEG prefix),
// appends newValue (null-terminated) at the end of the data, updates the
// tag's count and offset fields to point to it, and returns the modified
// bytes. Every other byte in rawTiff is left untouched.
// Returns an error if the tag is not found — caller should fall back to
// the IfdBuilder path.
func patchImageDescriptionInTiff(rawTiff []byte, newValue string) ([]byte, error) {
	if len(rawTiff) < 8 {
		return nil, fmt.Errorf("TIFF data too short (%d bytes)", len(rawTiff))
	}

	var bo binary.ByteOrder
	switch string(rawTiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return nil, fmt.Errorf("unrecognised TIFF byte-order marker")
	}

	ifd0Off := int(bo.Uint32(rawTiff[4:8]))
	if ifd0Off+2 > len(rawTiff) {
		return nil, fmt.Errorf("IFD0 offset %d out of bounds (len=%d)", ifd0Off, len(rawTiff))
	}

	entryCount := int(bo.Uint16(rawTiff[ifd0Off : ifd0Off+2]))

	const tagImageDescription = uint16(0x010E)

	for i := range entryCount {
		eOff := ifd0Off + 2 + i*12
		if eOff+12 > len(rawTiff) {
			break
		}
		if bo.Uint16(rawTiff[eOff:eOff+2]) != tagImageDescription {
			continue
		}

		// Append null-terminated new value at the end of the TIFF data.
		newBytes := append([]byte(newValue), 0)
		result := make([]byte, len(rawTiff)+len(newBytes))
		copy(result, rawTiff)
		newOff := uint32(len(rawTiff))
		copy(result[newOff:], newBytes)

		// Patch count (eOff+4..+8) and value-offset (eOff+8..+12).
		bo.PutUint32(result[eOff+4:eOff+8], uint32(len(newBytes)))
		bo.PutUint32(result[eOff+8:eOff+12], newOff)

		return result, nil
	}

	return nil, fmt.Errorf("ImageDescription tag (0x010E) not found in IFD0")
}

// addImageDescriptionToTiff appends a new IFD0 (containing all original IFD0
// entries plus a new ImageDescription tag) to the end of rawTiff and updates
// the TIFF header to point to it. Because nothing is shifted, all existing
// value offsets (sub-IFDs, GPS, MakerNote, …) remain valid. The old IFD0
// bytes become unreachable but harmless.
func addImageDescriptionToTiff(rawTiff []byte, newValue string) ([]byte, error) {
	if len(rawTiff) < 8 {
		return nil, fmt.Errorf("TIFF data too short (%d bytes)", len(rawTiff))
	}

	var bo binary.ByteOrder
	switch string(rawTiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return nil, fmt.Errorf("unrecognised TIFF byte-order marker")
	}

	ifd0Off := int(bo.Uint32(rawTiff[4:8]))
	if ifd0Off+2 > len(rawTiff) {
		return nil, fmt.Errorf("IFD0 offset %d out of bounds (len=%d)", ifd0Off, len(rawTiff))
	}

	oldCount := int(bo.Uint16(rawTiff[ifd0Off : ifd0Off+2]))
	entriesEnd := ifd0Off + 2 + oldCount*12
	if entriesEnd+4 > len(rawTiff) {
		return nil, fmt.Errorf("IFD0 entries exceed TIFF data length")
	}

	// Preserve the existing next-IFD (IFD1 / thumbnail) pointer.
	nextIfdOff := bo.Uint32(rawTiff[entriesEnd : entriesEnd+4])

	newBytes := append([]byte(newValue), 0) // null-terminated ASCII

	// Append layout:
	//   [new IFD0]  2-byte count | (oldCount+1)×12-byte entries | 4-byte next-IFD
	//   [value]     ImageDescription string bytes
	newIfd0Start := len(rawTiff)
	newIfd0Size := 2 + (oldCount+1)*12 + 4
	newValueStart := uint32(newIfd0Start + newIfd0Size)

	result := make([]byte, int(newValueStart)+len(newBytes))
	copy(result, rawTiff)

	off := newIfd0Start

	// Entry count.
	bo.PutUint16(result[off:off+2], uint16(oldCount+1))
	off += 2

	// Copy all original IFD0 entries verbatim (offsets stay valid).
	copy(result[off:], rawTiff[ifd0Off+2:ifd0Off+2+oldCount*12])
	off += oldCount * 12

	// New ImageDescription entry: tag=0x010E, type=ASCII(2), count, offset.
	bo.PutUint16(result[off:off+2], 0x010E)
	bo.PutUint16(result[off+2:off+4], 2)
	bo.PutUint32(result[off+4:off+8], uint32(len(newBytes)))
	bo.PutUint32(result[off+8:off+12], newValueStart)
	off += 12

	// Next-IFD offset.
	bo.PutUint32(result[off:off+4], nextIfdOff)

	// ImageDescription value data.
	copy(result[newValueStart:], newBytes)

	// Redirect the TIFF header to the new IFD0.
	bo.PutUint32(result[4:8], uint32(newIfd0Start))

	return result, nil
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
