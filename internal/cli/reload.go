package cli

import (
	"fmt"
	"net"
	"os"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/spf13/cobra"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func newReloadCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reload",
		Short: "Reload config and reconcile keys with the running agent",
		Args:  argsNoneOrHelp,
		RunE:  runReload,
	}
	cmd.Flags().StringP("config", "c", "", "path to config file")
	return cmd
}

type keyInfo struct {
	fingerprint string
	comment     string
	pubKey      ssh.PublicKey
	privKey     interface{}
}

func runReload(cmd *cobra.Command, _ []string) error {
	configPath, err := runtime.ResolveConfigPath(cmd)
	if err != nil {
		return err
	}
	newCfg, err := LoadMergedConfig(configPath, LoadOverrides{})
	if err != nil {
		return err
	}

	pidFilePath := runtime.PidFilePath()

	// Try connecting to the socket from the new config.
	conn, err := net.Dial("unix", newCfg.SocketPath)
	needsRestart := err != nil

	// If the new socket didn't work, try SSH_AUTH_SOCK (the old socket).
	if needsRestart {
		if authSock := os.Getenv("SSH_AUTH_SOCK"); authSock != "" && authSock != newCfg.SocketPath {
			conn, err = net.Dial("unix", authSock)
		}
	}

	// We have a connection to the live agent: show the diff.
	if err == nil {
		defer conn.Close()
		client := sshagent.NewClient(conn)
		needsRestart = needsRestart || newCfg.SocketPath != conn.RemoteAddr().String()

		if needsRestart {
			buildDiff(client, newCfg, true, configPath).Print()
		} else {
			buildDiff(client, newCfg, false, configPath).Print()
			applyDiff(client, newCfg)
			return nil
		}
	}

	// Socket changed or agent unreachable: stop the old daemon and start a new one.
	if needsRestart {
		_ = sshushd.StopDaemon(pidFilePath) // best-effort; may already be gone
		style.NewOutput().Info("restarting sshushd with new config...").Print()
		return runStartDaemon(cmd)
	}

	return nil
}

// buildDiff returns an Output containing the key diff, any parse warnings, and an
// optional socket-changed notice. The returned Output is ready to Print().
func buildDiff(client sshagent.ExtendedAgent, newCfg config.Config, socketChanged bool, configPath string) *style.Output {
	liveKeys, err := client.List()
	if err != nil {
		return style.NewOutput().Error(fmt.Sprintf("list agent keys: %v", err))
	}
	before := agentKeysToEntries(liveKeys)

	var after []diffEntry
	var skipWarnings []string
	for _, path := range newCfg.KeyPaths {
		pubKey, comment, _, err := agent.ParseKeyFromPath(path)
		if err != nil {
			skipWarnings = append(skipWarnings, fmt.Sprintf("skip %s: %v", path, err))
			continue
		}
		after = append(after, diffEntry{fp: ssh.FingerprintSHA256(pubKey), comment: comment, keyType: pubKey.Type()})
	}

	// printKeysDiff returns a pointer - append further lines directly onto it.
	out := style.NewOutput().Add(style.Green(fmt.Sprintf("Reloading keys from config file %s", configPath)))
	out.Spacer()
	out.Add(printKeysDiff(before, after).String())
	for _, w := range skipWarnings {
		out.Add(w)
	}
	if socketChanged {
		out.Add("socket_path changed - restart required")
	}
	return out
}

func applyDiff(client sshagent.ExtendedAgent, newCfg config.Config) {
	liveKeys, err := client.List()
	if err != nil {
		return
	}
	liveByFP := make(map[string]*sshagent.Key, len(liveKeys))
	for _, k := range liveKeys {
		liveByFP[ssh.FingerprintSHA256(k)] = k
	}

	configByFP := make(map[string]keyInfo)
	for _, path := range newCfg.KeyPaths {
		pubKey, comment, _, err := agent.ParseKeyFromPath(path)
		if err != nil {
			continue
		}
		fp := ssh.FingerprintSHA256(pubKey)
		configByFP[fp] = keyInfo{fingerprint: fp, comment: comment, pubKey: pubKey, privKey: nil}
	}

	applyErrs := style.NewOutput()

	// Re-parse to get private keys for adds.
	for _, path := range newCfg.KeyPaths {
		pubKey, comment, privKey, err := agent.ParseKeyFromPath(path)
		if err != nil {
			continue
		}
		fp := ssh.FingerprintSHA256(pubKey)
		if _, exists := liveByFP[fp]; !exists {
			if err := client.Add(sshagent.AddedKey{PrivateKey: privKey, Comment: comment}); err != nil {
				applyErrs.Error(fmt.Sprintf("add %s: %v", comment, err))
			}
		}
	}
	for fp, k := range liveByFP {
		if _, exists := configByFP[fp]; !exists {
			pubKey, err := ssh.ParsePublicKey(k.Marshal())
			if err != nil {
				continue
			}
			if err := client.Remove(pubKey); err != nil {
				applyErrs.Error(fmt.Sprintf("remove %s: %v", k.Comment, err))
			}
		}
	}

	if applyErrs.Len() > 0 {
		applyErrs.Print()
	}
}
