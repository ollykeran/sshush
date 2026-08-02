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
        act push --rm -W .github/workflows/ci.yml
        ;;
      lint)
        echo "=== Lint ==="
        act push --rm -W .github/workflows/ci.yml -j lint
        ;;
      lint-yml)
        echo "=== Lint YAML ==="
        act push --rm -W .github/workflows/ci.yml -j lint-yml
        ;;
      test)
        echo "=== Test ==="
        act push --rm -W .github/workflows/ci.yml -j test
        ;;
      build)
        echo "=== Build ==="
        act push --rm -W .github/workflows/ci.yml -j build
        ;;
      all)
        echo "=== All CI jobs ==="
        act push --rm -W .github/workflows/ci.yml -j lint -j lint-yml -j test -j build
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
        echo "Usage: just act <ci|release|lint|lint-yml|test|build|all>" >&2
        exit 1
        ;;
    esac
