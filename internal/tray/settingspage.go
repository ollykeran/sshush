package tray

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/vault"
)

// The Settings page edits the parts of config.toml the tray and the agent
// use, so nobody has to edit the file by hand: what the agent serves, where
// it listens, its key files, the folders searched for more, and where the
// vault is. Every change is checked here before Save is offered, and again
// by config.WriteCoreSettingsToPath, which writes only a file that loads.
// The SSH server's settings and the TUI's theme are left to the file. What
// the page shows and allows is worked out here, so it is tested on every OS;
// settings_windows_page.go only draws it.

// Settings page item IDs, apart from the main window's.
const (
	idSetAgentHeader = 400 + iota
	idSetTypeTitle
	idSetTypeCaption
	idSetType
	idSetSocketTitle
	idSetSocket
	idSetSocketDefault
	idSetSocketCaption
	idSetVaultHeader
	idSetVaultTitle
	idSetVaultPath
	idSetVaultBrowse
	idSetVaultCaption
	idSetTrayHeader
	idSetKeysHeader
	idSetKeysCaption
	idSetKeysList
	idSetKeysAdd
	idSetKeysRemove
	idSetDirsHeader
	idSetDirsCaption
	idSetDirsList
	idSetDirsAdd
	idSetDirsRemove
	idSetSave
	idSetDiscard
)

// agentTypeChoice is one of the Settings page's choices for [agent].type.
type agentTypeChoice struct {
	value, label string
}

// agentTypeChoices are what the agent can serve, as the page offers them.
// "windows" only means something inside WSL, so it is offered only to a
// file that already says it.
func agentTypeChoices(current string) []agentTypeChoice {
	c := []agentTypeChoice{
		{config.AgentTypeKeys, "Key files"},
		{config.AgentTypeVault, "The vault"},
		{config.AgentTypeExternal, "Another agent"},
	}
	if current == config.AgentTypeWindows {
		c = append(c, agentTypeChoice{config.AgentTypeWindows, "The Windows agent (WSL)"})
	}
	return c
}

// Captions the Settings page shows while a field has no problem.
var settingsCaptions = map[uint16]string{
	idSetTypeCaption:   "Where ssh gets its keys from.",
	idSetSocketCaption: `The pipe ssh reaches the agent on, such as \\.\pipe\openssh-ssh-agent.`,
	idSetVaultCaption:  "The vault file. It need not exist yet: create it on the Vault page.",
	idSetKeysCaption:   "Loaded when the agent serves key files.",
	idSetDirsCaption:   "Also searched for keys by the TUI and 'sshush find', besides your .ssh folder.",
}

// FieldProblem is what is wrong with one of the Settings page's fields. A
// blocker stops the settings being saved; anything else is a warning.
type FieldProblem struct {
	// Field is the ID of the caption the problem is shown in, in place of
	// what it says normally.
	Field   uint16
	Text    string
	Blocker bool
}

// settingsProblems checks s, most important first.
func settingsProblems(s config.CoreSettings) []FieldProblem {
	var out []FieldProblem
	block := func(field uint16, text string) { out = append(out, FieldProblem{field, text, true}) }
	warn := func(field uint16, text string) { out = append(out, FieldProblem{field, text, false}) }

	valid := slices.ContainsFunc(agentTypeChoices(s.AgentType), func(c agentTypeChoice) bool { return c.value == s.AgentType })
	if !valid {
		block(idSetTypeCaption, "Choose what the agent serves.")
	}

	socket := strings.TrimSpace(s.SocketPath)
	switch {
	case socket == "" && s.AgentType != config.AgentTypeExternal:
		block(idSetSocketCaption, "The agent needs a pipe to listen on.")
	case socket != "" && !isPipeName(socket) && !isAbsPath(socket):
		block(idSetSocketCaption, `Use a pipe name such as \\.\pipe\sshush, or a full path.`)
	case socket != s.SocketPath:
		block(idSetSocketCaption, "Remove the spaces at either end.")
	}

	vaultPath := strings.TrimSpace(s.VaultPath)
	switch {
	case vaultPath == "" && s.AgentType == config.AgentTypeVault:
		block(idSetVaultCaption, "An agent that serves the vault needs a vault file.")
	case vaultPath == "":
	case vaultPath != s.VaultPath:
		block(idSetVaultCaption, "Remove the spaces at either end.")
	case !isAbsPath(vaultPath):
		block(idSetVaultCaption, "Use a full path, such as one Browse picks.")
	default:
		file := vault.ResolveToFile(utils.ExpandHomeDirectory(vaultPath))
		if _, err := os.Stat(file); err != nil && s.AgentType == config.AgentTypeVault {
			warn(idSetVaultCaption, "There is no vault there yet: the agent waits until you create it on the Vault page.")
		} else if err != nil && !isDirPath(filepath.Dir(file)) {
			warn(idSetVaultCaption, "The folder does not exist yet; it is made when the vault is created.")
		}
	}

	if missing := missingPaths(s.KeyPaths, false); len(missing) > 0 {
		warn(idSetKeysCaption, notFound(missing, "file"))
	}
	if missing := missingPaths(s.SearchDirs, true); len(missing) > 0 {
		warn(idSetDirsCaption, notFound(missing, "folder"))
	}
	return out
}

