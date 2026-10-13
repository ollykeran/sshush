package vaultops

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ollykeran/sshush/internal/secfile"
)

func TestInitTarget_noVaultPathIsAnError(t *testing.T) {
	_, err := InitTarget(Env{})
	if got := CodeOf(err); got != CodeNoVaultPath {
		t.Fatalf("code: want %v, got %v (err %v)", CodeNoVaultPath, got, err)
	}
}

func TestInitTarget_existingVaultIsAnError(t *testing.T) {
	f := startVaultAgent(t, false)
	path, err := InitTarget(Env{VaultPath: f.vaultPath})
	if got := CodeOf(err); got != CodeVaultExists {
		t.Fatalf("code: want %v, got %v (err %v)", CodeVaultExists, got, err)
	}
	if path != f.vaultPath {
		t.Fatalf("path: want %q, got %q", f.vaultPath, path)
	}
}

func TestInit_withRecoveryWritesPhraseAndFile(t *testing.T) {
	dir := unixSocketTempDirVaultOps(t)
	env := Env{VaultPath: filepath.Join(dir, "vault.json")}

	res, err := Init(env, []byte("hunter2-ok"), InitOptions{Recovery: true})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if res.Mnemonic == "" {
		t.Fatal("mnemonic: want a phrase, got empty")
	}
	want := filepath.Join(dir, "recovery.txt")
	if res.RecoveryFile != want {
		t.Fatalf("recovery file: want %q, got %q", want, res.RecoveryFile)
	}
	body, err := os.ReadFile(res.RecoveryFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != res.Mnemonic+"\n" {
		t.Fatalf("recovery.txt: want %q, got %q", res.Mnemonic+"\n", string(body))
	}
	if private, err := secfile.IsPrivate(res.RecoveryFile); err != nil || !private {
		t.Fatalf("recovery.txt is readable by other users (private=%v, err=%v)", private, err)
	}
}

func TestInit_extraRecoveryFileGetsTheSamePhrase(t *testing.T) {
	dir := unixSocketTempDirVaultOps(t)
	extra := filepath.Join(dir, "elsewhere.txt")
	env := Env{VaultPath: filepath.Join(dir, "vault.json")}

	res, err := Init(env, []byte("hunter2-ok"), InitOptions{Recovery: true, RecoveryFile: extra})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if res.ExtraFile != extra {
		t.Fatalf("extra file: want %q, got %q", extra, res.ExtraFile)
	}
	body, err := os.ReadFile(extra)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != res.Mnemonic+"\n" {
		t.Fatalf("extra file: want %q, got %q", res.Mnemonic+"\n", string(body))
	}
	if private, err := secfile.IsPrivate(extra); err != nil || !private {
		t.Fatalf("the extra recovery file is readable by other users (private=%v, err=%v)", private, err)
	}
}

func TestInit_noRecoveryLeavesMnemonicEmpty(t *testing.T) {
	dir := unixSocketTempDirVaultOps(t)
	env := Env{VaultPath: filepath.Join(dir, "vault.json")}

	res, err := Init(env, []byte("hunter2-ok"), InitOptions{})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if res.Mnemonic != "" || res.RecoveryFile != "" {
		t.Fatalf("want no recovery, got mnemonic %q file %q", res.Mnemonic, res.RecoveryFile)
	}
	if _, err := os.Stat(filepath.Join(dir, "recovery.txt")); !os.IsNotExist(err) {
		t.Fatalf("recovery.txt: want absent, got %v", err)
	}
}

// TestInit_doesNotZeroCallerPassphrase pins the ownership contract: the caller
// owns the buffer it passes and is responsible for clearing it, so a front end
// that reuses one (the CLI confirms then initialises) is not surprised.
func TestInit_doesNotZeroCallerPassphrase(t *testing.T) {
	dir := unixSocketTempDirVaultOps(t)
	pass := []byte("hunter2-ok")
	want := append([]byte(nil), pass...)

	if _, err := Init(Env{VaultPath: filepath.Join(dir, "vault.json")}, pass, InitOptions{Recovery: true}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if !bytes.Equal(pass, want) {
		t.Fatalf("passphrase: want %q untouched, got %q", want, pass)
	}
}
