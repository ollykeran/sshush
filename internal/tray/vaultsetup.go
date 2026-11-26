package tray

import (
	"strings"

	"github.com/ollykeran/sshush/internal/utils"
)

// minPassphraseLen is the shortest vault passphrase the vault accepts.
const minPassphraseLen = 8

// newPassphraseProblem says what is wrong with a new vault passphrase and its
// confirmation, or "" when they will do.
func newPassphraseProblem(pass, confirm []byte) string {
	switch {
	case len([]rune(string(pass))) < minPassphraseLen:
		return "Use at least 8 characters."
	case confirm != nil && string(pass) != string(confirm):
		return "The passphrases did not match. Choose one again."
	}
	return ""
}

// recoveryMessage explains the recovery phrase shown once after creating a
// vault, and where else it was written.
func recoveryMessage(file string) string {
	msg := "Write these 24 words down and keep them somewhere safe, away from this computer. " +
		"If you forget the vault passphrase, they are the only way to unlock your keys."
	if file != "" {
		msg += " A copy was saved to " + utils.DisplayPath(file) + ", readable only by you; move it somewhere safer."
	}
	return msg
}

// replacedRecoveryMessage explains a recovery phrase that replaces the
// vault's old one, after a change of passphrase or on its own.
func replacedRecoveryMessage(file string) string {
	msg := "This is the vault's new recovery phrase: the old one no longer unlocks it. " +
		"Write these 24 words down and keep them somewhere safe, away from this computer."
	if file != "" {
		msg += " The copy in " + utils.DisplayPath(file) + " was replaced with it."
	}
	return msg
}

// phraseLines lays out a recovery phrase as numbered words, six to a line,
// so it is easy to copy out by hand.
func phraseLines(phrase string) string {
	words := strings.Fields(phrase)
	var b strings.Builder
	for i, w := range words {
		switch {
		case i == 0:
		case i%6 == 0:
			b.WriteString("\r\n")
		default:
			b.WriteString("   ")
		}
		b.WriteString(itoa(i+1) + ". " + w)
	}
	return b.String()
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
