// Command sshush-tray is a Windows notification-area front end for sshushd:
// an icon showing whether the vault is locked, a menu listing the agent's
// keys, and Windows' own credential prompt to unlock. It is a client of the
// agent like the CLI, so closing or crashing it never affects sshushd.
//
// Build it as a GUI program so Windows opens no console window:
//
//	go build -ldflags -H=windowsgui ./cmd/sshush-tray
package main

import (
	"os"

	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/tray"
	"golang.org/x/sys/windows"
)

func main() {
	cfg, err := tray.LoadConfig()
	if err != nil {
		fatal("could not read the sshush config: " + err.Error())
	}
	socketPath, err := runtime.SocketPathForSSHushGUI(cfg)
	if err != nil {
		fatal("could not resolve the agent pipe: " + err.Error())
	}
	tray.Run(tray.EnvFor(cfg, socketPath))
}

// fatal reports in a message box: a GUI program has no console to print to.
func fatal(text string) {
	title, _ := windows.UTF16PtrFromString("sshush")
	body, _ := windows.UTF16PtrFromString(text)
	windows.MessageBox(0, body, title, windows.MB_OK|windows.MB_ICONERROR)
	os.Exit(1)
}
