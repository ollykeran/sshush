package server

import (
	"net"
	"strconv"

	gliderlabs "github.com/gliderlabs/ssh"
	"golang.org/x/crypto/ssh"
)

// portForwardingRefusal is the reason a client is given when a forward is refused.
const portForwardingRefusal = "sshush server does not support port forwarding"

// serveOnlySessions makes sessions the one channel type the server accepts and
// refuses every other channel and global request, logging each refusal.
// gliderlabs refuses what nothing handles anyway, but with a bare "unsupported
// channel type" that leaves someone who tried `ssh -L` wondering whether they got
// the address wrong, and without a word in the log.
func (s *Server) serveOnlySessions(srv *gliderlabs.Server) error {
	srv.ChannelHandlers = map[string]gliderlabs.ChannelHandler{
		"session": gliderlabs.DefaultSessionHandler,
		"default": s.refuseChannel,
	}
	srv.RequestHandlers = map[string]gliderlabs.RequestHandler{
		"tcpip-forward":                   s.refuseRemoteForward,
		"streamlocal-forward@openssh.com": s.refuseRemoteForward,
		"default":                         s.refuseGlobalRequest,
	}
	return nil
}

// directTCPIPData is the payload of a direct-tcpip channel open (RFC 4254 §7.2).
type directTCPIPData struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

// remoteForwardData is the payload of a tcpip-forward request (RFC 4254 §7.1).
type remoteForwardData struct {
	BindAddr string
	BindPort uint32
}

// refuseChannel turns a channel away with a reason the client shows its user
// (OpenSSH prints "channel N: open failed: <reason>"). direct-tcpip is what -L, -D
// and -W open; direct-streamlocal is their Unix socket form.
func (s *Server) refuseChannel(_ *gliderlabs.Server, _ *ssh.ServerConn, newChan ssh.NewChannel, ctx gliderlabs.Context) {
	who := []any{"user", ctx.User(), "remote", remoteOf(ctx)}
	switch newChan.ChannelType() {
	case "direct-tcpip":
		var data directTCPIPData
		if ssh.Unmarshal(newChan.ExtraData(), &data) == nil {
			who = append(who, "destination", net.JoinHostPort(data.DestAddr, strconv.FormatUint(uint64(data.DestPort), 10)))
		}
		s.logger().Info("port forwarding refused", who...)
		_ = newChan.Reject(ssh.Prohibited, portForwardingRefusal)
	case "direct-streamlocal@openssh.com":
		s.logger().Info("socket forwarding refused", who...)
		_ = newChan.Reject(ssh.Prohibited, portForwardingRefusal)
	default:
		s.logger().Info("channel refused", append(who, "type", newChan.ChannelType())...)
		_ = newChan.Reject(ssh.UnknownChannelType, "sshush server does not support "+newChan.ChannelType()+" channels")
	}
}

// refuseRemoteForward turns down -R. The protocol's reply has no room for a
// reason, so the log is the only place the refusal is explained.
func (s *Server) refuseRemoteForward(ctx gliderlabs.Context, _ *gliderlabs.Server, req *ssh.Request) (bool, []byte) {
	who := []any{"user", ctx.User(), "remote", remoteOf(ctx)}
	if req.Type != "tcpip-forward" {
		s.logger().Info("remote socket forwarding refused", who...)
		return false, nil
	}
	var data remoteForwardData
	if ssh.Unmarshal(req.Payload, &data) == nil {
		who = append(who, "bind", net.JoinHostPort(data.BindAddr, strconv.FormatUint(uint64(data.BindPort), 10)))
	}
	s.logger().Info("remote port forwarding refused", who...)
	return false, nil
}

// refuseGlobalRequest turns down any other global request. Clients send some as a
// matter of course — keepalives, for one — so these are logged only at debug.
func (s *Server) refuseGlobalRequest(ctx gliderlabs.Context, _ *gliderlabs.Server, req *ssh.Request) (bool, []byte) {
	s.logger().Debug("global request refused", "type", req.Type, "remote", remoteOf(ctx))
	return false, nil
}
