package sshushd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// RunAgent runs the agent in the current process: loads keys and serves on the socket
// until ctx is done. Does not detach or write a pidfile. Use for in-process (e.g. subshell) mode.
// If vaultPath is non-empty, uses the vault at that path (starts locked; use sshush unlock).
func RunAgent(ctx context.Context, socketPath string, keyPaths []string, vaultPath string) error {
	absSocket, err := filepath.Abs(socketPath)
	if err != nil {
		return fmt.Errorf("socket path: %w", err)
	}
	defer os.Remove(absSocket)
	var ext sshagent.ExtendedAgent
	if vaultPath != "" {
		resolved := vault.ResolveToFile(vaultPath)
		if _, err := os.Stat(resolved); err != nil && os.IsNotExist(err) {
			// Vault file missing; fall back to key_paths
			keyring := sshagent.NewKeyring()
			if len(keyPaths) > 0 {
				agent.LoadKeys(keyring, keyPaths, os.Stderr)
			}
			ext = agent.NewKDFLockedKeyring(keyring.(sshagent.ExtendedAgent))
		} else {
			store, err := vault.Open(resolved)
			if err != nil {
				return fmt.Errorf("vault: %w", err)
			}
			ext = vault.NewVaultAgent(store)
		}
	} else {
		keyring := sshagent.NewKeyring()
		if len(keyPaths) > 0 {
			agent.LoadKeys(keyring, keyPaths, os.Stderr)
		}
		ext = agent.NewKDFLockedKeyring(keyring.(sshagent.ExtendedAgent))
	}
	return agent.ListenAndServe(ctx, absSocket, ext)
}

// RunDaemonOnly runs the agent daemon in the current process: detaches from terminal,
// writes pidfile, loads keys (or uses vault when vaultPath is set), and serves on the socket.
// Call only from the sshushd binary. Removes pidfile and socket on exit.
func RunDaemonOnly(cfg config.Config, pidFilePath string) error {
	absSocket, err := filepath.Abs(cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("socket path: %w", err)
	}
	socketPath := absSocket
	if err := detachProcess(); err != nil {
		return err
	}
	if pidFilePath != "" {
		if err := os.WriteFile(pidFilePath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644); err != nil {
			return fmt.Errorf("write pidfile: %w", err)
		}
		defer os.Remove(pidFilePath)
	}
	var ext sshagent.ExtendedAgent
	if vp := cfg.VaultPathForAgent(); vp != "" {
		resolved := vault.ResolveToFile(vp)
		if _, err := os.Stat(resolved); err != nil && os.IsNotExist(err) {
			keyring := sshagent.NewKeyring()
			if len(cfg.KeyPaths) > 0 {
				agent.LoadKeys(keyring, cfg.KeyPaths, os.Stderr)
			}
			ext = agent.NewKDFLockedKeyring(keyring.(sshagent.ExtendedAgent))
		} else {
			store, err := vault.Open(resolved)
			if err != nil {
				return fmt.Errorf("vault: %w", err)
			}
			ext = vault.NewVaultAgent(store)
		}
	} else {
		keyring := sshagent.NewKeyring()
		if len(cfg.KeyPaths) > 0 {
			agent.LoadKeys(keyring, cfg.KeyPaths, os.Stderr)
		}
		ext = agent.NewKDFLockedKeyring(keyring.(sshagent.ExtendedAgent))
	}
	defer os.Remove(socketPath)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = agent.ListenAndServe(ctx, socketPath, ext)
	if err != nil {
		if errors.Is(err, agent.ErrAlreadyRunning) {
			return fmt.Errorf("agent already running at %s", utils.DisplayPath(socketPath))
		}
		return err
	}
	return nil
}

// WaitForSocket waits until the socket at socketPath is accepting connections or timeout.
// Used by the CLI after starting sshushd to confirm the daemon is up before exiting.
func WaitForSocket(socketPath string, maxAttempts int, interval time.Duration) bool {
	for i := 0; i < maxAttempts; i++ {
		if conn, err := net.Dial("unix", socketPath); err == nil {
			conn.Close()
			return true
		}
		time.Sleep(interval)
	}
	return false
}

// CheckAlreadyRunning returns true if something is already listening on the socket.
func CheckAlreadyRunning(socketPath string) bool {
	return checkAlreadyRunning(socketPath)
}

func detachProcess() error {
	if _, err := syscall.Setsid(); err != nil {
		return err
	}
	devNull, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()
	syscall.Dup2(int(devNull.Fd()), 0)
	syscall.Dup2(int(devNull.Fd()), 1)
	syscall.Dup2(int(devNull.Fd()), 2)
	return nil
}
