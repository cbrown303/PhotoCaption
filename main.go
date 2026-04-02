// Copyright (c) 2026 Christopher Brown
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend
var assets embed.FS

func main() {
	app := NewApp()

	appMenu := menu.NewMenu()

	pcMenu := appMenu.AddSubmenu("PhotoCaption")
	pcMenu.AddText("About PhotoCaption", nil, func(_ *menu.CallbackData) {
		app.ShowAbout()
	})
	pcMenu.AddSeparator()
	pcMenu.AddText("Settings…", nil, func(_ *menu.CallbackData) {
		app.ShowSettings()
	})
	pcMenu.AddSeparator()
	pcMenu.AddText("Quit PhotoCaption", keys.CmdOrCtrl("q"), func(_ *menu.CallbackData) {
		app.QuitApp()
	})

	fileMenu := appMenu.AddSubmenu("File")
	fileMenu.AddText("Open", keys.CmdOrCtrl("o"), func(_ *menu.CallbackData) {
		app.OpenFile()
	})
	fileMenu.AddText("Save", keys.CmdOrCtrl("s"), func(_ *menu.CallbackData) {
		app.SaveFile()
	})
	fileMenu.AddText("Save As", keys.Combo("s", keys.CmdOrCtrlKey, keys.ShiftKey), func(_ *menu.CallbackData) {
		app.SaveAsFile()
	})
	fileMenu.AddSeparator()
	fileMenu.AddText("Close", keys.CmdOrCtrl("w"), func(_ *menu.CallbackData) {
		app.CloseFile()
	})

	editMenu := appMenu.AddSubmenu("Edit")
	editMenu.AddText("Undo", nil, func(_ *menu.CallbackData) { app.EmitUndo() })
	editMenu.AddSeparator()
	editMenu.AddText("Cut", nil, func(_ *menu.CallbackData) { app.EmitCut() })
	editMenu.AddText("Copy", nil, func(_ *menu.CallbackData) { app.EmitCopy() })
	editMenu.AddText("Paste", nil, func(_ *menu.CallbackData) { app.EmitPaste() })
	editMenu.AddText("Select All", nil, func(_ *menu.CallbackData) { app.EmitSelectAll() })

	err := wails.Run(&options.App{
		Title:  "PhotoCaption",
		Width:  1200,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
		Menu: appMenu,
		Mac: &mac.Options{
			TitleBar: mac.TitleBarDefault(),
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
