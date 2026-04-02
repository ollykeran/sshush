package config

import (
	"bytes"
	"os"
	"path/filepath"

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

// Config is the runtime view of the TOML file (flat fields for callers).
// On disk the file uses [agent], [vault], [server], and [theme] sections.
type Config struct {
	KeyPaths   []string // From [agent].key_paths; ignored in vault mode for initial load semantics.
	SocketPath string   // From [agent].socket_path.
	VaultPath  string   // From [vault].vault_path when [agent].vault is true.
	Theme      ThemeSection

	ServerListenPort     int64  // From [server].listen_port.
	ServerAuthorizedKeys string // From [server].authorized_keys.
	ServerHostKey        string // From [server].host_key.
}

// configDocument matches the on-disk TOML layout.
type configDocument struct {
	Agent  agentSection  `toml:"agent"`
	Vault  vaultSection  `toml:"vault"`
	Server serverSection `toml:"server"`
	Theme  ThemeSection  `toml:"theme"`
}

type agentSection struct {
	SocketPath string   `toml:"socket_path"`
	KeyPaths   []string `toml:"key_paths"`
	Vault      bool     `toml:"vault"`
}

type vaultSection struct {
	VaultPath string `toml:"vault_path"`
}

type serverSection struct {
	ListenPort     int64  `toml:"listen_port"`
	AuthorizedKeys string `toml:"authorized_keys"`
	HostKey        string `toml:"host_key"`
}

// configDocumentThemePreset is used when encoding theme preset-only (avoid empty hex keys in file).
type configDocumentThemePreset struct {
	Agent  agentSection  `toml:"agent"`
	Vault  vaultSection  `toml:"vault"`
	Server serverSection `toml:"server"`
	Theme  struct {
		Name string `toml:"name"`
	} `toml:"theme"`
}

func toDocument(cfg Config) configDocument {
	a := agentSection{
		SocketPath: cfg.SocketPath,
		KeyPaths:   cfg.KeyPaths,
		Vault:      cfg.VaultPath != "",
	}
	if a.KeyPaths == nil {
		a.KeyPaths = []string{}
	}
	doc := configDocument{Agent: a, Theme: cfg.Theme}
	if cfg.VaultPath != "" {
		doc.Vault = vaultSection{VaultPath: cfg.VaultPath}
	}
	if cfg.ServerListenPort != 0 || cfg.ServerAuthorizedKeys != "" || cfg.ServerHostKey != "" {
		doc.Server = serverSection{
			ListenPort:     cfg.ServerListenPort,
			AuthorizedKeys: cfg.ServerAuthorizedKeys,
			HostKey:        cfg.ServerHostKey,
		}
	}
	return doc
}

func (d configDocument) toPresetDocument(name string) configDocumentThemePreset {
	return configDocumentThemePreset{
		Agent:  d.Agent,
		Vault:  d.Vault,
		Server: d.Server,
		Theme: struct {
			Name string `toml:"name"`
		}{Name: name},
	}
}

// MarshalConfig serializes cfg to canonical sectioned TOML bytes.
func MarshalConfig(cfg Config) ([]byte, error) {
	doc := toDocument(cfg)
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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

	var doc configDocument
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return Config{}, err
	}

	cfg, err := documentToConfig(&doc)
	if err != nil {
		return Config{}, err
	}

	cfg.SocketPath = utils.ExpandHomeDirectory(cfg.SocketPath)
	if cfg.SocketPath != "" {
		absConfigPath, err := filepath.Abs(path)
		if err != nil {
			absConfigPath = path
		}
		// Relative socket_path (e.g. legacy "sshush.sock" when XDG was unset) must not
		// depend on the process cwd; anchor it to the config file's directory.
		if !filepath.IsAbs(cfg.SocketPath) {
			cfg.SocketPath = filepath.Clean(filepath.Join(filepath.Dir(absConfigPath), cfg.SocketPath))
		}
	}
	cfg.VaultPath = utils.ExpandHomeDirectory(cfg.VaultPath)
	cfg.ServerAuthorizedKeys = utils.ExpandHomeDirectory(cfg.ServerAuthorizedKeys)
	cfg.ServerHostKey = utils.ExpandHomeDirectory(cfg.ServerHostKey)
	for i, p := range cfg.KeyPaths {
		cfg.KeyPaths[i] = utils.ExpandHomeDirectory(p)
	}

	return cfg, nil
}

func documentToConfig(doc *configDocument) (Config, error) {
	if doc.Agent.SocketPath == "" {
		return Config{}, style.NewOutput().
			Error("[agent].socket_path is required").
			AsError()
	}

	vaultPath := doc.Vault.VaultPath
	if doc.Agent.Vault {
		if vaultPath == "" {
			return Config{}, style.NewOutput().
				Error("when [agent].vault is true, [vault].vault_path must be set").
				AsError()
		}
	} else {
		if vaultPath != "" {
			return Config{}, style.NewOutput().
				Error("[vault].vault_path is set but [agent].vault is false; set [agent].vault = true or remove [vault]").
				AsError()
		}
	}

	hasVault := doc.Agent.Vault && vaultPath != ""
	hasKeys := doc.Agent.KeyPaths != nil
	if !hasVault && !hasKeys {
		return Config{}, style.NewOutput().
			Error("config must set either [agent].vault = true with [vault].vault_path, or [agent].key_paths").
			Info("Put key_paths under [agent]. Use [vault] only when using a vault.").
			AsError()
	}

	cfg := Config{
		SocketPath:           doc.Agent.SocketPath,
		KeyPaths:             doc.Agent.KeyPaths,
		VaultPath:            vaultPath,
		Theme:                doc.Theme,
		ServerListenPort:     doc.Server.ListenPort,
		ServerAuthorizedKeys: doc.Server.AuthorizedKeys,
		ServerHostKey:        doc.Server.HostKey,
	}
	if !doc.Agent.Vault {
		cfg.VaultPath = ""
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
	var doc configDocument
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return theme.DefaultTheme()
	}
	return ResolveThemeFromSection(doc.Theme)
}

func decodeConfigDocument(data []byte) (configDocument, error) {
	var doc configDocument
	_, err := toml.Decode(string(data), &doc)
	return doc, err
}
