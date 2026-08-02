package style

import (
	"bytes"
	"strings"
	"testing"
)

func TestOutput_PrintTo_skip_empty(t *testing.T) {
	o := NewOutput()
	var buf bytes.Buffer
	o.PrintTo(&buf)
	if buf.Len() != 0 {
		t.Fatalf("PrintTo on empty Output: want no bytes, got %q", buf.String())
	}
}

func TestSetPlainMode_disables_styles(t *testing.T) {
	prev := IsPlainMode()
	SetPlainMode(true)
	defer SetPlainMode(prev)

	got := Success("hello")
	if strings.Contains(got, "\x1b") {
		t.Fatalf("Success in plain mode contains ANSI escape: %q", got)
	}
	if got != "hello" {
		t.Fatalf("Success in plain mode: want %q, got %q", "hello", got)
	}
}

func TestSetPlainMode_affects_all_style_functions(t *testing.T) {
	prev := IsPlainMode()
	SetPlainMode(true)
	defer SetPlainMode(prev)

	tests := []struct {
		name string
		got  string
	}{
		{"Success", Success("a")},
		{"Text", Text("a")},
		{"TextBold", TextBold("a")},
		{"Warn", Warn("a")},
		{"Err", Err("a")},
		{"Highlight", Highlight("a")},
		{"Focus", Focus("a")},
		{"Dim", Dim("a")},
	}
	for _, tt := range tests {
		if strings.Contains(tt.got, "\x1b") {
			t.Fatalf("%s in plain mode contains ANSI: %q", tt.name, tt.got)
		}
		if tt.got != "a" {
			t.Fatalf("%s in plain mode: want %q, got %q", tt.name, "a", tt.got)
		}
	}
}

func TestOutput_Box_returns_plain_string_when_plain(t *testing.T) {
	prev := IsPlainMode()
	SetPlainMode(true)
	defer SetPlainMode(prev)

	o := NewOutput().Success("line1").Info("line2")
	boxed := o.Box()
	if strings.Contains(boxed, "\x1b") {
		t.Fatalf("Box in plain mode contains ANSI: %q", boxed)
	}
	// Should contain the content without any Unicode box-drawing characters
	if strings.ContainsAny(boxed, "╭╮╰╯│─") {
		t.Fatalf("Box in plain mode contains box-drawing chars: %q", boxed)
	}
	// Content must still be there
	if !strings.Contains(boxed, "line1") || !strings.Contains(boxed, "line2") {
		t.Fatalf("Box in plain mode missing content: %q", boxed)
	}
	if boxed != o.String() {
		t.Fatalf("Box() in plain mode != String(): %q vs %q", boxed, o.String())
	}
}

func TestOutput_PrintTo_plain_mode(t *testing.T) {
	prev := IsPlainMode()
	SetPlainMode(true)
	defer SetPlainMode(prev)

	o := NewOutput().Success("data")
	var buf bytes.Buffer
	o.PrintTo(&buf)
	out := buf.String()
	if strings.Contains(out, "\x1b") {
		t.Fatalf("PrintTo in plain mode contains ANSI: %q", out)
	}
	if strings.ContainsAny(out, "╭╮╰╯│─") {
		t.Fatalf("PrintTo in plain mode contains box chars: %q", out)
	}
	if !strings.Contains(out, "data") {
		t.Fatalf("PrintTo in plain mode missing content: %q", out)
	}
}

func TestHexWithBackground_plain(t *testing.T) {
	prev := IsPlainMode()
	SetPlainMode(true)
	defer SetPlainMode(prev)

	got := HexWithBackground("#ff0000")
	if strings.Contains(got, "\x1b") {
		t.Fatalf("HexWithBackground in plain mode contains ANSI: %q", got)
	}
	if got != "#ff0000" {
		t.Fatalf("HexWithBackground in plain mode: want %q, got %q", "#ff0000", got)
	}
}

func TestIsPlainMode_reflects_state(t *testing.T) {
	prev := IsPlainMode()
	SetPlainMode(true)
	if !IsPlainMode() {
		t.Fatal("IsPlainMode() should be true after SetPlainMode(true)")
	}
	SetPlainMode(false)
	if IsPlainMode() {
		t.Fatal("IsPlainMode() should be false after SetPlainMode(false)")
	}
	SetPlainMode(prev)
}

func TestBox_returns_plain_string_when_plain(t *testing.T) {
	prev := IsPlainMode()
	SetPlainMode(true)
	defer SetPlainMode(prev)

	b := Box("hello")
	if strings.ContainsAny(b, "╭╮╰╯│─") {
		t.Fatalf("Box in plain mode contains box chars: %q", b)
	}
	if b != "hello" {
		t.Fatalf("Box in plain mode: want %q, got %q", "hello", b)
	}
}

func TestRenderBox_returns_content_when_plain(t *testing.T) {
	prev := IsPlainMode()
	SetPlainMode(true)
	defer SetPlainMode(prev)

	s := "hello\nworld"
	out := renderBox(s, 0)
	if out != s {
		t.Fatalf("renderBox in plain mode: want %q, got %q", s, out)
	}
	out = renderBox(s, 80)
	if out != s {
		t.Fatalf("renderBox in plain mode with limit: want %q, got %q", s, out)
	}
}
