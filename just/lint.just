# Run golangci-lint (auto-installs if missing)
lint:
    #!/usr/bin/env bash
    set -euo pipefail
    export PATH="$(go env GOPATH)/bin:$PATH"
    if ! command -v golangci-lint >/dev/null 2>&1; then
        echo "Installing golangci-lint..."
        go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
    fi
    golangci-lint run ./...

# Validate GitHub Actions workflows (installs actionlint if missing)
lint-yml:
    #!/usr/bin/env bash
    set -euo pipefail
    export PATH="$(go env GOPATH)/bin:$PATH"
    if ! command -v actionlint >/dev/null 2>&1; then
        echo "Installing actionlint..."
        go install github.com/rhysd/actionlint/cmd/actionlint@latest
    fi
    actionlint .github/workflows/*.yml

# Serve godoc at http://localhost:6060 (module-aware, use -http not -http=:6060)
doc:
    go doc -http

# Verify public API doc comments compile
doc-check:
    go build ./... && go doc -all ./internal/cli && go doc -all ./internal/tui && go doc -all ./internal/config
