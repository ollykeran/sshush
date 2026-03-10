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
// Preserves existing socket_path, key_paths, and any other content.
// If the file does not exist, returns an error (use CreateDefaultConfig to create the config first).
// Uses atomic write (temp file + rename). Path is expanded (~).
func WriteThemeToPath(path string, presetName string, custom *ThemeSection) error {
	path = utils.ExpandHomeDirectory(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s (run the app once to create it)", path)
		}
		return fmt.Errorf("read config: %w", err)
	}

	var raw struct {
		SocketPath string        `toml:"socket_path"`
		KeyPaths   []string      `toml:"key_paths"`
		Theme      *ThemeSection `toml:"theme"`
	}
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	if presetName != "" {
		raw.Theme = &ThemeSection{Name: presetName}
	} else if custom != nil {
		raw.Theme = custom
	} else {
		raw.Theme = &ThemeSection{}
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config.toml.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	// When writing a preset only, encode theme as just name so we don't emit empty hex fields.
	if presetName != "" {
		type presetOnly struct {
			SocketPath string   `toml:"socket_path"`
			KeyPaths   []string `toml:"key_paths"`
			Theme      struct {
				Name string `toml:"name"`
			} `toml:"theme"`
		}
		out := presetOnly{
			SocketPath: raw.SocketPath,
			KeyPaths:   raw.KeyPaths,
		}
		out.Theme.Name = presetName
		if err := toml.NewEncoder(tmp).Encode(out); err != nil {
			tmp.Close()
			return fmt.Errorf("encode config: %w", err)
		}
	} else {
		if err := toml.NewEncoder(tmp).Encode(raw); err != nil {
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
