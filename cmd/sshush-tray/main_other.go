//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "sshush-tray: Windows only; use sshush or sshush tui on this platform")
	os.Exit(1)
}
