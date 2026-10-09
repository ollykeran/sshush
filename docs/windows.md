# Windows

sshush runs natively on Windows as an SSH agent for the OpenSSH that ships with it (`ssh.exe`, `ssh-add.exe`), and anything else that reads `SSH_AUTH_SOCK`.

See also: [Setup](setup.md) | [Config](config.md)

## What works

| | Windows |
|---|---|
| Agent (`start`, `stop`, `reload`, `list`, `add`, `remove`) | yes |
| `ssh.exe`, `ssh-add.exe`, Git over SSH through the agent | yes |
| PowerShell integration (`--shell powershell`) | yes |
| Vault (`[agent].type = "vault"`, `sshush vault …`, `lock` / `unlock`) | yes |
| TUI, key creation and editing, clipboard | yes |
| SSH server (`sshush server`) | no: it refuses to start |

The SSH server gives whoever signs in a shell on a pseudo-terminal, with process groups to clean up after them. Both are Unix mechanisms the server is built on, so on Windows it says it is not supported rather than half work.

## Quick start

Put `sshush.exe` and `sshushd.exe` in the same directory, on your `PATH`, then in PowerShell:

```powershell
sshush start --shell powershell | Invoke-Expression
ssh-add -l
```

The first run creates `~\.config\sshush\config.toml`, listing the keys found in `~\.ssh`. `sshush start` starts the daemon in the background and prints the line that sets `SSH_AUTH_SOCK`; `Invoke-Expression` applies it to the current shell.

To do that in every new PowerShell window, add this line to your profile (`notepad $PROFILE`):

```powershell
if (Get-Command sshush -ErrorAction SilentlyContinue) { sshush start --shell powershell | Invoke-Expression }
```

If your profile already exists, sshush adds the line for you the first time it runs. It never creates a profile: PowerShell's default execution policy refuses to run profile scripts, and a profile it refuses means an error in every new window. If you do not have one yet, sshush prints the line once and leaves the rest to you.

## The agent's named pipe

On Unix the agent listens on a socket file. Windows OpenSSH talks to its agent over a named pipe instead, so on Windows `[agent].socket_path` is a pipe name:

```toml
[agent]
socket_path = '\\.\pipe\sshush-agent'
```

Single quotes make it a TOML literal string, so the backslashes need no escaping. Forward slashes (`"//./pipe/sshush-agent"`) work as well.

Only you (and the SYSTEM account) can connect to the pipe.

`\\.\pipe\sshush-agent` is the default. Programs find it through `SSH_AUTH_SOCK`, which is why the PowerShell line above matters. Two alternatives:

- Name the pipe for one host or all of them in `~\.ssh\config`, which needs no environment variable. Write it with forward slashes there: `ssh` treats a backslash in its config as an escape, so `\\.\pipe\sshush-agent` as written is not found.

  ```
  Host *
      IdentityAgent //./pipe/sshush-agent
  ```

  sshush adds this block for you the first time it runs, if you have a `~\.ssh` folder and its `config` does not set `IdentityAgent` already. It is written once, for the default pipe: if you change `socket_path` later, change it here too.

- Set `socket_path = '\\.\pipe\openssh-ssh-agent'`. That is the pipe `ssh.exe` uses when `SSH_AUTH_SOCK` is unset, so everything finds sshush with no setup at all. It only works while the Windows "OpenSSH Authentication Agent" service is stopped, since the two cannot share the name.

A path that is not a pipe name is treated as a Unix socket, which Windows supports but `ssh.exe` does not.

## Paths

| | |
|---|---|
| Config | `~\.config\sshush\config.toml` (or `$XDG_CONFIG_HOME\sshush\config.toml` if that is set), the same place as on Unix; `~` is `%USERPROFILE%` |
| Pidfile | `~\.config\sshush\sshush.pid` |
| Key paths in config | either separator; `"~/.ssh/id_ed25519"` and `'C:\Users\me\.ssh\id_ed25519'` both work. In a double-quoted TOML string a backslash must be doubled. |

## Who can read your files

On Unix sshush keeps its secrets private with file modes (`0600`). Windows has no such modes: a new file takes its permissions from the folder it is in. So on Windows sshush sets the permissions itself, on the vault file, the folder it creates for it, `recovery.txt`, and any private key it writes: your account and SYSTEM, nobody else, with inheritance from the parent folder switched off. To check one:

```powershell
icacls $HOME\.config\sshush\vault.json
```

Key files you already had are left as they are until sshush rewrites one (editing its comment, say).

## Editor and clipboard

`sshush edit` opens the comment in `$env:EDITOR`, or `--editor`; with neither set it uses vim or nano if installed, otherwise Notepad. A path with spaces works bare or quoted, and quotes are needed once there are arguments as well:

```powershell
$env:EDITOR = '"C:\Program Files\Notepad++\notepad++.exe" -multiInst -nosession'
```

The editor has to stay in the foreground until you close the file. For VS Code that is `code --wait`.

Copying a public key or a recovery phrase goes to the Windows clipboard directly; nothing extra to install.

## Stopping the daemon

`sshush stop` asks the daemon to exit and waits for it. If the daemon is killed some other way (Task Manager, a reboot) its pidfile is left behind; the next `sshush start` notices and starts normally.

## Using it from WSL

A Linux distro under WSL can run its own sshush on a Unix socket, which needs nothing from Windows.

To share the one Windows agent with WSL instead, relay a Unix socket in the distro to the Windows pipe. This takes two extra tools: `socat` in the distro, and [npiperelay](https://github.com/jstarks/npiperelay) on the Windows side (`go install github.com/jstarks/npiperelay@latest` with `GOOS=windows`, or a release binary), somewhere WSL can run it from.

In your WSL shell's startup file:

```sh
export SSH_AUTH_SOCK="$HOME/.ssh/sshush-windows.sock"
if ! ss -lx 2>/dev/null | grep -q "$SSH_AUTH_SOCK"; then
    rm -f "$SSH_AUTH_SOCK"
    (setsid socat UNIX-LISTEN:"$SSH_AUTH_SOCK",fork \
        EXEC:"/mnt/c/path/to/npiperelay.exe -ei -s //./pipe/sshush-agent",nofork &) >/dev/null 2>&1
fi
```

Each connection to the socket starts one `npiperelay.exe`, which connects to sshush's pipe as you, so the pipe's access list is satisfied without loosening it. `ssh-add -l` in WSL should then list the keys the Windows agent holds.

If it does not:

- **Nothing listed, or "connection refused"**: check that sshush is running on Windows (`sshush list` in PowerShell) and that the pipe name in the `socat` line matches `[agent].socket_path`.
- **"Error connecting to agent"** after a WSL restart: a stale socket file is left behind; the snippet above removes it, a hand-written one may not.
- **A vault that is locked** lists no keys from WSL either; unlock it on the Windows side.

## Installing from a release

Releases include `sshush-<version>-windows-amd64.zip`, holding `sshush.exe` and `sshushd.exe`. Unpack both into one folder and add it to your `PATH`. Each binary has a published checksum (`sshush-windows-amd64.sha256`), which `sshush selftest` checks for you.

## Building

Cross-compile from any platform:

```sh
just build windows
```

which writes `build/windows-amd64/sshush.exe` and `sshushd.exe`; `just pkg zip-windows` zips them as a release does.
