package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
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

// ErrPassphrasesDoNotMatch is returned when confirmation does not match the first entry.
var ErrPassphrasesDoNotMatch = errors.New("passphrases do not match")

// ClearBytes overwrites b with zeros. Use after handling sensitive passphrase material.
func ClearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// ReadPassphraseWithConfirm prompts twice (with two blank lines between, matching vault init UX)
// and returns the passphrase if both entries match. The confirmation buffer is cleared before return.
// On mismatch, both buffers are cleared. Caller should ClearBytes the returned slice when done.
func ReadPassphraseWithConfirm(firstPrompt, confirmPrompt string) ([]byte, error) {
	passphrase, err := readPassphrase(firstPrompt)
	if err != nil {
		return nil, err
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr)
	confirm, err := readPassphrase(confirmPrompt)
	if err != nil {
		ClearBytes(passphrase)
		return nil, err
	}
	if string(passphrase) != string(confirm) {
		ClearBytes(passphrase)
		ClearBytes(confirm)
		return nil, ErrPassphrasesDoNotMatch
	}
	ClearBytes(confirm)
	return passphrase, nil
}

// stdinLineReader is a shared reader for non-TTY stdin so multiple readPassphrase
// calls consume one line each instead of buffering the whole stream in the first call.
var stdinLineReader struct {
	sync.Once
	r *bufio.Reader
}

// readPassphrase reads a line from stdin. If stdin is a TTY, runs a Bubble Tea
// prompt with theme-styled box and password input. The returned slice must be
// cleared by the caller (e.g. ClearBytes).
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
	prompt        string
	input         textinput.Model
	boxStyle      lipgloss.Style
	done          bool
	cancelled     bool
	lastLineCount int // height of last View render; used to erase the block after Run (see bubbletea close + EraseScreenBelow)
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
	pm, _ := final.(*passphraseModel)
	if pm != nil && pm.lastLineCount > 0 {
		eraseInlineBlock(os.Stderr, pm.lastLineCount)
	}
	if err != nil {
		return nil, err
	}
	if pm == nil {
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
	rendered := m.boxStyle.Render(inner)
	m.lastLineCount = lipgloss.Height(rendered)
	v := tea.NewView(rendered)
	// Main buffer (not alt screen): prompt stays in normal scrollback like Unix passwd.
	return v
}

// eraseInlineBlock clears n lines ending at the cursor row. After a non-alt Bubble Tea
// program exits, the cursor sits on the bottom frame line and close() may use
// EraseScreenBelow, which strips the bottom border; clearing the whole block removes
// the passphrase UI so the next stderr line prints cleanly.
func eraseInlineBlock(w io.Writer, n int) {
	for i := 0; i < n; i++ {
		fmt.Fprint(w, "\r\033[2K")
		if i < n-1 {
			fmt.Fprint(w, "\033[1A")
		}
	}
}
