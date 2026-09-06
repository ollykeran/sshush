# sshush

[![tests](https://github.com/ollykeran/sshush/actions/workflows/test.yml/badge.svg)](https://github.com/ollykeran/sshush/actions/workflows/test.yml)
[![build](https://github.com/ollykeran/sshush/actions/workflows/build.yml/badge.svg)](https://github.com/ollykeran/sshush/actions/workflows/build.yml)
[![lint](https://github.com/ollykeran/sshush/actions/workflows/lint.yml/badge.svg)](https://github.com/ollykeran/sshush/actions/workflows/lint.yml)

OpenSSH compatible agent over a Unix socket (`SSH_AUTH_SOCK`, same agent protocol as `ssh-agent`) with a TUI, themes, and a config file. I wanted the agent to stay out of the way for normal `ssh` use but still be easy to inspect and reload from dotfiles, without piling on shell wrapper scripts. Optional passphrase vault stores derived keys in one file.

## Compared to `ssh-agent`

Stock agent is minimal: good for some workflows, easy to make invisible. sshush adds a `config.toml` with `sshush reload` to match the live agent to that file, plus `sshush tui` and a styled CLI. Optional vault and lock/unlock for encrypted at-rest key material in one place, instead of only relying on file permissions. Everything else: Unix socket, `ssh-add` / `ssh` work as usual.

**Tradeoff:** more surface area than a stock agent, so keep an eye on [issues](https://github.com/ollykeran/sshush/issues) if something surprises you on your OS or shell.

## Demo

[Asciinema recording](https://asciinema.org/a/917054) (click through for the player)

## Quick Start

```sh
eval $(sshush)
```

Starts the daemon if needed, loads keys from config, sets `SSH_AUTH_SOCK`. For shell rc and edge cases, see [Setup guide](docs/setup.md).

## Features

- **Agent**: start/stop, add/remove, list, reload from config, Unix socket (`SSH_AUTH_SOCK`).
- **Keys**: create, edit comment, find, export public key, validate. Editing a key's comment (`sshush edit`, or the TUI's `e` key on a loaded agent key) also updates its `.pub` companion file when one exists alongside the private key, so the two stay in sync.
- **Selftest**: `sshush selftest` checks agent connectivity — env, socket, key list, and signing.
- **TUI**: `sshush tui` for interactive key management.
- **Vault**: Encrypted on-disk key store with lock/unlock and recovery. See [docs/vault.md](docs/vault.md).
- **SSH server**: Optional TCP SSH server (`sshush server`) serving an interactive shell to authorized keys. See [Config Reference](docs/config.md) `[server]` section.
- **Reload**: `sshush reload` drops keys not in `config.toml` and adds new ones; changing `[agent].socket_path` can restart the daemon.
- **First run**: with no config, writes defaults under `$XDG_CONFIG_HOME/sshush` (or `~/.config/sshush`), discovers keys, picks a stable socket path. Regenerate the default file with `sshush generate config` (`--force` to overwrite).

## Common commands

| | |
| --- | --- |
| `eval $(sshush)` or `sshush start` | start daemon, export `SSH_AUTH_SOCK` |
| `sshush stop` | stop daemon |
| `sshush list` / `add` / `remove` | manage loaded keys |
| `sshush reload` | match agent to `config.toml` |
| `sshush tui` | interactive UI |
| `sshush create` / `edit` / `export` / `find` | key file operations |
| `sshush validate` | validate and inspect a key file (private or public) |
| `sshush selftest` | test agent connectivity (env, socket, list, sign) |
| `sshush vault …` / `lock` / `unlock` | optional encrypted vault (see [docs/vault.md](docs/vault.md)) |
| `sshush server` | optional TCP SSH server |
| `sshush theme` / `completion` / `version` | theming, shell completion, build info |

For every subcommand and flag, `sshush --help` and `sshush <subcommand> --help`.

**Config file:** `$XDG_CONFIG_HOME/sshush/config.toml` or `~/.config/sshush/config.toml`, or override with `-c` / `SSHUSH_CONFIG`. Reference: [Config](docs/config.md).

**Upgrading from older releases:** config layout changed from flat keys to `[agent]` / `[vault]` / `[server]` tables. See [Migration from flat TOML](docs/config.md#migration-from-flat-toml-breaking) before merging this branch into your setup.

## Installation

`sshush` and `sshushd` need to be visible together: either a directory on your `PATH`, or run them with explicit paths (same idea as a normal `ssh-agent` + client setup).

### Releases

[GitHub Releases](https://github.com/ollykeran/sshush/releases): `.deb`, `.rpm`, Arch `.pkg.tar.zst`, `tar.gz` (Linux and macOS arm/amd on the release page). Release archives ship both binaries; install or unpack as usual.

| Package | |
| --- | --- |
| **Debian/Ubuntu** | `sudo dpkg -i sshush-*-amd64.deb` |
| **RHEL/Fedora** | `sudo rpm -i sshush-*-amd64.rpm` |
| **Arch** | `sudo pacman -U sshush-*-amd64.pkg.tar.zst` |

### From source

```sh
go install github.com/ollykeran/sshush/cmd/sshush@latest
go install github.com/ollykeran/sshush/cmd/sshushd@latest
```

Binaries go to `$(go env GOBIN)` (or `GOPATH/bin`).

Clone (optional): `git clone https://github.com/ollykeran/sshush.git`

## Security

`sshushd` speaks the same agent protocol as `ssh-agent` over a local socket; private keys live in the agent process. Report vulnerabilities as described in [SECURITY.md](SECURITY.md) (not via public issues).

## Docs

- [Setup](docs/setup.md) – shell, eval, config path
- [Config](docs/config.md) – options, reload, flat TOML migration
- [Vault](docs/vault.md) – optional vault
- [TUI](docs/tui.md) – TUI structure
- [Architecture](docs/architecture.md) – packages
- [Godoc guide](docs/godoc-guide.md) – exported API comments
- [pkg.go.dev](https://pkg.go.dev/github.com/ollykeran/sshush) – API

**Developers:** [Contributing](CONTRIBUTING.md) · [Security](SECURITY.md) · [Internal boundary report](docs/internal-boundary-report.md) (auto-generated)

## Build

Go 1.26+, [`just`](https://github.com/casey/just) optional but recommended.

```sh
just build
```

Outputs `build/linux-amd64/sshush` and `build/linux-amd64/sshushd`. From the repo, run e.g. `./build/linux-amd64/sshush` or install the artifacts the same way you do other static binaries. macOS: `just build darwin`. Release layout and packaging: `just pkg all`, `just build darwin-arm64`, etc. (see [justfile](justfile)).
