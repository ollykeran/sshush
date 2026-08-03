# Create a GitHub release (tag at branch tip if missing; triggers CI on tag push)
release ver branch:
    gh release create "v{{ ver }}" --generate-notes --latest --target "{{ branch }}"

# Run workflows locally via act: `just act ci`, `just act pr`, `just act all`, `just act release v0.0.9 master`
act cmd ver="" branch="":
    #!/usr/bin/env bash
    set -euo pipefail
    export PATH="/opt/homebrew/bin:$PATH"
    run_act() {
      local event="$1" file="$2" extra="${3:-}"
      act "$event" --rm -W ".github/workflows/$file" --artifact-server-path .act-artifacts $extra
    }
    run_all() {
      local event="$1"
      for f in lint.yml test.yml build.yml; do
        echo "=== CI ($event): $f ==="
        run_act "$event" "$f"
      done
    }
    case "{{ cmd }}" in
      ci)
        run_all push
        ;;
      pr)
        run_all pull_request
        ;;
      all)
        run_all push
        run_all pull_request
        ;;
      lint)
        echo "=== Lint ==="
        run_act push lint.yml -j lint
        ;;
      lint-yml)
        echo "=== Lint YAML ==="
        run_act push lint.yml -j lint-yml
        ;;
      test)
        echo "=== Test ==="
        run_act push test.yml -j test
        ;;
      build)
        echo "=== Build ==="
        run_act push build.yml -j build
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
        echo "Usage: just act <ci|pr|all|lint|lint-yml|test|build|release>" >&2
        exit 1
        ;;
    esac

# Run the given event against a fresh checkout of HEAD (no untracked/gitignored local files),
# closest to a GitHub runner. e.g. `just act-clean push`, `just act-clean pull_request`
act-clean event="push":
    #!/usr/bin/env bash
    set -euo pipefail
    export PATH="/opt/homebrew/bin:$PATH"
    tmp=$(mktemp -d)
    git worktree add --detach "$tmp" HEAD >/dev/null
    trap 'git worktree remove --force "$tmp"; rm -rf "$tmp"' EXIT
    cp .actrc "$tmp/.actrc"
    for f in lint.yml test.yml build.yml; do
      echo "Running act {{ event }} on fresh checkout: $tmp ($f)"
      cd "$tmp"
      act {{ event }} --rm -W ".github/workflows/$f" --artifact-server-path "$tmp/.act-artifacts"
    done
