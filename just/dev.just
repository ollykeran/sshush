# Install and tidy Go module dependencies
deps:
    go mod tidy
    go mod download

# Set up development environment (tools + dependencies)
setup:
    #!/usr/bin/env bash
    set -euo pipefail
    export PATH="$(go env GOPATH)/bin:$PATH"
    echo "Installing Go tools..."
    go install golang.org/x/tools/gopls@latest
    go install github.com/rogpeppe/godef@latest
    go install github.com/ramya-rao-a/go-outline@latest
    go install github.com/go-delve/delve/cmd/dlv@latest
    go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
    go install github.com/air-verse/air@latest
    echo "Installing actionlint..."
    if ! command -v actionlint >/dev/null 2>&1; then
        go install github.com/rhysd/actionlint/cmd/actionlint@latest
    fi
    echo "Installing dependencies..."
    just deps
    echo "Setup complete!"

# Upgrade all dependencies to latest versions
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

# Run the sshush CLI directly via go run
run:
    go run ./cmd/sshush

# Run with live reload (installs air if missing)
dev:
    #!/usr/bin/env bash
    set -euo pipefail
    if ! command -v air >/dev/null 2>&1; then
        echo "Installing air..."
        go install github.com/air-verse/air@latest
    fi
    air

# Kill any running sshush/sshushd processes
kill:
    #!/usr/bin/env bash
    pkill -f 'sshushd' || true
    pkill -f 'sshush' --older 2 || true
    ps -w | grep sshush | grep -v grep || true

# Build and launch the TUI
tui: kill (build "linux")
    {{ build_dir }}/linux-amd64/sshush tui