// blocked reports whether any of problems stops a save.
func blocked(problems []FieldProblem) bool {
	return slices.ContainsFunc(problems, func(p FieldProblem) bool { return p.Blocker })
}

// settingsCaption is what the caption field shows: its first problem, or
// what it says normally.
func settingsCaption(field uint16, problems []FieldProblem) (text string, problem *FieldProblem) {
	for i := range problems {
		if problems[i].Field == field {
			return problems[i].Text, &problems[i]
		}
	}
	return settingsCaptions[field], nil
}

// needsRestart reports whether going from old to s changes what the running
// agent does, so it must restart to apply it. The search folders are only
// read when keys are looked for.
func needsRestart(old, s config.CoreSettings) bool {
	old.SearchDirs, s.SearchDirs = nil, nil
	return !old.Equal(s)
}

// settingsSavedStatus is the status line after a save.
func settingsSavedStatus(restarted bool) string {
	if restarted {
		return "Saved the settings and restarted the agent with them."
	}
	return "Saved the settings."
}

// addPaths adds paths to list, skipping any it has already, and reports how
// many it added.
func addPaths(list []string, paths ...string) ([]string, int) {
	n := 0
	for _, p := range paths {
		if p == "" || slices.ContainsFunc(list, func(q string) bool { return samePathText(p, q) }) {
			continue
		}
		list = append(list, p)
		n++
	}
	return list, n
}

// samePathText reports whether a and b name the same path as written, ~ and
// case aside.
func samePathText(a, b string) bool {
	return strings.EqualFold(filepath.Clean(utils.ExpandHomeDirectory(a)), filepath.Clean(utils.ExpandHomeDirectory(b)))
}

// homePath writes path under the home folder with ~, as the generated config
// does, so the file reads the same on any PC.
func homePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
		return "~/" + filepath.ToSlash(rel)
	}
	return path
}

func isPipeName(p string) bool {
	p = strings.ReplaceAll(p, "/", `\`)
	return strings.HasPrefix(strings.ToLower(p), `\\.\pipe\`) && len(p) > len(`\\.\pipe\`)
}

// isAbsPath reports whether p is a full path on any OS sshush runs on, ~
// counting as one.
func isAbsPath(p string) bool {
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) || filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
		return true
	}
	// A Windows drive path, or a share, checked here so the page's rules are
	// the same on every OS its tests run on.
	return (len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/')) || strings.HasPrefix(p, `\\`)
}

func isDirPath(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// missingPaths is the paths in list not found as a file, or with dirs as a
// folder.
func missingPaths(list []string, dirs bool) []string {
	var out []string
	for _, p := range list {
		fi, err := os.Stat(utils.ExpandHomeDirectory(p))
		if err != nil || fi.IsDir() != dirs {
			out = append(out, p)
		}
	}
	return out
}

// notFound says which of paths were not found.
func notFound(paths []string, what string) string {
	if len(paths) == 1 {
		return fmt.Sprintf("Not found: %s. It is skipped until it is back.", filepath.Base(paths[0]))
	}
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Base(p)
	}
	return fmt.Sprintf("%d %ss not found: %s. They are skipped until they are back.", len(paths), what, strings.Join(names, ", "))
}
