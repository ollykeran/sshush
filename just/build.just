# Build binaries: `just build linux`, `just build darwin`, `just build all`
build arch="": deps
    #!/usr/bin/env bash
    set -euo pipefail
    build_local() {
      local os="$1" goarch="$2" out_dir="$3"
      mkdir -p "$out_dir"
      CGO_ENABLED=0 GOOS="$os" GOARCH="$goarch" go build -ldflags "-X github.com/ollykeran/sshush/internal/version.Version={{ version }}" -o "$out_dir/sshushd" ./cmd/sshushd
      CGO_ENABLED=0 GOOS="$os" GOARCH="$goarch" go build -ldflags "-X github.com/ollykeran/sshush/internal/version.Version={{ version }}" -o "$out_dir/sshush" ./cmd/sshush
    }
    case "{{ arch }}" in
      linux)
        echo "Building linux/amd64..."
        build_local linux amd64 "{{ build_dir }}/linux-amd64"
        ;;
      darwin|darwin-arm64)
        echo "Building darwin/arm64..."
        build_local darwin arm64 "{{ build_dir }}/darwin-arm64"
        ;;
      darwin-amd64)
        echo "Building darwin/amd64..."
        build_local darwin amd64 "{{ build_dir }}/darwin-amd64"
        ;;
      darwin-all)
        echo "Building darwin/arm64..."
        build_local darwin arm64 "{{ build_dir }}/darwin-arm64"
        echo "Building darwin/amd64..."
        build_local darwin amd64 "{{ build_dir }}/darwin-amd64"
        ;;
      all)
        echo "Building linux/amd64..."
        build_local linux amd64 "{{ build_dir }}/linux-amd64"
        echo "Building darwin/arm64..."
        build_local darwin arm64 "{{ build_dir }}/darwin-arm64"
        echo "Building darwin/amd64..."
        build_local darwin amd64 "{{ build_dir }}/darwin-amd64"
        ;;
      "")
        echo "Building linux/amd64 (default)..."
        build_local linux amd64 "{{ build_dir }}/linux-amd64"
        ;;
      *)
        echo "Unknown arch: {{ arch }}" >&2
        echo "Usage: just build [linux|darwin|darwin-arm64|darwin-amd64|darwin-all|all]" >&2
        exit 1
        ;;
    esac

