package sshushd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/platform"
	"github.com/ollykeran/sshush/internal/readypipe"
	"github.com/ollykeran/sshush/internal/server"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
	"github.com/ollykeran/sshush/internal/version"
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
	os.Setenv("SSH_AUTH_SOCK", absSocket)
	return agent.ListenAndServe(ctx, absSocket, ext)
}

// RunDaemonOnly runs the agent daemon in the current process: detaches from terminal,
// writes pidfile, loads keys (or uses vault when vaultPath is set), and serves on the socket.
// Call only from the sshushd binary. Removes pidfile and socket on exit.
// ready, if non-nil, is signaled once the socket is accepting connections.
func RunDaemonOnly(cfg config.Config, pidFilePath string, ready *readypipe.Child) error {
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
	os.Setenv("SSH_AUTH_SOCK", socketPath)
	err = agent.ListenAndServe(ctx, socketPath, ext, agent.WithReady(ready.Ready))
	if err != nil {
		if errors.Is(err, agent.ErrAlreadyRunning) {
			return fmt.Errorf("agent already running at %s", utils.DisplayPath(socketPath))
		}
		return err
	}
	return nil
}

// RunServerOnly runs the TCP SSH server daemon in the current process: detaches, writes pidfile,
// and runs the server (connecting to the agent socket for auth when [server].authorized_keys is not set).
// Call only from the sshushd binary when invoked with --server. Removes pidfile on exit.
// ready, if non-nil, is signaled once the listener is accepting connections.
func RunServerOnly(cfg config.Config, pidFilePath string, ready *readypipe.Child) error {
	if cfg.ServerListenPort <= 0 {
		return fmt.Errorf("[server].listen_port must be set in config (e.g. listen_port = 2222 under [server])")
	}
	port := int(cfg.ServerListenPort)
	listenAddr := ":" + strconv.Itoa(port)

	var authSource server.AuthKeySource
	if cfg.ServerAuthorizedKeys != "" {
		fa, err := server.NewFileAuth(cfg.ServerAuthorizedKeys)
		if err != nil {
			return fmt.Errorf("server authorized_keys %s: %w", utils.DisplayPath(cfg.ServerAuthorizedKeys), err)
		}
		authSource = fa
	} else {
		// Not dialled here: the agent is asked per connection, so the server
		// tolerates it starting later or being replaced by a reload.
		authSource = &server.SocketAuth{SocketPath: cfg.SocketPath}
	}
	// Resolve and create the host key before detaching, so a bad path is reported
	// to the caller rather than disappearing into a background process.
	hostKeyPath := platform.ServerHostKeyPath(cfg.ServerHostKey)
	if _, err := server.EnsureHostKey(hostKeyPath); err != nil {
		return fmt.Errorf("server host key %s: %w", utils.DisplayPath(hostKeyPath), err)
	}
	// Same for the shell: a typo here would otherwise fail every connection, with
	// nothing on the client's side saying why.
	if cfg.ServerShell != "" {
		if _, err := server.ResolveShell(cfg.ServerShell); err != nil {
			return fmt.Errorf("server shell: %w", err)
		}
	}
	var passwords server.PasswordSource
	var vaultFile string
	if cfg.ServerPasswordAuth {
		var err error
		if vaultFile, err = passwordAuthVault(cfg); err != nil {
			return err
		}
		passwords = &server.VaultPassphraseAuth{VaultPath: vaultFile}
	}
	// And the log: a bad path or level found after detaching would leave a server
	// running with nothing recording what it does.
	logLevel, err := server.ParseLogLevel(cfg.ServerLogLevel)
	if err != nil {
		return fmt.Errorf("[server].%w", err)
	}
	logPath := platform.ServerLogPath(cfg.ServerLogFile)
	if err := prepareServerLog(logPath); err != nil {
		return fmt.Errorf("server log %s: %w", utils.DisplayPath(logPath), err)
	}
	logFile := newServerLogWriter(logPath)
	defer logFile.Close()
	logger := slog.New(server.NewLogHandler(logFile, logLevel))

	if err := detachProcess(); err != nil {
		return err
	}
	if pidFilePath != "" {
		if err := os.WriteFile(pidFilePath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644); err != nil {
			return fmt.Errorf("write pidfile: %w", err)
		}
		defer os.Remove(pidFilePath)
	}

	starting := []any{"version", version.Line("sshushd"), "pid", os.Getpid()}
	if cfg.ServerAuthorizedKeys != "" {
		starting = append(starting, "authorized_keys", cfg.ServerAuthorizedKeys)
	} else {
		starting = append(starting, "agent_socket", cfg.SocketPath)
	}
	if passwords != nil {
		starting = append(starting, "password_vault", vaultFile)
	}
	logger.Info("server starting", starting...)

	srv := &server.Server{
		ListenAddr:  listenAddr,
		AuthKeys:    authSource,
		HostKeyPath: hostKeyPath,
		Shell:       cfg.ServerShell,
		Passwords:   passwords,
		Log:         logger,
		Ready:       ready.Ready,
	}

	// SIGTERM is how `sshush server stop` asks the server to go. Catching it, rather
	// than dying mid-write, is what gives the log its last line and hangs up on live
	// sessions the way a disconnect would.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	go func() {
		logger.Info("server stopping", "signal", (<-signals).String())
		_ = srv.Close()
	}()

	if err := srv.ListenAndServe(); err != nil {
		logger.Error("server failed", "err", err)
		return err
	}
	return nil
}

// passwordAuthVault resolves the vault file [server].password_auth checks
// passwords against. No vault configured, a missing file, or a vault never
// initialized is an error before the server starts: each would refuse every
// password, with nothing saying why.
func passwordAuthVault(cfg config.Config) (string, error) {
	if cfg.VaultPath == "" {
		return "", fmt.Errorf("[server].password_auth needs [vault].vault_path: passwords are checked against the vault's passphrase")
	}
	path := vault.ResolveToFile(cfg.VaultPath)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("[server].password_auth: vault %s: %w", utils.DisplayPath(path), err)
	}
	store, err := vault.Open(path)
	if err != nil {
		return "", fmt.Errorf("[server].password_auth: %w", err)
	}
	if store.GetMetadata() == nil {
		return "", fmt.Errorf("[server].password_auth: vault %s is not initialized; run 'sshush vault init' first", utils.DisplayPath(path))
	}
	return path, nil
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
	dupFd(int(devNull.Fd()), 0)
	dupFd(int(devNull.Fd()), 1)
	dupFd(int(devNull.Fd()), 2)
	return nil
}
