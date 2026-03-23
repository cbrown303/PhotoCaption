package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// AppInfo is returned by GetAppInfo and consumed by the About modal.
type AppInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Author  string `json:"author"`
	GitHub  string `json:"github"`
	License string `json:"license"`
	Notices string `json:"notices"`
}

// App holds application state and exposes methods to the Wails JS bridge.
type App struct {
	ctx         context.Context
	currentFile string
	settings    Settings
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.settings = loadSettings()
}

// GetAppInfo returns static information shown in the About dialog.
func (a *App) GetAppInfo() AppInfo {
	return AppInfo{
		Name:    "PhotoCaption",
		Version: "0.2.0",
		Author:  "Christopher Brown",
		GitHub:  "https://github.com/cbrown303/PhotoCaption",
		License: "https://github.com/cbrown303/PhotoCaption/blob/main/LICENSE",
		Notices: "https://github.com/cbrown303/PhotoCaption/blob/main/NOTICES.md",
	}
}

// GetSettings returns the current user settings.
func (a *App) GetSettings() Settings {
	return a.settings
}

// UpdateSettings persists new settings and applies them immediately.
func (a *App) UpdateSettings(s Settings) error {
	if s.SaveAsSuffix == "" {
		s.SaveAsSuffix = "_caption"
	}
	a.settings = s
	return saveSettings(s)
}

// QuitApp checks for unsaved changes via the frontend before quitting.
func (a *App) QuitApp() {
	runtime.EventsEmit(a.ctx, "app:quitRequest")
}

// ConfirmQuit exits the application.
func (a *App) ConfirmQuit() {
	runtime.Quit(a.ctx)
}

// CloseFile checks for unsaved changes via the frontend before closing.
func (a *App) CloseFile() {
	if a.currentFile == "" {
		return
	}
	runtime.EventsEmit(a.ctx, "file:closeRequest")
}

// ConfirmClose clears the current file and signals the frontend to reset the UI.
func (a *App) ConfirmClose() {
	a.currentFile = ""
	runtime.EventsEmit(a.ctx, "file:path", "")
	runtime.EventsEmit(a.ctx, "file:closed")
}

// ShowAbout signals the frontend to open the About modal.
func (a *App) ShowAbout() {
	runtime.EventsEmit(a.ctx, "app:about")
}

// ShowSettings signals the frontend to open the Settings modal.
func (a *App) ShowSettings() {
	runtime.EventsEmit(a.ctx, "app:settings")
}

// OpenFile opens a native file picker and loads the chosen image.
func (a *App) OpenFile() {
	filePath, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Open Image",
		Filters: []runtime.FileFilter{
			{DisplayName: "Images (*.jpg, *.jpeg, *.png)", Pattern: "*.jpg;*.jpeg;*.png"},
		},
	})
	if err != nil || filePath == "" {
		return
	}
	a.currentFile = filePath
	a.loadAndEmitImage(filePath)
}

// SaveFile emits save:request so the frontend calls SaveWithDescription.
func (a *App) SaveFile() {
	if a.currentFile == "" {
		return
	}
	runtime.EventsEmit(a.ctx, "save:request")
}

// SaveAsFile opens a save dialog, copies the file, then triggers a save.
func (a *App) SaveAsFile() {
	if a.currentFile == "" {
		return
	}
	ext := filepath.Ext(a.currentFile)
	base := strings.TrimSuffix(filepath.Base(a.currentFile), ext)
	defaultName := base + a.settings.SaveAsSuffix + ext

	newPath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save As",
		DefaultFilename: defaultName,
		Filters: []runtime.FileFilter{
			{DisplayName: "Images (*.jpg, *.jpeg, *.png)", Pattern: "*.jpg;*.jpeg;*.png"},
		},
	})
	if err != nil || newPath == "" {
		return
	}

	// Ensure extension is preserved if the user omitted it.
	if filepath.Ext(newPath) == "" {
		newPath += ext
	}

	if err := copyFile(a.currentFile, newPath); err != nil {
		runtime.EventsEmit(a.ctx, "save:error", fmt.Sprintf("Failed to copy file: %v", err))
		return
	}

	a.currentFile = newPath
	runtime.EventsEmit(a.ctx, "save:request")
}

