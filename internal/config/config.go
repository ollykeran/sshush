package config

import (
	"os"

	"github.com/BurntSushi/toml"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/theme"
	"github.com/ollykeran/sshush/internal/utils"
)

// ThemeSection holds [theme] from the TOML config: either name = "preset" or hex keys.
type ThemeSection struct {
	Name    string `toml:"name"`
	Text    string `toml:"text"`
	Focus   string `toml:"focus"`
	Accent  string `toml:"accent"`
	Error   string `toml:"error"`
	Warning string `toml:"warning"`
}

// Config holds socket path, key paths, and optional theme from the TOML config file.
type Config struct {
	KeyPaths   []string    `toml:"key_paths"`   // Paths to private keys to load into the agent.
	SocketPath string      `toml:"socket_path"` // Unix socket path for the agent.
	Theme      ThemeSection `toml:"theme"`      // Optional theme (preset name or hex keys).
}

// EnsureSSHDirectory creates ~/.ssh with mode 0700 if it does not exist.
func EnsureSSHDirectory() {
	if err := os.MkdirAll(utils.ExpandHomeDirectory("~/.ssh"), 0o0700); err != nil {
		return
	}
}

// LoadConfig reads and parses a TOML config file. Paths are expanded (~).
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	cfg.SocketPath = utils.ExpandHomeDirectory(cfg.SocketPath)
	for i, p := range cfg.KeyPaths {
		cfg.KeyPaths[i] = utils.ExpandHomeDirectory(p)
	}

	if cfg.KeyPaths == nil {
		return Config{}, style.NewOutput().Error("key_paths is required").AsError()
	}
	if cfg.SocketPath == "" {
		return Config{}, style.NewOutput().Error("socket_path is required").AsError()
	}

	return cfg, nil
}

// ResolveThemeFromConfig returns the effective theme from config. If name is set, use that preset (name takes precedence over hex keys). Otherwise merge custom hex with default; invalid preset or hex falls back to default.
func ResolveThemeFromConfig(cfg Config) theme.Theme {
	return ResolveThemeFromSection(cfg.Theme)
}

// ResolveThemeFromSection returns the effective theme from a [theme] section.
func ResolveThemeFromSection(s ThemeSection) theme.Theme {
	if s.Name != "" {
		if t, ok := theme.ResolveTheme(s.Name); ok {
			return t
		}
		return theme.DefaultTheme()
	}
	custom := theme.Theme{
		Text:    s.Text,
		Focus:   s.Focus,
		Accent:  s.Accent,
		Error:   s.Error,
		Warning: s.Warning,
	}
	return theme.MergeWithDefault(custom, theme.DefaultTheme())
}

// LoadThemeFromPath reads the config file at path and returns the resolved theme. If the file is missing or unreadable, returns the default theme (no error). Used by theme show when config may not exist or may lack key_paths.
func LoadThemeFromPath(path string) theme.Theme {
	data, err := os.ReadFile(path)
	if err != nil {
		return theme.DefaultTheme()
	}
	var structWithTheme struct {
		Theme ThemeSection `toml:"theme"`
	}
	if err := toml.Unmarshal(data, &structWithTheme); err != nil {
		return theme.DefaultTheme()
	}
	return ResolveThemeFromSection(structWithTheme.Theme)
}
