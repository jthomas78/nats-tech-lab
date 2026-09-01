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
