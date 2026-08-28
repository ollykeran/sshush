# Run tests with race detection (optional: specific package, --nocache to bypass Go's test cache)
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
    go test "$pkg" -v -race "${extra[@]+"${extra[@]}"}"

# Run tests with coverage report (saved to benchmarks/)
test-cover:
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p benchmarks
    go test ./... -race -coverprofile=benchmarks/coverage.out -covermode=atomic
    go tool cover -func=benchmarks/coverage.out
    echo ""
    echo "View HTML report: go tool cover -html=benchmarks/coverage.out"

# Generate the same markdown coverage report CI posts on PRs (needs a coverprofile)
cover-report profile="benchmarks/coverage.out":
    go run ./scripts/cover-report -profile {{ profile }}

# Run e2e tests (builds for current platform automatically)
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
    SSHUSH_E2E_BIN_DIR="$(pwd)/{{ build_dir }}/${ARCH}" go test ./test/e2e/... -v -race

# Run Go benchmarks (optional: package, count)
bench pkg="./..." count="1":
    go test {{ if pkg == "./..." { pkg } else { "./" + pkg } }} -bench=. -benchmem -count={{ count }} -run=^$

# Run benchmarks via Python script (nicer formatting, optional -w to save)
bench-report count="1":
    python3 scripts/bench.py -c {{ count }}
