# Contributing to sshush

## Development setup

1. Clone the repo.
2. Install [just](https://github.com/casey/just) and Go 1.27+.
3. Run `just build` to build both binaries.
4. Run `just test` to run tests.
5. Run `just test all` before pushing: the tests, then `just check-windows`.

## What CI tests, and where

| Job | Runs on | Covers |
|---|---|---|
| `test`, `test-macos` | Linux, macOS | The whole suite, including `test/e2e` against built binaries. |
| `windows / test-windows` | Windows | The whole suite on Windows: the named pipe, file permissions, and `sshush relay` as a real process against a running `sshushd.exe`. |
| `windows / test-wsl` | Windows with a WSL 2 distro | `[agent].type = "windows"` across the real boundary: an sshush in the distro relaying to `sshush.exe` on the runner. |

`just act test` runs the Linux job locally. `act` cannot run the Windows jobs from macOS or Linux.

`just check-windows` is what macOS and Linux can do for Windows: it builds, vets and lints the code for `GOOS=windows` and links every test binary, then lists what it left to CI. It runs no Windows test.

To run the WSL tests yourself, on a Windows machine, from a checkout on a Windows drive (`/mnt/c/...`) inside the distro:

```sh
just e2e-wsl
```

They are skipped everywhere else (`SSHUSH_E2E_WSL` is unset). A tag is not released unless both Windows jobs pass.

## Code style

- Run `go fmt`, `just lint` (golangci-lint).
- Add godoc comments for exported symbols. See [docs/godoc-guide.md](docs/godoc-guide.md).

## Pull requests

- Open an issue first if the change is large or controversial.
- Keep PRs focused. One logical change per PR.
- Ensure tests pass before submitting.
