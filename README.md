# sshush

An SSH agent with a styled CLI + TUI. (Drop-in replacement for `ssh-agent`) 

Manages keys via a Unix socket, compatible with OpenSSH commands (`ssh`, `ssh-add`, etc.).

## Usage

```sh
sshush start                      # start daemon
sshush stop                       # stop daemon
sshush list                       # list loaded keys
sshush add <path>                 # add a key
sshush remove <fingerprint>       # remove a key
sshush tui                        # wip
```

Config: `~/.config/sshush/config.toml` (or `$SSHUSH_CONFIG`)

```toml
socket_path = "~/.ssh/sshush.sock"
key_paths   = ["~/.ssh/id_ed25519","~/.ssh/id_rsa"]
```

Add this to your `.bashrc` or `.bash_profile` 

```sh
eval $(sshush)
```

## Build

Go v.1.26
```sh
make build
```

Produces `sshush` (CLI) and `sshushd` (daemon). Both must be in `PATH` or in the same directory.

## Status (WIP) 

Core agent (start/stop/list/add/remove) works. 

`lock`/`unlock` not implemented.
