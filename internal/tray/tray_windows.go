package tray

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/systray"
	"github.com/ollykeran/sshush/internal/selftest"
	"github.com/ollykeran/sshush/internal/vaultops"
	"golang.org/x/sys/windows"
)

//go:embed icons/unreachable.ico
var iconUnreachable []byte

//go:embed icons/keys.ico
var iconKeys []byte

//go:embed icons/locked.ico
var iconLocked []byte

//go:embed icons/unlocked.ico
var iconUnlocked []byte

// pollInterval is how stale the tray may be about a change made elsewhere,
// such as 'sshush lock' in a terminal. A probe is one local pipe round-trip.
const pollInterval = 2 * time.Second

// Options are how the tray was started.
type Options struct {
	// OpenWindow opens the main window as well, as a launch from the Start
	// menu should; a start at sign-in leaves it closed.
	OpenWindow bool
	// KeyPaths are the config's key files, where a key in an agent that is
	// not a vault may have come from.
	KeyPaths []string
}

// Run shows the tray icon and blocks until the user picks Quit. Quitting the
// tray never touches the agent.
func Run(env vaultops.Env, opts Options) {
	t := &tray{env: env, kick: make(chan struct{}, 1), openOnReady: opts.OpenWindow, keyPaths: opts.KeyPaths}
	refreshStartup()
	initTheme()
	systray.Run(t.ready, func() {})
}

type tray struct {
	// env is read through environment: switching the agent between a vault
	// and key files changes it while other goroutines use it.
	envMu     sync.RWMutex
	env       vaultops.Env
	kick      chan struct{}
	prompting atomic.Bool
	// openOnReady opens the main window once the icon is up.
	openOnReady bool
	keyPaths    []string
}

func (t *tray) ready() {
	systray.SetIcon(iconUnreachable)
	systray.SetTooltip("sshush")
	// A click on the icon opens the main window; the menu is on right-click.
	systray.SetOnTapped(t.openMain)
	if t.openOnReady {
		t.openMain()
	}
	views := make(chan View)
	go Poll(context.Background(), t.environment().SocketPath, !t.environment().AgentUnmanaged, pollInterval, t.kick, views)
	go func() {
		for v := range views {
			t.render(v)
		}
	}()
}

// environment is the Env the tray's operations run in.
func (t *tray) environment() vaultops.Env {
	t.envMu.RLock()
	defer t.envMu.RUnlock()
	return t.env
}

// reloadEnv rebuilds the Env from the config file, after a change to it. The
// agent's pipe stays the one the tray started with: the poller watches it.
func (t *tray) reloadEnv() error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	t.envMu.Lock()
	defer t.envMu.Unlock()
	t.env = vaultops.EnvFromConfig(cfg, t.env.SocketPath)
	return nil
}

// refresh asks the poller to probe now rather than at its next tick.
func (t *tray) refresh() {
	select {
	case t.kick <- struct{}{}:
	default: // a probe is already pending
	}
}

// render rebuilds the whole menu: the agent and its keys, to read, and Open
// for everything else. The poller sends only changed Views, so this runs
// rarely and never under an open menu that is still accurate.
func (t *tray) render(v View) {
	notifyMainWindow()
	systray.SetIcon(iconFor(v.Icon))
	systray.SetTooltip(v.Tooltip)
	systray.ResetMenu()

	systray.AddMenuItem(v.Heading, "").Disable()
	if v.Status != "" {
		systray.AddMenuItem(v.Status, "").Disable()
	}
	for _, k := range v.Keys {
		systray.AddMenuItem("    "+k.Label, "").Disable()
	}
	systray.AddSeparator()
	onClick(systray.AddMenuItem("Open sshush", "Manage the agent and its keys"), t.openMain)
	onClick(systray.AddMenuItem("Quit", "Close the tray; the agent keeps running"), systray.Quit)
}

// onClick runs f for each click on item. The goroutine ends when ResetMenu
// removes the item and closes its channel.
func onClick(item *systray.MenuItem, f func()) {
	go func() {
		for range item.ClickedCh {
			f()
		}
	}()
}

// errPromptOpen reports that an unlock prompt was already open.
var errPromptOpen = errors.New("tray: an unlock prompt is already open")

