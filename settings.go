package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Settings holds user-configurable application preferences.
type Settings struct {
	SaveAsSuffix      string `json:"saveAsSuffix"`
	CopyrightText     string `json:"copyrightText"`
	CaptionFontSize   int    `json:"captionFontSize"`
	CaptionTextColor  string `json:"captionTextColor"`
	CaptionLabelBg    string `json:"captionLabelBg"`
	CaptionBackground string `json:"captionBackground"`
}

func defaultSettings() Settings {
	return Settings{
		SaveAsSuffix:      "_caption",
		CopyrightText:     "",
		CaptionFontSize:   0,
		CaptionTextColor:  "#000000",
		CaptionLabelBg:    "#ffffff",
		CaptionBackground: "#0a0a0a",
	}
}

func settingsFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "PhotoCaption", "settings.json"), nil
}

func loadSettings() Settings {
	path, err := settingsFilePath()
	if err != nil {
		return defaultSettings()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultSettings()
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return defaultSettings()
	}
	if s.SaveAsSuffix == "" {
		s.SaveAsSuffix = "_caption"
	}
	if s.CaptionTextColor == "" {
		s.CaptionTextColor = "#000000"
	}
	if s.CaptionLabelBg == "" {
		s.CaptionLabelBg = "#ffffff"
	}
	if s.CaptionBackground == "" {
		s.CaptionBackground = "#0a0a0a"
	}
	return s
}

func saveSettings(s Settings) error {
	path, err := settingsFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
