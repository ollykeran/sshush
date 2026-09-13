# Record VHS demo GIFs into demos/, using a throwaway HOME/config fixture
# (see demos/fixture.sh) so real ~/.ssh and ~/.config/sshush are never touched.
demo tape="all": (build "dev")
    #!/usr/bin/env bash
    set -euo pipefail
    if ! command -v vhs >/dev/null 2>&1; then
        echo "vhs not found: https://github.com/charmbracelet/vhs#installation" >&2
        exit 1
    fi
    host_os="$(go env GOOS)"
    host_arch="$(go env GOARCH)"
    export PATH="$(pwd)/{{ build_dir }}/${host_os}-${host_arch}:$PATH"
    record() {
        # Capture before eval: eval "$(demos/fixture.sh)" takes eval's status, so a
        # fixture that failed would go unnoticed and vhs would record without it.
        local fixture
        fixture="$(demos/fixture.sh)"
        eval "$fixture"
        vhs "demos/$1.tape"
    }
    case "{{ tape }}" in
      all)
        record cli-basics
        record tui-basics
        record server-connect
        ;;
      cli-basics|tui-basics|server-connect)
        record "{{ tape }}"
        ;;
      *)
        echo "Unknown tape: {{ tape }}" >&2
        echo "Usage: just demo [cli-basics|tui-basics|server-connect|all]" >&2
        exit 1
        ;;
    esac
