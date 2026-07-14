package keys

import "testing"

func BenchmarkKeyGenerate(b *testing.B) {
	for _, tc := range []struct {
		name    string
		keyType string
		bits    int
	}{
		{"ed25519", "ed25519", 0},
		{"RSA-2048", "rsa", 2048},
		{"RSA-4096", "rsa", 4096},
		{"ECDSA-P256", "ecdsa", 256},
		{"ECDSA-P384", "ecdsa", 384},
		{"ECDSA-P521", "ecdsa", 521},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, _, _ = Generate(tc.keyType, tc.bits, "bench")
			}
		})
	}
}
