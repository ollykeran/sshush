package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/ollykeran/sshush/internal/style"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"golang.org/x/term"
)

var errPassphraseCancelled = errors.New("cancelled")

// stdinLineReader is a shared reader for non-TTY stdin so multiple readPassphrase
// calls consume one line each instead of buffering the whole stream in the first call.
var stdinLineReader struct {
	sync.Once
	r *bufio.Reader
}

// readPassphrase reads a line from stdin. If stdin is a TTY, runs a Bubble Tea
// prompt with theme-styled box and password input. The returned slice must be
// cleared by the caller (e.g. clearBytes).
func readPassphrase(prompt string) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		return readPassphraseStyled(prompt)
	}
	// stdin is a pipe (e.g. test or script): read a line without terminal ioctl.
	// Use a single shared reader so multiple prompts consume one line each.
	stdinLineReader.Do(func() { stdinLineReader.r = bufio.NewReader(os.Stdin) })
	fmt.Fprint(os.Stderr, prompt)
	line, err := stdinLineReader.r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(line, "\n")), nil
}

// passphraseModel is a Bubble Tea model for a single passphrase prompt with styled box.
type passphraseModel struct {
	prompt    string
	input     textinput.Model
	boxStyle  lipgloss.Style
	done      bool
	cancelled bool
}

func readPassphraseStyled(prompt string) ([]byte, error) {
	boxStyle, focusStyle, blurredStyle := style.StylesForInput()
	in := textinput.New()
	in.Placeholder = "••••••••"
	in.EchoMode = textinput.EchoPassword
	in.EchoCharacter = '•'
	in.SetWidth(40)
	st := in.Styles()
	st.Cursor.Color = style.InputCursorColor()
	st.Focused.Prompt = focusStyle
	st.Focused.Text = focusStyle
	st.Focused.Placeholder = focusStyle
	st.Blurred.Prompt = blurredStyle
	st.Blurred.Text = blurredStyle
	st.Blurred.Placeholder = blurredStyle
	in.SetStyles(st)
	m := passphraseModel{
		prompt:   prompt,
		input:    in,
		boxStyle: boxStyle,
	}
	prog := tea.NewProgram(&m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stderr))
	final, err := prog.Run()
	if err != nil {
		return nil, err
	}
	pm, ok := final.(*passphraseModel)
	if !ok {
		return nil, errors.New("passphrase prompt failed")
	}
	if pm.cancelled {
		return nil, errPassphraseCancelled
	}
	return []byte(pm.input.Value()), nil
}

func (m *passphraseModel) Init() tea.Cmd {
	return tea.Batch(m.input.Focus(), textinput.Blink)
}

func (m *passphraseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.done || m.cancelled {
		return m, tea.Quit
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "enter":
			m.done = true
			return m, tea.Quit
		case "esc", "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *passphraseModel) View() tea.View {
	label := m.prompt
	if label != "" && !strings.HasSuffix(label, " ") {
		label += " "
	}
	inner := label + m.input.View()
	v := tea.NewView(m.boxStyle.Render(inner))
	// Alternate screen buffer so teardown does not overlap the next boxed message on stderr.
	v.AltScreen = true
	return v
}
