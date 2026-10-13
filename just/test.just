# Run tests with race detection (optional: specific package, --nocache to bypass Go's test cache).
# Output is formatted as readable sentences by gotestdox (pinned as a go.mod tool).
test *args:
    #!/usr/bin/env bash
    set -euo pipefail
    args=({{ args }})
    pkg="./..."
    extra=()
    for a in "${args[@]+"${args[@]}"}"; do
      if [ "$a" = "--nocache" ]; then
        extra+=(-count=1)
      else
        pkg="$a"
      fi
    done
    just _dox "$pkg" -race "${extra[@]+"${extra[@]}"}"

# Run tests with coverage report (saved to benchmarks/), output formatted by gotestdox
test-cover:
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p benchmarks
    just _dox ./... -race -coverprofile=benchmarks/coverage.out -covermode=atomic
    echo ""
    go tool cover -func=benchmarks/coverage.out
    echo ""
    echo "View HTML report: go tool cover -html=benchmarks/coverage.out"

# Generate the same markdown coverage report CI posts on PRs (needs a coverprofile)
cover-report profile="benchmarks/coverage.out":
    go run ./scripts/cover-report -profile {{ profile }}

# Run e2e tests (builds for current platform automatically), output formatted by gotestdox
e2e:
    #!/usr/bin/env bash
    set -euo pipefail
    case "$(uname -s)" in
      Darwin)
        case "$(uname -m)" in
          arm64) ARCH="darwin-arm64" ;;
          *) ARCH="darwin-amd64" ;;
        esac
        ;;
      Linux) ARCH="linux" ;;
      *) echo "Unsupported platform" >&2; exit 1 ;;
    esac
    just build "$ARCH"
    # "just build linux" writes to linux-amd64; the darwin targets are named for their directories.
    BIN_DIR="$ARCH"
    if [ "$ARCH" = "linux" ]; then BIN_DIR="linux-amd64"; fi
    SSHUSH_E2E_BIN_DIR="$(pwd)/{{ build_dir }}/${BIN_DIR}" just _dox ./test/e2e/... -race

# Each test gives both sides a config, pipe and pidfile of their own, so the
# agent you are using is left alone. The checkout has to be on a Windows drive
# (/mnt/c/...): Windows runs the sshush.exe built there.
# Run the WSL interop e2e tests against a real sshush.exe (inside a WSL distro only)
e2e-wsl:
    #!/usr/bin/env bash
    set -euo pipefail
    if ! ls /proc/sys/fs/binfmt_misc/WSLInterop* >/dev/null 2>&1; then
      echo "just e2e-wsl runs inside a WSL distro, with Windows interop on" >&2
      exit 1
    fi
    case "$(pwd)" in
      /mnt/*) ;;
      *) echo "just e2e-wsl needs the checkout on a Windows drive (/mnt/c/...), so Windows can run the sshush.exe it builds" >&2; exit 1 ;;
    esac
    just build linux
    just build windows
    SSHUSH_E2E_WSL=1 \
    SSHUSH_E2E_BIN_DIR="$(pwd)/{{ build_dir }}/linux-amd64" \
    SSHUSH_E2E_WINDOWS_BIN_DIR="$(pwd)/{{ build_dir }}/windows-amd64" \
      just _dox ./test/e2e/... -run '^TestE2E_WSL' -count=1

# Run Go benchmarks (optional: package, count)
bench pkg="./..." count="1":
    go test {{ if pkg == "./..." { pkg } else { "./" + pkg } }} -bench=. -benchmem -count={{ count }} -run=^$

# Run benchmarks via Python script (nicer formatting, optional -w to save)
bench-report count="1":
    python3 scripts/bench.py -c {{ count }}

# Pipe `go test -json` through gotestdox, keeping go test's exit status and
# surfacing build errors that gotestdox would otherwise swallow silently.
[private]
_dox *args:
    #!/usr/bin/env bash
    set -uo pipefail
    json=$(mktemp)
    trap 'rm -f "$json"' EXIT
    go test -json {{ args }} | tee "$json" | go tool gotestdox
    status=${PIPESTATUS[0]}
    if [ "$status" -ne 0 ]; then
        grep '"Action":"build-output"' "$json" \
            | sed -e 's/^.*"Output":"//' -e 's/"}$//' -e 's/\\n$//' \
                  -e 's/\\t/\t/g' -e 's/\\"/"/g' >&2
    fi
    exit "$status"
