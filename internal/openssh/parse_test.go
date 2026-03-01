package openssh

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	ssh "golang.org/x/crypto/ssh"
)

func marshalOpenSSHWithComment(key interface{}, comment string) []byte {
	block, err := ssh.MarshalPrivateKey(key, comment)
	if err != nil {
		panic(err)
	}
	return pem.EncodeToMemory(block)
}

func TestCommentFromPrivateKeyBlob_RSA(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	want := "rsa-key-comment"
	data := marshalOpenSSHWithComment(priv, want)
	parsed, _ := ParsePrivateKeyBlob(data)
	got := ""
	if parsed != nil {
		got = parsed.Comment
	}
	if got != want {
		t.Errorf("parsed.Comment(RSA) = %q, want %q", got, want)
	}
}

func TestCommentFromPrivateKeyBlob_Ed25519(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	want := "ed25519-key-comment"
	data := marshalOpenSSHWithComment(priv, want)
	parsed, _ := ParsePrivateKeyBlob(data)
	got := ""
	if parsed != nil {
		got = parsed.Comment
	}
	if got != want {
		t.Errorf("parsed.Comment(Ed25519) = %q, want %q", got, want)
	}
}

func TestCommentFromPrivateKeyBlob_ECDSA(t *testing.T) {
	curves := []struct {
		name  string
		curve elliptic.Curve
	}{
		{"P-256", elliptic.P256()},
		{"P-384", elliptic.P384()},
		{"P-521", elliptic.P521()},
	}
	for _, c := range curves {
		t.Run(c.name, func(t *testing.T) {
			priv, err := ecdsa.GenerateKey(c.curve, rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			want := "ecdsa-" + c.name + "-comment"
			data := marshalOpenSSHWithComment(priv, want)
			parsed, _ := ParsePrivateKeyBlob(data)
			got := ""
			if parsed != nil {
				got = parsed.Comment
			}
			if got != want {
				t.Errorf("parsed.Comment(ECDSA %s) = %q, want %q", c.name, got, want)
			}
		})
	}
}

func TestCommentFromPrivateKeyBlob_NonOpenSSHReturnsEmpty(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der := x509.MarshalPKCS1PrivateKey(priv)
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}
	data := pem.EncodeToMemory(block)
	parsed, _ := ParsePrivateKeyBlob(data)
	got := ""
	if parsed != nil {
		got = parsed.Comment
	}
	if got != "" {
		t.Errorf("parsed.Comment(non-OpenSSH) = %q, want \"\"", got)
	}
}

func TestParsePrivateKeyBlob_and_CommentFromParsedKey(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	wantComment := "parsed-rsa-comment"
	data := marshalOpenSSHWithComment(priv, wantComment)

	parsed, err := ParsePrivateKeyBlob(data)
	if err != nil {
		t.Fatalf("ParsePrivateKeyBlob: %v", err)
	}
	if parsed.KeyType != ssh.KeyAlgoRSA {
		t.Errorf("KeyType = %q, want %q", parsed.KeyType, ssh.KeyAlgoRSA)
	}
	if len(parsed.PrivateKey) == 0 {
		t.Error("PrivateKey blob should be non-empty")
	}
	if parsed.Comment != wantComment {
		t.Errorf("parsed.Comment = %q, want %q", parsed.Comment, wantComment)
	}
}

func TestParsePrivateKeyBlob_NonOpenSSHReturnsError(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der := x509.MarshalPKCS1PrivateKey(priv)
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}
	data := pem.EncodeToMemory(block)

	parsed, err := ParsePrivateKeyBlob(data)
	if err != ErrNotOpenSSHKey {
		t.Errorf("ParsePrivateKeyBlob err = %v, want ErrNotOpenSSHKey", err)
	}
	if parsed != nil {
		t.Errorf("ParsePrivateKeyBlob parsed = %v, want nil", parsed)
	}
}
