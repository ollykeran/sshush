//go:build !gui

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "sshush-gui: built without -tags=gui. Use `just build-gui` or `go build -tags=gui ./cmd/sshush-gui` (see docs/gui.md).")
	os.Exit(1)
}
