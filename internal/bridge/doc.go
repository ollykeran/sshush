// Package bridge lets an sshush inside WSL use the agent of the sshush on the
// Windows side, so one daemon, one vault and one unlock serve both.
//
// Neither a named pipe nor a Unix socket crosses the WSL boundary, but a
// process does: a Linux process can start a Windows one and talk to it over
// its stdin and stdout. So the Linux daemon listens on a Unix socket as it
// always does, and hands each connection to a freshly started
// "sshush.exe relay", which connects its stdin and stdout to the Windows
// agent's pipe. The agent protocol passes through untouched, and nothing on the
// Linux side holds a key or an unlock: the agent is the one on Windows.
package bridge
