# Setup Guide

How to get sshush running and integrated with your shell.

See also: [Config](config.md) | [TUI](tui.md)

## eval $(sshush)

To start the agent and export `SSH_AUTH_SOCK` for your shell:

```sh
$ eval $(sshush)
```

Running `sshush` with no arguments starts the daemon (if needed), loads keys from config, and prints the export line. Piping to `eval` applies it to the current shell.

You can also use the explicit `start` command:

```sh
$ eval $(sshush start)
```

Both are equivalent. The export line goes to stdout so `eval $(sshush)` works; other output (status, warnings) goes to stderr.

## Automatic setup (SetupConfig)

On every run, before loading config, sshush runs `SetupConfig()`. It does two things if needed:

1. **CreateDefaultConfig** (when the default config file does not exist):
   - Renders that file from an embedded template (`internal/config/default_config.toml.tmpl`) so the layout stays easy to edit in the repo
   - Creates the config directory: `$XDG_CONFIG_HOME/sshush/` if `XDG_CONFIG_HOME` is set, otherwise `~/.config/sshush/`
   - Scans `~/.ssh` for valid private keys (skips dirs and `.pub` files)
   - Writes `[agent].socket_path` (shown with `~` when under your home directory):
     - `$XDG_RUNTIME_DIR/sshush.sock` when `XDG_RUNTIME_DIR` is set (common on Linux desktops)
     - otherwise `~/.config/sshush/sshush.sock` (stable on macOS and minimal Linux environments)
   - Writes `[agent].vault` = false and `[agent].key_paths` = discovered keys from `~/.ssh`
   - Writes `[theme]` with `name = "default"` and commented custom colour hints
   - Appends commented-out `[vault]` and `[server]` sections with example keys so you can enable them later
   - Does not overwrite an existing config
2. **AddEvalToShell** (when your shell rc file does not already contain `eval $(sshush)`):
   - Chooses the rc file from `$SHELL` when possible (`zsh` → `~/.zshrc`, `bash` → `~/.bashrc`)
   - On macOS, defaults to `~/.zshrc` when `SHELL` is empty or not zsh/bash
   - On other Unix systems, if neither rc file exists yet, sshush may create `~/.bashrc` and add the line
   - If the rc file already exists, sshush appends the line

## Shell startup (.zshrc / .bashrc / .bash_profile)

Add this line so each new shell gets the agent:

```sh
eval $(sshush)
```

On macOS with zsh, put it in `~/.zshrc`. With bash, use `~/.bashrc` or `~/.bash_profile` as you prefer. If sshush auto-setup created or updated your rc file, you may already have this line.

On login, `sshush` will start the daemon if needed and export `SSH_AUTH_SOCK` so `ssh`, `git`, and other tools can use your keys.
