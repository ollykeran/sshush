# Architecture

High-level package layout and data flow. For detailed TUI architecture, see [TUI](tui.md). For config flow, see [Config](config.md).

## Layout

- **cmd/sshush** – CLI entry point
- **cmd/sshushd** – Daemon entry point (runs the agent)
- **internal/agent** – SSH agent protocol: serving it over a Unix socket, and `Session`, the single client entry point for reaching a running agent
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

## Talking to a running agent

Every client-side conversation with a running agent goes through `agent.Session` (`internal/agent/session.go`). `agent.Open(socketPath)` dials the socket and returns a Session that owns the connection; `Close` releases it. Three rules keep this seam intact:

**One Session per unit of work.** A CLI command opens one Session and carries all of its work over it; a TUI `tea.Cmd` opens one and closes it before returning. Nothing else dials the agent socket. This matters because the interface used to be one function per call, each dialling its own connection: `sshush edit` cost around thirteen connections, and the TUI's two-second vault poll cost two every tick. `Session.Backend` is the reason most of that collapsed — it reports the backend mode *and* whether a vault is locked in one round-trip, where the old probe discarded the response body and forced callers to ask twice.

**`internal/agent` owns the transport; `internal/vault` owns the extension vocabulary.** `Session.Extension` takes an extension name as given. The named wrappers — `vault.AddPrivateKeyFile`, `SetAutoload`, `SetComment`, `SessionLoad`, `SessionUnload`, `UnlockWithRecoveryPhrase` — live in `internal/vault/session_ops.go`, next to the payload builders they use.

**`internal/agent` must not import `internal/vault`.** The dependency runs the other way. That is why `extensionVaultLocked` is duplicated at `internal/agent/backend_kind.go` with a comment saying it must match `vault.ExtensionVaultLocked`, and why `Session.Backend` is the only extension `internal/agent` names for itself.

### Why a failure knows its own reason

Server-side error text never crosses the wire. `ServeAgent` answers a failed `Extension` call with a bare `SSH_AGENT_EXTENSION_FAILURE` byte and **discards the response body**, so the client can only synthesise `"agent: generic extension failure"`; a failed `Lock` or `Unlock` becomes `"agent: failure"`. Every distinct cause — locked vault, unknown identity, wrong passphrase, a vault created with `--no-recovery` — arrived as one string, and callers guessed at the reason by matching it.

Because a returned error discards the body, the reason cannot ride on the failure path. The `sshush-op` extension (`internal/agent/op.go`) therefore answers a *failed* request with protocol-level **success**, carrying a status byte in the body. `Session.Op` decodes that into a sentinel — `agent.ErrVaultLocked`, `ErrIdentityNotFound`, `ErrNoRecovery`, `ErrWrongPassphrase`, `ErrNotLocked` — which callers match with `errors.Is`.

One extension wraps every operation rather than nine extensions each growing their own status format. Requests are `[version][op][payload]`, responses `[version][status][data]`.

**The wrapper exists for compatibility.** Had the existing extensions started answering success on failure, an older `sshush` against a newer `sshushd` would read a failure as a success — silently, and for an operation like `vault load` that matters. Instead the legacy extensions are untouched, and:

| | |
|---|---|
| new `sshush`, older `sshushd` | `sshush-op` is unsupported, `Session.Op` returns `ErrOpUnsupported`, the caller falls back to the legacy extension |
| older `sshush`, new `sshushd` | the legacy extensions still answer exactly as before |
| new against new | typed reasons |

`ErrOpUnknown` is deliberately distinct from `ErrOpUnsupported`: an agent that speaks the extension but not one particular op must not send the caller down the legacy fallback path.

A handful of `err.Error() == "agent: generic extension failure"` comparisons survive in `internal/cli` and `internal/tui`, each as the last case after the typed ones. They are not oversights: against an older daemon there is no reason byte, and the opaque string is all there is. Session's other pass-through methods still return the client's error unmodified, so those comparisons keep working.

Agent state — a vault's master key and session-load sets, a keyring's lock state — lives in the agent process and is shared by every connection it serves (`internal/agent/server.go` hands the same keyring to every `ServeAgent`). How many Sessions a caller opens therefore has no bearing on what the agent reports.

Two things deliberately do not use a Session. `internal/sshushd`'s liveness probes (`CheckAlreadyRunning`, `WaitForSocket`) dial and close without speaking the protocol, so a Session would spawn a client and a goroutine to learn nothing. And `RunServerOnly` holds one long-lived agent client for the life of the SSH server daemon, handing it to `server.AgentAuth`, which is typed against `sshagent.Agent`.

## Daemon startup

`sshush start` (and `sshush server`) fork `sshushd` as a detached background process (`internal/sshushd.StartDaemon` / `StartServerDaemon` in `control.go`) and need to know once it's actually up before returning. Printing the `export SSH_AUTH_SOCK=...` line, or the server's listen confirmation, only makes sense once the socket or port is really accepting connections.

That readiness check is a synchronous handshake, not a poll loop:

1. The parent opens a pipe (`internal/readypipe.New`) and passes the write end to the child as an inherited file descriptor (`exec.Cmd.ExtraFiles`), telling it which fd number to use via the `SSHUSH_READY_FD` environment variable.
2. The child (`cmd/sshushd/main.go`) picks the pipe up with `readypipe.FromEnv()`. Every failure path (config load, an already-running check, vault/key errors, a listen failure) calls `ready.Fail(err)` with the real error text before exiting.
3. On success, the child signals readiness with a bare close of the pipe, fired at the exact point the listener comes up: inside `agent.ListenAndServe` (via the `WithReady` option) for the agent socket, and inside `server.Server.ListenAndServe` (via the `Ready` field) for the TCP server.
4. The parent blocks on `Parent.Wait`, reading until EOF. No data read means success; data read is the child's real error text, returned verbatim; a 5-second deadline is the fallback for a child that never signals either way. It's a dead man's switch, not the primary mechanism.

Because the child's write end is the only thing keeping the pipe open, an unclean child death (crash, `SIGKILL`) closes it via the kernel automatically, so the parent still unblocks with an EOF-based failure without either side needing extra bookkeeping.

`sshushd` launched by hand (outside `sshush start`/`sshush server`) has no `SSHUSH_READY_FD` set, so `readypipe.FromEnv()` returns `nil` and every `Child` method becomes a no-op. The daemon starts the same way either way.
