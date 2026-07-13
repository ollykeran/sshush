package vault

import (
	"errors"
	"testing"
)

func TestPassphrasePolicy_ValidateNew_empty(t *testing.T) {
	p := DefaultPassphrasePolicy
	if err := p.ValidateNew(nil); !errors.Is(err, ErrPassphraseEmpty) {
		t.Fatalf("nil: got %v, want ErrPassphraseEmpty", err)
	}
	if err := p.ValidateNew([]byte{}); !errors.Is(err, ErrPassphraseEmpty) {
		t.Fatalf("empty: got %v, want ErrPassphraseEmpty", err)
	}
	if err := p.ValidateNew([]byte("   \t\n")); !errors.Is(err, ErrPassphraseEmpty) {
		t.Fatalf("whitespace: got %v, want ErrPassphraseEmpty", err)
	}
}

func TestPassphrasePolicy_ValidateNew_minLen(t *testing.T) {
	p := PassphrasePolicy{MinLen: 3}
	if err := p.ValidateNew([]byte("ab")); err == nil {
		t.Fatal("want error for short passphrase")
	}
	if err := p.ValidateNew([]byte("abc")); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestPassphrasePolicy_ValidateNew_characterClasses(t *testing.T) {
	p := PassphrasePolicy{MinLen: 1, RequireUpper: true, RequireLower: true, RequireDigit: true, RequireSpecial: true}
	if err := p.ValidateNew([]byte("Aa1!")); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if err := p.ValidateNew([]byte("aa1!")); err == nil {
		t.Fatal("want error without upper")
	}
	if err := p.ValidateNew([]byte("AA1!")); err == nil {
		t.Fatal("want error without lower")
	}
	if err := p.ValidateNew([]byte("Aa!x")); err == nil {
		t.Fatal("want error without digit")
	}
	if err := p.ValidateNew([]byte("Aa1x")); err == nil {
		t.Fatal("want error without special")
	}
}
