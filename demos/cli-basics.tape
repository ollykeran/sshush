Output demos/cli-basics.gif
Source demos/common.tape

Require sshush

Type "eval $(sshush)"
Enter
Sleep 1500ms

Type "sshush list"
Enter
Sleep 1200ms

Type "sshush add ~/.ssh/id_ed25519_work"
Enter
Sleep 1s

Type "sshush list"
Enter
Sleep 1200ms

Type "sshush selftest"
Enter
Sleep 1500ms

Type "sshush stop"
Enter
Sleep 1s
