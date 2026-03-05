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

1. **CreateDefaultConfig** (when `~/.config/sshush/config.toml` does not exist):
  - Creates `~/.config/sshush/` if needed
  - Scans `~/.ssh` for valid private keys (skips dirs and `.pub` files)
  - Writes `socket_path` = `$XDG_RUNTIME_DIR/sshush.sock` (when set) 
  - Writes `key_paths` = discovered keys from `~/.ssh` 
  - Does not overwrite an existing config
2. **AddEvalToShell** (when `~/.bashrc` exists and does not already contain `eval $(sshush)`):
  - Appends `eval $(sshush)` to `~/.bashrc`
  - Only modifies `~/.bashrc` (not `.bash_profile`); fails if `~/.bashrc` does not exist

## Shell startup (.bashrc / .bash_profile)

Add this line to `.bashrc` or `.bash_profile` so each new shell gets the agent:

```sh
eval $(sshush)
```

If you use bash and `~/.bashrc` exists, sshush may add it for you on first run (see AddEvalToShell above). Otherwise add it manually. For other shells or `.bash_profile`, add it yourself. On login, `sshush` will start the daemon if needed and export `SSH_AUTH_SOCK` so `ssh`, `git`, and other tools can use your keys.