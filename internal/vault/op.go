package vault

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/ollykeran/sshush/internal/agent"
	"github.com/ollykeran/sshush/internal/openssh"
	ssh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// This file serves agent.ExtensionOp, the wrapped operation extension that lets
// a failure carry its reason. The legacy per-operation extensions in agent.go
// stay exactly as they were, so an older sshush still works against this daemon.
//
// Payload formats are shared with the legacy extensions: an op request body is
// byte-for-byte what the matching legacy extension would receive.

// statusForError maps this package's sentinels onto wire statuses. Anything
// unrecognised becomes StatusInternal rather than leaking an error string.
func statusForError(err error) byte {
	switch {
	case err == nil:
		return agent.StatusOK
	case errors.Is(err, errLocked), errors.Is(err, errAgentLocked):
		return agent.StatusLocked
	case errors.Is(err, errKeyNotFound):
		return agent.StatusNotFound
	case errors.Is(err, errNoRecovery):
		return agent.StatusNoRecovery
	case errors.Is(err, errWrongPassphrase):
		return agent.StatusWrongPassphrase
	case errors.Is(err, errAgentNotLocked):
		return agent.StatusNotLocked
	default:
		return agent.StatusInternal
	}
}

// resultFor turns an operation's error into the response body it should send.
func resultFor(err error) []byte {
	if err == nil {
		return agent.OKResponse()
	}
	return agent.EncodeOpResponse(statusForError(err), nil)
}

// handleOp dispatches an agent.ExtensionOp request. It never returns an error:
// the reason lives in the response body, which is the whole point.
func (a *VaultAgent) handleOp(contents []byte) []byte {
	op, payload, err := agent.DecodeOpRequest(contents)
	if err != nil {
		return agent.EncodeOpResponse(agent.StatusBadRequest, nil)
	}

	switch op {
	case agent.OpVaultLocked:
		a.mu.RLock()
		locked := a.masterKey == nil
		a.mu.RUnlock()
		data := []byte{0}
		if locked {
			data = []byte{1}
		}
		return agent.EncodeOpResponse(agent.StatusOK, data)

	case agent.OpLock:
		return resultFor(a.Lock(nil))

	case agent.OpUnlock:
		return resultFor(a.Unlock(payload))

	case agent.OpUnlockRecovery:
		mnemonic := strings.Join(strings.Fields(strings.TrimSpace(string(payload))), " ")
		return resultFor(a.UnlockWithRecovery(mnemonic))

	case agent.OpSessionLoad:
		fp := strings.TrimSpace(string(payload))
		if fp == "" {
			return agent.EncodeOpResponse(agent.StatusBadRequest, nil)
		}
		return resultFor(a.sessionLoad(fp))

	case agent.OpSessionUnload:
		fp := strings.TrimSpace(string(payload))
		if fp == "" {
			return agent.EncodeOpResponse(agent.StatusBadRequest, nil)
		}
		return resultFor(a.sessionUnload(fp))

	case agent.OpSetAutoload:
		fp, on, perr := parseSetAutoloadPayload(payload)
		if perr != nil {
			return agent.EncodeOpResponse(agent.StatusBadRequest, nil)
		}
		return resultFor(a.setIdentityAutoload(fp, on))

	case agent.OpSetComment:
		fp, comment, perr := parseSetCommentPayload(payload)
		if perr != nil {
			return agent.EncodeOpResponse(agent.StatusBadRequest, nil)
		}
		return resultFor(a.setIdentityComment(fp, comment))

	case agent.OpAddKey:
		addedKey, autoload, keyFilepath, perr := parseAddKeyOptsPayload(payload)
		if perr != nil {
			return agent.EncodeOpResponse(agent.StatusBadRequest, nil)
		}
		return resultFor(a.addKeyWithAutoload(addedKey, autoload, keyFilepath))

	default:
		return agent.EncodeOpResponse(agent.StatusUnsupportedOp, nil)
	}
}

// parseSetAutoloadPayload parses the vault-set-autoload body:
// 4-byte big-endian fingerprint length, fingerprint, 1 byte autoload flag.
func parseSetAutoloadPayload(contents []byte) (fingerprint string, autoload bool, err error) {
	if len(contents) < 5 {
		return "", false, fmt.Errorf("vault: set-autoload: payload too short (%d bytes)", len(contents))
	}
	fpLen64 := binary.BigEndian.Uint32(contents[:4])
	if int(fpLen64)+5 != len(contents) {
		return "", false, fmt.Errorf("vault: set-autoload: fingerprint length %d does not match payload size", fpLen64)
	}
	fpLen := int(fpLen64)
	flag := contents[4+fpLen]
	if flag != 0 && flag != 1 {
		return "", false, fmt.Errorf("vault: set-autoload: invalid flag byte %d", flag)
	}
	return string(contents[4 : 4+fpLen]), flag == 1, nil
}