// promptUnlock asks for the vault passphrase until it unlocks the vault or
// the user cancels, which returns errCancelled. The dialog offers the
// recovery phrase instead, for a forgotten passphrase. A vault found already
// unlocked is not an error.
func (t *tray) promptUnlock() error {
	if !t.prompting.CompareAndSwap(false, true) {
		return errPromptOpen
	}
	defer t.prompting.Store(false)
	spec := inputSpec{
		title:      "sshush",
		message:    "Enter the vault passphrase to unlock the sshush agent.",
		okLabel:    "Unlock",
		extraLabel: "Use &recovery phrase…",
		masked:     true,
	}
	for {
		pass, button, err := promptInput(mainWindowHwnd.Load(), spec)
		if err != nil {
			return err
		}
		switch button {
		case idCancel:
			return errCancelled
		case idExtra:
			return t.promptRecovery()
		}
		err = vaultops.UnlockPassphrase(t.environment(), pass)
		clear(pass)
		switch vaultops.CodeOf(err) {
		case vaultops.CodeWrongPassphrase:
			spec.errText = "Wrong passphrase. Try again."
			continue
		case vaultops.CodeNotLocked:
			return nil
		}
		return err
	}
}

// promptRecovery asks for the vault's recovery phrase until it unlocks the
// vault or the user cancels.
func (t *tray) promptRecovery() error {
	spec := inputSpec{
		title:   "Unlock with recovery phrase",
		message: "Enter the 24 words you were given when the vault was created, separated by spaces.",
		okLabel: "Unlock",
	}
	for {
		b, button, err := promptInput(mainWindowHwnd.Load(), spec)
		if err != nil {
			return err
		}
		if button != idOK {
			return errCancelled
		}
		phrase := strings.Join(strings.Fields(strings.ToLower(string(b))), " ")
		clear(b)
		err = vaultops.UnlockRecovery(t.environment(), phrase)
		switch {
		case err == nil, vaultops.CodeOf(err) == vaultops.CodeNotLocked:
			return nil
		case vaultops.CodeOf(err) == vaultops.CodeWrongPassphrase:
			spec.errText = "That phrase does not unlock this vault. Check each word."
			continue
		}
		return err
	}
}

// askNewPassphrase asks for a new vault passphrase and for it again, until
// the two match and are long enough, or the user cancels.
func askNewPassphrase(owner uintptr) ([]byte, error) {
	first := inputSpec{
		title:   "Create vault",
		message: "Choose a passphrase for the vault. You will type it to unlock your keys, so make it one you will remember.",
		okLabel: "Next",
		masked:  true,
	}
	for {
		pass, button, err := promptInput(owner, first)
		if err != nil {
			return nil, err
		}
		if button != idOK {
			return nil, errCancelled
		}
		if problem := newPassphraseProblem(pass, nil); problem != "" {
			clear(pass)
			first.errText = problem
			continue
		}
		confirm, button, err := promptInput(owner, inputSpec{
			title: "Create vault", message: "Type the passphrase again.", okLabel: "Create", masked: true,
		})
		if err != nil || button != idOK {
			clear(pass)
			if err == nil {
				err = errCancelled
			}
			return nil, err
		}
		problem := newPassphraseProblem(pass, confirm)
		clear(confirm)
		if problem == "" {
			return pass, nil
		}
		clear(pass)
		first.errText = problem
	}
}

// askKeyPassphrase prompts for a key file's passphrase in the unlock dialog.
func askKeyPassphrase(path string, wrong bool) ([]byte, error) {
	return promptPassphrase("Enter the passphrase for the key file "+filepath.Base(path)+".", wrong)
}

func selftestText(r selftest.Report) string {
	var b strings.Builder
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "%s:  %s\n", c.Name, c.Detail)
		if c.Hint != "" {
			fmt.Fprintf(&b, "        %s\n", c.Hint)
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func selftestIcon(s selftest.Status) uint32 {
	switch s {
	case selftest.OK:
		return mbIconInformation
	case selftest.Warn:
		return mbIconWarning
	default:
		return mbIconError
	}
}

const (
	mbIconInformation = windows.MB_ICONINFORMATION
	mbIconWarning     = windows.MB_ICONWARNING
	mbIconError       = windows.MB_ICONERROR
)

func showError(text string) { messageBox(text, mbIconError) }

// messageBox shows text with flags, brought to the front: the tray has no
// window of its own for it to sit above.
func messageBox(text string, flags uint32) {
	title, _ := windows.UTF16PtrFromString("sshush")
	body, _ := windows.UTF16PtrFromString(text)
	_, _ = windows.MessageBox(0, body, title, flags|windows.MB_SETFOREGROUND)
}

func iconFor(i Icon) []byte {
	switch i {
	case IconKeys:
		return iconKeys
	case IconLocked:
		return iconLocked
	case IconUnlocked:
		return iconUnlocked
	default:
		return iconUnreachable
	}
}
