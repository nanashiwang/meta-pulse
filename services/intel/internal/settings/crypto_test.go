package settings

import (
	"bytes"
	"strings"
	"testing"
)

func TestSealOpenAndRotation(t *testing.T) {
	a, err := NewBox([]byte(strings.Repeat("a", 32)))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := a.Seal("sk_ze_secret_value_123")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("sk_ze_secret")) {
		t.Fatal("plaintext visible in ciphertext")
	}
	got, err := a.Open(sealed, a.KeyID())
	if err != nil || got != "sk_ze_secret_value_123" {
		t.Fatalf("open %q %v", got, err)
	}
	again, _ := a.Seal("sk_ze_secret_value_123")
	if bytes.Equal(again, sealed) {
		t.Fatal("nonce must make each seal unique")
	}
	b, _ := NewBox([]byte(strings.Repeat("b", 32)))
	if _, err := b.Open(sealed, a.KeyID()); err != ErrStale {
		t.Fatalf("rotated secret must report stale, got %v", err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := a.Open(sealed, a.KeyID()); err != ErrStale {
		t.Fatal("tampered ciphertext accepted")
	}
	if _, err := NewBox([]byte("short")); err != ErrUnavailable {
		t.Fatal("short secret accepted")
	}
	if Hint("sk_ze_abcd1234") != "1234" || Hint("abc") != "" {
		t.Fatal("hint")
	}
}
