# PhotoCaption — Product Description

## Overview

PhotoCaption is a lightweight, privacy-first desktop application for adding captions to photos. Captions are rendered directly onto the image as a styled text band below the photo, and caption text is also embedded in the file's EXIF metadata for interoperability. All processing happens locally — no internet connection, no data collection, no telemetry.

**Version:** 0.2.0  
**License:** AGPL-3.0  
**Platforms:** macOS (Intel & Apple Silicon), Windows

---

## Supported Image Formats

- JPEG (`.jpg`, `.jpeg`)
- PNG (`.png`)

---

## Core Features

### Caption Rendering

Captions are rendered as a styled banner below the original image:

- **Rounded rectangle label** centered horizontally over a solid background band
- **Responsive sizing** — font size, padding, and label dimensions all scale with image width (3% of width, minimum 28px), ensuring captions look proportional across all image sizes
- **Configurable colors** — text color, label background, and outer background are all user-selectable via color pickers
- **Optional fixed font size** — overrides auto-sizing (8–200 px range)
- **Word wrapping** — long captions wrap automatically; hard line breaks (`Enter`) are honored
- **500-character limit** with a live character counter

### EXIF Metadata Handling

- Caption text is stored in the EXIF `ImageDescription` tag, making it readable by standard photo tools
- All original camera metadata (Make, Model, GPS, DateTimeOriginal, MakerNotes, etc.) is preserved bit-for-bit
- Original image height is tracked in a hidden EXIF marker so repeated saves always crop and re-render the caption area cleanly, never accumulating extra pixels
- Writes are atomic (temp file + rename) to prevent file corruption

### File Operations

| Operation | Description |
|-----------|-------------|
| **Open** | Native file picker; loads image and reads any existing caption from EXIF |
| **Save** | Overwrites current file in place, preserving all EXIF metadata |
| **Save As** | Creates a new file with a configurable suffix (default: `_caption`); prompts for filename via an in-app dialog |
| **Close** | Closes the current file; prompts to save if there are unsaved changes |

### Unsaved Changes Protection

When closing a file or quitting the application with unsaved caption edits, a confirmation dialog offers three options: **Cancel**, **Don't Save**, or **Save**.

---

## User Interface

### Layout

- **Left panel (70%)** — Image canvas with file path header
- **Right panel (30%)** — Caption editing controls
- **Dark theme** throughout

### Canvas Area

- Displays the opened image at full resolution (scaled to fit the window)
- Shows a placeholder with keyboard shortcut hint when no file is open
- Updates in real time when **Apply Caption** is clicked

### Caption Panel

- Multi-line text area (disabled until a file is open)
- Live character counter (turns amber above 450 characters)
- **© Insert Copyright** button — inserts a pre-configured copyright template at the cursor
- **Apply Caption** button — renders the current caption onto the canvas preview

### Clipboard Support

Full cut/copy/paste/undo support in the caption field and Save As filename input, accessible via both keyboard shortcuts and the Edit menu.

---

## Menu Structure

### PhotoCaption Menu
- About PhotoCaption
- Settings…
- Quit (`⌘Q` / `Ctrl+Q`)

### File Menu
- Open (`⌘O` / `Ctrl+O`)
- Save (`⌘S` / `Ctrl+S`)
- Save As (`⇧⌘S` / `Ctrl+Shift+S`)
- Close (`⌘W` / `Ctrl+W`)

### Edit Menu
- Undo (`⌘Z` / `Ctrl+Z`)
- Cut (`⌘X` / `Ctrl+X`)
- Copy (`⌘C` / `Ctrl+C`)
- Paste (`⌘V` / `Ctrl+V`)
- Select All

---

## Settings

All settings persist across sessions in a local JSON file:

| Setting | Default | Description |
|---------|---------|-------------|
| Font Size | Auto | Fixed pixel size (8–200 px); blank = auto-scale with image width |
| Letter Color | `#000000` | Caption text color |
| Label Background | `#ffffff` | Rounded label fill color |
| Background Color | `#0a0a0a` | Outer caption band color |
| Save As Suffix | `_caption` | Appended to filename on Save As (e.g. `photo_caption.jpg`) |
| Copyright Template | _(empty)_ | Text inserted by the © Insert Copyright button |

**Settings file location:**
- macOS: `~/Library/Application Support/PhotoCaption/settings.json`
- Windows: `%AppData%/PhotoCaption/settings.json`

---

## Undo History

The caption field maintains an undo stack of up to 100 states. Each text change is recorded, and Undo (`⌘Z` / `Ctrl+Z` or **Edit > Undo**) steps back through the history one entry at a time. The history resets when a new file is opened or closed.

---

## Privacy & Security

- **No network access** — the application makes no outbound connections
- **No data collection** — no analytics, crash reporting, or telemetry
- **Local-only storage** — settings and saved images remain on the user's machine
- **File safety** — all writes use atomic temp-file-then-rename to prevent corruption

---

## Platform Notes

### macOS
The application is not yet notarized. On first run, macOS Gatekeeper may block it. To allow:
> **System Settings → Privacy & Security → Open Anyway**

### Windows
The application is not code-signed. Windows SmartScreen may warn on first run. To proceed:
> Click **More info → Run anyway**

---

## Technology Stack

- **Backend:** Go with [Wails v2](https://wails.io) (native desktop shell)
- **Frontend:** HTML/CSS/JavaScript (single-file, no framework dependencies)
- **Image processing:** Go standard library + `golang/freetype` for caption rendering
- **EXIF handling:** `go-exif`, `go-jpeg-image-structure`, `go-png-image-structure`
