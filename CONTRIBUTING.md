# Contributing to sshush

## Development setup

1. Clone the repo.
2. Install [just](https://github.com/casey/just) and Go 1.26+.
3. Run `just build` to build both binaries.
4. Run `just test` to run tests.

## Code style

- Run `go fmt`, `golangci-lint` (if configured).
- Add godoc comments for exported symbols. See [docs/godoc-guide.md](docs/godoc-guide.md).

## Pull requests

- Open an issue first if the change is large or controversial.
- Keep PRs focused. One logical change per PR.
- Ensure tests pass before submitting.
