# Upgrade Go to the latest stable release and update project references
go-upgrade:
    #!/usr/bin/env bash
    set -euo pipefail

    # --- Fetch latest stable version from go.dev API ---
    echo "Fetching latest Go version..."
    json=$(curl -sS 'https://go.dev/dl/?mode=json')
    latest=$(echo "$json" | jq -r '.[0].version' | sed 's/^go//')
    if [[ -z "$latest" ]]; then
        echo "ERROR: Could not determine latest Go version" >&2
        exit 1
    fi
    echo "Latest version: go$latest"

    # --- Detect OS and arch ---
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$(uname -m)" in
        arm64|aarch64) goarch="arm64" ;;
        x86_64|amd64)  goarch="amd64" ;;
        *) echo "ERROR: Unsupported architecture $(uname -m)" >&2; exit 1 ;;
    esac

    archive="go${latest}.${os}-${goarch}.tar.gz"
    url="https://go.dev/dl/${archive}"
    echo "Downloading $url ..."

    # --- Download to temp dir ---
    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT
    curl -sS -L -o "$tmpdir/$archive" "$url"

    # --- Verify checksum ---
    echo "Verifying checksum..."
    expected=$(echo "$json" | jq -r ".[0].files[] | select(.os==\"$os\" and .arch==\"$goarch\" and .kind==\"archive\") | .sha256")
    actual=$(shasum -a 256 "$tmpdir/$archive" | cut -d' ' -f1)
    if [[ "$expected" != "$actual" ]]; then
        echo "ERROR: Checksum mismatch!" >&2
        echo "  expected: $expected" >&2
        echo "  actual:   $actual" >&2
        exit 1
    fi
    echo "Checksum OK"

    # --- Stop any running Go processes that might lock the install dir ---
    # (not typically needed on macOS/Linux but safe to do)

    # --- Extract (replaces /usr/local/go) ---
    install_dir="/usr/local/go"
    if [[ -d "$install_dir" ]]; then
        echo "Removing old installation at $install_dir ..."
        sudo rm -rf "$install_dir"
    fi
    echo "Installing to $install_dir ..."
    sudo tar -C /usr/local -xzf "$tmpdir/$archive"

    # --- Verify ---
    new_version=$("$install_dir/bin/go" version | awk '{print $3}' | sed 's/^go//')
    echo "Installed: go$new_version"

    # --- Update project references ---
    echo "Updating project references..."

    # go.mod: update "go X.Y" directive
    if [[ -f go.mod ]]; then
        minor=$(echo "$latest" | cut -d. -f1,2)
        if grep -q '^go [0-9]' go.mod; then
            sed -i.bak "s/^go [0-9][0-9]*\.[0-9][0-9]*$/go ${minor}/" go.mod
            rm -f go.mod.bak
            echo "  Updated go.mod -> go $minor"
        fi
    fi

    # .devcontainer/devcontainer.json: update golang image tag
    devcontainer=".devcontainer/devcontainer.json"
    if [[ -f "$devcontainer" ]]; then
        minor=$(echo "$latest" | cut -d. -f1,2)
        if grep -q '"image": "golang:' "$devcontainer"; then
            sed -i.bak "s|\"image\": \"golang:[0-9][0-9]*\.[0-9][0-9]*-|\"image\": \"golang:${minor}-|" "$devcontainer"
            rm -f "$devcontainer.bak"
            echo "  Updated $devcontainer -> golang:${minor}-bookworm"
        fi
    fi

    # --- Tidy module ---
    if [[ -f go.mod ]]; then
        echo "Running go mod tidy..."
        "$install_dir/bin/go" mod tidy
    fi

    echo ""
    echo "Done! Go $latest installed and project references updated."
    echo "Restart your shell or run: export PATH=$install_dir/bin:\$PATH"
