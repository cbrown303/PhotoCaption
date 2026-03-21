package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Settings holds user-configurable application preferences.
type Settings struct {
	SaveAsSuffix  string `json:"saveAsSuffix"`
	CopyrightText string `json:"copyrightText"`
}

func defaultSettings() Settings {
	return Settings{
		SaveAsSuffix:  "_caption",
		CopyrightText: "",
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
