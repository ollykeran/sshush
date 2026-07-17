build_dir := "build"
binary := build_dir / "sshush"
binaryd := build_dir / "sshushd"
version := env("VERSION", "dev")
ldflags := "-X github.com/ollykeran/sshush/internal/version.Version=" + version

deps:
    go mod tidy
    go mod download

update:
    go get -u -t ./...
    go mod tidy
    go mod download

# Bump only modules with known vulns (govulncheck). Unlike `update`, does not upgrade unrelated deps.
update-security:
    #!/usr/bin/env bash
    set -euo pipefail
    export PATH="$(go env GOPATH)/bin:$PATH"
    if ! command -v govulncheck >/dev/null 2>&1; then
        go install golang.org/x/vuln/cmd/govulncheck@latest
    fi
    bumped=0
    for _ in $(seq 1 20); do
        tmp="$(mktemp)"
        status=0
        # govulncheck exits 3 when vulns found; still emit JSON for fixes
        govulncheck -json ./... >"$tmp" || status=$?
        if [[ "$status" -ne 0 && "$status" -ne 3 ]]; then
            rm -f "$tmp"
            exit "$status"
        fi
        modules=()
        while IFS= read -r line; do
            [[ -n "$line" ]] && modules+=("$line")
        done < <(python3 scripts/govulncheck-bumps.py "$tmp")
        rm -f "$tmp"
        if [[ ${#modules[@]} -eq 0 ]]; then
            if [[ "$bumped" -eq 0 ]]; then
                echo "No vulnerable third-party modules to bump."
            else
                echo "Security bumps complete ($bumped)."
            fi
            go mod tidy
            go mod download
            exit 0
        fi
        # One module per round so MVS can raise transitive floors (e.g. crypto
        # pulling a newer x/sys) without a later lower floor downgrading it.
        spec="${modules[0]}"
        echo "Security bump: $spec"
        go get "$spec"
        bumped=$((bumped + 1))
    done
    echo "update-security: still vulnerable after $bumped bumps; re-run or inspect govulncheck" >&2
    exit 1

build-sshushd: deps
    mkdir -p {{ build_dir }}
    go build -ldflags '-X github.com/ollykeran/sshush/internal/version.Version={{ version }}' -o {{ binaryd }} ./cmd/sshushd

build: build-sshushd
    go build -ldflags '-X github.com/ollykeran/sshush/internal/version.Version={{ version }}' -o {{ binary }} ./cmd/sshush

test pkg="./...":
    go test {{ if pkg == "./..." { pkg } else { "./" + pkg } }} -v -race

bench pkg="./..." count="1":
    go test {{ if pkg == "./..." { pkg } else { "./" + pkg } }} -bench=. -benchmem -count={{ count }} -run=^$

# Run benchmarks via Python script (nicer formatting, optional -w to save)
bench-report count="1":
    python3 scripts/bench.py -c {{ count }}

lint:
    #!/usr/bin/env bash
    set -euo pipefail
    export PATH="$(go env GOPATH)/bin:$PATH"
    if ! command -v golangci-lint >/dev/null 2>&1; then
        echo "Installing golangci-lint..."
        go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
    fi
    golangci-lint run ./...

# Cross-compile macOS to build/darwin-<goarch>/ (goarch: arm64 | amd64). Used by tarballs and build-mac.
build-darwin goarch: deps
    mkdir -p {{ build_dir }}/darwin-{{ goarch }}
    CGO_ENABLED=0 GOOS=darwin GOARCH={{ goarch }} go build -ldflags '-X github.com/ollykeran/sshush/internal/version.Version={{ version }}' -o {{ build_dir }}/darwin-{{ goarch }}/sshushd ./cmd/sshushd
    CGO_ENABLED=0 GOOS=darwin GOARCH={{ goarch }} go build -ldflags '-X github.com/ollykeran/sshush/internal/version.Version={{ version }}' -o {{ build_dir }}/darwin-{{ goarch }}/sshush ./cmd/sshush

# Copies build/darwin-<arch>/ into build/sshush(d). On Darwin, arch matches the machine. Else cross-compile; set MAC_ARCH=amd64 for Intel Mac from Linux.
build-mac:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ "$(uname -s)" == Darwin ]]; then
      case "$(uname -m)" in
        arm64) goarch=arm64 ;;
        x86_64) goarch=amd64 ;;
        *) echo "unsupported Mac architecture: $(uname -m)" >&2; exit 1 ;;
      esac
    else
      goarch="${MAC_ARCH:-arm64}"
    fi
    just build-darwin "$goarch"
    mkdir -p "{{ build_dir }}"
    cp "{{ build_dir }}/darwin-$goarch/sshush" "{{ binary }}"
    cp "{{ build_dir }}/darwin-$goarch/sshushd" "{{ binaryd }}"

# Cross-compile for all supported platforms (catches platform-specific issues)
build-all: deps
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Building for linux/amd64..."
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/sshush
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/sshushd
    echo "Building for linux/arm64..."
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/sshush
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/sshushd
    echo "Building for darwin/arm64..."
    CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/sshush
    CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/sshushd
    echo "Building for darwin/amd64..."
    CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o /dev/null ./cmd/sshush
    CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o /dev/null ./cmd/sshushd
    echo "All platforms build successfully."

# Serve godoc at http://localhost:6060 (module-aware, use -http not -http=:6060)
doc:
    go doc -http

doc-check:
    go build ./... && go doc -all ./internal/cli && go doc -all ./internal/tui && go doc -all ./internal/config

run:
    go run ./cmd/sshush

clean:
    rm -rf {{ build_dir }}

[no-exit-message]
kill:
    #!/usr/bin/env bash
    pkill -f 'sshushd' || true
    pkill -f 'sshush' --older 2 || true
    ps -w | grep sshush | grep -v grep || true

