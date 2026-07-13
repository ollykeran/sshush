package vault

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PassphrasePolicy defines requirements for a new vault master passphrase.
// It is enforced only when creating a vault (Init), not when unlocking an existing one.
// Strength is policy-based (length and character classes), not cryptographic entropy;
// entropy estimation would need a separate approach (e.g. a dedicated library).
type PassphrasePolicy struct {
	MinLen int

	// Character classes use Unicode categories. "Special" means ASCII non-letter,
	// non-digit, non-space (punctuation and symbols in the ASCII range).
	RequireUpper   bool
	RequireLower   bool
	RequireDigit   bool
	RequireSpecial bool
}

// DefaultPassphrasePolicy is the policy applied by Init for new vaults.
var DefaultPassphrasePolicy = PassphrasePolicy{
	MinLen:           1,
	RequireUpper:     false,
	RequireLower:     false,
	RequireDigit:     false,
	RequireSpecial:   false,
}

// ErrPassphraseEmpty is returned when the passphrase is empty or whitespace-only.
var ErrPassphraseEmpty = errors.New("vault: passphrase must not be empty")

// ValidateNew returns an error if passphrase does not satisfy this policy.
func (p PassphrasePolicy) ValidateNew(passphrase []byte) error {
	if len(strings.TrimSpace(string(passphrase))) == 0 {
		return ErrPassphraseEmpty
	}
	n := utf8.RuneCount(passphrase)
	if n < p.MinLen {
		return fmt.Errorf("vault: passphrase must be at least %d characters", p.MinLen)
	}
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range string(passphrase) {
		switch {
		case unicode.IsUpper(r) && unicode.IsLetter(r):
			hasUpper = true
		case unicode.IsLower(r) && unicode.IsLetter(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case isASCIISpecial(r):
			hasSpecial = true
		}
	}
	if p.RequireUpper && !hasUpper {
		return errors.New("vault: passphrase must contain an uppercase letter")
	}
	if p.RequireLower && !hasLower {
		return errors.New("vault: passphrase must contain a lowercase letter")
	}
	if p.RequireDigit && !hasDigit {
		return errors.New("vault: passphrase must contain a digit")
	}
	if p.RequireSpecial && !hasSpecial {
		return errors.New("vault: passphrase must contain an ASCII special character (punctuation or symbol)")
	}
	return nil
}

func isASCIISpecial(r rune) bool {
	if r >= 128 {
		return false
	}
	if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
		return false
	}
	return true
}
