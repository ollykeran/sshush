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

test:
    go test ./... -v -race

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

package: tarball deb rpm source archlinux

tarball: build
    tar czf {{ build_dir }}/sshush-{{ version }}-linux-amd64.tar.gz -C {{ build_dir }} sshush sshushd

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
    check "$dir/sshush-{{ ver }}-amd64.deb"            "Debian"
    check "$dir/sshush-{{ ver }}-amd64.rpm"            "RPM"
    check "$dir/sshush-{{ ver }}.src.rpm"              "RPM"
    check "$dir/sshush-{{ ver }}-amd64.pkg.tar.zst"    "Zstandard"
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
