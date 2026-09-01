package domain

import (
	"errors"
	"fmt"
	"sort"

	"github.com/nats-io/nkeys"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

// A publisher is a stable identity that holds keys and owns plugin ids
// (BR-AS38, BR-AS46; decisions 69, 97, 103).
//
// Two separations are load-bearing here and are easy to collapse by accident.
//
// The publisher is not its key. Rotation adds a successor and retires the old
// key; every entry that old key signed stays valid. Revocation withdraws
// trust and forces those entries to be re-evaluated (7d). Making the key the
// identity would make rotation and revocation the same event.
//
// Ownership is not the origin. A key that verifies proves who signed; it does
// not say what that signer is allowed to speak for. Ownership is carried by
// this row as an explicit list of plugin ids, because origin-derived
// ownership collapses the moment two teams deploy behind one host — the
// normal case, not the exotic one.
//
// The state and op names themselves are wire vocabulary, shared with the
// operator clients that send them (shared/mferegistry). Aliased rather than
// re-declared for the same reason the announce outcomes are: the registry's
// internal package is unreachable from cmd/, and a second copy of a closed
// set is a second copy that can drift. The rules below are still this
// package's, and only this package's.
const (
	// KeyEnabled — may sign new announcements.
	KeyEnabled = mferegistry.KeyEnabled
	// KeyRetired — superseded. Signs nothing new; everything it already
	// signed remains admitted.
	KeyRetired = mferegistry.KeyRetired
	// KeyRevoked — trust withdrawn. Signs nothing new, and the entries it
	// signed are re-evaluated and withheld.
	KeyRevoked = mferegistry.KeyRevoked
)

// KeyStates returns every legal key state. A state added without a rule fails
// the spec that pins this list.
func KeyStates() []string { return []string{KeyEnabled, KeyRetired, KeyRevoked} }

// Publisher write ops. Exhaustive, and deliberately without a delete: a trust
// anchor that can be silently emptied is not a trust anchor, so trust is
// withdrawn by state and a row is never removed.
const (
	OpPublisherUpsert      = mferegistry.OpPublisherUpsert
	OpPublisherAddKey      = mferegistry.OpPublisherAddKey
	OpPublisherSetKeyState = mferegistry.OpPublisherSetKeyState
	OpPublisherTransfer    = mferegistry.OpPublisherTransfer
)

// PublisherWriteOps returns every legal publisher op.
func PublisherWriteOps() []string {
	return []string{OpPublisherUpsert, OpPublisherAddKey, OpPublisherSetKeyState, OpPublisherTransfer}
}

// Errors the adapters translate into refusals.
var (
	ErrNoPublisher         = errors.New("registry: write names no publisher")
	ErrNoPluginID          = errors.New("registry: write names no plugin")
	ErrPublisherIDMismatch = errors.New("registry: publisher body does not match the id it is filed under")
	ErrBadPublisherKey     = errors.New("registry: not a public NKey")
	ErrBadKeyState         = errors.New("registry: unknown key state")
	ErrNoPublisherRow      = errors.New("registry: no such publisher")
	ErrNoPublisherKey      = errors.New("registry: publisher does not hold that key")
)

// PublisherKey is one signing key and the trust the operator places in it.
type PublisherKey struct {
	PublicKey string `json:"publicKey"`
	State     string `json:"state"`
	AddedAt   string `json:"addedAt,omitempty"`
	ChangedAt string `json:"changedAt,omitempty"`
}

// Publisher is one trusted identity.
type Publisher struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// Keys and Plugins are both projections of their own tables; a write
	// touches one or the other, never both (see the transfer op).
	Keys    []PublisherKey `json:"keys"`
	Plugins []string       `json:"plugins"`
}

// Owns reports whether this publisher may speak for a plugin id.
func (p Publisher) Owns(pluginID string) bool {
	for _, id := range p.Plugins {
		if id == pluginID {
			return true
		}
	}
	return false
}

// Key returns the named key's record.
func (p Publisher) Key(publicKey string) (PublisherKey, bool) {
	for _, k := range p.Keys {
		if k.PublicKey == publicKey {
			return k, true
		}
	}
	return PublisherKey{}, false
}

// PublisherDocument is the whole trust table at one revision.
//
// Its revision is its own, separate from the plugin document's: an operator
// adding a key has not changed the catalogue, and making every shell re-read
// the plugin document because a publisher was renamed would be noise. The
// two counters meet in 7d, where a revocation withholds entries and so does
// consume a plugin revision.
type PublisherDocument struct {
	Revision   int64       `json:"revision"`
	Publishers []Publisher `json:"publishers"`
}

