// Package vaultops performs the vault operations sshush's front ends offer:
// init, list, add, remove, session-load, autoload, lock and unlock. Every verb
// does the whole job — find the vault, reach the agent, check the backend,
// unlock it when the front end can prompt, resolve the identity, act — so the
// CLI and the TUI each reduce to argument parsing and rendering.
//
// A verb opens at most one [github.com/ollykeran/sshush/internal/agent.Session]
// and closes it before returning, however many selectors it is given. That is
// the "one Session per unit of work" rule in docs/architecture.md, and it is
// also why the verbs take a slice rather than making the caller loop.
//
// Failures come back as [*OpError]: one sentence in Msg, an optional remedy in
// Hint, a [Code] to branch on, and the underlying cause still reachable with
// errors.Is against the agent and vault sentinels.
package vaultops
