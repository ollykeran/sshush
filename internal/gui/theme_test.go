//go:build gui

package gui

import (
	"image/color"
	"testing"

	fynetheme "fyne.io/fyne/v2/theme"
	sshushtheme "github.com/ollykeran/sshush/internal/theme"
)

func TestParseHex(t *testing.T) {
	t.Parallel()
	c, ok := parseHex("#7EE787")
	if !ok {
		t.Fatal("expected ok")
	}
	want := color.NRGBA{R: 0x7e, G: 0xe7, B: 0x87, A: 0xff}
	if c != want {
		t.Fatalf("got %+v want %+v", c, want)
	}
	if _, ok := parseHex("bad"); ok {
		t.Fatal("expected false for bad input")
	}
}

func TestNewFyneThemeScaled_IncreasesTextSize(t *testing.T) {
	t.Parallel()
	base := NewFyneTheme(sshushtheme.Presets["dracula"])
	scaled := NewFyneThemeScaled(sshushtheme.Presets["dracula"], 1.2)
	bt := base.Size(fynetheme.SizeNameText)
	st := scaled.Size(fynetheme.SizeNameText)
	if st <= bt {
		t.Fatalf("expected scaled text size %v > base %v", st, bt)
	}
}

func TestNewFyneTheme_DraculaUsesDarkSurfaces(t *testing.T) {
	t.Parallel()
	f := NewFyneTheme(sshushtheme.Presets["dracula"])
	bg := f.Color(fynetheme.ColorNameBackground, 0)
	r, _, _, _ := bg.RGBA()
	// Derived dark chrome (sshush “light text” presets).
	if r>>8 > 0x40 {
		t.Fatalf("expected dark background for dracula, R=%d", r>>8)
	}
}

func TestLuminance(t *testing.T) {
	t.Parallel()
	light := color.NRGBA{R: 0xf8, G: 0xf8, B: 0xf2, A: 0xff}
	if luminance(light) <= 0.45 {
		t.Fatalf("dracula text should read as light, got %v", luminance(light))
	}
	dark := color.NRGBA{R: 0x4c, G: 0x4f, B: 0x69, A: 0xff}
	if luminance(dark) > 0.45 {
		t.Fatalf("latte text should read as dark, got %v", luminance(dark))
	}
}
