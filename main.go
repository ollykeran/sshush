package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/style"

	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func LoadKeys(keyring sshagent.Agent, keyPaths []string) error {
	for _, path := range keyPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Println(style.Err(fmt.Sprintf("error reading key %s: %s", path, err)))
			continue
		}
		key, err := ssh.ParseRawPrivateKey(data)
		if err != nil {
			fmt.Println(style.Err(fmt.Sprintf("error parsing key %s: %s", path, err)))
			continue
		}
		keyring.Add(sshagent.AddedKey{PrivateKey: key})
	}
	return nil
}

func ListKeys(keyring sshagent.Agent) error {
	keys, err := keyring.List()
	if err != nil {
		return err
	}
	var lines []string
	for _, key := range keys {
		lines = append(lines, style.Pink(ssh.FingerprintSHA256(key)+" "+key.Comment))
	}
	fmt.Println(style.Box(strings.Join(lines, "\n")))
	return nil
}

func main() {
	cfg, _ := config.Load("./internal/config/config.toml")
	keyring := sshagent.NewKeyring()

	cfgLines := []string{
		style.Green("* sshush"),
		style.Purple("socket ") + style.Pink(cfg.SocketPath),
		style.Purple("keys  ") + style.Pink(strings.Join(cfg.KeyPaths, ", ")),
	}
	fmt.Println(style.Box(strings.Join(cfgLines, "\n")))
	fmt.Println()

	if cfg.KeyPaths != nil {
		LoadKeys(keyring, cfg.KeyPaths)
	}

	ListKeys(keyring)
	fmt.Println()

	ctx := context.Background()
	go func() {
		if err := agent.ListenAndServe(ctx, cfg.SocketPath, keyring); err != nil {
			log.Print(err)
		}
	}()

	select {}
}
