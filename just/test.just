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
    SSHUSH_E2E_BIN_DIR="$(pwd)/{{ build_dir }}/${ARCH}" just _dox ./test/e2e/... -race

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
