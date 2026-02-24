package cli

import (
	"fmt"
	"net"
	"os"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func newReloadCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "reload",
		Short: "Reload config and reconcile keys with the running agent",
		RunE:  runReload,
	}
}

type keyInfo struct {
	fingerprint string
	comment     string
	pubKey      ssh.PublicKey
	privKey     interface{}
}

func runReload(cmd *cobra.Command, _ []string) error {
	configPath, err := utils.ResolveConfigPath(cmd)
	if err != nil {
		return err
	}
	newCfg, err := config.LoadConfig(configPath)
	if err != nil {
		return err
	}

	pidFilePath := utils.PidFilePath()

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
			printDiff(client, newCfg, true)
		} else {
			printDiff(client, newCfg, false)
			applyDiff(client, newCfg)
			return nil
		}
	}

	// Socket changed or agent unreachable: stop the old daemon and start a new one.
	if needsRestart {
		_ = stopDaemon(pidFilePath) // best-effort; may already be gone
		fmt.Fprintln(os.Stderr, style.Green("restarting sshushd with new config..."))
		return runStartDaemon(cmd)
	}

	return nil
}

func printDiff(client sshagent.ExtendedAgent, newCfg config.Config, socketChanged bool) {
	liveKeys, err := client.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, style.Err(fmt.Sprintf("list agent keys: %v", err)))
		return
	}
	before := agentKeysToEntries(liveKeys)

	var after []diffEntry
	for _, path := range newCfg.KeyPaths {
		pubKey, comment, _, err := agent.ParseKeyFromPath(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, style.Err(fmt.Sprintf("  skip %s: %v", path, err)))
			continue
		}
		after = append(after, diffEntry{fp: ssh.FingerprintSHA256(pubKey), comment: comment})
	}

	printKeysDiff(before, after, true)
	if socketChanged {
		fmt.Println(style.Err("socket_path changed"))
	}
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

	// Re-parse to get private keys for adds.
	for _, path := range newCfg.KeyPaths {
		pubKey, comment, privKey, err := agent.ParseKeyFromPath(path)
		if err != nil {
			continue
		}
		fp := ssh.FingerprintSHA256(pubKey)
		if _, exists := liveByFP[fp]; !exists {
			if err := client.Add(sshagent.AddedKey{PrivateKey: privKey, Comment: comment}); err != nil {
				fmt.Fprintln(os.Stderr, style.Err(fmt.Sprintf("  add %s: %v", comment, err)))
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
				fmt.Fprintln(os.Stderr, style.Err(fmt.Sprintf("  remove %s: %v", k.Comment, err)))
			}
		}
	}
}