// SaveWithDescription is called from JS to persist the image with the given description.
// It is exposed on the Wails JS bridge as window.go.main.App.SaveWithDescription.
func (a *App) SaveWithDescription(description string) {
	fmt.Printf("[DEBUG] SaveWithDescription called — file=%q desc=%q\n", a.currentFile, description)
	if a.currentFile == "" {
		fmt.Println("[DEBUG] SaveWithDescription: no current file, returning early")
		return
	}
	const maxDescriptionLen = 500
	if len([]rune(description)) > maxDescriptionLen {
		runtime.EventsEmit(a.ctx, "save:error", fmt.Sprintf("Caption is too long (%d characters). Maximum is %d.", len([]rune(description)), maxDescriptionLen))
		return
	}

	// Step 1 — snapshot raw EXIF bytes before any file writes.
	// This captures the pristine original camera metadata (Make, Model, GPS, …).
	origExif, err := SnapshotExifSegment(a.currentFile)
	if err != nil {
		runtime.EventsEmit(a.ctx, "save:error", fmt.Sprintf("Failed to snapshot EXIF: %v", err))
		return
	}
	fmt.Printf("[DEBUG] Step 1 — EXIF snapshot: %d bytes\n", len(origExif))

	// Step 2 — determine the original image height.
	// On first save origHeight is 0; derive it from the pixel dimensions.
	origHeight, err := ReadOriginalHeight(a.currentFile)
	if err != nil {
		runtime.EventsEmit(a.ctx, "save:error", fmt.Sprintf("Failed to read metadata: %v", err))
		return
	}
	if origHeight == 0 {
		img, err := loadImage(a.currentFile)
		if err != nil {
			runtime.EventsEmit(a.ctx, "save:error", fmt.Sprintf("Failed to load image: %v", err))
			return
		}
		origHeight = img.Bounds().Dy()
	}
	fmt.Printf("[DEBUG] Step 2 — origHeight: %d\n", origHeight)

	// Step 3 — patch ImageDescription inside the in-memory EXIF snapshot.
	// UpdateExifDescription modifies only that one tag; every other byte is
	// preserved. The result is used in step 4 so no separate WriteDescription
	// call is ever needed.
	updatedExif, exifPatchErr := UpdateExifDescription(origExif, description, origHeight)
	if exifPatchErr != nil {
		// Tag not yet present (first save of an original photo) or no EXIF at all.
		// Fall back to using the unmodified snapshot; WriteDescription (step 5)
		// will add the tag via the IfdBuilder path.
		fmt.Printf("[DEBUG] Step 3 — in-memory patch skipped (%v); will write via IfdBuilder\n", exifPatchErr)
		updatedExif = origExif
	} else {
		fmt.Printf("[DEBUG] Step 3 — in-memory EXIF patch OK: %d bytes\n", len(updatedExif))
	}

	// Step 4 — pixel write + EXIF inject in one pass.
	// AppendCaptionToImage crops to origHeight, renders the caption, saves the
	// pixel data (which strips EXIF), then injects updatedExif back into the file.
	if err := AppendCaptionToImage(a.currentFile, origHeight, description, updatedExif, a.settings); err != nil {
		runtime.EventsEmit(a.ctx, "save:error", fmt.Sprintf("Failed to save image: %v", err))
		return
	}

	// Step 5 — fallback: if the in-memory patch failed (ImageDescription tag was
	// absent), write it now via the IfdBuilder path. The injected EXIF from
	// step 4 is still on disk so all original tags are preserved.
	if exifPatchErr != nil {
		if err := WriteDescription(a.currentFile, description, origHeight); err != nil {
			fmt.Printf("warning: could not write description EXIF: %v\n", err)
		}
	}

	// Step 6 — reload saved file into the canvas.
	a.loadAndEmitImage(a.currentFile)
	runtime.EventsEmit(a.ctx, "save:success")
}

// loadAndEmitImage reads filePath, emits image events so the frontend refreshes.
func (a *App) loadAndEmitImage(filePath string) {
	// Emit metadata first so the values are set before the image onload fires.
	desc, origHeight, _ := ReadMetadata(filePath)
	runtime.EventsEmit(a.ctx, "image:originalHeight", origHeight)
	runtime.EventsEmit(a.ctx, "metadata:description", desc)

	runtime.EventsEmit(a.ctx, "file:path", filePath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		runtime.EventsEmit(a.ctx, "save:error", fmt.Sprintf("Failed to read file: %v", err))
		return
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	mimeType := "image/jpeg"
	if ext == ".png" {
		mimeType = "image/png"
	}
	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
	runtime.EventsEmit(a.ctx, "image:loaded", dataURL)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