// parseSetCommentPayload parses the vault-set-comment body: 4-byte big-endian
// fingerprint length, fingerprint, 4-byte big-endian comment length, comment.
func parseSetCommentPayload(contents []byte) (fingerprint, comment string, err error) {
	if len(contents) < 8 {
		return "", "", fmt.Errorf("vault: set-comment: payload too short (%d bytes)", len(contents))
	}
	fpLen := int(binary.BigEndian.Uint32(contents[:4]))
	if 8+fpLen > len(contents) {
		return "", "", fmt.Errorf("vault: set-comment: fingerprint length %d exceeds payload", fpLen)
	}
	fingerprint = string(contents[4 : 4+fpLen])
	commentOffset := 4 + fpLen
	commentLen := int(binary.BigEndian.Uint32(contents[commentOffset : commentOffset+4]))
	if commentOffset+4+commentLen != len(contents) {
		return "", "", fmt.Errorf("vault: set-comment: comment length %d does not match payload size", commentLen)
	}
	return fingerprint, string(contents[commentOffset+4 : commentOffset+4+commentLen]), nil
}

// splitAddKeyOptsPayload parses the add-key-opts body in either the versioned
// (v1, carrying the source filepath) or the legacy layout.
func splitAddKeyOptsPayload(contents []byte) (pemData []byte, autoload bool, keyFilepath string, err error) {
	if len(contents) < 5 {
		return nil, false, "", fmt.Errorf("vault: add-key-opts: payload too short (%d bytes)", len(contents))
	}
	// Version detection: the legacy format starts with a 4-byte PEM length, whose
	// first byte is 0x00 for any realistic key; v1 starts with version byte 0x01.
	if contents[0] == 1 && len(contents) >= 10 {
		// [1 version][4 PEM len][PEM][1 autoload][4 filepath len][filepath]
		pemLen := int(binary.BigEndian.Uint32(contents[1:5]))
		if 5+pemLen > len(contents) {
			return nil, false, "", fmt.Errorf("vault: add-key-opts: PEM length %d exceeds payload", pemLen)
		}
		pemData = contents[5 : 5+pemLen]
		autoloadByte := contents[5+pemLen]
		if autoloadByte != 0 && autoloadByte != 1 {
			return nil, false, "", fmt.Errorf("vault: add-key-opts: invalid autoload byte %d", autoloadByte)
		}
		autoload = autoloadByte == 1
		fpOffset := 5 + pemLen + 1
		if fpOffset+4 <= len(contents) {
			fpLen := int(binary.BigEndian.Uint32(contents[fpOffset : fpOffset+4]))
			if fpOffset+4+fpLen <= len(contents) {
				keyFilepath = string(contents[fpOffset+4 : fpOffset+4+fpLen])
			}
		}
		return pemData, autoload, keyFilepath, nil
	}
	// Legacy: [4 PEM len][PEM][1 autoload]
	pemLen := int(binary.BigEndian.Uint32(contents[:4]))
	if pemLen > len(contents)-5 {
		return nil, false, "", fmt.Errorf("vault: add-key-opts: PEM length %d exceeds payload", pemLen)
	}
	autoloadByte := contents[4+pemLen]
	if autoloadByte != 0 && autoloadByte != 1 {
		return nil, false, "", fmt.Errorf("vault: add-key-opts: invalid autoload byte %d", autoloadByte)
	}
	return contents[4 : 4+pemLen], autoloadByte == 1, "", nil
}

// addedKeyFromPEM parses PEM key material into an AddedKey, carrying the
// comment across when the blob has one.
func addedKeyFromPEM(pemData []byte) (sshagent.AddedKey, error) {
	key, err := ssh.ParseRawPrivateKey(pemData)
	if err != nil {
		return sshagent.AddedKey{}, fmt.Errorf("vault: add-key-opts: parse PEM: %w", err)
	}
	comment := ""
	if parsed, perr := openssh.ParsePrivateKeyBlob(pemData); perr == nil && parsed.Comment != "" {
		comment = parsed.Comment
	}
	return sshagent.AddedKey{PrivateKey: key, Comment: comment}, nil
}

// parseAddKeyOptsPayload parses the add-key-opts body and returns the key ready to add.
func parseAddKeyOptsPayload(contents []byte) (addedKey sshagent.AddedKey, autoload bool, keyFilepath string, err error) {
	pemData, autoload, keyFilepath, err := splitAddKeyOptsPayload(contents)
	if err != nil {
		return sshagent.AddedKey{}, false, "", err
	}
	addedKey, err = addedKeyFromPEM(pemData)
	if err != nil {
		return sshagent.AddedKey{}, false, "", err
	}
	return addedKey, autoload, keyFilepath, nil
}
