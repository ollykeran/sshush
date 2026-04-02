// Package config loads and creates sshush configuration from TOML.
// The file uses tables [agent], [vault], [server], and [theme].
// Default config path is $XDG_CONFIG_HOME/sshush/config.toml when set, otherwise ~/.config/sshush/config.toml
// (override with $SSHUSH_CONFIG or --config). SetupConfig creates a default config from an embedded template
// on first run and may append eval $(sshush) to ~/.zshrc or ~/.bashrc (see internal/platform for rules).
package config
