package vault

import (
	"encoding/binary"
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

// BuildAddKeyOptsPayload builds the extension payload for add-key-opts from a key file path.
// Payload: 4-byte big-endian PEM length, PEM bytes, 1 byte autoload (0 or 1).
func BuildAddKeyOptsPayload(path string, autoload bool) ([]byte, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	payload := make([]byte, 4+len(pem)+1)
	binary.BigEndian.PutUint32(payload[:4], uint32(len(pem)))
	copy(payload[4:], pem)
	if autoload {
		payload[4+len(pem)] = 1
	}
	return payload, nil
}
