package config

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"text/template"

	"github.com/ollykeran/sshush/internal/openssh"
	"github.com/ollykeran/sshush/internal/platform"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/theme"
	"github.com/ollykeran/sshush/internal/utils"
)

//go:embed default_config.toml.tmpl
var defaultConfigTemplateFS embed.FS

var defaultConfigTemplate = template.Must(
	template.ParseFS(defaultConfigTemplateFS, "default_config.toml.tmpl"),
)

// defaultConfigTemplateData is input for default_config.toml.tmpl.
type defaultConfigTemplateData struct {
	SocketPath    string
	KeyPathsTOML  string
	Shell         string
	ServerHostKey string // where the server keeps its host key when [server].host_key is unset
	ServerLogFile string // where the server logs when [server].log_file is unset
	ThemeText     string
	ThemeFocus    string
	ThemeAccent   string
	ThemeError    string
	ThemeWarning  string
}

func findDefaultKeys() []string {
	const sshHome = "~/.ssh"

	sshPath := utils.ExpandHomeDirectory(sshHome)

	seen := make(map[string]bool)
	var paths []string

	addPath := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if seen[abs] {
			return
		}
		if _, err := os.Stat(abs); err != nil {
			return
		}
		seen[abs] = true
		paths = append(paths, abs)
	}

	files, err := os.ReadDir(sshPath)
	if err != nil {
		return nil
	}
	for _, f := range files {
		if f.IsDir() || strings.HasSuffix(f.Name(), ".pub") {
			continue
		}
		path := filepath.Join(sshPath, f.Name())
		if seen[path] {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			continue
		}
		if _, err := openssh.ParsePrivateKeyBlob(data); err == nil {
			addPath(path)
		}
	}
	return paths
}

