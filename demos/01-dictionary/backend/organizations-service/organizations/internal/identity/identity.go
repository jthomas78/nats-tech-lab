// Package identity mints and validates this service's entity identifiers.
//
// organizations-service identifies its entities with ULIDs, not UUIDs
// (ADR-051). The short version of why: a ULID is 26 Crockford-base32
// characters against a UUID's 36, it is lexicographically sortable by
// creation time, and it is safe in both of the places an organizations-service
// ID has to survive — a NATS subject token and a NATS KV key.
//
// The subject-safety point is the load-bearing one. A TransporterProfile's
// identity appears in every event subject it ever publishes
// (evt.{context}.organizations.transporter.{id}.{event}), those subjects are
// how per-aggregate replay and per-subject optimistic concurrency both work,
// and the TRANSPORTER stream is LimitsPolicy — so the ID is in the log
// permanently. Crockford base32 is [0-9A-HJKMNP-TV-Z]: no '.', so it cannot
// split a subject token, and it is inside the [-/_=.a-zA-Z0-9] set NATS KV
// keys allow. That is not true of every candidate — see ADR-051 for why a
// country-prefixed company registration number was rejected.
//
// Deliberately NOT a domain port. ID minting has no business rule attached to
// it: nothing in domain/ branches on the shape of an ID, so an IDGenerator
// interface would be a seam with one implementation and no second caller.
// Handlers that need to inject a deterministic ID for a test already do it
// with a func() string field (see ComplianceDocumentHandler.newID), which is
// the narrower tool for that job.
package identity

import "github.com/oklog/ulid/v2"

// Size is the encoded length of every ID this package mints: 26 characters.
const Size = ulid.EncodedSize

// New returns a fresh ULID: a 48-bit millisecond timestamp followed by 80 bits
// of entropy, encoded as 26 uppercase Crockford base32 characters.
//
// Entropy is monotonic within a millisecond, so two IDs minted in the same
// millisecond still sort in the order they were minted. Callers get that for
// free and some rely on it: an ID that sorts by creation time gives Postgres
// sequential primary-key inserts (better B-tree locality than a random UUID)
// and gives a stream's subject space a stable chronological ordering.
//
// Safe for concurrent use.
func New() string { return ulid.Make().String() }

// IsValid reports whether s is a syntactically well-formed ULID.
//
// This is stricter than a length check on purpose, and rejects three distinct
// things: the wrong length, a character outside the Crockford alphabet (which
// would be unsafe in a subject or KV key), and the overflow case — 26 base32
// characters address 130 bits where a ULID holds 128, so the first character
// is capped at '7' and anything from '8' to 'Z' there is malformed rather than
// merely unusual.
func IsValid(s string) bool {
	_, err := ulid.ParseStrict(s)
	return err == nil
}
