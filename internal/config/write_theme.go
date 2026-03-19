package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/ollykeran/sshush/internal/utils"
)

// WriteThemeToPath updates the [theme] section in the config file at path.
// Use presetName for a preset (e.g. "dracula"); then custom is ignored and only name is written.
// Use custom (non-nil) for custom hex theme; then presetName is ignored.
// Preserves existing [agent], [vault], [server], and [theme] content.
// If the file does not exist, returns an error (use CreateDefaultConfig to create the config first).
// Uses atomic write (temp file + rename). Path is expanded (~).
func WriteThemeToPath(path string, presetName string, custom *ThemeSection) error {
	path = utils.ExpandHomeDirectory(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s (run the app once to create it)", utils.DisplayPath(path))
		}
		return fmt.Errorf("read config: %w", err)
	}

	doc, err := decodeConfigDocument(data)
	if err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	if presetName != "" {
		doc.Theme = ThemeSection{Name: presetName}
	} else if custom != nil {
		doc.Theme = *custom
	} else {
		doc.Theme = ThemeSection{}
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config.toml.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if presetName != "" {
		presetDoc := doc.toPresetDocument(presetName)
		if err := toml.NewEncoder(tmp).Encode(presetDoc); err != nil {
			tmp.Close()
			return fmt.Errorf("encode config: %w", err)
		}
	} else {
		if err := toml.NewEncoder(tmp).Encode(doc); err != nil {
			tmp.Close()
			return fmt.Errorf("encode config: %w", err)
		}
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}
