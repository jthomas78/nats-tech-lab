// Package cache implements the non-canonical TransporterProfile KV cache.
package cache

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/nats-io/nats.go/jetstream"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
)

const BucketName = "organizations"

type Store struct{ kv jetstream.KeyValue }

func New(ctx context.Context, js jetstream.JetStream) (*Store, error) {
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: BucketName})
	if err != nil {
		return nil, err
	}
	return &Store{kv: kv}, nil
}

func (s *Store) Put(ctx context.Context, state profiledomain.State) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = s.kv.Put(ctx, key(state.Context, state.ID), data)
	return err
}

func key(contextKey, id string) string {
	return strings.Join([]string{contextKey, "transporter", id}, ".")
}
