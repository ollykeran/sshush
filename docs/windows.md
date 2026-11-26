# Windows

sshush runs natively on Windows as an SSH agent for the OpenSSH that ships with it (`ssh.exe`, `ssh-add.exe`), and anything else that reads `SSH_AUTH_SOCK`.

See also: [Setup](setup.md) | [Config](config.md)

## What works

| | Windows |
|---|---|
| Agent (`start`, `stop`, `reload`, `list`, `add`, `remove`) | yes |
| `ssh.exe`, `ssh-add.exe`, Git over SSH through the agent | yes |
| Pageant programs: MobaXterm, WinSCP, PuTTY ([below](#pageant-programs-mobaxterm-winscp-putty)) | yes |
| Git Bash and the `ssh` bundled with Git for Windows, MSYS2, Cygwin ([below](#git-bash-msys2-and-cygwin)) | yes |
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
socket_path = '\\.\pipe\openssh-ssh-agent'
```

Single quotes make it a TOML literal string, so the backslashes need no escaping. Forward slashes (`"//./pipe/openssh-ssh-agent"`) work as well.

Only you (and the SYSTEM account) can connect to the pipe.

That covers a pipe sshush is serving. The name, though, belongs to whichever account creates it first, and a pipe name is one name for the whole machine, not one per user. So before sending anything, sshush also checks who owns the pipe it has connected to, and refuses to talk to one served by another account: your passphrase and keys are not handed to whoever got to the name before your agent did. (A pipe owned by SYSTEM or by the Administrators group is accepted, since that is what a service or a daemon started from an administrator prompt looks like.) On a machine several people use, the first to start sshush holds the default name and everyone else is refused, so give each user a `socket_path` of their own, such as `'\\.\pipe\sshush-agent-alice'`.

### The default: Windows' own agent's pipe

`\\.\pipe\openssh-ssh-agent` is the default. It is the pipe Windows' own OpenSSH agent uses, and the one `ssh.exe` talks to when `SSH_AUTH_SOCK` is unset, so `ssh`, Git and anything else built on Windows OpenSSH find sshush with no `SSH_AUTH_SOCK` and no `IdentityAgent`. The config sshush writes on its first run says so above `socket_path`.

Only one agent can hold the name, so the Windows "OpenSSH Authentication Agent" service (`ssh-agent`) has to be stopped. sshush says so on its first run if the service is running, and `sshush start` refuses with the same advice rather than mistaking that agent for its own. To stop it for good, from an administrator PowerShell:

```powershell
Stop-Service ssh-agent; Set-Service ssh-agent -StartupType Disabled
```

### A pipe of its own

If you would rather keep Windows' agent, or another agent needs that pipe, give sshush its own. The generated config has the line ready, commented out under the default:

```toml
socket_path = '\\.\pipe\openssh-ssh-agent'
# socket_path = '\\.\pipe\sshush-agent'
```

Swap which one is commented out. Programs then find sshush through `SSH_AUTH_SOCK`, which the PowerShell line above sets, or through `~\.ssh\config`, which needs no environment variable. Write the pipe with forward slashes there: `ssh` treats a backslash in its config as an escape, so `\\.\pipe\sshush-agent` as written is not found.

```
Host *
    IdentityAgent //./pipe/sshush-agent
```

Adding `IdentityAgent` takes `ssh` away from whichever agent it used before, so sshush leaves this to you.

The `ssh` that comes with Git for Windows also reads `~\.ssh\config`, but it cannot connect to a named pipe at all, so don't put this block where it applies to that `ssh`. It reaches sshush another way: see [Git Bash, MSYS2 and Cygwin](#git-bash-msys2-and-cygwin).

A path that is not a pipe name is treated as a Unix socket, which Windows supports but `ssh.exe` does not.

## Pageant programs (MobaXterm, WinSCP, PuTTY)

Some programs never look at `SSH_AUTH_SOCK` or the OpenSSH pipe. They come from the PuTTY family and only know PuTTY's agent, Pageant. MobaXterm, WinSCP, FileZilla and PuTTY itself are all like this. They find Pageant by a hidden window it keeps and pass their requests to it through shared memory.

The agent also acts as Pageant, using the same keys. This is `[agent].pageant_emulation`. It is on unless the config says `pageant_emulation = false`, and the tray's Settings page has it as **Also answer as Pageant**. Turning it on or off takes effect when the agent restarts.

To use it in MobaXterm:
1. Open Settings > Configuration > SSH.
2. Untick "Use internal SSH agent MobAgent".
3. Tick the PuTTY agent option. Older versions call it "Use external Pageant".
4. For agent forwarding, also tick "Forward SSH agents".

Newer MobaXterm versions also offer a "Windows SSH agent" option. That one reaches sshush through the OpenSSH pipe and doesn't need Pageant. Either option works.

WinSCP and PuTTY need nothing more: they use Pageant whenever it is there.

Some limits:
- **Only one program can be Pageant at a time.** If PuTTY's own Pageant, KeeAgent, or gpg-agent with PuTTY support is already running when the agent starts, sshush leaves it alone and says so on the Settings page. Quit the other one and restart the agent to take over.
- **Only you can use it.** Requests from another account are ignored.
- **Requests and replies are limited to 8 KiB,** the most a PuTTY-family program has room for. That covers dozens of keys; an agent with too many keys to list in that space answers with a failure.
- **Only the window is served,** not Pageant's newer named pipe. PuTTY 0.75 and later try the pipe first and then use the window.

## Git Bash, MSYS2 and Cygwin

Git for Windows comes with its own `ssh`, built for MSYS2. Git Bash runs it, and so does `git` itself unless told otherwise. That includes `git` run from VS Code, JetBrains IDEs and other tools. Cygwin's and MSYS2's `ssh` work the same way. None of them can use a named pipe or Pageant. They expect `SSH_AUTH_SOCK` to name a Unix socket, which Cygwin imitates with a small file pointing at a local network port.

The agent writes such a file and answers on it, with the same keys. By default it is `cygwin-agent.sock` beside `config.toml` (`~\.config\sshush\cygwin-agent.sock`). `[agent].cygwin_socket` puts it somewhere else, and `cygwin_socket = ""` turns it off. The tray's Settings page has the switch as **Also answer Git Bash (Cygwin socket)**, with the `SSH_AUTH_SOCK` line under it; a path of your own is set in the file. Either takes effect when the agent restarts. The file only exists while the agent runs.

In Git Bash, `sshush start --shell bash` prints the line that points `SSH_AUTH_SOCK` at that file. `--shell bash` matters when the config says `shell = "powershell"`, as it does if sshush first ran in PowerShell. To set it in every Git Bash window, add this to `~/.bashrc`:

```bash
eval "$(sshush start --shell bash)"
```

`ssh`, `ssh-add` and `git` in that shell then use sshush. For `git` started from somewhere other than Git Bash, such as an IDE, either set `SSH_AUTH_SOCK` for your Windows account to the file's path, or have `git` use Windows' OpenSSH, which finds sshush with no setting:

```powershell
git config --global core.sshCommand C:/Windows/System32/OpenSSH/ssh.exe
```

Only you can use the file. A program has to read the secret in it to get past the first exchange, and the file is readable by you alone.

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

`sshush-tray.exe` puts sshush in the notification area. Its icon shows whether the agent is running and, for a vault, whether it is locked; its menu names the agent and lists its keys. Clicking the icon opens the main window, which does what the TUI's Agent and Vault tabs do. Its header starts and stops the agent, and two pages sit below it.

**Keys**

- list the agent's keys, or the vault's, with the selected key's details beside them;
- add a key file or create a new key;
- remove, rename, load or unload a key, switch its autoload, and copy its fingerprint or public key.

**Vault**

- see the vault's state, where its file is, its size, when it was created and last changed, how many keys it holds, and whether a recovery phrase can unlock it;
- create a vault when there is none, and lock or unlock it;
- change the vault's passphrase, or give it a new recovery phrase (each asks for the current passphrase or recovery phrase, and shows the new phrase once);
- show the vault file in Explorer, or copy its path;
- back the vault up to a file of your choosing (`vault-YYYYMMDD.json` in Documents, by default);
- import a vault from another PC;
- switch the agent between serving the vault and serving your key files;
- choose when the tray locks the vault by itself (**Auto-lock**): when Windows locks or you switch user, when the PC goes to sleep, and after 5 minutes to an hour without keyboard or mouse input. All three are off by default, so the vault stays unlocked until you lock it. The settings are kept in the registry, under `HKCU\Software\sshush\Tray`.

Auto-lock is the tray's: it locks the vault only while the tray is running, and an agent you run without the tray never locks by itself.

**Moving a vault to another PC.** The vault is one file, and its keys stay encrypted under its passphrase wherever it goes. Back it up on the old PC, then on the new one choose **Import…** on the Vault page and pick the file. The tray checks the passphrase before changing anything. Then it stops the agent and copies the file in. If a vault was already there, it is kept beside it as `vault.json.bak-<time>`, along with its `recovery.txt`. The config is switched to vault mode if needed, and the agent is started and unlocked. The imported vault's recovery phrase is whatever it was on the old PC.

**Switching modes.** **Use a vault** sets `[agent].type = "vault"` in `config.toml`, and sets `[vault].vault_path` to the default path beside it if none is configured. **Use key files instead** sets it back to `"keys"`. Either way the agent restarts. The tray edits only those lines, so the file's comments and other settings stay as they were.

**Settings** edits what the agent serves and where: the agent type, its pipe, whether it also answers as Pageant and on a Cygwin socket for Git Bash, the vault file, the key files and the folders searched for keys. Changes to what the agent does take effect when you press **Restart agent**.

The footer runs `sshush selftest`. When it passes, the report offers the extended test (`sshush selftest --extended`): Yes starts a temporary sshush SSH server on 127.0.0.1, logs in to it with the agent's first key, and shuts it down again.

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
        EXEC:"/mnt/c/path/to/npiperelay.exe -ei -s //./pipe/openssh-ssh-agent",nofork &) >/dev/null 2>&1
fi
```

The same thing for fish, in `~/.config/fish/config.fish`:

```fish
set -gx SSH_AUTH_SOCK $HOME/.ssh/sshush-windows.sock
if not ss -lx 2>/dev/null | string match -q "*$SSH_AUTH_SOCK*"
    rm -f $SSH_AUTH_SOCK
    setsid socat UNIX-LISTEN:$SSH_AUTH_SOCK,fork,mode=600 EXEC:"/mnt/c/path/to/npiperelay.exe -ei -s //./pipe/openssh-ssh-agent",nofork >/dev/null 2>&1 &
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
