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
- **internal/server** – The TCP SSH server: public-key auth against a file or the agent, and the pty shell each session gets
- **internal/sshushd** – Daemon start/stop/reload control
- **internal/style** – Styled terminal output
- **internal/tui** – Bubble Tea TUI (Agent, Create, Edit, Export screens)
- **internal/utils** – Path expansion, helpers
- **internal/vault** – The encrypted vault: on-disk store, `VaultAgent`, and the `sshush-op` vocabulary
- **internal/vaultops** – The vault operations the CLI and TUI both offer, implemented once
- **internal/version** – Version string

CLI loads config and starts the daemon; daemon runs the agent on a Unix socket. OpenSSH (`ssh`, `ssh-add`) connect via `SSH_AUTH_SOCK`.

## Talking to a running agent

Every client-side conversation with a running agent goes through `agent.Session` (`internal/agent/session.go`). `agent.Open(socketPath)` dials the socket and returns a Session that owns the connection; `Close` releases it. Three rules keep this seam intact:

**One Session per unit of work.** A CLI command opens one Session and carries all of its work over it; a TUI `tea.Cmd` opens one and closes it before returning. Nothing else dials the agent socket. This matters because the interface used to be one function per call, each dialling its own connection: `sshush edit` cost around thirteen connections, and the TUI's two-second vault poll cost two every tick. `Session.Backend` is the reason most of that collapsed — it reports the backend mode *and* whether a vault is locked in one round-trip, where the old probe discarded the response body and forced callers to ask twice.

**`internal/agent` owns the transport; `internal/vault` owns the extension vocabulary.** `Session.Extension` takes an extension name as given. The named wrappers — `vault.AddPrivateKeyFile`, `SetAutoload`, `SetComment`, `SessionLoad`, `SessionUnload`, `UnlockWithRecoveryPhrase` — live in `internal/vault/session_ops.go`, next to the payload builders they use.

**`internal/agent` must not import `internal/vault`.** The dependency runs the other way. That is why `extensionVaultLocked` is duplicated at `internal/agent/backend_kind.go` with a comment saying it must match `vault.ExtensionVaultLocked`, and why `Session.Backend` is the only extension `internal/agent` names for itself.

**`internal/vaultops` owns the vault operations; the CLI and TUI only render them.** Init, list, add, remove, session-load, autoload, lock and unlock each existed twice — as a cobra `RunE` and as a `tea.Cmd` — and each copy re-derived the same preamble and invented its own wording for the same failure. They now live once in `internal/vaultops`, which takes an `Env` (vault path, socket, and whether the front end can prompt) and returns a typed result. Two properties are worth keeping:

- A verb opens **one** Session however many selectors it is given, which is why the verbs take a slice rather than making callers loop. `RequireVaultAgent` is the one deliberate exception: `sshush vault unlock-recovery` uses it to check the gate, closes that Session, and dials again after the user has typed 24 words, because holding a socket open across an interactive prompt is worse than dialling twice.
- `Env.AskPassphrase` is nil for a front end that cannot block for input. The CLI passes `readPassphrase` and gets the transparent unlock it always had; the TUI passes nothing, because a `tea.Cmd` cannot prompt, and drives its own passphrase modal instead. `Env.AgentVaultPath` keeps the guard that goes with it — a passphrase is only ever asked for on an agent serving the very vault being operated on.

Failures come back as `*vaultops.OpError`: a `Code` to branch on, one sentence in `Msg`, an optional remedy in `Hint`, and the original cause still reachable with `errors.Is`. The CLI renders `Msg` as its error line and `Hint` as the line beneath; the TUI joins them into its status line.

### Why a failure knows its own reason

Server-side error text never crosses the wire. `ServeAgent` answers a failed `Extension` call with a bare `SSH_AGENT_EXTENSION_FAILURE` byte and **discards the response body**, so the client can only synthesise `"agent: generic extension failure"`; a failed `Lock` or `Unlock` becomes `"agent: failure"`. Every distinct cause — locked vault, unknown identity, wrong passphrase, a vault created with `--no-recovery` — arrived as one string, and callers guessed at the reason by matching it.

Because a returned error discards the body, the reason cannot ride on the failure path. The `sshush-op` extension (`internal/agent/op.go`) therefore answers a *failed* request with protocol-level **success**, carrying a status byte in the body. `Session.Op` decodes that into a sentinel — `agent.ErrVaultLocked`, `ErrIdentityNotFound`, `ErrNoRecovery`, `ErrWrongPassphrase`, `ErrNotLocked` — which callers match with `errors.Is`.

**`sshush-op` is the only way in.** One extension carries every operation: requests are `[version][op][payload]`, responses `[version][status][data]`. `VaultAgent` and `KDFLockedKeyring` each serve the ops that make sense for them, and a keyring reports the vault ops unknown. The per-operation extensions that preceded it — `vault-locked`, `unlock-recovery`, `add-key-opts`, `vault-session-load`, `vault-session-unload`, `vault-set-autoload`, `vault-set-comment` — are gone, so `query` now returns just `query` and `sshush-op`.

That makes the wire format a breaking change between sshush versions, which is why `Backend` carries `SpeaksOps`:

| the agent | `Mode` | `SpeaksOps` |
|---|---|---|
| this sshushd, vault mode | `vault` | true |
| this sshushd, keys mode | `keys` | true |
| a foreign agent, or an sshushd older than `sshush-op` | `keys` | false |

A command that needs a vault and finds `SpeaksOps` false says so, because the usual cause is upgrading `sshush` while the old daemon is still running — `sshush reload` fixes it. Nothing else can tell those two situations apart, since neither answers the extension.

`ErrOpUnknown` is deliberately distinct from `ErrOpUnsupported`: the first means this agent speaks the protocol but not that operation, the second that it does not speak the protocol at all.

`Session.Lock` and `Session.Unlock` still fall back to the plain agent protocol when `sshush-op` is unsupported. That is not a legacy path — a real `ssh-agent` or 1Password implements lock and unlock natively, and `[agent].type = "external"` points at exactly those.

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
