package vaultops

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ollykeran/sshush/internal/vault"
)

// newVaultFile makes an initialized vault at path, protected by pass.
func newVaultFile(t *testing.T, path string, pass []byte, withRecovery bool) {
	t.Helper()
	store, err := vault.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(store, pass); err != nil {
		t.Fatal(err)
	}
	if withRecovery {
		phrase, err := vault.GenerateRecoveryMnemonic()
		if err != nil {
			t.Fatal(err)
		}
		if err := vault.EnableRecoveryWithPassphrase(store, pass, phrase); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBackup(t *testing.T) {
	f := startVaultAgent(t, true)
	f.addKey(t, "id_a", "a@test", true)

	dst := filepath.Join(t.TempDir(), "backups", "vault-20261125.json")
	sum, err := Backup(f.vaultPath, dst)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Keys != 1 || !sum.Recovery {
		t.Errorf("summary %+v", sum)
	}
	orig, _ := os.ReadFile(f.vaultPath)
	copied, _ := os.ReadFile(dst)
	if !bytes.Equal(orig, copied) {
		t.Error("backup differs from the vault")
	}
	if _, err := CheckVaultFile(dst, f.pass); err != nil {
		t.Errorf("backup does not open with the passphrase: %v", err)
	}

	if _, err := Backup(f.vaultPath, f.vaultPath); err == nil {
		t.Error("backed up a vault over itself")
	}
	notVault := filepath.Join(t.TempDir(), "notes.json")
	_ = os.WriteFile(notVault, []byte(`{"hello": 1}`), 0o600)
	if _, err := Backup(notVault, filepath.Join(t.TempDir(), "x.json")); CodeOf(err) != CodeNotAVault {
		t.Errorf("non-vault: got %v", err)
	}
}

func TestCheckVaultFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	newVaultFile(t, path, []byte("right-pass"), false)
	if _, err := CheckVaultFile(path, []byte("wrong-pass")); CodeOf(err) != CodeWrongPassphrase {
		t.Errorf("wrong passphrase: got %v", err)
	}
	sum, err := CheckVaultFile(path, []byte("right-pass"))
	if err != nil || sum.Recovery || sum.Created.IsZero() {
		t.Errorf("got %+v, %v", sum, err)
	}
	if _, err := CheckVaultFile(filepath.Join(t.TempDir(), "missing.json"), []byte("x")); CodeOf(err) != CodeNotAVault {
		t.Errorf("missing file: got %v", err)
	}
}

func TestImport_replacesTheVaultAndKeepsTheOldOne(t *testing.T) {
	home := t.TempDir()
	dst := filepath.Join(home, "sshush", "vault.json")
	newVaultFile(t, dst, []byte("old-passphrase"), true)
	recovery := filepath.Join(filepath.Dir(dst), "recovery.txt")
	if err := os.WriteFile(recovery, []byte("old phrase\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldBytes, _ := os.ReadFile(dst)

	src := filepath.Join(t.TempDir(), "from-laptop.json")
	newVaultFile(t, src, []byte("new-passphrase"), false)

	env := Env{VaultPath: dst, SocketPath: filepath.Join(unixSocketTempDirVaultOps(t), "none.sock")}
	if _, err := Import(env, src, []byte("old-passphrase")); CodeOf(err) != CodeWrongPassphrase {
		t.Fatalf("wrong passphrase: got %v", err)
	}
	if now, _ := os.ReadFile(dst); !bytes.Equal(now, oldBytes) {
		t.Fatal("a failed import changed the vault")
	}

	res, err := Import(env, src, []byte("new-passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	if res.VaultPath != dst || res.Previous == "" || res.PreviousRecovery == "" {
		t.Errorf("result %+v", res)
	}
	if prev, _ := os.ReadFile(res.Previous); !bytes.Equal(prev, oldBytes) {
		t.Error("the old vault was not kept")
	}
	if _, err := os.Stat(recovery); !os.IsNotExist(err) {
		t.Error("the old recovery.txt is still beside the imported vault")
	}
	if !strings.HasPrefix(filepath.Base(res.PreviousRecovery), "recovery.txt.bak-") {
		t.Errorf("recovery moved to %s", res.PreviousRecovery)
	}
	if _, err := CheckVaultFile(dst, []byte("new-passphrase")); err != nil {
		t.Errorf("imported vault: %v", err)
	}
}

func TestImport_intoAnEmptyPlace(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "new", "vault.json")
	src := filepath.Join(t.TempDir(), "v.json")
	newVaultFile(t, src, []byte("passphrase"), false)
	env := Env{VaultPath: dst, SocketPath: filepath.Join(unixSocketTempDirVaultOps(t), "none.sock")}
	res, err := Import(env, src, []byte("passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Previous != "" || res.PreviousRecovery != "" {
		t.Errorf("result %+v", res)
	}
	if _, err := Import(env, dst, []byte("passphrase")); CodeOf(err) != CodeVaultExists {
		t.Errorf("import of the vault into itself: got %v", err)
	}
}

func TestImport_refusedWhileTheAgentServesAVault(t *testing.T) {
	f := startVaultAgent(t, false)
	before, _ := os.ReadFile(f.vaultPath)
	src := filepath.Join(t.TempDir(), "v.json")
	newVaultFile(t, src, []byte("passphrase"), false)

	if _, err := Import(f.env(), src, []byte("passphrase")); CodeOf(err) != CodeVaultInUse {
		t.Fatalf("got %v", err)
	}
	if after, _ := os.ReadFile(f.vaultPath); !bytes.Equal(before, after) {
		t.Error("the served vault changed")
	}
}

func TestImport_needsAVaultPath(t *testing.T) {
	if _, err := Import(Env{}, "x.json", nil); CodeOf(err) != CodeNoVaultPath {
		t.Errorf("got %v", err)
	}
}
