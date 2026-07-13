package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// VaultFileVersion is the current vault JSON format version for future migrations.
const VaultFileVersion = 1

// VaultFile is the on-disk representation: version, metadata, and identities.
// Blobs are stored as base64 in JSON (encoding/json marshals []byte as base64).
type VaultFile struct {
	Version    int            `json:"version"`
	Metadata   *VaultMetadata `json:"metadata,omitempty"`
	Identities []Identity     `json:"identities"`
}

// VaultMetadata holds salt, canary, and optional recovery fields. All blobs are base64 in JSON.
type VaultMetadata struct {
	Salt             []byte `json:"salt"`
	Canary           []byte `json:"canary"`
	RecoverySalt     []byte `json:"recovery_salt,omitempty"`
	WrappedMasterKey []byte `json:"wrapped_master_key,omitempty"`
	CreatedAt        string `json:"created_at,omitempty"`
}

// Identity is one vault identity: fingerprint, public key, encrypted private key blob, and metadata.
type Identity struct {
	Fingerprint   string `json:"fingerprint"`
	PublicKey     []byte `json:"public_key"`
	EncryptedBlob []byte `json:"encrypted_blob"`
	Comment       string `json:"comment,omitempty"`
	Filepath      string `json:"filepath,omitempty"`
	AccessTime    string `json:"access_time,omitempty"`
	AddedAt       string `json:"added_at,omitempty"`
	Autoload      bool   `json:"autoload"`
}

// VaultStore holds the vault path, in-memory metadata and identities, and a mutex.
// Call Open to load or create; call Save after any mutation.
type VaultStore struct {
	path       string
	metadata   *VaultMetadata
	identities []Identity
	mu         sync.RWMutex
}

// Open reads the vault at path, or creates an empty store if the file does not exist.
// Creates parent directory with 0700. If file is missing, the store is ready for Init.
// Caller does not need to close the store; no persistent connection is held.
func Open(path string) (*VaultStore, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &VaultStore{
				path:       path,
				metadata:   nil,
				identities: []Identity{},
			}, nil
		}
		return nil, err
	}
	var f VaultFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if f.Identities == nil {
		f.Identities = []Identity{}
	}
	return &VaultStore{
		path:       path,
		metadata:   f.Metadata,
		identities: f.Identities,
	}, nil
}

// Save serializes the store to JSON and writes atomically to path (.tmp then rename), then chmod 0600.
func (s *VaultStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f := VaultFile{
		Version:    VaultFileVersion,
		Metadata:   s.metadata,
		Identities: s.identities,
	}
	data, err := json.MarshalIndent(f, "", "    ")
	if err != nil {
		return err
	}
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	fh, err := os.Open(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := fh.Sync(); err != nil {
		fh.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := fh.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Chmod(s.path, 0600)
}

// GetMetadata returns a copy of the vault metadata, or nil if not initialized.
func (s *VaultStore) GetMetadata() *VaultMetadata {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.metadata == nil {
		return nil
	}
	// Return a copy so callers cannot mutate store state.
	m := *s.metadata
	m.Salt = append([]byte(nil), m.Salt...)
	m.Canary = append([]byte(nil), m.Canary...)
	if len(m.RecoverySalt) > 0 {
		m.RecoverySalt = append([]byte(nil), m.RecoverySalt...)
	}
	if len(m.WrappedMasterKey) > 0 {
		m.WrappedMasterKey = append([]byte(nil), m.WrappedMasterKey...)
	}
	return &m
}

// SetMetadata sets the vault metadata and does not Save; caller must call Save() after Init or EnableRecovery.
func (s *VaultStore) SetMetadata(m *VaultMetadata) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata = m
}

// ListIdentitiesForAgent returns identities that have autoload true or fingerprint in sessionSet.
// Returns public_key and comment only (for List() response).
func (s *VaultStore) ListIdentitiesForAgent(sessionSet map[string]struct{}) ([]identityRow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []identityRow
	for _, id := range s.identities {
		if id.Autoload {
			out = append(out, identityRow{id.PublicKey, id.Comment})
			continue
		}
		if _, ok := sessionSet[id.Fingerprint]; ok {
			out = append(out, identityRow{id.PublicKey, id.Comment})
		}
	}
	return out, nil
}

type identityRow struct {
	PublicKey []byte
	Comment   string
}

// GetIdentity returns the encrypted blob and autoload for the given fingerprint, or false if not found.
func (s *VaultStore) GetIdentity(fingerprint string) (encryptedBlob []byte, autoload bool, found bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.identities {
		if s.identities[i].Fingerprint == fingerprint {
			return append([]byte(nil), s.identities[i].EncryptedBlob...), s.identities[i].Autoload, true
		}
	}
	return nil, false, false
}

// AddOrReplaceIdentity adds or updates an identity. Call Save() after.
func (s *VaultStore) AddOrReplaceIdentity(id Identity) error {
	id.AddedAt = time.Now().UTC().Format(time.RFC3339)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.identities {
		if s.identities[i].Fingerprint == id.Fingerprint {
			s.identities[i] = id
			return nil
		}
	}
	s.identities = append(s.identities, id)
	return nil
}

// RemoveIdentity removes the identity with the given fingerprint. Call Save() after.
func (s *VaultStore) RemoveIdentity(fingerprint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, id := range s.identities {
		if id.Fingerprint != fingerprint {
			s.identities[n] = id
			n++
		}
	}
	s.identities = s.identities[:n]
}

// RemoveAllIdentities removes all identities. Call Save() after.
func (s *VaultStore) RemoveAllIdentities() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.identities = nil
}

// AllIdentities returns a copy of all identities (for ListIdentities CLI).
func (s *VaultStore) AllIdentities() []Identity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Identity, len(s.identities))
	for i := range s.identities {
		out[i] = s.identities[i]
		out[i].PublicKey = append([]byte(nil), out[i].PublicKey...)
		out[i].EncryptedBlob = append([]byte(nil), out[i].EncryptedBlob...)
	}
	return out
}
