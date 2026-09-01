package theme

import "testing"

func TestValidHex(t *testing.T) {
	valid := []string{
		"#000000", "#ffffff", "#FFAABB", "#123456", "#abcdef",
	}
	for _, h := range valid {
		if !ValidHex(h) {
			t.Errorf("ValidHex(%q) = false, want true", h)
		}
	}

	invalid := []string{
		"", "#", "#000", "#00000", "#0000000", "#gggggg", "#12345g",
		"000000", "#AAA", "#AAAAAAFF",
	}
	for _, h := range invalid {
		if ValidHex(h) {
			t.Errorf("ValidHex(%q) = true, want false", h)
		}
	}
}

func TestHexToRGBA(t *testing.T) {
	tests := []struct {
		hex     string
		r, g, b uint8
		ok      bool
	}{
		{"#000000", 0, 0, 0, true},
		{"#ffffff", 255, 255, 255, true},
		{"#FF0000", 255, 0, 0, true},
		{"#00FF00", 0, 255, 0, true},
		{"#0000FF", 0, 0, 255, true},
		{"#808080", 128, 128, 128, true},
		{"invalid", 0, 0, 0, false},
		{"#GG0000", 0, 0, 0, false},
	}
	for _, tt := range tests {
		c, ok := HexToRGBA(tt.hex)
		if ok != tt.ok {
			t.Errorf("HexToRGBA(%q) ok = %v, want %v", tt.hex, ok, tt.ok)
			continue
		}
		if ok && (c.R != tt.r || c.G != tt.g || c.B != tt.b) {
			t.Errorf("HexToRGBA(%q) = {%d,%d,%d}, want {%d,%d,%d}", tt.hex, c.R, c.G, c.B, tt.r, tt.g, tt.b)
		}
		if ok && c.A != 255 {
			t.Errorf("HexToRGBA(%q) A = %d, want 255", tt.hex, c.A)
		}
	}
}

func TestContrastForeground(t *testing.T) {
	fg, ok := ContrastForeground("#ffffff")
	if !ok || fg != "#000000" {
		t.Errorf("ContrastForeground(#ffffff) = %q, %v; want #000000, true", fg, ok)
	}
	fg, ok = ContrastForeground("#000000")
	if !ok || fg != "#ffffff" {
		t.Errorf("ContrastForeground(#000000) = %q, %v; want #ffffff, true", fg, ok)
	}
	_, ok = ContrastForeground("invalid")
	if ok {
		t.Error("ContrastForeground(invalid) ok = true, want false")
	}
}

func TestMergeWithDefault(t *testing.T) {
	d := DefaultTheme()
	custom := Theme{Text: "#AABBCC"}
	merged := MergeWithDefault(custom, d)
	if merged.Text != "#AABBCC" {
		t.Errorf("merged.Text = %q, want #AABBCC", merged.Text)
	}
	if merged.Focus != d.Focus {
		t.Errorf("merged.Focus = %q, want default %q", merged.Focus, d.Focus)
	}

	bad := Theme{Text: "nothex", Focus: ""}
	merged = MergeWithDefault(bad, d)
	if merged.Text != d.Text {
		t.Errorf("invalid custom text not rejected: got %q", merged.Text)
	}
	if merged.Focus != d.Focus {
		t.Errorf("empty custom focus not using default: got %q", merged.Focus)
	}
}

func TestResolveTheme(t *testing.T) {
	th, ok := ResolveTheme("dracula")
	if !ok {
		t.Fatal("ResolveTheme(dracula) not found")
	}
	if th.Focus != "#50FA7B" {
		t.Errorf("dracula.Focus = %q", th.Focus)
	}

	th, ok = ResolveTheme("  Nord  ")
	if !ok || th.Focus != "#a3be8c" {
		t.Errorf("ResolveTheme(Nord) failed: %v, %v", th, ok)
	}

	th, ok = ResolveTheme("nonexistent")
	if ok {
		t.Error("ResolveTheme(nonexistent) should return false")
	}
	if th != DefaultTheme() {
		t.Error("fallback should be DefaultTheme()")
	}
}

func TestPresetNamesOrdered(t *testing.T) {
	names := PresetNamesOrdered()
	if len(names) != len(Presets) {
		t.Fatalf("got %d names, want %d", len(names), len(Presets))
	}
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("names not sorted: %q after %q", names[i], names[i-1])
		}
	}
}

func TestAllPresetThemesHaveValidHex(t *testing.T) {
	for name, th := range Presets {
		fields := map[string]string{
			"text":    th.Text,
			"focus":   th.Focus,
			"accent":  th.Accent,
			"error":   th.Error,
			"warning": th.Warning,
		}
		for field, val := range fields {
			if !ValidHex(val) {
				t.Errorf("preset %q has invalid %s hex: %q", name, field, val)
			}
		}
	}
}
