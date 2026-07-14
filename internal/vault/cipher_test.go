package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"io"
	"testing"

	"github.com/ollykeran/sshush/internal/kdf"
)

var benchSink []byte

func BenchmarkAESGCMEncrypt_Raw(b *testing.B) {
	masterKey := make([]byte, kdf.KeyLen)
	if _, err := rand.Read(masterKey); err != nil {
		b.Fatal(err)
	}
	plain := []byte("benchmark plaintext for AES-256-GCM encryption test data")
	b.ReportAllocs()
	b.ResetTimer()
	var out []byte
	for b.Loop() {
		block, _ := aes.NewCipher(masterKey)
		aead, _ := cipher.NewGCM(block)
		iv := make([]byte, gcmIVSize)
		_, _ = io.ReadFull(rand.Reader, iv)
		out = aead.Seal(iv, iv, plain, nil)
	}
	benchSink = out
}

func BenchmarkEncryptBlob_Sshush(b *testing.B) {
	masterKey := make([]byte, kdf.KeyLen)
	if _, err := rand.Read(masterKey); err != nil {
		b.Fatal(err)
	}
	plain := []byte("benchmark plaintext for AES-256-GCM encryption test data")
	b.ReportAllocs()
	b.ResetTimer()
	var out []byte
	for b.Loop() {
		out, _ = encryptBlob(masterKey, plain)
	}
	benchSink = out
}

func BenchmarkAESGCMDecrypt_Raw(b *testing.B) {
	masterKey := make([]byte, kdf.KeyLen)
	if _, err := rand.Read(masterKey); err != nil {
		b.Fatal(err)
	}
	plain := []byte("benchmark plaintext for AES-256-GCM decryption test data")
	ciphertext, err := encryptBlob(masterKey, plain)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	var out []byte
	for b.Loop() {
		block, _ := aes.NewCipher(masterKey)
		aead, _ := cipher.NewGCM(block)
		iv, ct := ciphertext[:gcmIVSize], ciphertext[gcmIVSize:]
		out, _ = aead.Open(nil, iv, ct, nil)
	}
	benchSink = out
}

func BenchmarkDecryptBlob_Sshush(b *testing.B) {
	masterKey := make([]byte, kdf.KeyLen)
	if _, err := rand.Read(masterKey); err != nil {
		b.Fatal(err)
	}
	plain := []byte("benchmark plaintext for AES-256-GCM decryption test data")
	ciphertext, err := encryptBlob(masterKey, plain)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	var out []byte
	for b.Loop() {
		out, _ = decryptBlob(masterKey, ciphertext)
	}
	benchSink = out
}
