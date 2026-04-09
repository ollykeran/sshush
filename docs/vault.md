# Vault

The sshush **vault** is an optional mode where SSH private keys live in a single JSON file on disk. Material is **encrypted at rest** with a **master key** derived from your passphrase. The running agent holds the master key only while **unlocked**; locking wipes it from memory. The `sshush vault` command group creates the vault file and manages identities (list, add, remove, autoload, session load, recovery unlock).

This page matches the implementation in `internal/vault/` and `internal/cli/vault.go`. For config keys and high-level setup, see [Config Reference](config.md) (sections `[agent]`, `[vault]`, and **Vault**).

## What the vault does

1. **On disk**: One file (default name `vault.json`, or a path you choose) stores metadata and one record per identity: public key, comment, autoload flag, and an **AES-256-GCM** ciphertext of the private key material (`encrypted_blob` in JSON). The file is human-readable JSON; private keys are not stored in plaintext.
2. **Master key**: A 32-byte key derived from your **master passphrase** and a random **salt** using **Argon2id** (`internal/kdf`). That key encrypts a **canary** string at init time; successful unlock decrypts the canary and proves the passphrase is correct (constant-time compare).
3. **Agent**: In vault mode, the daemon uses `VaultAgent`, which implements the SSH agent protocol with encrypted storage. While **locked**, the agent lists no keys (same idea as a locked OpenSSH agent). While **unlocked**, the master key stays in memory so identities can be listed and signing can decrypt blobs temporarily.
4. **Recovery (optional)**: If you did not pass `--no-recovery` at `vault init`, a **BIP-39** 24-word mnemonic is generated. A separate salt and Argon2id derivation wrap the master key for recovery-based unlock. The phrase is written to `recovery.txt` beside the vault (mode `0600`) and shown once in the terminal.

**Path resolution**: If `vault_path` is a directory or ends with a path separator, the vault file is `vault.json` inside that directory (`internal/vault/path.go`). Otherwise the path is used as the file path.

## Cryptography

