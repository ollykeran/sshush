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
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
	"github.com/spf13/cobra"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func newReloadCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "reload",
		Short:   "Reload config and reconcile keys with the running agent",
		Example: "sshush reload --config ~/.sshush/config",
		Long: `Reload the config file and reconcile keys with the running agent. 
If the agent is not running, it will be started. This command is affected by config.`,
		Args: argsNoneOrHelp,
		RunE: runReload,
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

// desiredKeysFromConfig returns the set of keys that should be in the agent after reload:
// key_paths (from disk) plus vault identities (when VaultPath is set). skipWarnings is populated for key_paths parse errors.
func desiredKeysFromConfig(newCfg config.Config) (after []diffEntry, configByFP map[string]keyInfo, skipWarnings []string) {
	configByFP = make(map[string]keyInfo)
	for _, path := range newCfg.KeyPaths {
		pubKey, comment, privKey, err := agent.ParseKeyFromPath(path)
		if err != nil {
			skipWarnings = append(skipWarnings, fmt.Sprintf("skip %s: %v", utils.DisplayPath(path), err))
			continue
		}
		fp := ssh.FingerprintSHA256(pubKey)
		after = append(after, diffEntry{fp: fp, comment: comment, keyType: pubKey.Type()})
		configByFP[fp] = keyInfo{fingerprint: fp, comment: comment, pubKey: pubKey, privKey: privKey}
	}
	if newCfg.VaultPath != "" {
		resolved := vault.ResolveToFile(newCfg.VaultPath)
		store, err := vault.Open(resolved)
		if err != nil {
			skipWarnings = append(skipWarnings, fmt.Sprintf("vault %s: %v", utils.DisplayPath(resolved), err))
			return after, configByFP, skipWarnings
		}
		for _, id := range store.AllIdentities() {
			pubKey, err := ssh.ParsePublicKey(id.PublicKey)
			if err != nil {
				continue
			}
			fp := ssh.FingerprintSHA256(pubKey)
			if _, exists := configByFP[fp]; exists {
				continue
			}
			after = append(after, diffEntry{fp: fp, comment: id.Comment, keyType: pubKey.Type()})
			configByFP[fp] = keyInfo{fingerprint: fp, comment: id.Comment, pubKey: pubKey, privKey: nil}
		}
	}
	return after, configByFP, skipWarnings
}

// buildDiff returns an Output containing the key diff, any parse warnings, and an
// optional socket-changed notice. The returned Output is ready to Print().
func buildDiff(client sshagent.ExtendedAgent, newCfg config.Config, socketChanged bool, configPath string) *style.Output {
	liveKeys, err := client.List()
	if err != nil {
		return style.NewOutput().Error(fmt.Sprintf("list agent keys: %v", err))
	}
	before := agentKeysToEntries(liveKeys)

	after, _, skipWarnings := desiredKeysFromConfig(newCfg)

	out := style.NewOutput().Add(style.Success(fmt.Sprintf("Reloading keys from config file %s", utils.DisplayPath(configPath))))
	out.Spacer()
	out.Add(printKeysDiff(before, after).String())
	for _, w := range skipWarnings {
		out.Add(w)
	}
	if socketChanged {
		out.Add("[agent].socket_path changed - restart required")
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

	_, configByFP, _ := desiredKeysFromConfig(newCfg)

	applyErrs := style.NewOutput()

	// Add keys from key_paths that are in configByFP and have privKey (not in live).
	for fp, info := range configByFP {
		if info.privKey == nil {
			continue
		}
		if _, exists := liveByFP[fp]; !exists {
			if err := client.Add(sshagent.AddedKey{PrivateKey: info.privKey, Comment: info.comment}); err != nil {
				applyErrs.Error(fmt.Sprintf("add %s: %v", info.comment, err))
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
