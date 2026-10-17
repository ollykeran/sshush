package tray

import (
	"context"
	_ "embed"
	"errors"
	"sync/atomic"
	"time"

	"fyne.io/systray"
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
	views := make(chan View)
	go Poll(context.Background(), t.env.SocketPath, pollInterval, t.kick, views)
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

// render rebuilds the whole menu. The poller sends only changed Views, so this
// runs rarely and never under an open menu that is still accurate.
func (t *tray) render(v View) {
	systray.SetIcon(iconFor(v.Icon))
	systray.SetTooltip(v.Tooltip)
	systray.ResetMenu()

	if v.Status != "" {
		systray.AddMenuItem(v.Status, "").Disable()
	}
	for _, k := range v.Keys {
		systray.AddMenuItem(k, "").Disable()
	}
	systray.AddSeparator()
	if v.CanUnlock {
		onClick(systray.AddMenuItem("Unlock…", "Unlock the vault with its passphrase"), t.unlock)
	}
	if v.CanLock {
		onClick(systray.AddMenuItem("Lock", "Wipe the vault's master key from the agent"), t.lock)
	}
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

func (t *tray) unlock() {
	if !t.prompting.CompareAndSwap(false, true) {
		return // a prompt is already open
	}
	go func() {
		defer t.prompting.Store(false)
		defer t.refresh()
		wrong := false
		for {
			pass, err := promptPassphrase("Enter the vault passphrase to unlock the sshush agent.", wrong)
			if errors.Is(err, errCancelled) {
				return
			}
			if err != nil {
				showError(err.Error())
				return
			}
			err = vaultops.UnlockPassphrase(t.env, pass)
			clear(pass)
			if vaultops.CodeOf(err) == vaultops.CodeWrongPassphrase {
				wrong = true
				continue
			}
			if err != nil && vaultops.CodeOf(err) != vaultops.CodeNotLocked {
				showError(describe(err))
			}
			return
		}
	}()
}

func (t *tray) lock() {
	if err := vaultops.Lock(t.env); err != nil {
		showError(describe(err))
	}
	t.refresh()
}

// describe renders a vaultops failure the way the CLI does: the sentence, then
// the remedy.
func describe(err error) string {
	if hint := vaultops.HintOf(err); hint != "" {
		return err.Error() + "\n\n" + hint
	}
	return err.Error()
}

func showError(text string) {
	title, _ := windows.UTF16PtrFromString("sshush")
	body, _ := windows.UTF16PtrFromString(text)
	windows.MessageBox(0, body, title, windows.MB_OK|windows.MB_ICONERROR)
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
