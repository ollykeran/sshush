# Windows

sshush runs natively on Windows as an SSH agent for the OpenSSH that ships with it (`ssh.exe`, `ssh-add.exe`), and anything else that reads `SSH_AUTH_SOCK`.

See also: [Setup](setup.md) | [Config](config.md)

## What works

| | Windows |
|---|---|
| Agent with `[agent].type = "keys"` (`start`, `stop`, `reload`, `list`, `add`, `remove`) | yes |
| `ssh.exe`, `ssh-add.exe`, Git over SSH through the agent | yes |
| PowerShell integration (`--shell powershell`) | yes |
| Vault (`[agent].type = "vault"`, `sshush vault …`, `lock` / `unlock`) | not yet |
| SSH server (`sshush server`) | not yet: it refuses to start |

The vault is held back on purpose. Its protection on disk rests on file modes (`0600`), which Windows does not have; until the vault file gets a proper access control list it would be readable more widely than it should be.

## Quick start

Put `sshush.exe` and `sshushd.exe` in the same directory, on your `PATH`, then in PowerShell:

```powershell
sshush start --shell powershell | Invoke-Expression
ssh-add -l
```

The first run creates `%LOCALAPPDATA%\sshush\config.toml`, listing the keys found in `~\.ssh`. `sshush start` starts the daemon in the background and prints the line that sets `SSH_AUTH_SOCK`; `Invoke-Expression` applies it to the current shell.

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

- Set `socket_path = '\\.\pipe\openssh-ssh-agent'`. That is the pipe `ssh.exe` uses when `SSH_AUTH_SOCK` is unset, so everything finds sshush with no setup at all. It only works while the Windows "OpenSSH Authentication Agent" service is stopped, since the two cannot share the name.

A path that is not a pipe name is treated as a Unix socket, which Windows supports but `ssh.exe` does not.

## Paths

| | |
|---|---|
| Config | `%LOCALAPPDATA%\sshush\config.toml` (or `$XDG_CONFIG_HOME\sshush\config.toml` if that is set) |
| Pidfile | `%LOCALAPPDATA%\sshush\sshush.pid` |
| Key paths in config | either separator; `"~/.ssh/id_ed25519"` and `'C:\Users\me\.ssh\id_ed25519'` both work. In a double-quoted TOML string a backslash must be doubled. |

## Stopping the daemon

`sshush stop` asks the daemon to exit and waits for it. If the daemon is killed some other way (Task Manager, a reboot) its pidfile is left behind; the next `sshush start` notices and starts normally.

## Using it from WSL

A Linux distro under WSL can run its own sshush on a Unix socket, which needs nothing from Windows. Sharing the Windows agent with WSL (through a relay such as npiperelay) is not documented yet.

## Building

Cross-compile from any platform:

```sh
just build windows
```

which writes `build/windows-amd64/sshush.exe` and `sshushd.exe`.
