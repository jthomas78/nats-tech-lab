// Package objectstore holds compliance document bytes in a NATS Object Store
// bucket — the repo's first use of Object Store (Phase 38c-ii, ADR-048).
//
// Chosen over S3/MinIO because tenant isolation comes free from the NATS
// account boundary that already isolates every stream and KV bucket here, and
// because the pattern is itself worth evaluating in this lab. The honest cost,
// per ADR-048: an Object Store bucket *is* a JetStream stream, so document
// bytes and the TRANSPORTER event log share one failure domain and one 1 GiB
// per-tenant storage limit. That is why MaxBucketBytes below is not optional
// decoration — without it, enough uploads stop event publishing for the whole
// tenant. S3 would be better on failure isolation and on byte transport
// (presigned URLs); it teaches this lab nothing, which is why the choice
// stands with its cost stated rather than hidden.
//
// One bucket per tenant account, matching the repo-wide KV convention: the
// tenant is the account, {context} lives in the key (see domain's
// DocumentObjectName), never in the bucket name.
package objectstore

import (
	"context"
	"errors"
	"io"

	"github.com/nats-io/nats.go/jetstream"
)

// BucketName is the per-account bucket. It becomes the JetStream stream
// OBJ_organizations-docs, which counts against the tenant's MaxStreams.
const BucketName = "organizations-docs"

// MaxBucketBytes is BR-TP44's bucket-level cap: a quarter of a default
// tenant's 1 GiB JetStream disk allowance, leaving the event log — the thing
// that cannot be re-derived — the clear majority of the budget. Paired with
// domain.MaxDocumentFileBytes at the service boundary, since a bucket cap
// alone would let one oversized upload consume the whole allowance.
const MaxBucketBytes int64 = 256 << 20

// ErrObjectNotFound is returned when the named object is absent. It maps
// jetstream's own sentinel so callers don't import jetstream just to compare.
var ErrObjectNotFound = errors.New("document object not found")

type Store struct{ obs jetstream.ObjectStore }

func New(ctx context.Context, js jetstream.JetStream) (*Store, error) {
	obs, err := js.CreateOrUpdateObjectStore(ctx, jetstream.ObjectStoreConfig{
		Bucket:      BucketName,
		Description: "Compliance document bytes for trading partners (Phase 38c-ii)",
		MaxBytes:    MaxBucketBytes,
	})
	if err != nil {
		return nil, err
	}
	return &Store{obs: obs}, nil
}

// Put streams r into the named object, returning the number of bytes stored.
//
// The size is taken from what the store actually accepted, not from anything
// the client declared — BR-TP44's cap is only meaningful if it is enforced on
// real bytes. fileName and contentType ride along as object metadata so the
// bucket alone is enough to reconstruct a download, even though the Postgres
// projection is what readers normally consult (BR-TP45).
func (s *Store) Put(ctx context.Context, name, fileName, contentType string, r io.Reader) (int64, error) {
	info, err := s.obs.Put(ctx, jetstream.ObjectMeta{
		Name: name,
		Metadata: map[string]string{
			"fileName":    fileName,
			"contentType": contentType,
		},
	}, r)
	if err != nil {
		return 0, err
	}
	return int64(info.Size), nil
}

// Get opens the named object for reading. The caller must Close the result.
func (s *Store) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	res, err := s.obs.Get(ctx, name)
	if err != nil {
		if errors.Is(err, jetstream.ErrObjectNotFound) {
			return nil, ErrObjectNotFound
		}
		return nil, err
	}
	return res, nil
}
