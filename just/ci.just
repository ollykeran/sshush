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
