// Package config loads and creates sshush configuration from TOML.
// Default config path is $XDG_CONFIG_HOME/sshush/config.toml when set, otherwise ~/.config/sshush/config.toml
// (override with $SSHUSH_CONFIG or --config). SetupConfig creates a default config on first run and may append
// eval $(sshush) to ~/.zshrc or ~/.bashrc (see internal/platform for rules).
package config
