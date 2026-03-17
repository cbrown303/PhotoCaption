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

// App holds application state and exposes methods to the Wails JS bridge.
type App struct {
	ctx         context.Context
	currentFile string
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
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
	defaultName := base + "_caption" + ext

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
	if a.currentFile == "" {
		return
	}

	// Step 1 — ensure original height is stored in EXIF before pixels are modified.
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
		if err := WriteDescription(a.currentFile, description, origHeight); err != nil {
			// Non-fatal: log and continue — the pixel write is more important.
			fmt.Printf("warning: could not write original height to EXIF: %v\n", err)
		}
	}

	// Step 2 — crop to original height and append caption pixels.
	if err := AppendCaptionToImage(a.currentFile, origHeight, description); err != nil {
		runtime.EventsEmit(a.ctx, "save:error", fmt.Sprintf("Failed to save image: %v", err))
		return
	}

	// Step 3 — write description to EXIF after pixel write (encoding strips EXIF).
	if err := WriteDescription(a.currentFile, description, origHeight); err != nil {
		fmt.Printf("warning: could not write description EXIF: %v\n", err)
	}

	// Step 4 — reload saved file into the canvas.
	a.loadAndEmitImage(a.currentFile)
	runtime.EventsEmit(a.ctx, "save:success")
}

// loadAndEmitImage reads filePath, emits image events so the frontend refreshes.
func (a *App) loadAndEmitImage(filePath string) {
	// Emit metadata first so the values are set before the image onload fires.
	desc, origHeight, _ := ReadMetadata(filePath)
	runtime.EventsEmit(a.ctx, "image:originalHeight", origHeight)
	runtime.EventsEmit(a.ctx, "metadata:description", desc)

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
