package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// The signed unregister command (BR-AS54).
//
// An unregister is the one message a publisher sends that takes running code
// off an operator's screen, so the ACTION is inside the signed bytes. An
// announcement's manifest cannot say "unregister", which is exactly the
// point: without domain separation, an announcement captured off the wire is
// a valid unregister for whoever replays it.
//
// The publisher and the signing key are inside the bytes too. The transport
// carries its own copies as lookup hints; binding them here means a signature
// cannot be lifted onto a request that names some other key.
const (
	UnregisterAction        = "unregister"
	UnregisterSchemaVersion = 1
)

var (
	// ErrNotUnregister refuses bytes that do not say they are an unregister
	// — including a perfectly valid announcement replayed here.
	ErrNotUnregister = errors.New("registry: those signed bytes are not an unregister command")

	// ErrUnregisterVersion refuses a command shape this code does not know.
	// Refusing is the safe direction for a message that removes running code.
	ErrUnregisterVersion = errors.New("registry: unknown unregister command version")

	// ErrUnregisterMalformed refuses a command that will not parse, unknown
	// fields included.
	ErrUnregisterMalformed = errors.New("registry: the unregister command is malformed")

	// ErrUnregisterKeyMismatch refuses a request naming a key the signed
	// bytes do not name.
	ErrUnregisterKeyMismatch = errors.New("registry: the request names a different key from the signed command")

	// ErrUnregisterPublisherMismatch refuses a command whose claimed
	// publisher does not hold the key that signed it.
	ErrUnregisterPublisherMismatch = errors.New("registry: the signed command claims a publisher that does not hold that key")

	// ErrReleaseReused refuses a release the running announcement already
	// spent. Separate from ErrReleaseBackwards: the number is not old, it
	// belongs to something else.
	ErrReleaseReused = errors.New("registry: that release is already accepted for an announcement")

	// ErrUnknownEntry refuses an unregister for an id the registry does not
	// hold. An unknown id cannot gain a row — or approval — by being
	// unregistered (BR-AS55).
	ErrUnknownEntry = errors.New("registry: no entry is registered under that id")
)

// UnregisterCommand is the parsed envelope.
type UnregisterCommand struct {
	SchemaVersion int    `json:"schemaVersion"`
	Action        string `json:"action"`
	PluginID      string `json:"plugin"`
	Publisher     string `json:"publisher"`
	SigningKey    string `json:"signingKey"`
	// Release is the same per-plugin counter an announcement carries: one
	// sequence over everything a publisher does to one plugin, so an old
	// message of either kind cannot reverse a newer decision (BR-AS47).
	Release int64 `json:"release"`
}

// ParseUnregister reads the exact signed bytes. The action is checked first,
// leniently, so that bytes meant for another purpose are refused as "not an
// unregister" rather than as a malformed one — a publisher debugging a replay
// cannot act on "malformed".
func ParseUnregister(payload []byte) (UnregisterCommand, error) {
	var probe struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return UnregisterCommand{}, fmt.Errorf("%w: %s", ErrUnregisterMalformed, err)
	}
	if probe.Action != UnregisterAction {
		return UnregisterCommand{}, ErrNotUnregister
	}

	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	var cmd UnregisterCommand
	if err := dec.Decode(&cmd); err != nil {
		return UnregisterCommand{}, fmt.Errorf("%w: %s", ErrUnregisterMalformed, err)
	}
	if cmd.SchemaVersion != UnregisterSchemaVersion {
		return UnregisterCommand{}, ErrUnregisterVersion
	}
	if cmd.PluginID == "" {
		return UnregisterCommand{}, ErrNoEntryID
	}
	if cmd.Release <= 0 {
		return UnregisterCommand{}, ErrNoRelease
	}
	return cmd, nil
}

// Unregister is everything the gate needs, gathered by the caller, so the
// gate reads no state and can be exercised without a database.
type Unregister struct {
	// Command is the parsed envelope; Payload is the byte sequence it was
	// parsed from, and the signature is checked over those bytes, never a
	// remarshal of the command.
	Command   UnregisterCommand
	Payload   []byte
	Signature string
	// SigningKey is the key the request names. It must match the one inside
	// the signed bytes.
	SigningKey string
	// Accepted is the highest release already accepted for this plugin id,
	// and Withdrawn says whether that release was itself an unregister.
	// Together they separate a duplicate delivery from a replay.
	Accepted  int64
	Withdrawn bool
}

// AdmitUnregister runs the same gate an announcement passes, in the same
// order and for the same reasons (see verify.go), plus the two checks that
// are unique to a command which removes running code: the request's key must
// be the one inside the signed bytes, and the claimed publisher must be the
// one that holds it.
func AdmitUnregister(trust PublisherDocument, v Verifier, u Unregister) (Admission, error) {
	cmd := u.Command
	if cmd.PluginID == "" {
		return Admission{}, ErrNoEntryID
	}
	if u.SigningKey != cmd.SigningKey {
		return Admission{}, ErrUnregisterKeyMismatch
	}

	admission, err := AdmitAnnouncement(trust, v, Announcement{
		PluginID:   cmd.PluginID,
		SigningKey: cmd.SigningKey,
		Payload:    u.Payload,
		Signature:  u.Signature,
		Release:    cmd.Release,
		// Ordering is decided below: an unregister and an announcement
		// treat an equal release differently, so the shared gate is asked
		// only whether the number goes backwards.
		Accepted: u.Accepted,
	})
	if err != nil {
		return Admission{}, err
	}
	if cmd.Publisher != "" && cmd.Publisher != admission.PublisherID {
		return Admission{}, ErrUnregisterPublisherMismatch
	}
	if cmd.Release == u.Accepted && !u.Withdrawn {
		// The number is not old — it is the one the running announcement
		// spent. Accepting it would let a captured release undo a newer
		// decision (decision 98).
		return Admission{}, ErrReleaseReused
	}
	return admission, nil
}

// UnregisterOutcome is what an unregister did.
type UnregisterOutcome string

const (
	// UnregisterWithdrawn — availability removed, approval untouched.
	UnregisterWithdrawn UnregisterOutcome = "withdrawn"
	// UnregisterIgnored — a static entry outranks a publisher, always
	// (decision 77, BR-AS55). Recorded and shown, never silently dropped.
	UnregisterIgnored UnregisterOutcome = "ignored"
)

// DecideUnregister returns the entry the store should hold afterwards. It
// withdraws AVAILABILITY, and availability is not approval: the row, the
// operator's enable flag, the signed manifest and the history all survive
// (BR-AS55).
func DecideUnregister(existing *Entry, cmd UnregisterCommand) (UnregisterOutcome, Entry, error) {
	if existing == nil {
		return "", Entry{}, ErrUnknownEntry
	}
	if LifecycleOf(*existing) != LifecycleDynamic {
		// A static — or unclassified, which reads as static — entry is the
		// operator's, and a publisher's availability does not move it.
		return UnregisterIgnored, *existing, nil
	}
	next := *existing
	next.Withdrawn = true
	// The release advances so a stale reannounce carrying the old number
	// cannot undo this.
	next.Release = cmd.Release
	return UnregisterWithdrawn, next, nil
}

// UnregisterWrite builds the write an accepted unregister performs, filed
// under the key that signed it so the audit names the true actor (BR-AS42,
// BR-AS54).
func UnregisterWrite(e Entry, publisherKey string, ifRevision int64) Write {
	return Write{Op: OpUpsert, EntryID: e.ID, Actor: publisherKey, Entry: &e, IfRevision: ifRevision}
}
