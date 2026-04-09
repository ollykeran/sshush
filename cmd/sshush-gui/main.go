// sshush-gui is an optional Fyne desktop UI (Linux PoC). Build with: just build-gui
package main

import (
	"fmt"
	"os"

	"github.com/ollykeran/sshush/internal/gui"
)

func main() {
	if err := gui.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
