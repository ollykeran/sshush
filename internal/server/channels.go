package server

import (
	gliderlabs "github.com/gliderlabs/ssh"
	"golang.org/x/crypto/ssh"
)

// serveOnlySessions makes sessions the one channel type the server accepts, and
// refuses every other with a reason. gliderlabs refuses a channel type nothing
// handles anyway, but with a bare "unsupported channel type", which leaves
// someone who tried `ssh -L` wondering whether they got the address wrong.
func serveOnlySessions(srv *gliderlabs.Server) error {
	srv.ChannelHandlers = map[string]gliderlabs.ChannelHandler{
		"session": gliderlabs.DefaultSessionHandler,
		"default": refuseChannel,
	}
	return nil
}

// refuseChannel turns a channel away with a reason the client shows its user
// (OpenSSH prints "channel N: open failed: <reason>"). direct-tcpip is what -L, -D
// and -W open; direct-streamlocal is their Unix socket form.
func refuseChannel(_ *gliderlabs.Server, _ *ssh.ServerConn, newChan ssh.NewChannel, _ gliderlabs.Context) {
	switch newChan.ChannelType() {
	case "direct-tcpip", "direct-streamlocal@openssh.com":
		_ = newChan.Reject(ssh.Prohibited, "sshush server does not support port forwarding")
	default:
		_ = newChan.Reject(ssh.UnknownChannelType, "sshush server does not support "+newChan.ChannelType()+" channels")
	}
}