// keyPathsToTOMLArray formats discovered key paths as a TOML array literal (tilde-prefixed when under $HOME).
func keyPathsToTOMLArray(keyPaths []string) string {
	if len(keyPaths) == 0 {
		return "[]"
	}
	home, _ := os.UserHomeDir()
	quoted := make([]string, len(keyPaths))
	for i, p := range keyPaths {
		if strings.HasPrefix(p, home) {
			p = "~" + strings.TrimPrefix(p, home)
		}
		// Forward slashes: a backslash starts an escape in a TOML string, and
		// Windows takes either separator.
		quoted[i] = `"` + filepath.ToSlash(p) + `"`
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// renderDefaultConfigBytes renders the embedded default config template. Exposed for tests.
func renderDefaultConfigBytes(socketPath string, keyPaths []string, shell string, def theme.Theme) ([]byte, error) {
	data := defaultConfigTemplateData{
		SocketPath:    socketPath,
		KeyPathsTOML:  keyPathsToTOMLArray(keyPaths),
		Shell:         shell,
		ServerHostKey: utils.ContractHomeDirectory(platform.ServerHostKeyPath("")),
		ServerLogFile: utils.ContractHomeDirectory(platform.ServerLogPath("")),
		ThemeText:     def.Text,
		ThemeFocus:    def.Focus,
		ThemeAccent:   def.Accent,
		ThemeError:    def.Error,
		ThemeWarning:  def.Warning,
	}
	var buf bytes.Buffer
	if err := defaultConfigTemplate.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// StandardConfigFile returns the absolute path to the default config file.
func StandardConfigFile() string {
	return platform.DefaultConfigPath()
}

// WriteDefaultConfigFile renders the default config template and writes it to path.
// Parent directories are created as needed. If overwrite is false and path already
// exists, it returns an error.
func WriteDefaultConfigFile(path string, overwrite bool) error {
	if _, err := os.Stat(path); err == nil && !overwrite {
		return style.NewOutput().
			Error("config file already exists: " + utils.DisplayPath(path)).
			Info("use --force to overwrite").
			AsError()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: create directory %s: %w", dir, err)
	}
	keyPaths := findDefaultKeys()
	socketDisplay := utils.ContractHomeDirectory(platform.DefaultSocketPath())
	def := theme.DefaultTheme()
	data, err := renderDefaultConfigBytes(socketDisplay, keyPaths, platform.DetectShell(), def)
	if err != nil {
		return fmt.Errorf("config: render default config: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// CreateDefaultConfig creates the default config directory and config.toml if the file
// does not exist yet.
func CreateDefaultConfig() error {
	p := platform.DefaultConfigPath()
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	if err := WriteDefaultConfigFile(p, false); err != nil {
		return fmt.Errorf("config: write default config: %w", err)
	}
	fmt.Fprintln(os.Stderr, "Default config created")
	return nil
}

// AddEvalToShell adds the agent startup snippet to the preferred shell startup file
// (see platform.ShellSetupForAutoSetup): eval $(sshush) appended to the bash/zsh rc file,
// or a dedicated conf.d/sshush.fish for fish. Creates the file if it does not exist.
func AddEvalToShell() error {
	setup, ok := platform.ShellSetupForAutoSetup()
	if !ok {
		return style.NewOutput().Error("cannot determine shell rc file (no home directory)").AsError()
	}
	rcPath := setup.RcPath

	if _, err := os.Stat(rcPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(rcPath), 0o755); err != nil {
			return style.NewOutput().Error("failed to create " + filepath.Dir(rcPath) + ": " + err.Error()).AsError()
		}
		header := "# sshush: start agent in new shells\n"
		if err := os.WriteFile(rcPath, []byte(header+setup.Snippet), 0o644); err != nil {
			return style.NewOutput().Error("failed to create " + rcPath + ": " + err.Error()).AsError()
		}
		return nil
	} else if err != nil {
		return style.NewOutput().Error("cannot read " + rcPath + ": " + err.Error()).AsError()
	}

	f, err := os.OpenFile(rcPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return style.NewOutput().Error("failed to open " + rcPath + ": " + err.Error()).AsError()
	}
	defer f.Close()

	if _, err := f.WriteString(setup.Snippet); err != nil {
		return style.NewOutput().Error("failed to write " + rcPath + ": " + err.Error()).AsError()
	}
	return nil
}

// SetupConfig ensures default config exists and the agent startup snippet is added to the shell rc if needed.
// Call from root PersistentPreRunE or main before loading config.
func SetupConfig() {
	expanded := platform.DefaultConfigPath()

	firstRun := false
	if _, err := os.Stat(expanded); os.IsNotExist(err) {
		firstRun = CreateDefaultConfig() == nil
	}
	if firstRun && goruntime.GOOS == "windows" {
		// The config just written names the default pipe, so that is the one to point ssh at.
		setupSSHIdentityAgent(utils.ExpandHomeDirectory("~/.ssh"), platform.DefaultSocketPath(), detectOtherSSHAgent(), os.Stderr)
	}

	setup, ok := platform.ShellSetupForAutoSetup()
	if !ok {
		return
	}
	if setup.PowerShell {
		setupPowerShellProfile(setup, firstRun, os.Stderr)
		return
	}
	if setup.Fish {
		// conf.d/sshush.fish is ours: once it exists (even edited or emptied) leave it alone.
		if _, err := os.Stat(setup.RcPath); !os.IsNotExist(err) {
			return
		}
		// Respect a hand-written start line in config.fish.
		configFish := filepath.Join(filepath.Dir(filepath.Dir(setup.RcPath)), "config.fish")
		if content, err := os.ReadFile(configFish); err == nil && platform.FishConfigStartsAgent(string(content)) {
			return
		}
		_ = AddEvalToShell()
		return
	}
	content, err := os.ReadFile(setup.RcPath)
	if err != nil && !os.IsNotExist(err) {
		return
	}
	if err == nil && strings.Contains(string(content), "eval $(sshush)") {
		return
	}
	_ = AddEvalToShell()
}

// setupPowerShellProfile adds the agent startup line to an existing PowerShell
// profile that does not start sshush yet. It never creates the profile (see
// platform.ShellSetup.PowerShell); when there is none it says, once — on the run
// that created the config — what to add, and leaves the choice to the user.
//
// The profile is read and added to in its own encoding and with its own line
// ending (see textEncoding for why that cannot be assumed to be UTF-8). One
// that does not decode cleanly is treated like one that is missing: sshush
// cannot tell whether it already starts the agent, and anything appended could
// break it, so it is left alone and the line is shown instead.
func setupPowerShellProfile(setup platform.ShellSetup, firstRun bool, hint io.Writer) {
	content, err := os.ReadFile(setup.RcPath)
	if os.IsNotExist(err) {
		if firstRun {
			fmt.Fprintln(hint, "To start sshush in new PowerShell windows, add this line to "+setup.RcPath+":")
			fmt.Fprint(hint, "  "+setup.Snippet)
		}
		return
	}
	if err != nil {
		return
	}
	text, enc, ok := decodeTextFile(content)
	if !ok {
		if firstRun {
			fmt.Fprintln(hint, "Could not read "+setup.RcPath+" as UTF-8 or UTF-16 text, so it was left alone.")
			fmt.Fprintln(hint, "To start sshush in new PowerShell windows, add this line to it:")
			fmt.Fprint(hint, "  "+setup.Snippet)
		}
		return
	}
	if platform.PowerShellProfileStartsAgent(text) {
		return
	}
	addition := "# sshush: start agent in new shells\n" + setup.Snippet
	if text != "" && !strings.HasSuffix(text, "\n") {
		addition = "\n" + addition
	}
	if strings.Contains(text, "\r\n") {
		addition = strings.ReplaceAll(addition, "\n", "\r\n")
	}
	f, err := os.OpenFile(setup.RcPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(encodeText(addition, enc))
}

// opensshAgentService is the service name of Windows' own agent, shown in the
// Services list as "OpenSSH Authentication Agent".
const opensshAgentService = "ssh-agent"

// otherSSHAgent is what was seen on this machine that suggests ssh already
// talks to an agent other than sshush. It is gathered by the caller and passed
// to setupSSHIdentityAgent, which keeps that function free of Windows calls.
type otherSSHAgent struct {
	// ServiceRunning is true when Windows' "OpenSSH Authentication Agent"
	// service is running: the agent ssh.exe uses when nothing says otherwise.
	ServiceRunning bool
	// EnvAuthSock is SSH_AUTH_SOCK in this process's environment.
	EnvAuthSock string
	// PersistedAuthSock is the SSH_AUTH_SOCK new processes are given (see
	// platform.PersistedEnv), which a window opened before it was set lacks.
	PersistedAuthSock string
}

// detectOtherSSHAgent looks for the signs of another agent. It is a variable so
// tests of SetupConfig do not depend on what the machine running them has.
var detectOtherSSHAgent = func() otherSSHAgent {
	persisted, _ := platform.PersistedEnv("SSH_AUTH_SOCK")
	return otherSSHAgent{
		ServiceRunning:    platform.WindowsServiceRunning(opensshAgentService),
		EnvAuthSock:       os.Getenv("SSH_AUTH_SOCK"),
		PersistedAuthSock: persisted,
	}
}

// signals describes what in o points at an agent other than the one on pipe,
// one phrase per sign, for the hint. SSH_AUTH_SOCK naming sshush's own pipe is
// not a sign of anything: it is what the PowerShell profile line sets.
func (o otherSSHAgent) signals(pipe string) []string {
	var seen []string
	if o.ServiceRunning {
		seen = append(seen, `the Windows "OpenSSH Authentication Agent" service is running`)
	}
	env, persisted := strings.TrimSpace(o.EnvAuthSock), strings.TrimSpace(o.PersistedAuthSock)
	if env != "" && !samePipe(env, pipe) {
		seen = append(seen, "SSH_AUTH_SOCK is set to "+env)
	}
	if persisted != "" && !samePipe(persisted, pipe) && persisted != env {
		seen = append(seen, "SSH_AUTH_SOCK is set to "+persisted+" for new programs")
	}
	return seen
}

// samePipe reports whether a and b name the same pipe. Windows ignores case in
// pipe names and takes either slash, and ssh's config needs forward ones, so
// the one pipe is commonly written several ways.
func samePipe(a, b string) bool {
	norm := func(s string) string { return strings.ReplaceAll(strings.TrimSpace(s), "/", `\`) }
	return strings.EqualFold(norm(a), norm(b))
}

// setupSSHIdentityAgent names the agent's pipe in sshDir's config file, so ssh
// finds sshush even where SSH_AUTH_SOCK is not set (cmd, Git, a shell with no
// profile). It does nothing without an existing sshDir, and leaves alone a
// config that already sets IdentityAgent anywhere: that is the user's choice of
// agent. The block goes last, where ssh reads it after every more specific Host.
//
// A config without IdentityAgent is not proof that no agent was chosen, though:
// ssh.exe finds Windows' own agent with no configuration at all, and any agent
// can be named by SSH_AUTH_SOCK. IdentityAgent overrides both, so writing it
// would quietly take ssh away from an agent that is working. When other shows
// either sign, the config is left alone and the block is printed for the user
// to add if sshush is the agent they want.
func setupSSHIdentityAgent(sshDir, pipe string, other otherSSHAgent, note io.Writer) {
	if st, err := os.Stat(sshDir); err != nil || !st.IsDir() {
		return
	}
	path := filepath.Join(sshDir, "config")
	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return
	}
	if sshConfigSetsIdentityAgent(string(content)) {
		return
	}
	// Forward slashes: ssh treats a backslash in its config as an escape.
	block := "Host *\n    IdentityAgent " + strings.ReplaceAll(pipe, `\`, "/") + "\n"
	if seen := other.signals(pipe); len(seen) > 0 {
		fmt.Fprintln(note, "Another ssh agent appears to be in use ("+strings.Join(seen, "; ")+"), so "+utils.DisplayPath(path)+" was left alone.")
		fmt.Fprintln(note, "To have ssh use sshush instead, add this to that file:")
		fmt.Fprint(note, "  "+strings.ReplaceAll(strings.TrimSuffix(block, "\n"), "\n", "\n  ")+"\n")
		return
	}
	addition := "# sshush: use the sshush agent\n" + block
	if len(content) > 0 {
		if content[len(content)-1] != '\n' {
			addition = "\n" + addition
		}
		addition = "\n" + addition
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.WriteString(addition); err == nil {
		fmt.Fprintln(note, "Added IdentityAgent for sshush to "+utils.DisplayPath(path))
	}
}

// sshConfigSetsIdentityAgent reports whether ssh config content has an
// IdentityAgent line that is not commented out.
func sshConfigSetsIdentityAgent(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "identityagent") {
			return true
		}
	}
	return false
}
