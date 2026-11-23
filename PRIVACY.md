# Privacy policy

*Effective 23 November 2026. Applies to sshush and to "sshush agent" in the Microsoft Store.*

sshush is an SSH agent that runs on your computer. It collects no personal data, has no accounts, and sends nothing to its developer: no telemetry, analytics, crash reports or advertising.

## What stays on your computer

sshush reads and writes these files on your own machine, and nowhere else:

- your SSH keys, and the encrypted key vault if you create one;
- its configuration (`~/.config/sshush`), pidfile and logs;
- `~/.ssh/config` and your shell or PowerShell profile, only when you run the setup that adds sshush to them.

Your private keys are never sent anywhere. The agent answers only programs on your computer that connect to its socket or named pipe.

## When sshush uses the network

sshush makes no network requests unless you run one of these commands:

| Command | Contacts | Why |
|---|---|---|
| `sshush version --check` | `api.github.com` | to see whether a newer release exists |
| `sshush selftest` | `github.com`, or `sum.golang.org` for a build from `go install` | to check the program you are running matches the published release |

These requests send nothing about you or your keys. Like any web request they reveal your IP address to GitHub (Microsoft) or Google, whose own privacy policies apply.

**`sshush server`** is an optional SSH server that runs only if you start it. It listens on the address you configure and logs connections to a file on your computer. That log stays on your machine.

## The Microsoft Store

If you install sshush from the Microsoft Store, Microsoft handles the download and updates and may collect install and diagnostic data under the [Microsoft Privacy Statement](https://privacy.microsoft.com/privacystatement). The developer sees only Microsoft's aggregated, anonymous reports, such as install counts.

## Changes and contact

Changes to this policy are published in this file, and its history is in the repository. For questions, open an issue at <https://github.com/ollykeran/sshush/issues>.
