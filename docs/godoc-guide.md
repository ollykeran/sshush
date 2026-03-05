# Godoc Implementation Guide

How to add and maintain godoc comments in sshush. Godoc is Go's built-in documentation extracted from source comments.

## Best Practices

- **Package comment**: Start with "Package \<name\>". First sentence appears in package listings. Place in `doc.go` for packages needing more than a few lines.
- **Exported names**: Every exported function, type, const, var gets a comment immediately above it (no blank line).
- **Style**: Complete sentences, end with period. Begin with the name being described (e.g. "ResolveConfigPath returns...").
- **Links**: Use `[path/filepath]` or `[Config]` to link to other packages or types.

## Current State

Many internal packages already have good doc comments:
- `internal/config/default.go`: CreateDefaultConfig, SetupConfig, AddEvalToShell
- `internal/runtime/runtime.go`: ResolveConfigPath, ResolveDaemonConfigPath, PidFilePath, ResolveSocketPath
- `internal/cli/root.go`: LoadMergedConfig, LoadOverrides

Gaps: package-level comments, some exported types, `internal/agent`, `internal/keys`, `internal/tui`.

## Implementation Steps

1. **Add package docs**: Create `doc.go` in each internal package with a 1–3 line package comment.
2. **Fill gaps**: Add doc comments for exported functions/types missing them.
3. **View locally**: Run `go doc -all` or `godoc -http=:6060` (if installed) to browse.
4. **CI**: `golangci-lint` with `exhaustive` or `godot` can enforce comment style.

## Example doc.go

```go
// Package config loads and creates sshush configuration.
// Config is read from TOML at ~/.config/sshush/config.toml (or $SSHUSH_CONFIG).
// SetupConfig creates a default config and adds eval to bashrc on first run.
package config
```

## Example function comment

```go
// ResolveConfigPath returns the config file path using: --config flag,
// ~/.config/sshush/config.toml, $SSHUSH_CONFIG, or ./config.toml.
func ResolveConfigPath(cmd *cobra.Command) (string, error) {
```

## Viewing godoc

At http://localhost:6060/pkg/github.com/ollykeran/sshush/ click **Subdirectories** to reach the internal packages (cli, config, tui, agent, keys, etc.). The cmd package is the entry point; the function docs are in internal.

Correct URLs (the module is github.com/ollykeran/sshush, not sshushd):
- Module root: .../github.com/ollykeran/sshush/
- CLI command: .../github.com/ollykeran/sshush/cmd/sshush/
- Daemon command: .../github.com/ollykeran/sshush/cmd/sshushd/
- Internal sshushd pkg: .../github.com/ollykeran/sshush/internal/sshushd/

## Tools

- `go doc <package>` – show package docs
- `go doc <package>.<Symbol>` – show symbol docs
- `golang.org/x/tools/cmd/godoc` – local server for browsing (optional)
