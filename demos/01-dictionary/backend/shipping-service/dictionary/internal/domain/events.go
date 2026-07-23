package domain

import (
	"strings"
	"time"
)

// Both aggregates (Ship and Container) are co-located on the single SHIPPING
// stream, partitioned by subject. This keeps every cross-aggregate rule
// checkable from one atomic replay (Phase 8 baseline). Phase 16 extracts the
// container subjects into a dedicated TERMINAL stream to expose the
// distributed-consistency problem.
const (
	StreamName = "SHIPPING"
	// Domain identifies this service in the shared evt.<tenant>.<domain>...
	// subject taxonomy — a fixed literal, not a wildcard.
	Domain = "shipping"

	ShipRegisteredEvent      = "registered"
	ShipArrivedEvent         = "arrived"
	ShipDepartedEvent        = "departed"
	ShipIDCorrectedEvent     = "corrected"
	ContainerRegisteredEvent = "registered"
	ContainerLoadedEvent     = "loaded"
	ContainerUnloadedEvent   = "unloaded"

	// SubjectWildcard matches every event in the SHIPPING stream, any context.
	//
	// The leading token is the fixed literal "evt", not a wildcard — a
	// stream subject filter whose first token is an unbounded wildcard (e.g.
	// "*.shipping.>") can textually overlap "$SYS.>"/"$JS.API.>" (a bare "*"
	// accepts "$SYS" as its value), and JetStream refuses to create such a
	// stream without NoAck — which would break the synchronous
	// Publish/PubAck flow every command handler relies on. Putting "evt"
	// first avoids the overlap while leaving context equally filterable
	// (FilterSubject/account permissions match a wildcard token in any
	// position, not just a leading prefix).
	SubjectWildcard = "evt.*." + Domain + ".>"
	// SubjectShipWildcard matches ship movement events for every context —
	// Shape A/B/meta projectors are intentionally tenant-agnostic (they
	// project every tenant into its own context-scoped KV bucket via
	// event.Context, not via per-tenant subject filtering).
	SubjectShipWildcard = "evt.*." + Domain + ".ship.>"
	// SubjectContainerWildcard matches container lifecycle events for every context.
	SubjectContainerWildcard = "evt.*." + Domain + ".container.>"
)

// ShipSubject builds a ship-movement event subject:
// evt.{context}.shipping.ship.{shipID}.{event}.
func ShipSubject(context, shipID, event string) string {
	return strings.Join([]string{"evt", context, Domain, "ship", shipID, event}, ".")
}

// ContainerSubject builds a container-lifecycle event subject:
// evt.{context}.shipping.container.{id}.{event}.
func ContainerSubject(context, id, event string) string {
	return strings.Join([]string{"evt", context, Domain, "container", id, event}, ".")
}

// ShipInstanceSubject builds the replay filter for one ship within one
// context: evt.{context}.shipping.ship.{shipID}.>.
func ShipInstanceSubject(context, shipID string) string {
	return strings.Join([]string{"evt", context, Domain, "ship", shipID, ">"}, ".")
}

// ShipContextWildcard builds the replay filter for every ship in one
// context: evt.{context}.shipping.ship.*.>. Needed because a ship's natural
// key (ShipID) is mutable (BR-022) — resolving "which surrogate does this
// shipID currently belong to" can't target one id the way
// ShipInstanceSubject does; it must fold every ship's current state and
// match by name.
func ShipContextWildcard(context string) string {
	return strings.Join([]string{"evt", context, Domain, "ship", "*", ">"}, ".")
}

// SubjectDetails returns the aggregate identity and event tokens from a
// production-form subject: evt.{context}.shipping.{aggregate}.{id}.{event}.
func SubjectDetails(subject string) (aggregate, id, event string, ok bool) {
	parts := strings.Split(subject, ".")
	if len(parts) != 6 || parts[0] != "evt" || parts[2] != Domain || parts[4] == "" {
		return "", "", "", false
	}
	return parts[3], parts[4], parts[5], true
}

func SubjectTokens(subject string) (aggregate, event string, ok bool) {
	aggregate, _, event, ok = SubjectDetails(subject)
	return
}

// StreamSubjects lists the subjects bound to the SHIPPING stream.
func StreamSubjects() []string {
	return []string{
		SubjectShipWildcard,
		SubjectContainerWildcard,
	}
}

// ShipEvent is the envelope published on every ship subject. Only the fields
// relevant to the subject are populated; the rest are zero values.
//
// ID is the ship's immutable surrogate key (UUID), minted once at
// registration and carried in every event subject — not ShipID, which is a
// mutable natural key (call-sign / internal fleet code) correctable via
// CorrectShipID (BR-022). This mirrors ContainerEvent's ID/ContainerID split.
type ShipEvent struct {
	Context        string    `json:"context"`                  // fleet / KV-bucket qualifier
	ID             string    `json:"-"`                        // aggregate identity comes from the subject
	ShipID         string    `json:"shipID"`                   // mutable natural key
	PreviousShipID string    `json:"previousShipID,omitempty"` // .corrected only — lets projectors rekey KV
	ShipName       string    `json:"shipName,omitempty"`       // carried on .registered/.arrived for replay
	Port           string    `json:"port,omitempty"`           // .arrived / .departed
	OccurredAt     time.Time `json:"occurredAt"`
}

// ContainerEvent is the envelope published on every container subject.
//
// ID is the container's immutable surrogate key (UUID), minted once at
// registration and carried in every event subject (Phase 8.3). It — not the ISO
// 6346 ContainerID — is the aggregate identity that hydration folds by.
type ContainerEvent struct {
	Context     string    `json:"context"`              // fleet / KV-bucket qualifier
	ID          string    `json:"-"`                    // aggregate identity comes from the subject
	ContainerID string    `json:"containerID"`          // ISO 6346 natural key
	Cargo       string    `json:"cargo,omitempty"`      // .registered
	OriginPort  string    `json:"originPort,omitempty"` // .registered
	DestPort    string    `json:"destPort,omitempty"`   // .registered
	Port        string    `json:"port,omitempty"`       // terminal port (.registered/.unloaded) or ship's port (.loaded)
	ShipID      string    `json:"shipID,omitempty"`     // .loaded / .unloaded
	OccurredAt  time.Time `json:"occurredAt"`
}
