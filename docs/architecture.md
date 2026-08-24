# Architecture

High-level package layout and data flow. For detailed TUI architecture, see [TUI](tui.md). For config flow, see [Config](config.md).

## Layout

- **cmd/sshush** – CLI entry point
- **cmd/sshushd** – Daemon entry point (runs the agent)
- **internal/agent** – SSH agent logic, socket ops, key list/add/remove
- **internal/cli** – Cobra commands (start, stop, list, add, remove, reload, create, edit, export, find, tui, completion)
- **internal/config** – Config load, default creation, shell rc setup
- **internal/platform** – Portable defaults for config dir, socket/pid paths, shell rc selection
- **internal/keys** – Key generation, load, save, comment edit, format
- **internal/openssh** – OpenSSH key parsing
- **internal/readypipe** – Parent/child readiness handshake used when forking `sshushd`
- **internal/runtime** – Config/socket path resolution
- **internal/sshushd** – Daemon start/stop/reload control
- **internal/style** – Styled terminal output
- **internal/tui** – Bubble Tea TUI (Agent, Create, Edit, Export screens)
- **internal/utils** – Path expansion, helpers
- **internal/version** – Version string

CLI loads config and starts the daemon; daemon runs the agent on a Unix socket. OpenSSH (`ssh`, `ssh-add`) connect via `SSH_AUTH_SOCK`.

## Daemon startup

`sshush start` (and `sshush server`) fork `sshushd` as a detached background process (`internal/sshushd.StartDaemon` / `StartServerDaemon` in `control.go`) and need to know once it's actually up before returning. Printing the `export SSH_AUTH_SOCK=...` line, or the server's listen confirmation, only makes sense once the socket or port is really accepting connections.

That readiness check is a synchronous handshake, not a poll loop:

1. The parent opens a pipe (`internal/readypipe.New`) and passes the write end to the child as an inherited file descriptor (`exec.Cmd.ExtraFiles`), telling it which fd number to use via the `SSHUSH_READY_FD` environment variable.
2. The child (`cmd/sshushd/main.go`) picks the pipe up with `readypipe.FromEnv()`. Every failure path (config load, an already-running check, vault/key errors, a listen failure) calls `ready.Fail(err)` with the real error text before exiting.
3. On success, the child signals readiness with a bare close of the pipe, fired at the exact point the listener comes up: inside `agent.ListenAndServe` (via the `WithReady` option) for the agent socket, and inside `server.Server.ListenAndServe` (via the `Ready` field) for the TCP server.
4. The parent blocks on `Parent.Wait`, reading until EOF. No data read means success; data read is the child's real error text, returned verbatim; a 5-second deadline is the fallback for a child that never signals either way. It's a dead man's switch, not the primary mechanism.

Because the child's write end is the only thing keeping the pipe open, an unclean child death (crash, `SIGKILL`) closes it via the kernel automatically, so the parent still unblocks with an EOF-based failure without either side needing extra bookkeeping.

`sshushd` launched by hand (outside `sshush start`/`sshush server`) has no `SSHUSH_READY_FD` set, so `readypipe.FromEnv()` returns `nil` and every `Child` method becomes a no-op. The daemon starts the same way either way.
