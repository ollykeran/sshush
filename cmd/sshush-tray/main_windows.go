// Command sshush-tray is a Windows notification-area front end for sshushd:
// an icon showing whether the vault is locked, a menu naming the agent and
// listing its keys, and a main window, opened by clicking the icon or from
// the menu, to start and stop the agent, unlock it, and manage its keys.
// Starting it replaces any sshush-tray already running for the user, and
// opens the main window unless it starts at sign-in: given --background, as
// the Run key entry does, or started by the Store package's StartupTask.
// It is a client of the agent like the CLI, so closing or crashing it never
// affects sshushd, though it can start and stop it as 'sshush start' and
// 'sshush stop' do.
//
// Build it as a GUI program so Windows opens no console window:
//
//	go build -ldflags -H=windowsgui ./cmd/sshush-tray
//
// The rsrc_windows_*.syso files beside this one carry the program's icon and
// a manifest that gives the dialog themed controls and per-monitor DPI
// awareness; go build links them in. Regenerate them with go generate.
package main

//go:generate go run github.com/tc-hib/go-winres@v0.3.3 simply --arch amd64,arm64 --manifest gui --icon ../../internal/tray/icons/keys.ico --product-name sshush --file-description "sshush tray" --out rsrc

import (
	"flag"
	"os"

	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/tray"
	"github.com/ollykeran/sshush/internal/vaultops"
	"golang.org/x/sys/windows"
)

func main() {
	background := flag.Bool("background", false, "start in the notification area without opening the window")
	flag.Parse()
	cfg, err := tray.LoadConfig()
	if err != nil {
		fatal("could not read the sshush config: " + err.Error())
	}
	socketPath, err := runtime.SocketPathForSSHushGUI(cfg)
	if err != nil {
		fatal("could not resolve the agent pipe: " + err.Error())
	}
	// Only once this tray can run does it replace a running one.
	if err := tray.TakeOver(); err != nil {
		fatal("could not replace the running tray: " + err.Error())
	}
	// A Store install starts at sign-in through its StartupTask, which passes
	// no arguments, so it asks Windows instead of reading -background.
	openWindow := !*background && !tray.StartedAtSignIn()
	tray.Run(vaultops.EnvFromConfig(cfg, socketPath), tray.Options{OpenWindow: openWindow, KeyPaths: cfg.KeyPaths})
}

// fatal reports in a message box: a GUI program has no console to print to.
func fatal(text string) {
	title, _ := windows.UTF16PtrFromString("sshush")
	body, _ := windows.UTF16PtrFromString(text)
	windows.MessageBox(0, body, title, windows.MB_OK|windows.MB_ICONERROR)
	os.Exit(1)
}