// KeyHolder finds the publisher holding a key, and that key's record. This is
// the lookup verification will do in 7c; it lives here because "who holds
// this key, and in what state" is a question about the trust table alone.
func (d PublisherDocument) KeyHolder(publicKey string) (Publisher, PublisherKey, bool) {
	for _, p := range d.Publishers {
		if k, ok := p.Key(publicKey); ok {
			return p, k, true
		}
	}
	return Publisher{}, PublisherKey{}, false
}

// OwnerOf names the publisher that owns a plugin id, if any owns it.
func (d PublisherDocument) OwnerOf(pluginID string) (string, bool) {
	for _, p := range d.Publishers {
		if p.Owns(pluginID) {
			return p.ID, true
		}
	}
	return "", false
}

// PublisherWrite is one curated change to the trust table. Same shape as
// Write, and for the same reason: an authorless change to a trust anchor
// cannot be audited, and BR-AS38 requires every one of them to leave a row.
type PublisherWrite struct {
	Op          string
	PublisherID string
	Actor       string

	Publisher *Publisher // upsert
	PublicKey string     // add-key, set-key-state
	KeyState  string     // set-key-state
	PluginID  string     // transfer

	// IfRevision is the trust table's revision the author read.
	IfRevision int64
}

// Subject is what this write is about, for the audit row. A transfer is
// audited under the plugin id that moved, because that — not the publisher
// receiving it — is the thing an operator will later search for.
func (w PublisherWrite) Subject() string {
	if w.Op == OpPublisherTransfer {
		return w.PluginID
	}
	return w.PublisherID
}

// Validate checks everything about a publisher write that is true without
// reading the trust table.
func (w PublisherWrite) Validate() error {
	if w.Actor == "" {
		return ErrNoActor
	}
	if w.PublisherID == "" {
		return ErrNoPublisher
	}
	switch w.Op {
	case OpPublisherUpsert:
		if w.Publisher != nil && w.Publisher.ID != w.PublisherID {
			return ErrPublisherIDMismatch
		}
		return nil
	case OpPublisherAddKey:
		return ValidatePublicKey(w.PublicKey)
	case OpPublisherSetKeyState:
		if err := ValidatePublicKey(w.PublicKey); err != nil {
			return err
		}
		return ValidateKeyState(w.KeyState)
	case OpPublisherTransfer:
		if w.PluginID == "" {
			return ErrNoPluginID
		}
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnknownOp, w.Op)
	}
}

// ValidatePublicKey accepts an NKey public key and nothing else.
//
// Publisher keypairs are minted outside the nsc trust chain (gate answer 2):
// nothing here is enrolled in an account, so a leaked publisher key cannot
// connect to NATS as anything — it can only sign manifests, which is the
// whole capability being granted. What is reused is the encoding and the
// Ed25519 verification the rest of the lab already uses.
//
// A seed is refused explicitly. FromPublicKey rejects one anyway, but a seed
// arriving on this path means somebody pasted a private key into an operator
// surface, and that deserves its own refusal rather than a generic one.
func ValidatePublicKey(publicKey string) error {
	if publicKey == "" {
		return fmt.Errorf("%w: empty", ErrBadPublisherKey)
	}
	if nkeys.IsValidEncoding([]byte(publicKey)) && len(publicKey) > 0 && publicKey[0] == 'S' {
		return fmt.Errorf("%w: that is a seed, not a public key", ErrBadPublisherKey)
	}
	if _, err := nkeys.FromPublicKey(publicKey); err != nil {
		return fmt.Errorf("%w: %s", ErrBadPublisherKey, err)
	}
	return nil
}

// ValidateKeyState accepts one of the three states and nothing else.
func ValidateKeyState(state string) error {
	for _, s := range KeyStates() {
		if s == state {
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrBadKeyState, state)
}

// SortPublishers puts the document in a stable order — publishers by id, keys
// by public key, plugin ids ascending — so two reads of one revision are the
// same bytes.
func SortPublishers(ps []Publisher) {
	sort.Slice(ps, func(i, j int) bool { return ps[i].ID < ps[j].ID })
	for i := range ps {
		sort.Slice(ps[i].Keys, func(a, b int) bool { return ps[i].Keys[a].PublicKey < ps[i].Keys[b].PublicKey })
		sort.Strings(ps[i].Plugins)
	}
}
