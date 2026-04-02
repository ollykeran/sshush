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
- **internal/runtime** – Config/socket path resolution
- **internal/sshushd** – Daemon start/stop/reload control
- **internal/style** – Styled terminal output
- **internal/tui** – Bubble Tea TUI (Agent, Create, Edit, Export screens)
- **internal/utils** – Path expansion, helpers
- **internal/version** – Version string

CLI loads config and starts the daemon; daemon runs the agent on a Unix socket. OpenSSH (`ssh`, `ssh-add`) connect via `SSH_AUTH_SOCK`.
