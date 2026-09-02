package mferegistry

import "encoding/json"

// Request is the signed envelope accepted by both publisher RPC endpoints.
// Payload is opaque to this contract package: the registry domain parses it
// and decides whether it is admissible.
type Request struct {
	Payload    json.RawMessage `json:"payload"`
	Signature  string          `json:"signature"`
	SigningKey string          `json:"signingKey"`
}

// AnnounceOutcome is the closed wire vocabulary for a successful announce.
type AnnounceOutcome string

const (
	AnnounceInserted AnnounceOutcome = "inserted"
	AnnouncePending  AnnounceOutcome = "pending"
	AnnounceUpdated  AnnounceOutcome = "updated"
	AnnounceRequeued AnnounceOutcome = "requeued"
	AnnounceIgnored  AnnounceOutcome = "ignored"
	// AnnounceConverged — the announcement was admitted and changed nothing.
	// A resync at a higher release carrying identical content: no catalogue
	// revision, no audit row, but the release watermark still advances.
	//
	// Deliberately NOT the same as Response.NoOp, which is the narrower
	// "this exact release is already stored" (a literal replay). The two look
	// alike from a publisher's chair and are different facts on the wire:
	// NoOp spent no release number, converged spent one. Collapsing them
	// would make the watermark's advance invisible to anyone reading a
	// response (BR-AS73, decision 10).
	AnnounceConverged AnnounceOutcome = "converged"
)

// Recorded reports whether an announce outcome represents an audited
// observation. The empty value is the only non-outcome.
func (o AnnounceOutcome) Recorded() bool { return o != "" }

// Response is the announcement endpoint's wire response.
type Response struct {
	OK       bool            `json:"ok"`
	Outcome  AnnounceOutcome `json:"outcome,omitempty"`
	Revision int64           `json:"revision"`
	Error    string          `json:"error,omitempty"`
	Code     string          `json:"code,omitempty"`
	// NoOp is a successful retry of the release already accepted.
	NoOp bool `json:"noOp,omitempty"`
}

// UnregisterOutcome is the closed wire vocabulary for a successful
// unregister.
type UnregisterOutcome string

const (
	UnregisterWithdrawn UnregisterOutcome = "withdrawn"
	UnregisterIgnored   UnregisterOutcome = "ignored"
)

// UnregisterResponse is deliberately separate from Response because its
// outcome vocabulary is different.
type UnregisterResponse struct {
	OK       bool              `json:"ok"`
	Outcome  UnregisterOutcome `json:"outcome,omitempty"`
	Revision int64             `json:"revision"`
	Error    string            `json:"error,omitempty"`
	Code     string            `json:"code,omitempty"`
	// NoOp is a successful duplicate of the withdrawal already accepted.
	NoOp bool `json:"noOp,omitempty"`
}

// UnregisterAction and UnregisterSchemaVersion bind a withdrawal to its own
// signed command vocabulary. They express wire shape, not admission policy;
// parsing and all admission gates remain in the registry domain.
const (
	UnregisterAction        = "unregister"
	UnregisterSchemaVersion = 1
)

// Publisher trust vocabulary — the closed sets the
// api._platform.mfe-registry.publishers.write.v1 surface accepts. They live here
// with the rest of the wire contract so an operator client outside this
// service (cmd/seed-publishers, the Admin UI's Registry Publishers panel)
// can name an op or a key state without reaching into the registry's own
// internal domain package, which the Go internal rule forbids anyway.
//
// Vocabulary only. Which op an actor may send, what a state means for the
// entries a key signed, and every admission consequence of a revocation
// remain in the registry domain (BR-AS38).
const (
	// OpPublisherUpsert creates the publisher row, or renames it. Deliberately
	// no delete op: a trust anchor that can be silently emptied is not one.
	OpPublisherUpsert = "publisher-upsert"
	// OpPublisherAddKey attaches a signing key, enabled.
	OpPublisherAddKey = "publisher-add-key"
	// OpPublisherSetKeyState moves an existing key between the states below.
	OpPublisherSetKeyState = "publisher-set-key-state"
	// OpPublisherTransfer moves ownership of one plugin id.
	OpPublisherTransfer = "publisher-transfer"
)

const (
	// KeyEnabled — may sign new announcements.
	KeyEnabled = "enabled"
	// KeyRetired — superseded. Signs nothing new; what it already signed stays.
	KeyRetired = "retired"
	// KeyRevoked — trust withdrawn, and the entries it signed are withheld.
	KeyRevoked = "revoked"
)
