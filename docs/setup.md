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
   - Writes `[agent].type` = `"keys"` and `[agent].key_paths` = discovered keys from `~/.ssh`
   - Writes `[agent].shell` = the shell that ran sshush (its parent process when that is `fish`, `bash`, `zsh` or `sh`; otherwise `$SHELL`; otherwise `"posix"`)
   - Writes `[theme]` with `name = "default"`, `no_color = false` and commented custom colour hints
   - Appends commented-out `[vault]` and `[server]` sections so you can enable them later. `[server]` lists every server option, each with its default or an example, all commented out: the SSH server gives whoever signs in a shell as you, so it stays off until you uncomment `[server]` and `listen_port` yourself (see [Config: `[server]`](config.md#server))
   - Does not overwrite an existing config
2. **AddEvalToShell** (when your shell startup files do not already start sshush):
   - Chooses the rc file from `$SHELL` when possible (`zsh` → `~/.zshrc`, `bash` → `~/.bashrc`)
   - For `fish`, creates `~/.config/fish/conf.d/sshush.fish` (under `$XDG_CONFIG_HOME/fish` when set) instead of touching an rc file. It is skipped if that file already exists, or if `config.fish` already starts sshush (for example `eval (sshush start)`)
   - On macOS, defaults to `~/.zshrc` when `SHELL` is empty or not zsh/bash
   - On other Unix systems, uses `~/.bashrc` if it exists, else `~/.zshrc` if that exists, else creates `~/.bashrc` and adds the line
   - If the rc file already exists, sshush appends the line

## Shell startup (.zshrc / .bashrc / .bash_profile)

Add this line so each new shell gets the agent:

```sh
eval $(sshush)
```

On macOS with zsh, put it in `~/.zshrc`. With bash, use `~/.bashrc` or `~/.bash_profile` as you prefer. If sshush auto-setup created or updated your rc file, you may already have this line.

## Fish

Fish uses its own syntax, so ask for it with `--shell fish` and pipe the result to `source`:

```fish
if status is-interactive
    sshush start --shell fish | source
end
```

This prints `set -gx SSH_AUTH_SOCK '...';` instead of the POSIX `export` line. With fish as your login shell, sshush auto-setup writes exactly this to `~/.config/fish/conf.d/sshush.fish`, so you normally do not need to add it yourself. Delete the contents of that file (but keep the file) if you would rather manage the line in `config.fish`.

The syntax comes from `[agent].shell` in your config, which first-time setup fills in from the shell that ran sshush, so on a fish-first machine plain `sshush start | source` already prints fish syntax. `--shell` overrides the config for one call and accepts `posix` (`sh`, `bash` and `zsh` are aliases) and `fish`; without either, the output is POSIX.

If you use more than one shell, pass `--shell` explicitly in each shell's startup line (for example `eval $(sshush --shell posix)` in `~/.bashrc`), because `[agent].shell` applies to every caller.

For completions, add `sshush completion fish | source`.

On login, `sshush` will start the daemon if needed and export `SSH_AUTH_SOCK` so `ssh`, `git`, and other tools can use your keys.

Starting the daemon waits for it to confirm it's actually ready rather than guessing on a fixed delay, so this works reliably even when the shell startup environment is busy (many rc scripts launching at once). If startup does fail, the error shown is the daemon's actual failure reason (bad config, a vault problem, and so on), not a generic timeout. See [Architecture: Daemon startup](architecture.md#daemon-startup) for how this works.
