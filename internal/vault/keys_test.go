package vault

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func BenchmarkMarshalPrivateKey(b *testing.B) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	var out []byte
	for b.Loop() {
		out, _ = marshalPrivateKey(priv)
	}
	benchSink = out
}

func BenchmarkUnmarshalPrivateKey(b *testing.B) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}
	seed, err := marshalPrivateKey(priv)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	var out interface{}
	for b.Loop() {
		out, _ = unmarshalPrivateKey(seed, "ssh-ed25519")
	}
	_ = out
}
