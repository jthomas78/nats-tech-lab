package client

import (
	"fmt"

	"github.com/nats-io/nkeys"
)

// NKeySigner owns the publisher signing keypair. These keys are deliberately
// separate from NATS transport credentials: possessing one cannot connect a
// publisher to the broker.
type NKeySigner struct {
	keyPair nkeys.KeyPair
}

var _ Signer = (*NKeySigner)(nil)

func NewNKeySigner(seed []byte) (*NKeySigner, error) {
	keyPair, err := nkeys.FromSeed(seed)
	if err != nil {
		return nil, fmt.Errorf("load publisher signing seed: %w", err)
	}
	return &NKeySigner{keyPair: keyPair}, nil
}

func (s *NKeySigner) PublicKey() (string, error) { return s.keyPair.PublicKey() }
func (s *NKeySigner) Sign(payload []byte) ([]byte, error) {
	return s.keyPair.Sign(payload)
}

// Wipe clears seed material held by the underlying NKey keypair.
func (s *NKeySigner) Wipe() { s.keyPair.Wipe() }
