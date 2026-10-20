package tray

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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

// Run shows the tray icon and blocks until the user picks Quit. Quitting the
// tray never touches the agent.
func Run(env vaultops.Env) {
	t := &tray{env: env, kick: make(chan struct{}, 1)}
	initTheme()
	systray.Run(t.ready, func() {})
}

type tray struct {
	env       vaultops.Env
	kick      chan struct{}
	prompting atomic.Bool
}

func (t *tray) ready() {
	systray.SetIcon(iconUnreachable)
	systray.SetTooltip("sshush")
	// A click on the icon opens the main window; the menu is on right-click.
	systray.SetOnTapped(t.openMain)
	views := make(chan View)
	go Poll(context.Background(), t.env.SocketPath, !t.env.AgentUnmanaged, pollInterval, t.kick, views)
	go func() {
		for v := range views {
			t.render(v)
		}
	}()
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
// the user cancels, which returns errCancelled. A vault found already
// unlocked is not an error.
func (t *tray) promptUnlock() error {
	if !t.prompting.CompareAndSwap(false, true) {
		return errPromptOpen
	}
	defer t.prompting.Store(false)
	wrong := false
	for {
		pass, err := promptPassphrase("Enter the vault passphrase to unlock the sshush agent.", wrong)
		if err != nil {
			return err
		}
		err = vaultops.UnlockPassphrase(t.env, pass)
		clear(pass)
		switch vaultops.CodeOf(err) {
		case vaultops.CodeWrongPassphrase:
			wrong = true
			continue
		case vaultops.CodeNotLocked:
			return nil
		}
		return err
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
