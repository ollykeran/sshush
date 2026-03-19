package vault

import (
	"encoding/binary"
	"os"
)

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
