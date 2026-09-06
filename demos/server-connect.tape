Output demos/server-connect.gif
Source demos/common.tape

Require sshush
Require ssh

Type "eval $(sshush)"
Enter
Sleep 1500ms

Type "sshush list"
Enter
Sleep 1200ms

Type "sshush server &"
Enter
Sleep 1500ms

Type "ssh -F ~/.ssh/config demo-server"
Enter
Sleep 2s

Type "sshush server stop"
Enter
Sleep 1s

Type "sshush stop"
Enter
Sleep 1s
