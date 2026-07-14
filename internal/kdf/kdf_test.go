package kdf

import "testing"

func TestDeriveKey(t *testing.T) {
	salt := make([]byte, 16)
	for i := range salt {
		salt[i] = byte(i)
	}
	key1 := DeriveKey([]byte("passphrase"), salt)
	key2 := DeriveKey([]byte("passphrase"), salt)
	if len(key1) != KeyLen {
		t.Fatalf("key length = %d, want %d", len(key1), KeyLen)
	}
	if len(key1) != len(key2) {
		t.Fatal("key lengths differ")
	}
	for i := range key1 {
		if key1[i] != key2[i] {
			t.Fatalf("same input produced different keys at byte %d", i)
		}
	}
	key3 := DeriveKey([]byte("different"), salt)
	same := true
	for i := range key1 {
		if key1[i] != key3[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different passphrases produced identical keys")
	}
}

func TestGenerateSalt(t *testing.T) {
	s1, err := GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt: %v", err)
	}
	if len(s1) != 16 {
		t.Fatalf("salt length = %d, want 16", len(s1))
	}
	s2, err := GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt: %v", err)
	}
	same := true
	for i := range s1 {
		if s1[i] != s2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("two salts are identical (astronomically unlikely)")
	}
}

func TestConstantTimeCompare(t *testing.T) {
	a := []byte("hello")
	b := []byte("hello")
	if !ConstantTimeCompare(a, b) {
		t.Error("ConstantTimeCompare(same) = false")
	}
	c := []byte("world")
	if ConstantTimeCompare(a, c) {
		t.Error("ConstantTimeCompare(different) = true")
	}
	if ConstantTimeCompare(a, nil) {
		t.Error("ConstantTimeCompare(nil) = true")
	}
}