# Package: `just pkg tarball`, `just pkg deb`, `just pkg all`
pkg target="": (build "linux")
    #!/usr/bin/env bash
    set -euo pipefail
    make_tarball() {
      local src_dir="$1" suffix="$2"
      tar czf "{{ build_dir }}/sshush-{{ version }}-${suffix}.tar.gz" -C "{{ build_dir }}/${src_dir}" sshush sshushd
    }
    case "{{ target }}" in
      tarball)
        echo "Creating Linux amd64 tarball..."
        make_tarball "linux-amd64" "linux-amd64"
        ;;
      tarball-darwin-arm64)
        just build darwin-arm64
        echo "Creating macOS arm64 tarball..."
        make_tarball "darwin-arm64" "darwin-arm64"
        ;;
      tarball-darwin-amd64)
        just build darwin-amd64
        echo "Creating macOS amd64 tarball..."
        make_tarball "darwin-amd64" "darwin-amd64"
        ;;
      tarball-all)
        just build darwin-all
        echo "Creating Linux amd64 tarball..."
        make_tarball "linux-amd64" "linux-amd64"
        echo "Creating macOS arm64 tarball..."
        make_tarball "darwin-arm64" "darwin-arm64"
        echo "Creating macOS amd64 tarball..."
        make_tarball "darwin-amd64" "darwin-amd64"
        ;;
      deb)
        echo "Building .deb package..."
        VERSION={{ version }} nfpm pkg --packager deb --target {{ build_dir }}/sshush-{{ version }}-amd64.deb
        ;;
      rpm)
        echo "Building .rpm package..."
        VERSION={{ version }} nfpm pkg --packager rpm --target {{ build_dir }}/sshush-{{ version }}-amd64.rpm
        ;;
      source)
        echo "Building source RPM..."
        VERSION={{ version }} nfpm pkg --packager srpm --target {{ build_dir }}/sshush-{{ version }}.src.rpm
        ;;
      archlinux|arch)
        echo "Building Arch Linux package..."
        VERSION={{ version }} nfpm pkg --packager archlinux --target {{ build_dir }}/sshush-{{ version }}-amd64.pkg.tar.zst
        ;;
      all)
        echo "Creating Linux amd64 tarball..."
        make_tarball "linux-amd64" "linux-amd64"
        just build darwin-all
        echo "Creating macOS arm64 tarball..."
        make_tarball "darwin-arm64" "darwin-arm64"
        echo "Creating macOS amd64 tarball..."
        make_tarball "darwin-amd64" "darwin-amd64"
        echo "Building .deb package..."
        VERSION={{ version }} nfpm pkg --packager deb --target {{ build_dir }}/sshush-{{ version }}-amd64.deb
        echo "Building .rpm package..."
        VERSION={{ version }} nfpm pkg --packager rpm --target {{ build_dir }}/sshush-{{ version }}-amd64.rpm
        echo "Building source RPM..."
        VERSION={{ version }} nfpm pkg --packager srpm --target {{ build_dir }}/sshush-{{ version }}.src.rpm
        echo "Building Arch Linux package..."
        VERSION={{ version }} nfpm pkg --packager archlinux --target {{ build_dir }}/sshush-{{ version }}-amd64.pkg.tar.zst
        ;;
      "")
        echo "Usage: just pkg [tarball|tarball-darwin-arm64|tarball-darwin-amd64|tarball-all|deb|rpm|source|archlinux|all]" >&2
        exit 1
        ;;
      *)
        echo "Unknown target: {{ target }}" >&2
        echo "Usage: just pkg [tarball|tarball-darwin-arm64|tarball-darwin-amd64|tarball-all|deb|rpm|source|archlinux|all]" >&2
        exit 1
        ;;
    esac

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
    check "$dir/linux-amd64/sshush"                     "ELF"
    check "$dir/linux-amd64/sshushd"                    "ELF"
    check "$dir/darwin-arm64/sshush"                    "Mach-O"
    check "$dir/darwin-arm64/sshushd"                   "Mach-O"
    check "$dir/darwin-amd64/sshush"                    "Mach-O"
    check "$dir/darwin-amd64/sshushd"                   "Mach-O"
    check "$dir/sshush-{{ ver }}-linux-amd64.tar.gz"   "gzip"
    check "$dir/sshush-{{ ver }}-darwin-arm64.tar.gz"  "gzip"
    check "$dir/sshush-{{ ver }}-darwin-amd64.tar.gz"  "gzip"
    check "$dir/sshush-{{ ver }}-amd64.deb"            "Debian"
    check "$dir/sshush-{{ ver }}-amd64.rpm"            "RPM"
    check "$dir/sshush-{{ ver }}.src.rpm"              "RPM"
    check "$dir/sshush-{{ ver }}-amd64.pkg.tar.zst"    "Zstandard"
    echo
    echo "$pass passed, $fail failed"
    [ "$fail" -eq 0 ]

# Generate SHA256 checksum files for all built binaries
checksums:
    #!/usr/bin/env bash
    set -euo pipefail
    dir="{{ build_dir }}"
    count=0
    for plat in linux-amd64 darwin-arm64 darwin-amd64; do
        for bin in sshush sshushd; do
            f="$dir/$plat/$bin"
            if [ -f "$f" ]; then
                sha=$(sha256sum "$f" | cut -d' ' -f1)
                echo "$sha" > "$dir/${bin}-${plat}.sha256"
                count=$((count + 1))
            fi
        done
    done
    echo "Generated $count .sha256 files in $dir"

# Remove all build artifacts
clean:
    rm -rf {{ build_dir }}
