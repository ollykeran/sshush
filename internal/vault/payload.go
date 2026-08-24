package vault

import (
	"encoding/binary"
	"fmt"
	"os"
)

// BuildSetAutoloadPayload builds the extension payload for vault-set-autoload.
func BuildSetAutoloadPayload(fingerprint string, autoload bool) []byte {
	fp := []byte(fingerprint)
	payload := make([]byte, 4+len(fp)+1)
	binary.BigEndian.PutUint32(payload[:4], uint32(len(fp)))
	copy(payload[4:], fp)
	if autoload {
		payload[4+len(fp)] = 1
	}
	return payload
}

// BuildSetCommentPayload builds the extension payload for vault-set-comment.
// Format: 4-byte big-endian fingerprint length, fingerprint bytes,
// 4-byte big-endian comment length, comment UTF-8 bytes.
func BuildSetCommentPayload(fingerprint, comment string) []byte {
	fp := []byte(fingerprint)
	cmt := []byte(comment)
	payload := make([]byte, 4+len(fp)+4+len(cmt))
	binary.BigEndian.PutUint32(payload[:4], uint32(len(fp)))
	copy(payload[4:], fp)
	commentOffset := 4 + len(fp)
	binary.BigEndian.PutUint32(payload[commentOffset:commentOffset+4], uint32(len(cmt)))
	copy(payload[commentOffset+4:], cmt)
	return payload
}

// BuildAddKeyOptsPayload builds the extension payload for add-key-opts from a key file path.
// Format (version 1): 1-byte version (0x01), 4-byte big-endian PEM length, PEM bytes,
// 1 byte autoload (0 or 1), 4-byte big-endian filepath length, filepath UTF-8 bytes.
func BuildAddKeyOptsPayload(path string, autoload bool) ([]byte, error) {
	pemData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("vault: read key file %s: %w", path, err)
	}
	fpBytes := []byte(path)
	payload := make([]byte, 1+4+len(pemData)+1+4+len(fpBytes))
	payload[0] = 1 // version 1: includes filepath
	binary.BigEndian.PutUint32(payload[1:5], uint32(len(pemData)))
	copy(payload[5:], pemData)
	autoloadByte := byte(0)
	if autoload {
		autoloadByte = 1
	}
	payload[5+len(pemData)] = autoloadByte
	filepathOffset := 5 + len(pemData) + 1
	binary.BigEndian.PutUint32(payload[filepathOffset:filepathOffset+4], uint32(len(fpBytes)))
	copy(payload[filepathOffset+4:], fpBytes)
	return payload, nil
}
