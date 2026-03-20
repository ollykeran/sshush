# Desktop GUI (Fyne, Linux PoC)

Optional windowed UI in `cmd/sshush-gui` and `internal/gui`. It calls the same packages as the TUI (`internal/sshushd`, `internal/agent`, `internal/config`, `internal/keys`, `internal/editcomment`, etc.), not a second implementation.

## Prerequisites (build)

Fyne uses **CGO**, **`pkg-config`**, and X11 / OpenGL **development** headers. Without them, `just build-gui` fails (same as plain `go build`).

### Debian / Ubuntu (and most derivatives)

```sh
sudo apt-get update
sudo apt-get install -y build-essential pkg-config \
  libgl1-mesa-dev xorg-dev libx11-dev libxcursor-dev libxrandr-dev \
  libxinerama-dev libxi-dev
```

### Fedora / RHEL-style

```sh
sudo dnf install gcc pkg-config mesa-libGL-devel libX11-devel libXcursor-devel \
  libXrandr-devel libXinerama-devel libXi-devel
```

### If you see these errors

| Message | Fix |
|--------|-----|
| `fatal error: X11/Xlib.h: No such file` | Install X11 dev packages (e.g. `libx11-dev` on Debian, `libX11-devel` on Fedora). |
| `exec: "pkg-config": executable file not found` | Install the `pkg-config` package (Debian/Fedora names above). |

WSL2: use WSLg or an X server; you still need the dev packages above on the Linux side to **compile**.

## Build and run

From the repo root (see `justfile`):

- `just build-gui` — produces `build/sshush-gui`
- `just run-gui` — `go run ./cmd/sshush-gui`

Default `just build` only builds `sshush` and `sshushd`; the GUI is opt-in.

## Tests

- `just test-gui` runs `go test` for `internal/gui` (currently the hex colour helper). **Building `internal/gui` still requires the same Fyne/CGO toolchain as `build-gui`.**
- Headless CI often has no GPU/X11 dev libraries; compiling the GUI target there is optional. Add a dedicated workflow job if you want to gate on `build-gui`.

## Manual PoC checklist

1. **Agent**: Top status strip shows agent state (colour-coded) and shared feedback messages; Start / Stop / Reload and key actions update both lines.
2. **Keys**: Refresh list; Add key via file dialog; Remove selected; cancel file dialog adds nothing.
3. **Discovered**: Refresh; add selected path to agent.
4. **Lock / Unlock**: Passphrase dialogs; agent reports errors if not supported.
5. **Theme**: Apply preset writes `[theme]` via `config.WriteThemeToPath` (requires an existing config file). The same tab has **Smaller / Larger / Default scale** to zoom the whole UI (text, padding, controls). File open/save dialogs are sized from the main window with padding (minimum about 640×520).
6. **Create**: Generate keypair; files appear under chosen directory.
7. **Edit**: Open key; optional “Edit comment in $EDITOR”; Save (uses `keys.SaveWithComment`).
8. **Export**: Load from file or agent list; copy to clipboard; save to file.

## References

- [Fyne](https://github.com/fyne-io/fyne)
