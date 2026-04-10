build_dir := "build"
binary := build_dir / "sshush"
binaryd := build_dir / "sshushd"
binary_gui := build_dir / "sshush-gui"
version := env("VERSION", "dev")
ldflags := "-X github.com/ollykeran/sshush/internal/version.Version=" + version

deps:
    go mod tidy
    go mod download

update:
    go get -u -t ./...
    go mod tidy
    go mod download

build-sshushd: deps
    mkdir -p {{ build_dir }}
    go build -ldflags '-X github.com/ollykeran/sshush/internal/version.Version={{ version }}' -o {{ binaryd }} ./cmd/sshushd

build: build-sshushd
    go build -ldflags '-X github.com/ollykeran/sshush/internal/version.Version={{ version }}' -o {{ binary }} ./cmd/sshush

# Quick check before a long Fyne compile; see docs/gui.md for install commands.
check-gui-deps:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ "$(uname -s)" != Linux ]]; then
        echo 'check-gui-deps: skipped (X11/Mesa pkg-config checks are Linux-only)' >&2
        exit 0
    fi
    if ! command -v pkg-config >/dev/null 2>&1; then
        echo 'GUI build: pkg-config not found in PATH.' >&2
        echo '  Install: sudo apt install pkg-config   (Debian/Ubuntu)' >&2
        echo '  Or see docs/gui.md' >&2
        exit 1
    fi
    if ! pkg-config --exists x11 2>/dev/null; then
        echo 'GUI build: pkg-config cannot find x11 (X11 development files).' >&2
        echo '  Install: sudo apt install libx11-dev   (Debian/Ubuntu)' >&2
        echo '  Or see docs/gui.md' >&2
        exit 1
    fi
    if ! pkg-config --exists gl 2>/dev/null; then
        echo 'GUI build: pkg-config cannot find gl (OpenGL/Mesa development files).' >&2
        echo '  Install: sudo apt install libgl1-mesa-dev   (Debian/Ubuntu)' >&2
        echo '  Or see docs/gui.md' >&2
        exit 1
    fi

# Fyne desktop PoC (Linux + CGO + X11/Wayland dev libs). See docs/gui.md. Requires -tags=gui.
build-gui: deps build-sshushd
    #check-gui-deps
    mkdir -p {{ build_dir }}
    go build -tags=gui -ldflags '-X github.com/ollykeran/sshush/internal/version.Version={{ version }}' -o {{ binary_gui }} ./cmd/sshush-gui

run-gui:
    go run -tags=gui ./cmd/sshush-gui

# Compiles internal/gui with Fyne (same prerequisites as build-gui).
test-gui:
    go test -tags=gui ./internal/gui/... -count=1

test pkg="./...":
    go test {{ if pkg == "./..." { pkg } else { "./" + pkg } }} -v -race

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

# Serve godoc at http://localhost:6060 (module-aware, use -http not -http=:6060)
doc:
    go doc -http

# Default build omits Fyne (use -tags=gui for GUI); same as CI.
doc-check:
    #!/usr/bin/env bash
    set -euo pipefail
    LDF='-X github.com/ollykeran/sshush/internal/version.Version={{ version }}'
    go build -ldflags "$LDF" -o /dev/null ./...
    go doc -all ./internal/cli && go doc -all ./internal/tui && go doc -all ./internal/config

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

# Create a GitHub release (creates tag, triggers CI to build packages)
release ver:
    gh release create v{{ ver }} --generate-notes --latest

# List jobs in the release workflow (via act)
act-list:
    act -l push -e .github/events/push-tag.json -W .github/workflows/release.yml

# Run the release workflow locally (via act); Release step is skipped
act-release ver:
    #!/usr/bin/env bash
    echo '{"ref": "refs/tags/v{{ ver }}"}' > .github/events/push-tag.json
    act push -e .github/events/push-tag.json -W .github/workflows/release.yml

# Dry-run the release workflow locally (via act)
act-release-dry ver:
    #!/usr/bin/env bash
    echo '{"ref": "refs/tags/v{{ ver }}"}' > .github/events/push-tag.json
    act -n push -e .github/events/push-tag.json -W .github/workflows/release.yml
