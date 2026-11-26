package config

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/ollykeran/sshush/internal/utils"
)

// CoreSettings are the settings a front end may edit for the user in place of
// the file: what the agent serves and where, and where the vault is. They are
// the values as the file writes them, ~ and all. The [server] and [theme]
// tables, [agent].shell and [agent].windows_sshush are not among them, and
// WriteCoreSettingsToPath leaves them as they are.
type CoreSettings struct {
	AgentType  string   // [agent].type
	SocketPath string   // [agent].socket_path; "" leaves it out
	KeyPaths   []string // [agent].key_paths
	SearchDirs []string // [agent].search_dirs; empty leaves it out
	VaultPath  string   // [vault].vault_path; "" leaves it out
	// PageantEmulation is [agent].pageant_emulation, true when the file leaves
	// it out.
	PageantEmulation bool
}

// ReadCoreSettings reads the CoreSettings in the config file at path, as the
// file writes them.
func ReadCoreSettings(path string) (CoreSettings, error) {
	path = utils.ExpandHomeDirectory(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return CoreSettings{}, fmt.Errorf("read config: %w", err)
	}
	text, _, ok := decodeTextFile(data)
	if !ok {
		return CoreSettings{}, fmt.Errorf("config %s is not a text file sshush can read", utils.DisplayPath(path))
	}
	doc, err := decodeConfigDocument([]byte(text))
	if err != nil {
		return CoreSettings{}, fmt.Errorf("parse config: %w", err)
	}
	return coreOf(doc), nil
}

func coreOf(doc configDocument) CoreSettings {
	return CoreSettings{
		AgentType:  doc.Agent.Type,
		SocketPath: doc.Agent.SocketPath,
		KeyPaths:   doc.Agent.KeyPaths,
		SearchDirs: doc.Agent.SearchDirs,
		VaultPath:  doc.Vault.VaultPath,

		PageantEmulation: doc.Agent.pageantEmulation(),
	}
}

// Equal reports whether s and o say the same thing.
func (s CoreSettings) Equal(o CoreSettings) bool {
	return s.AgentType == o.AgentType && s.SocketPath == o.SocketPath && s.VaultPath == o.VaultPath &&
		s.PageantEmulation == o.PageantEmulation &&
		slices.Equal(s.KeyPaths, o.KeyPaths) && slices.Equal(s.SearchDirs, o.SearchDirs)
}

// WriteCoreSettingsToPath makes the config file at path say s. Like
// WriteAgentTypeToPath it edits the file's text, and only the lines of the
// settings that change, so the file's comments and every other setting
// survive. The result must load as a valid config that says s, or the file is
// left as it was.
func WriteCoreSettingsToPath(path string, s CoreSettings) error {
	return editConfigText(path, func(text string, doc configDocument) (string, error) {
		old := coreOf(doc)
		if s.AgentType != old.AgentType {
			text = setKey(text, "agent", "type", fmt.Sprintf("type = %q", s.AgentType))
		}
		if s.SocketPath != old.SocketPath {
			text = setKey(text, "agent", "socket_path", optionalLine("socket_path", s.SocketPath))
		}
		if !slices.Equal(s.KeyPaths, old.KeyPaths) {
			// An empty list is kept, not left out: it is how a keys agent
			// that serves no key files says so.
			text = setKey(text, "agent", "key_paths", "key_paths = "+tomlArray(s.KeyPaths))
		}
		if !slices.Equal(s.SearchDirs, old.SearchDirs) {
			line := ""
			if len(s.SearchDirs) > 0 {
				line = "search_dirs = " + tomlArray(s.SearchDirs)
			}
			text = setKey(text, "agent", "search_dirs", line)
		}
		if s.PageantEmulation != old.PageantEmulation {
			text = setKey(text, "agent", "pageant_emulation", fmt.Sprintf("pageant_emulation = %t", s.PageantEmulation))
		}
		if s.VaultPath != old.VaultPath {
			if old.VaultPath == "" && s.VaultPath != "" && !hasKey(text, "vault", "vault_path") {
				text = setVaultPath(text, s.VaultPath) // uses the generated example
			} else {
				text = setKey(text, "vault", "vault_path", optionalLine("vault_path", s.VaultPath))
			}
		}
		return text, nil
	}, func(edited configDocument) error {
		if !coreOf(edited).Equal(s) {
			return fmt.Errorf("edit config: the file does not say what was asked; edit %s by hand", utils.DisplayPath(path))
		}
		return nil
	})
}

