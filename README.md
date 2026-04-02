# sshush

An SSH agent with a styled CLI and TUI. Drop-in replacement for `ssh-agent`. Manages keys via a Unix socket, compatible with OpenSSH (`ssh`, `ssh-add`, etc.).

## Quick Start

```sh
eval $(sshush)
```

That starts the daemon (if needed), loads keys from config, and exports `SSH_AUTH_SOCK`. Add it to `.bashrc` or `.bash_profile` for persistent setup. See [Setup Guide](docs/setup.md) for details.

## Features

- **Agent**: Start, stop, list keys, add, remove. Uses a Unix socket; works with OpenSSH.
- **Create/Edit/Export**: Generate keys (`create`), edit comments (`edit`), export public keys (`export`).
- **TUI**: Interactive terminal UI to manage keys, generate, edit, and export. Run `sshush tui`.
- **Reload**: `sshush reload` reconciles the agent to the config file. Keys not in config are removed; keys in config are added. If you change `socket_path`, the daemon restarts.
- **Config auto-setup**: On first run, if no config exists, sshush creates `~/.config/sshush/config.toml` with discovered keys and default socket path.

## Commands


| Sub Command      | Description                               | Example                                   |
| ---------------- | ----------------------------------------- | ----------------------------------------- |
| (none) / `start` | Start daemon, export `SSH_AUTH_SOCK`      | `eval $(sshush)`                          |
| `stop`           | Stop the daemon                           | `sshush stop`                             |
| `list`           | List keys in the agent                    | `sshush list`                             |
| `add`            | Add key(s) to the agent                   | `sshush add ~/.ssh/id_ed25519`            |
| `remove`         | Remove key(s) by path or comment          | `sshush remove id_ed25519`                |
| `reload`         | Reconcile agent to config file            | `sshush reload`                           |
| `create`         | Generate a new keypair                    | `sshush create rsa 2048 -o ~/.ssh/id_rsa` |
| `edit`           | Edit key comment                          | `sshush edit ~/.ssh/id_ed25519`           |
| `export`         | Export public key                         | `sshush export ~/.ssh/id_ed25519`         |
| `find`           | Find private keys (defaults: cwd, ~/.ssh) | `sshush find` or `sshush find /path`      |
| `tui`            | Start the TUI                             | `sshush tui`                              |
| `theme`          | Show or set colour theme                  | `sshush theme show`, `sshush theme list`, `sshush theme set dracula` |
| `completion`     | Shell completion script                   | `sshush completion bash`                  |
| `version`        | Print version                             | `sshush version`                          |


Config: `~/.config/sshush/config.toml` (override with `-c`). See [Config Reference](docs/config.md).

## Installation

### From releases

Download from [GitHub Releases](https://github.com/ollykeran/sshush/releases):

| Package | Install |
|---------|---------|
| **Debian/Ubuntu** (`.deb`) | `sudo dpkg -i sshush-*-amd64.deb` |
| **RHEL/Fedora** (`.rpm`) | `sudo rpm -i sshush-*-amd64.rpm` |
| **Arch Linux** (`.pkg.tar.zst`) | `sudo pacman -U sshush-*-amd64.pkg.tar.zst` |
| **Binary tarball** | Extract `sshush` and `sshushd` from the `.tar.gz`, place in `PATH` |

### From source

```sh
go install github.com/ollykeran/sshush/cmd/sshush@latest
go install github.com/ollykeran/sshush/cmd/sshushd@latest
```

Both binaries must be in `PATH`.

## Docs

- [Setup Guide](docs/setup.md) – eval, config creation, bashrc
- [Config Reference](docs/config.md) – options, reload behavior
- [TUI Architecture](docs/tui.md) – TUI structure and internals
- [Architecture](docs/architecture.md) – package layout
- [Godoc Guide](docs/godoc-guide.md) – adding godoc comments
- [pkg.go.dev](https://pkg.go.dev/github.com/ollykeran/sshush) – API documentation

### Developer docs

- [Contributing](CONTRIBUTING.md) – how to contribute
- [Internal Boundary Report](docs/internal-boundary-report.md) – package call graph (auto-generated)

## Build

Go 1.26+:

```sh
just build
```

Produces `sshush` (CLI) and `sshushd` (daemon) in `build/`. Both must be in `PATH` or the same directory.

On Apple Silicon, `just build` is already native darwin/arm64. For an explicit cross-compile or release layout, use `just build-darwin-arm64` (outputs under `build/darwin-arm64/`) and `just tarball-darwin-arm64` for `build/sshush-<version>-darwin-arm64.tar.gz`.