tui: kill build
    {{ binary }} tui

package: tarball tarball-darwin-arm64 tarball-darwin-amd64 deb rpm source archlinux

tarball: build
    tar czf {{ build_dir }}/sshush-{{ version }}-linux-amd64.tar.gz -C {{ build_dir }} sshush sshushd

tarball-darwin-arm64: (build-darwin "arm64")
    tar czf {{ build_dir }}/sshush-{{ version }}-darwin-arm64.tar.gz -C {{ build_dir }}/darwin-arm64 sshush sshushd

tarball-darwin-amd64: (build-darwin "amd64")
    tar czf {{ build_dir }}/sshush-{{ version }}-darwin-amd64.tar.gz -C {{ build_dir }}/darwin-amd64 sshush sshushd

deb: build
    VERSION={{ version }} nfpm pkg --packager deb --target {{ build_dir }}/sshush-{{ version }}-amd64.deb

rpm: build
    VERSION={{ version }} nfpm pkg --packager rpm --target {{ build_dir }}/sshush-{{ version }}-amd64.rpm

source: build
    VERSION={{ version }} nfpm pkg --packager srpm --target {{ build_dir }}/sshush-{{ version }}.src.rpm

archlinux: build
    VERSION={{ version }} nfpm pkg --packager archlinux --target {{ build_dir }}/sshush-{{ version }}-amd64.pkg.tar.zst

# Validate build artifacts exist and have correct format
check-artifacts ver="dev":
    #!/usr/bin/env bash
    set -euo pipefail
    dir="{{ build_dir }}"
    pass=0; fail=0
    check() {
        local file="$1" expected_type="$2"
        if [ ! -f "$file" ]; then
            echo "MISS  $file"
            fail=$((fail + 1)); return
        fi
        actual=$(file -b "$file")
        if echo "$actual" | grep -qi "$expected_type"; then
            echo "OK    $file  ($actual)"
            pass=$((pass + 1))
        else
            echo "FAIL  $file  (expected $expected_type, got $actual)"
            fail=$((fail + 1))
        fi
    }
    echo "Checking artifacts for version {{ ver }}..."
    echo
    check "$dir/sshush"                                "ELF"
    check "$dir/sshushd"                               "ELF"
    check "$dir/sshush-{{ ver }}-linux-amd64.tar.gz"   "gzip"
    check "$dir/sshush-{{ ver }}-darwin-arm64.tar.gz"  "gzip"
    check "$dir/sshush-{{ ver }}-darwin-amd64.tar.gz"  "gzip"
    check "$dir/sshush-{{ ver }}-amd64.deb"            "Debian"
    check "$dir/sshush-{{ ver }}-amd64.rpm"            "RPM"
    check "$dir/sshush-{{ ver }}.src.rpm"              "RPM"
    check "$dir/sshush-{{ ver }}-amd64.pkg.tar.zst"    "Zstandard"
    echo
    echo "Darwin tarball contents (sshush binary):"
    for pair in "sshush-{{ ver }}-darwin-arm64.tar.gz:arm64" "sshush-{{ ver }}-darwin-amd64.tar.gz:x86_64"; do
      tzf="${pair%%:*}"
      want="${pair##*:}"
      tmp=$(mktemp)
      if ! tar xOzf "$dir/$tzf" sshush >"$tmp" 2>/dev/null; then
        echo "FAIL  $tzf  (could not extract sshush from tarball)"
        rm -f "$tmp"
        fail=$((fail + 1))
        continue
      fi
      inner=$(file -b "$tmp" 2>/dev/null || true)
      rm -f "$tmp"
      if echo "$inner" | grep -qi "Mach-O.*$want"; then
        echo "OK    $tzf  ($inner)"
        pass=$((pass + 1))
      else
        echo "FAIL  $tzf  (expected Mach-O $want, got $inner)"
        fail=$((fail + 1))
      fi
    done
    echo
    echo "$pass passed, $fail failed"
    [ "$fail" -eq 0 ]

# Create a GitHub release (tag at branch tip if missing; triggers CI on tag push)
release ver branch:
    gh release create "v{{ ver }}" --generate-notes --latest --target "{{ branch }}"

# Run workflows locally via act: `just act ci`, `just act release v0.0.9 master`, `just act all`
act cmd ver="" branch="":
    #!/usr/bin/env bash
    set -euo pipefail
    export PATH="/opt/homebrew/bin:$PATH"
    case "{{ cmd }}" in
      ci)
        echo "=== CI ==="
        act push -W .github/workflows/ci.yml
        ;;
      release)
        echo "=== Release ==="
        if [[ -z "{{ ver }}" || -z "{{ branch }}" ]]; then
          echo "Usage: just act release <version> <branch>" >&2
          exit 1
        fi
        ver="{{ ver }}"; br="{{ branch }}"
        if git show-ref --verify --quiet "refs/heads/$br"; then
          sha=$(git rev-parse "refs/heads/$br")
        elif git show-ref --verify --quiet "refs/remotes/origin/$br"; then
          sha=$(git rev-parse "refs/remotes/origin/$br")
        else
          echo "act: unknown branch '$br'" >&2
          exit 1
        fi
        tmp_event=$(mktemp)
        printf '{"ref":"refs/tags/v%s","before":"0000000000000000000000000000000000000000","after":"%s"}\n' "$ver" "$sha" > "$tmp_event"
        act push -e "$tmp_event" -W .github/workflows/release.yml --defaultbranch "$br"
        rm -f "$tmp_event"
        ;;
      *)
        echo "Unknown command: {{ cmd }}" >&2
        echo "Usage: just act <ci|release|all>" >&2
        exit 1
        ;;
    esac
