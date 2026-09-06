#!/usr/bin/env bash
# Builds a throwaway, self-contained environment for recording demos: its own
# $HOME, its own ~/.ssh, its own sshush config. Nothing here ever touches the
# real ~/.ssh or ~/.config/sshush, and none of it is committed to the repo.
#
# Usage: eval "$(demos/fixture.sh)" sets HOME/XDG_CONFIG_HOME/PATH for the
# current shell; a VHS recording run in that shell then only ever sees the
# fixture's keys, config, and sockets.
set -euo pipefail

FIXTURE_DIR="$(mktemp -d -t sshush-demo)"
mkdir -p "$FIXTURE_DIR/.ssh" "$FIXTURE_DIR/.config/sshush"
chmod 700 "$FIXTURE_DIR/.ssh"

# Primary identity: preloaded into the agent via key_paths.
ssh-keygen -q -t ed25519 -N "" -C "demo@sshush" -f "$FIXTURE_DIR/.ssh/id_ed25519"

# Second identity: left off key_paths so the CLI/TUI demos can `sshush add` it live.
ssh-keygen -q -t ed25519 -N "" -C "work@sshush" -f "$FIXTURE_DIR/.ssh/id_ed25519_work"

# Fixed host key for the demo SSH server, so its fingerprint is stable across runs.
ssh-keygen -q -t ed25519 -N "" -f "$FIXTURE_DIR/.config/sshush/host_key"

cat > "$FIXTURE_DIR/.config/sshush/config.toml" <<EOF
[agent]
socket_path = "$FIXTURE_DIR/.config/sshush/sshush.sock"
type        = "keys"
key_paths   = ["$FIXTURE_DIR/.ssh/id_ed25519"]

[server]
listen_port     = 2222
authorized_keys = "$FIXTURE_DIR/.ssh/id_ed25519.pub"
host_key        = "$FIXTURE_DIR/.config/sshush/host_key"

[theme]
name = "dracula"
EOF

# ssh client config for the "connect to a server" demo: points at the fixture
# identity and known_hosts so the recording never prompts or touches ~/.ssh.
cat > "$FIXTURE_DIR/.ssh/config" <<EOF
Host demo-server
    HostName 127.0.0.1
    Port 2222
    User demo
    IdentityFile $FIXTURE_DIR/.ssh/id_ed25519
    UserKnownHostsFile $FIXTURE_DIR/.ssh/known_hosts
    StrictHostKeyChecking accept-new
    IdentityAgent $FIXTURE_DIR/.config/sshush/sshush.sock
EOF
touch "$FIXTURE_DIR/.ssh/known_hosts"
chmod 600 "$FIXTURE_DIR"/.ssh/id_ed25519 "$FIXTURE_DIR"/.ssh/id_ed25519_work "$FIXTURE_DIR/.config/sshush/host_key"

echo "export HOME=$FIXTURE_DIR"
echo "export XDG_CONFIG_HOME=$FIXTURE_DIR/.config"
echo "export SSHUSH_DEMO_FIXTURE=$FIXTURE_DIR"
