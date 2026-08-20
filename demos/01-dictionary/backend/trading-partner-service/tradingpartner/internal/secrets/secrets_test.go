package secrets

import (
	"bytes"
	"strings"
	"testing"
)

// The sealing half of BR-TP52 is testable without NATS: it is the property
// that a payload handed to this package is opaque by the time it would reach
// the bucket. These specs exercise the cipher directly, so they run in the
// unit suite rather than requiring a live JetStream.

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := newSealer(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatalf("newSealer: %v", err)
	}
	return s
}

func TestSealedPayloadDoesNotContainThePlaintext(t *testing.T) {
	const secret = "sk-live-SUPERSECRET-9f3a"
	s := newTestStore(t)

	sealed, err := s.seal([]byte(secret))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if strings.Contains(string(sealed), secret) {
		t.Fatalf("BR-TP52: sealed value still contains the plaintext: %q", sealed)
	}
	if bytes.Equal(sealed, []byte(secret)) {
		t.Fatal("BR-TP52: value was stored unchanged")
	}
}

func TestSealIsNonDeterministic(t *testing.T) {
	// A fresh nonce per write. Without it, two transporters using the same
	// API key would produce identical ciphertext, leaking that fact to anyone
	// who can list the bucket.
	s := newTestStore(t)
	a, err := s.seal([]byte("same-secret"))
	if err != nil {
		t.Fatalf("seal a: %v", err)
	}
	b, err := s.seal([]byte("same-secret"))
	if err != nil {
		t.Fatalf("seal b: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("BR-TP52: sealing is deterministic — equal secrets are distinguishable in the bucket")
	}
}

func TestRoundTrip(t *testing.T) {
	s := newTestStore(t)
	const secret = "user:hunter2"

	sealed, err := s.seal([]byte(secret))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	got, err := s.open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(got) != secret {
		t.Fatalf("round trip: got %q, want %q", got, secret)
	}
}

func TestTamperedCiphertextIsRejected(t *testing.T) {
	// GCM authenticates. A bucket value edited in place must fail loudly
	// rather than decrypt to garbage a caller might send to a provider.
	s := newTestStore(t)
	sealed, err := s.seal([]byte("secret"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	sealed[len(sealed)-1] ^= 0xff

	if _, err := s.open(sealed); err == nil {
		t.Fatal("BR-TP52: tampered ciphertext decrypted without error")
	}
}

func TestKeyMustBe32Bytes(t *testing.T) {
	if _, err := newSealer(nil); err != ErrNoEncryptionKey {
		t.Errorf("empty key: got %v, want ErrNoEncryptionKey", err)
	}
	if _, err := newSealer([]byte("short")); err == nil {
		t.Error("a short key must be refused rather than padded into shape")
	}
}
