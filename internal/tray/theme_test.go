package tray

import "testing"

func TestParseThemeChoice(t *testing.T) {
	for in, want := range map[string]ThemeChoice{
		"light": ThemeLight, "dark": ThemeDark, "system": ThemeSystem, "": ThemeSystem, "Dark": ThemeSystem, "solarized": ThemeSystem,
	} {
		if got := ParseThemeChoice(in); got != want {
			t.Errorf("ParseThemeChoice(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestThemeChoiceDark(t *testing.T) {
	tests := []struct {
		choice                   ThemeChoice
		systemDark, highContrast bool
		want                     bool
	}{
		{ThemeSystem, false, false, false},
		{ThemeSystem, true, false, true},
		{ThemeLight, true, false, false},
		{ThemeDark, false, false, true},
		{ThemeDark, true, true, false},
		{ThemeSystem, true, true, false},
	}
	for _, tc := range tests {
		if got := tc.choice.Dark(tc.systemDark, tc.highContrast); got != tc.want {
			t.Errorf("%s with system dark %v, high contrast %v: got %v, want %v", tc.choice, tc.systemDark, tc.highContrast, got, tc.want)
		}
	}
}

func TestRGBIsBGR(t *testing.T) {
	if got := rgb(0xC4, 0x2B, 0x1C); got != lightErrorText {
		t.Errorf("rgb(#c42b1c) = %#08x, want %#08x", got, lightErrorText)
	}
}

func TestToneColors(t *testing.T) {
	for _, dark := range []bool{false, true} {
		if _, ok := toneColor(ToneNeutral, dark); ok {
			t.Error("neutral has a colour of its own")
		}
		seen := map[colorRef]Tone{}
		for _, tone := range []Tone{ToneGood, ToneWarn, ToneBad} {
			c, ok := toneColor(tone, dark)
			if !ok {
				t.Errorf("tone %d has no colour", tone)
			}
			if other, dup := seen[c]; dup {
				t.Errorf("tones %d and %d share a colour", other, tone)
			}
			seen[c] = tone
		}
	}
}

func TestGlyphColor(t *testing.T) {
	good, _ := toneColor(ToneGood, false)
	bad, _ := toneColor(ToneBad, false)
	start, stop := agentGlyph(MainActions{Start: true}), agentGlyph(MainActions{Stop: true})
	if got := glyphColor(idMainAgentToggle, start, true, false); got != good {
		t.Errorf("start: got %#x, want the good tone", got)
	}
	if got := glyphColor(idMainAgentToggle, stop, true, false); got != bad {
		t.Errorf("stop: got %#x, want the bad tone", got)
	}
	if got := glyphColor(idMainRemove, buttonGlyphs[idMainRemove], true, false); got != bad {
		t.Errorf("remove: got %#x, want the bad tone", got)
	}
	if got := glyphColor(idMainAdd, buttonGlyphs[idMainAdd], true, true); got != pick(glyphText, true) {
		t.Errorf("add in dark: got %#x, want the text colour", got)
	}
	for _, dark := range []bool{false, true} {
		if got := glyphColor(idMainRemove, buttonGlyphs[idMainRemove], false, dark); got != pick(glyphDisabled, dark) {
			t.Errorf("disabled remove, dark %v: got %#x, want grey", dark, got)
		}
	}
}
