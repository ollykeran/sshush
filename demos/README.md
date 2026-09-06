# Demos

VHS (`.tape`) scripts that record sshush's CLI, TUI, and a real SSH connection
authenticated through sshush's own agent + server.

```sh
just demo               # record all three
just demo cli-basics    # or one at a time: cli-basics | tui-basics | server-connect
```

Requires [vhs](https://github.com/charmbracelet/vhs) on `PATH`.

## How the fixture works

Nothing here is recorded against your real `~/.ssh` or `~/.config/sshush`.
`fixture.sh` creates a fresh temp directory each run with its own keys,
`config.toml`, and `ssh` client config, then prints `export HOME=...` /
`export XDG_CONFIG_HOME=...` for the recording shell to `eval`. sshush and
`ssh` both resolve everything from there, so the fixture never touches, and
isn't derived from, anything already on your machine — and there's nothing
generated to keep under source control.

`server-connect.tape` is the one that shows the actual point of an agent:
it starts `sshush server` (a small TCP SSH server built into sshush, see
[../docs/config.md](../docs/config.md#server)) backed by the same key the
agent is holding, then runs a real `ssh` against it — no external host
needed to demonstrate a real authenticated connection.

## Regenerating GIFs

Run `just demo` locally and commit the result, or trigger the `demos-render`
GitHub Actions workflow (`workflow_dispatch`), which does the same and opens
a PR. Not run automatically on every push: VHS's GIF output isn't
byte-for-byte deterministic across runs, so diffing it on every commit would
be flaky. `demos-smoke` covers that instead — it re-runs every tape on PRs
touching CLI/TUI/server code and fails if any of them error out, without
checking the rendered pixels.
