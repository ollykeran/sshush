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
| Tray (`sshush-tray.exe`): agent state, unlock, keys and vault in a window | yes |
| One agent shared with WSL (`[agent].type = "windows"` in the distro) | yes |
| SSH server (`sshush server`) | no: it refuses to start |

The SSH server gives whoever signs in a shell on a pseudo-terminal, with process groups to clean up after them. Both are Unix mechanisms the server is built on, so on Windows it says it is not supported rather than half work.

## Quick start

Unpack the release zip into a folder on your `PATH` (see [Installing](#installing); a Microsoft Store package is coming soon). Then in PowerShell:

```powershell
sshush start --shell powershell | Invoke-Expression
ssh-add -l
```

The first run creates `~\.config\sshush\config.toml`, listing the keys found in `~\.ssh`. `sshush start` starts the daemon in the background and prints the line that sets `SSH_AUTH_SOCK`; `Invoke-Expression` applies it to the current shell.

To do that in every new PowerShell window, add this line to your profile (`notepad $PROFILE`):

```powershell
if (Get-Command sshush -ErrorAction SilentlyContinue) { sshush start --shell powershell | Invoke-Expression }
```

If your profile already exists, sshush adds the line for you the first time it runs, in the encoding and with the line endings the file already has (Windows PowerShell saves profiles as UTF-16 when they are made with `>` or `Out-File`); a profile it cannot read as text is left alone, and the line is printed instead. It never creates a profile: PowerShell's default execution policy refuses to run profile scripts, and a profile it refuses means an error in every new window. If you do not have one yet, sshush prints the line once and leaves the rest to you.

## The agent's named pipe

On Unix the agent listens on a socket file. Windows OpenSSH talks to its agent over a named pipe instead, so on Windows `[agent].socket_path` is a pipe name:

```toml
[agent]
socket_path = '\\.\pipe\sshush-agent'
```

Single quotes make it a TOML literal string, so the backslashes need no escaping. Forward slashes (`"//./pipe/sshush-agent"`) work as well.

Only you (and the SYSTEM account) can connect to the pipe.

That covers a pipe sshush is serving. The name, though, belongs to whichever account creates it first, and `\\.\pipe\sshush-agent` is one name for the whole machine, not one per user. So before sending anything, sshush also checks who owns the pipe it has connected to, and refuses to talk to one served by another account: your passphrase and keys are not handed to whoever got to the name before your agent did. (A pipe owned by SYSTEM or by the Administrators group is accepted, since that is what a service or a daemon started from an administrator prompt looks like.) On a machine several people use, the first to start sshush holds the default name and everyone else is refused, so give each user a `socket_path` of their own, such as `'\\.\pipe\sshush-agent-alice'`.

`\\.\pipe\sshush-agent` is the default. Programs find it through `SSH_AUTH_SOCK`, which is why the PowerShell line above matters. Two alternatives:

- Name the pipe for one host or all of them in `~\.ssh\config`, which needs no environment variable. Write it with forward slashes there: `ssh` treats a backslash in its config as an escape, so `\\.\pipe\sshush-agent` as written is not found.

  ```
  Host *
      IdentityAgent //./pipe/sshush-agent
  ```

  sshush adds this block for you the first time it runs, if you have a `~\.ssh` folder and its `config` does not set `IdentityAgent` already. It is written once, for the default pipe: if you change `socket_path` later, change it here too.

  It does not add it when another agent looks to be in use, since `IdentityAgent` would take `ssh` away from that agent. That means either of:

  - the Windows "OpenSSH Authentication Agent" service (`ssh-agent`) is running;
  - `SSH_AUTH_SOCK` is already set to something other than sshush's pipe, in the shell sshush was run from or for new programs (`setx`, System Properties).

  sshush then says which it saw, leaves `~\.ssh\config` alone and prints the block for you to add yourself. The `ssh` that comes with Git for Windows reads the same `~\.ssh\config`, so the block applies to it as well.

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

Copying a public key or a recovery phrase goes to the Windows clipboard directly; nothing extra to install. The recovery phrase that `sshush vault init` copies is marked so that Windows keeps it out of clipboard history (Win+V) and does not sync it to your other devices; a clipboard manager that ignores those marks can still record it.

`SSHUSH_COPY_CMD` replaces the clipboard API with a command of your own that reads the text on stdin, as on Linux and macOS (see [Setup: Clipboard](setup.md#clipboard)). That command gets the recovery phrase too, without the marks: only the default keeps it out of history and cloud sync.

## The tray

`sshush-tray.exe` puts sshush in the notification area. Its icon shows whether the agent is running and, for a vault, whether it is locked; its menu names the agent and lists its keys. Clicking the icon opens the main window, which does what the TUI's Agent and Vault tabs do:

- start and stop the agent, and lock or unlock a vault;
- list the agent's keys, or the vault's, with the selected key's details beside them;
- add a key file, create a new key, or set up a vault when there is none;
- remove, rename, load or unload a key, switch its autoload, and copy its fingerprint or public key;
- run `sshush selftest`.

The window follows Windows' light or dark mode. "Start at sign-in" starts the tray, without opening the window, when you sign in. Starting the tray again replaces the one already running rather than adding a second icon.

The tray is a client of the agent, as the CLI is: closing it, or it crashing, leaves the agent running. It reads the same `~\.config\sshush\config.toml` and talks to the same pipe, so the tray, PowerShell and WSL all see one agent.

## Stopping the daemon

`sshush stop` asks the daemon to exit and waits for it. If the daemon is killed some other way (Task Manager, a reboot) its pidfile is left behind; the next `sshush start` notices and starts normally.

## Using it from WSL

A Linux distro under WSL can run its own sshush on a Unix socket, which needs nothing from Windows. Or it can share the agent sshush runs on Windows: one daemon, one vault and one unlock for PowerShell and the distro alike.

To share it, install sshush in the distro as well, the same version as on Windows, and tell it where the agent is. In the distro's `~/.config/sshush/config.toml`:

```toml
[agent]
type = "windows"
socket_path = "~/.ssh/sshush-windows.sock"
```

Then `sshush start` in the distro, or the `eval $(sshush)` line sshush adds to your shell's startup file, does the rest:

- it starts a small daemon in the distro that listens on `socket_path` and holds no keys: it passes every connection on to the agent on Windows;
- if sshush is not running on Windows it starts it there;
- if that agent is a locked vault it asks for the passphrase, once, and both sides are unlocked.

From then on the distro and PowerShell are two views of the same agent:

- `ssh`, `git` and `ssh-add` in the distro use the keys the Windows agent holds.
- `sshush list`, `add`, `remove`, `lock` and `unlock` act on the Windows agent, and locking or unlocking on one side does it for the other.
- `sshush vault list`, `add`, `remove`, `load` and `autoload` work on the Windows vault, by fingerprint, comment or key file. A locked vault is unlocked in passing, with a prompt for its passphrase. There is no `[vault]` section in the distro's config: the daemon answers for its own vault, so the vault file stays on Windows and WSL never reads it.
- `sshush tui` shows the Vault tab once it has heard from the agent.
- `sshush stop` and `sshush reload` in the distro act on the relay only. The agent, and its keys, are stopped and reloaded from Windows. `sshush vault init` has nothing to create in the distro; make the vault on Windows.

`sshush vault add ~/.ssh/id_ed25519` in the distro reads the key file there and sends its contents, so the key need not exist on Windows. The path the vault shows for it is the WSL one, which means nothing to the Windows side; that column is a note of where a key came from, not something sshush opens again.

### How it gets across

A Unix socket does not reach out of WSL and a named pipe does not reach in, but a Linux process can start a Windows one and talk to it over its input and output. For each connection to `socket_path` that sends a request, the daemon in the distro runs `sshush.exe relay`, which connects that input and output to the agent's pipe as you, so the pipe's access list is satisfied without loosening it. That costs one short-lived Windows process per connection, about 70 ms on the machine this was measured on. A connection that only checks whether the socket is there, and hangs up, costs none.

[Architecture](architecture.md#one-agent-for-windows-and-wsl) has this as a diagram, along with what `sshush start` does on each side.

The daemon finds `sshush.exe` on `PATH`, which under WSL includes the Windows `PATH`. If you have turned that off, or sshush is not on the Windows `PATH`, say where it is:

```toml
[agent]
type = "windows"
socket_path = "~/.ssh/sshush-windows.sock"
windows_sshush = "/mnt/c/Tools/sshush/sshush.exe"
```

With sshush installed from the Store, `sshush.exe` on the Windows `PATH` is the package's app execution alias, `%LOCALAPPDATA%\Microsoft\WindowsApps\sshush.exe`, and WSL runs it like any other Windows program. To name it in `windows_sshush`, use the path as WSL sees it, such as `/mnt/c/Users/<you>/AppData/Local/Microsoft/WindowsApps/sshush.exe`.

If something does not work:

- **"the sshush agent on Windows is not answering"**: the relay in the distro is up and the agent behind it is not. `sshush start`, in the distro or in PowerShell, starts it.
- **"cannot find sshush.exe on PATH"**: set `windows_sshush` as above.
- **`sshush start` says the agent is not answering through the relay, straight after starting it**: the two installs are different versions. It prints what `sshush.exe relay` said; an `sshush.exe` from before this feature does not know the command.
- **A vault that is locked** lists no keys in the distro either. `sshush unlock` on either side opens it for both.

### Without sshush in the distro

If all the distro needs is for `ssh` and `git` to find the Windows agent, two other tools do the relaying with no sshush on the Linux side: `socat` in the distro, and [npiperelay](https://github.com/jstarks/npiperelay) on the Windows side (`go install github.com/jstarks/npiperelay@latest` with `GOOS=windows`, or a release binary), somewhere WSL can run it from.

In your WSL shell's startup file:

```sh
export SSH_AUTH_SOCK="$HOME/.ssh/sshush-windows.sock"
if ! ss -lx 2>/dev/null | grep -q "$SSH_AUTH_SOCK"; then
    rm -f "$SSH_AUTH_SOCK"
    (setsid socat UNIX-LISTEN:"$SSH_AUTH_SOCK",fork \
        EXEC:"/mnt/c/path/to/npiperelay.exe -ei -s //./pipe/sshush-agent",nofork &) >/dev/null 2>&1
fi
```

The same thing for fish, in `~/.config/fish/config.fish`:

```fish
set -gx SSH_AUTH_SOCK $HOME/.ssh/sshush-windows.sock
if not ss -lx 2>/dev/null | string match -q "*$SSH_AUTH_SOCK*"
    rm -f $SSH_AUTH_SOCK
    setsid socat UNIX-LISTEN:$SSH_AUTH_SOCK,fork,mode=600 EXEC:"/mnt/c/path/to/npiperelay.exe -ei -s //./pipe/sshush-agent",nofork >/dev/null 2>&1 &
    disown
end
```

`ssh-add -l` in WSL should then list the keys the Windows agent holds. Nothing here starts the Windows agent or the relay for you, and after a WSL restart a stale socket file is left behind, which the snippets remove and a hand-written one may not.

An sshush in the distro can use a relay like this one too, as an agent it does not run:

```toml
[agent]
type = "external"
socket_path = "~/.ssh/sshush-windows.sock"
```

Everything in the list above then works the same way, except that `sshush start`, `stop` and `reload` refuse: neither the relay nor the agent is sshush's to manage there.

## Installing

### From the Microsoft Store (coming soon)

sshush is on its way to the Microsoft Store as **sshush agent**; this section describes it for when it is listed. The Store installs and updates it, and it runs where Windows blocks an unsigned program downloaded from the internet. It puts the tray in the Start menu, starts it at sign-in, and makes `sshush` a command in PowerShell, cmd and WSL.

To stop the tray starting at sign-in, untick "Start at sign-in" in it, or turn it off in Settings > Apps > Startup. Once it has been turned off in Settings, only Settings can turn it back on; the tray opens that page for you.

Your config, keys and vault are kept in your profile as usual, not in the package, so uninstalling the app leaves them where they are.

### From a release

Releases include `sshush-<version>-windows-amd64.zip`, holding `sshush.exe`, `sshushd.exe` and `sshush-tray.exe`. Unpack them into one folder and add it to your `PATH`. Each binary has a published checksum (`sshush-windows-amd64.sha256`, `sshushd-windows-amd64.sha256` and `sshush-tray-windows-amd64.sha256`); `sshush selftest` checks its own for you.

Run `sshush-tray.exe` for the tray; its "Start at sign-in" adds it to your user's `Run` key.

If both are installed, the Store's `sshush` comes first on `PATH`, ahead of the one from the zip.

## Building

Cross-compile from any platform:

```sh
just build windows
```

which writes `build/windows-amd64/sshush.exe`, `sshushd.exe` and `sshush-tray.exe`; `just pkg zip-windows` zips them as a release does. The Store package is built by `scripts/msix.sh`; [packaging/msix/README.md](../packaging/msix/README.md) has how to build and try it locally.