| Piece | Mechanism | Code / reference |
|--------|-----------|------------------|
| Key derivation (passphrase and recovery phrase) | Argon2id, 32-byte output | `internal/kdf`: time cost `3`, memory `64 MiB`, `1` thread; see [RFC 9106](https://datatracker.ietf.org/doc/html/rfc9106) |
| Per-blob encryption | AES-256-GCM, 12-byte random IV prepended to ciphertext | `internal/vault/cipher.go`; GCM is an AEAD mode ([NIST SP 800-38D](https://csrc.nist.gov/publications/detail/sp/800-38d/final)) |
| Salt generation | 16 random bytes | `kdf.GenerateSalt` |
| Recovery mnemonic | 256-bit entropy, 24 words | `github.com/tyler-smith/go-bip39` ([BIP-39](https://github.com/bitcoin/bips/blob/master/bip-0039.mediawiki)) |
| Passphrase policy for **new** vaults | Non-empty (whitespace-only rejected); minimum length and optional character classes from `DefaultPassphrasePolicy` | `internal/vault/passphrase_policy.go` (policy applies at `vault init` only, not on unlock) |

**Signing path**: `Sign` decrypts the identity blob with the master key, builds a signer, signs, and **wipes** the decrypted buffer (`internal/vault/agent.go`). The agent does **not** expose `Signers()` with long-lived decrypted keys; it returns "not implemented" so clients cannot keep raw `ssh.Signer` instances for every key.

**On-disk write**: `Save` writes a temp file, syncs, renames over the vault path, then `chmod 0600` (`internal/vault/store.go`).

## Why this is considered secure (threat model)

- **At-rest secrecy**: Without the passphrase (or recovery phrase, if enabled), disk theft or backup leakage does not expose private keys; only ciphertext, salts, and public data are present.
- **Strong KDF**: Argon2id is a modern memory-hard password hash, intended to resist GPU and ASIC guessing compared to fast hashes.
- **Authenticated encryption**: GCM provides confidentiality and integrity for each blob; tampering should fail at decrypt.
- **Lock**: `Lock` zeroes the master key slice in process memory; until unlock, operations that need decryption fail.

**Limits (honest caveats)**:

- While **unlocked**, the master key exists in the agent process. Anyone who can read that process memory, attach a debugger, or coerce the OS could still attack keys, similar to any unlocked agent.
- Passphrase **strength** is your responsibility. Policy enforces structural rules for new vaults but is not an entropy meter; a short or guessable passphrase weakens offline attacks against the vault file.
- Recovery phrase storage is as sensitive as the passphrase; anyone with the phrase and the vault file can derive the recovery key and unwrap the master key.
- The vault file and `recovery.txt` must be protected by filesystem permissions and backups you trust.

## Comparison to classic `ssh-agent` and in-memory keys

OpenSSH’s `ssh-agent` loads **decrypted** key material into the agent and keeps it there until the key is removed or the agent exits (see `ssh-agent(1)` and [OpenSSH agent protocol](https://datatracker.ietf.org/doc/html/draft-miller-ssh-agent-04)). Keys are **not** stored encrypted by the agent on disk; usual practice is encrypted PEM files on disk, decrypted when you `ssh-add`.

**What the vault adds:**

| Aspect | Typical ssh-agent + encrypted PEM on disk | sshush vault |
|--------|-------------------------------------------|--------------|
| On-disk private keys | PEM may be passphrase-encrypted per file; many keys mean many passphrases or scripts | Single vault file; one master passphrase encrypts all identities |
| Agent memory while running | Decrypted keys often stay in agent memory for the session | Master key in memory when unlocked; signing decrypts per operation and wipes decrypted material after `Sign` |
| Lock | OpenSSH agent supports lock with a password; behavior depends on client | `sshush lock` wipes the master key; list/sign stop until unlock |
| Recovery | Usually backup of PEM files or export | Optional BIP-39 phrase wrapping the master key (`unlock-recovery`) |
| Portability | Copy key files | Copy `vault.json` (and protect recovery material if used) |

The vault does **not** remove the need to trust the machine while the agent is **unlocked**. It improves **at-rest** handling (one encrypted bundle, Argon2, AEAD) and gives a clear **lock** semantics tied to wiping the derived master key.

## Configuration prerequisites

- **`sshush vault init`**: Set `[vault].vault_path` in config or pass `--vault-path`. Does not require the agent.
- **Most other `sshush vault` subcommands**: Need config loaded (so `vault_path` resolves), and usually a **running** vault agent (`sshush start` with `[agent].vault = true`).
- **`sshush vault list`**: Reads the vault file directly; if the running agent uses the same vault and is locked, the CLI may prompt to unlock so the **LOADED** column can be computed.

Use `-c` / `--config` and `-s` / `--socket` as documented in global flags when needed.

---

## `sshush vault` subcommands

Global flags (all subcommands): `-c, --config`, `-s, --socket`.

### `vault init`

Creates a new vault. Prompts for passphrase twice (confirm).

| Flag | Meaning |
|------|---------|
| `--vault-path` | Vault file path (default: `[vault].vault_path` from config) |
| `--no-recovery` | Do not generate a 24-word recovery phrase |
| `--recovery-file` | Also write the recovery phrase to this file (mode `0600`) |

If recovery is enabled (default): generates a mnemonic, enables recovery in the store, writes `recovery.txt` next to the vault, optionally `--recovery-file`, prints the phrase (with layout to reduce copy mistakes), and may copy to clipboard when supported.

Fails if the vault file already exists at the resolved path.

### `vault list`

Lists all identities: SHA256 fingerprint, whether each key is loaded in the **current** agent (if reachable), autoload flag, comment, key type.

| Flag | Meaning |
|------|---------|
| `--vault-path` | Vault file (default: config) |

Works with the vault file alone; uses the agent only for the **LOADED** column when the socket is available and the config’s vault path matches.

### `vault add`

Adds **unencrypted** OpenSSH private key files through the **running** vault agent. Keys are encrypted and persisted to the vault.

```
sshush vault add <key_paths...> [--no-autoload]
```

| Flag | Meaning |
|------|---------|
| `--no-autoload` | Store without autoload (visible until daemon restart only, unless you `vault load` later) |

Requires `[agent].vault = true`, initialized vault, and `sshush start`. Default is autoload **on** (keys return after restart). In vault mode, `sshush add` uses the same agent extension as `vault add`; `vault add` refuses a non-vault agent so you get a clear error if vault mode is off.

### `vault remove`

Removes identities from the vault **by talking to the agent** (so the store is updated consistently). Selectors:

- SHA256 fingerprint (`SHA256:...`)
- Comment (unique; ambiguous comments error)
- Path to a private key file (resolves to fingerprint)

```
sshush vault remove <fingerprint|comment|key_path...> [--vault-path ...]
```

Requires a running, **unlocked** vault agent. Can remove keys that are not currently listed (e.g. after restart with autoload off).

### `vault load`

For identities with **autoload off**, registers them in the **current** agent session until restart so SSH can use them without the PEM file.

```
sshush vault load <fingerprint|comment|key_path...> [--vault-path ...]
```

Requires minimum one selector argument; running, unlocked vault agent.

### `vault autoload`

Persists whether identities load automatically after daemon restart.

```
sshush vault autoload (on|off) <fingerprint|comment|key_path...> [--vault-path ...]
```

First argument: `on` / `off` (also `yes`, `true`, `1` or `no`, `false`, `0`). Requires running, unlocked vault agent.

Examples:

```text
sshush vault autoload on SHA256:abcd...
sshush vault autoload off my-key-comment
```

### `vault unlock-recovery`

Connects to the agent socket from config and unlocks using the **24-word** recovery phrase (stdin line). Fails if the vault was created with `--no-recovery` or the phrase is wrong.

No subcommand-specific flags beyond global `-c` / `-s`.

---

## Related commands (outside `sshush vault`)

- **`sshush start`**: Unlocks the vault (passphrase prompt) when in vault mode.
- **`sshush unlock`**: Unlocks a locked agent (passphrase or recovery path depending on setup); see `internal/cli/unlock.go`.
- **`sshush lock`**: Locks the agent; for vault, wipes the master key from memory.
- **`sshush add`**: When the agent is a vault, adds keys into the vault (with agent-specific autoload defaults); see `internal/cli/add.go`.

For reload and keyring reconciliation, see [Config Reference](config.md) **Reload Behavior**.
