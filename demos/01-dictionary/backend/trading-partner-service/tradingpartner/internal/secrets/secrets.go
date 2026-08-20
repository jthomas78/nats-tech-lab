// Package secrets holds BR-TP52's tracking-credential payload store: the one
// place a telematics secret exists in this system.
//
// # Why the payload is encrypted here rather than by the server
//
// BR-TP52 specifies "a NATS KV bucket with at-rest encryption enabled". That
// wording assumes a per-bucket switch, and there isn't one: NATS at-rest
// encryption is a server-wide `jetstream { key: ... }` directive covering
// every stream and bucket on the server, and this lab's nats.conf does not
// set it. Turning it on would re-key the whole lab's storage to protect one
// bucket.
//
// So the payload is sealed here, by the service, with AES-256-GCM before it
// ever reaches NATS. That is strictly stronger than the rule asked for: the
// ciphertext is opaque to anyone reading the bucket directly — a NATS
// operator, a `nats kv get`, a JetStream backup — not merely to someone
// with the disk. It also keeps the guarantee local to the feature that needs
// it instead of making it a property of the deployment.
//
// The trade recorded honestly: this service now holds a key, and a key it
// holds is a key that can be lost or leaked. It is read from the environment
// and never persisted; losing it makes stored credentials unrecoverable,
// which for rotatable secrets (BR-TP54) means re-entering them, not data
// loss of anything irreplaceable.
package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"github.com/nats-io/nats.go/jetstream"
)

// BucketName is BR-TP52's bucket. One per tenant account, like every other
// bucket in this lab — the account boundary is the tenancy enforcement, so
// the name carries no tenant token.
const BucketName = "organizations-secrets"

// MaxBucketBytes caps the bucket. A KV bucket is a JetStream stream, so
// these bytes compete with the TRANSPORTER event log for the tenant's
// allowance — the same reasoning BR-TP44 applies to the document bucket.
// Credentials are tiny, so this is deliberately small: it is a blast-radius
// limit, not a capacity plan.
const MaxBucketBytes = 8 * 1024 * 1024

// ErrNoEncryptionKey — the service was started without a key. Fail closed:
// storing a credential in the clear because configuration was missing is
// exactly the outcome BR-TP52 exists to prevent, so the store refuses to
// exist rather than silently degrading.
var ErrNoEncryptionKey = errors.New("secrets: no encryption key configured")

// ErrNotFound — no credential stored for that key.
var ErrNotFound = errors.New("secrets: no credential stored")

// Store seals and stores tracking-credential payloads.
type Store struct {
	kv   jetstream.KeyValue
	aead cipher.AEAD
}

// newSealer prepares the cipher. Split from New so the sealing guarantees
// (BR-TP52) are unit-testable without a live JetStream — the property being
// asserted is about the bytes, not about NATS.
//
// key must be exactly 32 bytes (AES-256); anything else is a configuration
// error rather than something to pad or hash into shape silently, because a
// key quietly derived from a short string is a key nobody audited.
func newSealer(key []byte) (*Store, error) {
	if len(key) == 0 {
		return nil, ErrNoEncryptionKey
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets: encryption key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Store{aead: aead}, nil
}

// New opens (or creates) the bucket and prepares the cipher.
func New(ctx context.Context, js jetstream.JetStream, key []byte) (*Store, error) {
	s, err := newSealer(key)
	if err != nil {
		return nil, err
	}

	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      BucketName,
		Description: "Tracking-credential payloads, sealed by the service (BR-TP52)",
		MaxBytes:    MaxBucketBytes,
		// History 1: BR-TP54 makes a credential current state, not evidence.
		// Retaining superseded revisions would keep compromised material
		// alive for no reader — the opposite of BR-TP43's write-once
		// documents, and deliberately so.
		History: 1,
	})
	if err != nil {
		return nil, err
	}
	s.kv = kv
	return s, nil
}

// Put seals payload and stores it, overwriting any existing value (BR-TP54).
func (s *Store) Put(ctx context.Context, key string, payload []byte) error {
	sealed, err := s.seal(payload)
	if err != nil {
		return err
	}
	_, err = s.kv.Put(ctx, key, sealed)
	return err
}

// seal returns nonce||ciphertext. A fresh nonce per write is what stops two
// transporters with the same API key producing identical bucket values.
func (s *Store) seal(payload []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, payload, nil), nil
}

// open reverses seal, and fails on tampering because GCM authenticates.
func (s *Store) open(sealed []byte) ([]byte, error) {
	if len(sealed) < s.aead.NonceSize() {
		return nil, errors.New("secrets: stored value is too short to be sealed")
	}
	nonce, ciphertext := sealed[:s.aead.NonceSize()], sealed[s.aead.NonceSize():]
	return s.aead.Open(nil, nonce, ciphertext, nil)
}

// Get opens a stored credential. Deliberately present but unused by any read
// path that reaches a browser (BR-TP52): it exists for a future outbound
// integration that must actually authenticate to the provider, and nothing
// in the api.* surface calls it.
func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	entry, err := s.kv.Get(ctx, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.open(entry.Value())
}

// Delete removes a stored credential — used when a partner is removed, not
// as part of rotation (BR-TP54 overwrites in place).
func (s *Store) Delete(ctx context.Context, key string) error {
	return s.kv.Delete(ctx, key)
}
