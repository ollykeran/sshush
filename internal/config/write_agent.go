package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ollykeran/sshush/internal/utils"
)

// WriteAgentTypeToPath sets [agent].type in the config file at path to
// agentType and, when vaultPath is not empty and the file names no vault yet,
// [vault].vault_path to vaultPath. A vault path the file already names is kept:
// it is the vault the user chose.
//
// Unlike WriteThemeToPath it edits the file's text rather than re-encoding it,
// so the comments the generated config explains itself with survive. The
// result must load as a valid config, or the file is left as it was.
func WriteAgentTypeToPath(path, agentType, vaultPath string) error {
	return editConfigText(path, func(text string, doc configDocument) (string, error) {
		text, err := setAgentType(text, agentType)
		if err != nil {
			return "", err
		}
		if vaultPath != "" && doc.Vault.VaultPath == "" {
			text = setVaultPath(text, vaultPath)
		}
		return text, nil
	}, func(edited configDocument) error {
		if edited.Agent.Type != agentType {
			return fmt.Errorf("edit config: could not set [agent].type in %s; set type = %q there by hand", utils.DisplayPath(path), agentType)
		}
		return nil
	})
}

var (
	// tableHeader matches a [table] line, not an [[array]] one.
	tableHeader  = regexp.MustCompile(`^\s*\[\s*([A-Za-z0-9_.-]+)\s*\]\s*(#.*)?$`)
	typeKey      = regexp.MustCompile(`^(\s*)type\s*=`)
	vaultPathKey = regexp.MustCompile(`^\s*vault_path\s*=`)
	// vaultExample is the generated config's commented-out [vault] header.
	vaultExample = regexp.MustCompile(`^#\s*\[vault\]\s*$`)
)

// setAgentType rewrites the type line of the [agent] table, adding one at the
// top of the table when there is none.
func setAgentType(text, agentType string) (string, error) {
	lines := strings.Split(text, "\n")
	table, header := "", -1
	for i, line := range lines {
		if m := tableHeader.FindStringSubmatch(line); m != nil {
			table = m[1]
			if table == "agent" {
				header = i
			}
			continue
		}
		if table == "agent" {
			if m := typeKey.FindStringSubmatch(line); m != nil {
				lines[i] = fmt.Sprintf("%stype = %q", m[1], agentType)
				return strings.Join(lines, "\n"), nil
			}
		}
	}
	if header < 0 {
		return "", fmt.Errorf("edit config: no [agent] table to set the type in")
	}
	lines = append(lines[:header+1], append([]string{fmt.Sprintf("type = %q", agentType)}, lines[header+1:]...)...)
	return strings.Join(lines, "\n"), nil
}

// setVaultPath sets vault_path in the [vault] table. With no such table it
// makes one: in place of the generated config's commented-out example when
// there is one, else at the end of the file. The caller has checked there is
// no vault_path to keep.
func setVaultPath(text, vaultPath string) string {
	line := "vault_path = " + tomlLiteral(vaultPath)
	lines := strings.Split(text, "\n")
	table := ""
	for i, l := range lines {
		if m := tableHeader.FindStringSubmatch(l); m != nil {
			table = m[1]
			if table == "vault" {
				lines = append(lines[:i+1], append([]string{line}, lines[i+1:]...)...)
				return strings.Join(lines, "\n")
			}
			continue
		}
		// An empty vault_path = "" in [vault] is replaced where it stands.
		if table == "vault" && vaultPathKey.MatchString(l) {
			lines[i] = line
			return strings.Join(lines, "\n")
		}
	}
	for i, l := range lines {
		if vaultExample.MatchString(l) {
			lines = append(lines[:i], append([]string{"[vault]", line}, lines[i+1:]...)...)
			return strings.Join(lines, "\n")
		}
	}
	if !strings.HasSuffix(text, "\n") && text != "" {
		text += "\n"
	}
	return text + "\n[vault]\n" + line + "\n"
}

// tomlLiteral quotes s for TOML: a literal string, which needs no escaping of
// a Windows path's backslashes, unless s holds a quote of that kind.
func tomlLiteral(s string) string {
	if !strings.ContainsAny(s, "'\n") {
		return "'" + s + "'"
	}
	return fmt.Sprintf("%q", s)
}

// encodeFile is text as a whole file in enc, byte-order mark included.
func encodeFile(text string, enc textEncoding) []byte {
	var bom []byte
	switch enc {
	case encodingUTF8BOM:
		bom = bomUTF8
	case encodingUTF16LE:
		bom = bomUTF16LE
	case encodingUTF16BE:
		bom = bomUTF16BE
	}
	return append(append([]byte(nil), bom...), encodeText(text, enc)...)
}

// replaceFile writes data over path by way of a temporary file beside it, so a
// failure leaves the old file whole.
func replaceFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config.toml.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}
