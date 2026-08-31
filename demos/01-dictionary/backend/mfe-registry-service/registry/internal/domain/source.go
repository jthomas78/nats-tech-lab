package domain

// Where an entry came from, as an operator needs to read it (decision 80).
//
// This is deliberately NOT a stored field on Entry. A stored `source` would be
// a claim, and BR-AS43 refuses claims: a publisher must not be able to say how
// it arrived, and an operator editing an announced entry must not silently
// turn it into a curated one. The fact is already recorded — the audit trail
// says who created the row — so the source is read back out of it.
//
// The row it is read from is the FIRST accepted write for that id, never the
// latest. "Who put this here" and "who touched it last" are different
// questions, and only the first one is what the badge claims to answer: an
// operator disabling an announced plugin has not made it a curated one.
const (
	SourceCurated   = "curated"
	SourcePreload   = "preload"
	SourceAnnounced = "announced"
	// SourceUnknown is for a row whose creating write predates the audit
	// trail, or whose history has aged out. Shown as its own word rather
	// than guessed at: "we cannot tell" is honest, and defaulting to
	// `curated` would dress up the one case an operator most wants to see.
	SourceUnknown = "unknown"
)

// SourceOf maps a creating actor to the tier it registered through.
//
// The two platform actors are named literals; everything else is a publisher
// key, and a publisher can only ever have arrived by announcing (BR-AS35).
// That is why this is a default rather than a third literal: the set of
// publisher keys is open, and a new one must not read as unknown.
func SourceOf(creatingActor string) string {
	switch creatingActor {
	case "":
		return SourceUnknown
	case SharedAdminActor:
		return SourceCurated
	case PreloadActor:
		return SourcePreload
	default:
		return SourceAnnounced
	}
}

// Registration is how one entry got here: the tier, and the actor that put it
// there.
//
// Both, not just the tier, because "announced" without a name is the one thing
// an operator cannot act on — approving an announcement means deciding whether
// you trust that publisher, and a badge reading only `announced` asks them to
// decide about nobody in particular. The tier is derived from the actor and
// travels beside it rather than replacing it.
type Registration struct {
	Source string `json:"source"`
	// By is the raw creating actor: the shared operator identity, the preload
	// actor, or a publisher key. Shown for an announcement and largely noise
	// for the other two, which name a mechanism rather than a party.
	By string `json:"by,omitempty"`
}

// RegistrationOf pairs an actor with the tier it registered through.
func RegistrationOf(creatingActor string) Registration {
	return Registration{Source: SourceOf(creatingActor), By: creatingActor}
}
