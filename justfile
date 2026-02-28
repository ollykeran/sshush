build_dir := "build"
binary := build_dir / "sshush"
binaryd := build_dir / "sshushd"
ldflags := "-X github.com/ollykeran/sshush/internal/version.Version=dev"
version := env("VERSION", "dev")

deps:
    go mod tidy
    go mod download

build-sshushd: deps
    mkdir -p {{ build_dir }}
    go build -ldflags '{{ ldflags }}' -o {{ binaryd }} ./cmd/sshushd

build: build-sshushd
    go build -ldflags '{{ ldflags }}' -o {{ binary }} .

test:
    go test ./... -v

run:
    go run .

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
    VERSION={{ version }} nfpm pkg --packager srpm --target {{ build_dir }}/sshush-{{ version }}.tar.gz

archlinux: build
    VERSION={{ version }} nfpm pkg --packager archlinux --target {{ build_dir }}/sshush-{{ version }}-amd64.pkg.tar.zst

# Create a GitHub release (creates tag, triggers CI to build packages)
release ver:
    gh release create v{{ ver }} --generate-notes --latest

# List jobs in the release workflow (via act)
act-list:
    act -l push -e .github/events/push-tag.json -W .github/workflows/release.yml

# Run the release workflow locally (via act); Release step is skipped
act-release:
    act push -e .github/events/push-tag.json -W .github/workflows/release.yml

# Dry-run the release workflow locally (via act)
act-release-dry:
    act -n push -e .github/events/push-tag.json -W .github/workflows/release.yml
