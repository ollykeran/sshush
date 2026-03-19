// Package config loads and creates sshush configuration from TOML.
// The file uses tables [agent], [vault], [server], and [theme].
// Config is read from ~/.config/sshush/config.toml (or $SSHUSH_CONFIG).
// SetupConfig creates a default config and adds eval to bashrc on first run.
package config
