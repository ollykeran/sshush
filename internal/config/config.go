package config

import (
	"bytes"
	"fmt"
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
	NoColor bool   `toml:"no_color"`
}

// Agent backend types for [agent].type.
const (
	AgentTypeVault    = "vault"    // sshushd uses VaultPath as the agent backend.
	AgentTypeKeys     = "keys"     // sshushd loads keys from KeyPaths into an in-memory keyring.
	AgentTypeExternal = "external" // socket_path points at an agent sshush does not own; sshush never starts, stops, or reloads a daemon for it.
)

// Config is the runtime view of the TOML file (flat fields for callers).
// On disk the file uses [agent], [vault], [server], and [theme] sections.
type Config struct {
	KeyPaths   []string // From [agent].key_paths; when AgentType is not "vault", keys load from these paths.
	SocketPath string   // From [agent].socket_path.
	AgentType  string   // From [agent].type: "vault", "keys", or "external".
	VaultPath  string   // From [vault].vault_path; set whenever the file lists a path (also for CLI when AgentType is not "vault").
	Theme      ThemeSection

	ServerListenPort     int64  // From [server].listen_port.
	ServerAuthorizedKeys string // From [server].authorized_keys.
	ServerHostKey        string // From [server].host_key.
}

// IsVault reports whether the agent uses the vault backend.
func (c Config) IsVault() bool { return c.AgentType == AgentTypeVault }

// IsExternal reports whether socket_path points at an agent sshush does not own.
func (c Config) IsExternal() bool { return c.AgentType == AgentTypeExternal }

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
	Type       string   `toml:"type"`
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
		Type:       cfg.AgentType,
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
		return nil, fmt.Errorf("config: marshal toml: %w", err)
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
		return Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}

	var doc configDocument
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return Config{}, fmt.Errorf("config: parse toml %s: %w", path, err)
	}

	cfg, err := documentToConfig(&doc)
	if err != nil {
		return Config{}, fmt.Errorf("config: validate %s: %w", path, err)
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

// VaultPathForAgent returns the vault file path when the agent should use the vault backend; otherwise empty.
func (c Config) VaultPathForAgent() string {
	if !c.IsVault() || c.VaultPath == "" {
		return ""
	}
	return c.VaultPath
}

// AgentBackendMode returns a short label for the agent storage backend: "vault" or "keys".
// External agents are reported as "keys" since they never expose sshush's vault extensions.
func (c Config) AgentBackendMode() string {
	if c.IsVault() {
		return AgentTypeVault
	}
	return AgentTypeKeys
}

func documentToConfig(doc *configDocument) (Config, error) {
	switch doc.Agent.Type {
	case AgentTypeVault, AgentTypeKeys, AgentTypeExternal:
	default:
		return Config{}, style.NewOutput().
			Error("[agent].type is required and must be \"vault\", \"keys\", or \"external\"").
			Info("Configs from before this option existed used [agent].vault = true/false; replace that with type = \"vault\" or type = \"keys\".").
			AsError()
	}

	// socket_path is required for vault/keys (sshush owns and dials a fixed
	// path), but optional for external: a real ssh-agent's socket often
	// changes every launch, so it's resolved from SSH_AUTH_SOCK at runtime instead.
	if doc.Agent.SocketPath == "" && doc.Agent.Type != AgentTypeExternal {
		return Config{}, style.NewOutput().
			Error("[agent].socket_path is required").
			AsError()
	}

	vaultPath := doc.Vault.VaultPath
	switch doc.Agent.Type {
	case AgentTypeVault:
		if vaultPath == "" {
			return Config{}, style.NewOutput().
				Error("when [agent].type is \"vault\", [vault].vault_path must be set").
				AsError()
		}
	case AgentTypeKeys:
		hasKeys := doc.Agent.KeyPaths != nil
		hasVaultPath := vaultPath != ""
		if !hasKeys && !hasVaultPath {
			return Config{}, style.NewOutput().
				Error("config must set [agent].key_paths and/or [vault].vault_path when [agent].type is \"keys\"").
				Info("Use key_paths for the agent; optional vault_path for sshush vault commands while the agent uses key_paths.").
				AsError()
		}
	case AgentTypeExternal:
		// No key material requirement: sshush is a pure client of an agent it does not manage.
	}

	cfg := Config{
		SocketPath:           doc.Agent.SocketPath,
		KeyPaths:             doc.Agent.KeyPaths,
		AgentType:            doc.Agent.Type,
		VaultPath:            vaultPath,
		Theme:                doc.Theme,
		ServerListenPort:     doc.Server.ListenPort,
		ServerAuthorizedKeys: doc.Server.AuthorizedKeys,
		ServerHostKey:        doc.Server.HostKey,
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