// editConfigText reads the config file at path, has edit change its text,
// checks the result parses, passes check and loads as a valid config, and
// only then writes it back, in the encoding and with the line ends it had.
func editConfigText(path string, edit func(text string, doc configDocument) (string, error), check func(edited configDocument) error) error {
	path = utils.ExpandHomeDirectory(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s (run the app once to create it)", utils.DisplayPath(path))
		}
		return fmt.Errorf("read config: %w", err)
	}
	text, enc, ok := decodeTextFile(data)
	if !ok {
		return fmt.Errorf("config %s is not a text file sshush can edit", utils.DisplayPath(path))
	}
	doc, err := decodeConfigDocument([]byte(text))
	if err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	// Edited with \n line ends and put back as they were.
	crlf := strings.Contains(text, "\r\n")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text, err = edit(text, doc)
	if err != nil {
		return err
	}
	if crlf {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}

	edited, err := decodeConfigDocument([]byte(text))
	if err != nil {
		return fmt.Errorf("edit config: the result does not parse: %w", err)
	}
	if check != nil {
		if err := check(edited); err != nil {
			return err
		}
	}
	if _, err := documentToConfig(&edited); err != nil {
		return err
	}
	return replaceFile(path, encodeFile(text, enc))
}

// optionalLine is key = value, or "" to leave the key out when value is "".
func optionalLine(key, value string) string {
	if value == "" {
		return ""
	}
	return key + " = " + tomlLiteral(value)
}

// tomlArray writes values as a one-line TOML array of strings.
func tomlArray(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = tomlLiteral(v)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func keyPattern(key string) *regexp.Regexp {
	return regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)
}

// hasKey reports whether table sets key.
func hasKey(text, table, key string) bool {
	pat, current := keyPattern(key), ""
	for _, l := range strings.Split(text, "\n") {
		if m := tableHeader.FindStringSubmatch(l); m != nil {
			current = m[1]
			continue
		}
		if current == table && pat.MatchString(l) {
			return true
		}
	}
	return false
}

// setKey makes key in table say line, a whole "key = value" line, or with
// line "" takes the key out. A value that runs over several lines, as an
// array may, is replaced whole. A key that is not there yet goes first in its
// table, and a table that is not there yet at the end of the file.
func setKey(text, table, key, line string) string {
	lines := strings.Split(text, "\n")
	pat, current, header := keyPattern(key), "", -1
	for i := 0; i < len(lines); i++ {
		if m := tableHeader.FindStringSubmatch(lines[i]); m != nil {
			current = m[1]
			if current == table && header < 0 {
				header = i
			}
			continue
		}
		if current != table || !pat.MatchString(lines[i]) {
			continue
		}
		end := valueEnd(lines, i)
		var with []string
		if line != "" {
			// Keep the key's indent.
			indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " \t"))]
			with = []string{indent + line}
		}
		lines = slices.Concat(lines[:i], with, lines[end+1:])
		return strings.Join(lines, "\n")
	}
	if line == "" {
		return text
	}
	if header >= 0 {
		lines = slices.Concat(lines[:header+1], []string{line}, lines[header+1:])
		return strings.Join(lines, "\n")
	}
	if !strings.HasSuffix(text, "\n") && text != "" {
		text += "\n"
	}
	return text + "\n[" + table + "]\n" + line + "\n"
}

// valueEnd is the index of the line the value of the key on lines[start]
// ends on: the same line, unless it opens brackets it does not close there.
func valueEnd(lines []string, start int) int {
	depth := 0
	_, rest, _ := strings.Cut(lines[start], "=")
	for i := start; i < len(lines); i++ {
		if i > start {
			rest = lines[i]
		}
		depth = bracketDepth(rest, depth)
		if depth <= 0 {
			return i
		}
	}
	return len(lines) - 1
}

// bracketDepth adds the brackets s opens and closes to depth, skipping those
// in strings and comments.
func bracketDepth(s string, depth int) int {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote == '"' && c == '\\':
			i++ // an escape: the next byte is not a quote
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#':
			return depth
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
		}
	}
	return depth
}
