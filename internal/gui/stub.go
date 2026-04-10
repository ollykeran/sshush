//go:build !gui

// Package gui is the Fyne desktop UI; it is only compiled with -tags=gui.
// Default builds use this stub so headless CI and toolchains without Mesa/X11 dev libs skip Fyne.
package gui

var _ struct{}
