package domain

import (
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/nats-io/nkeys"
)

// Verification is the gate an announcement passes before the registry
// considers what it says (decision 67, BR-AS35).
//
// It runs here, in the domain, and never in the browser: a browser-side
// verifier arrives from the same origin as the code it would be checking, so
// whoever can tamper with the load can tamper with the verifier. The shell
// reads the outcome and trusts the service it already trusts for the whole
// document.
//
// The order of the four checks below is itself the rule. Ownership first,
// because a valid signature authorises nothing on its own (decision 97) and
// because a publisher whose key is both revoked and speaking for someone
// else's plugin has to be told the true reason. The release number last,
// because a stale release reported over an unverified payload would be
// telling an attacker something about state they have not earned the right
// to learn.
var (
	// ErrNotOwned refuses an announcement for a plugin id the signing
	// publisher does not own. Its own cause, deliberately: "your signature
	// is fine, you may not speak for this" is a different conversation from
	// "your signature is wrong".
	ErrNotOwned = errors.New("registry: the signing publisher does not own that plugin id")

	// ErrKeyNotTrusted refuses a key the trust table has never seen.
	ErrKeyNotTrusted = errors.New("registry: the signing key is not trusted")

	// ErrKeyRetired refuses a key that has been superseded. Separate from
	// ErrKeyRevoked because the two mean opposite things about the entries
	// that key already signed (BR-AS38).
	ErrKeyRetired = errors.New("registry: the signing key is retired and signs nothing new")

	// ErrKeyRevoked refuses a key whose trust was withdrawn.
	ErrKeyRevoked = errors.New("registry: the signing key is revoked")

	// ErrNoRelease refuses signed bytes carrying no release counter. Without
	// one there is nothing to make a replay of an old manifest fail.
	ErrNoRelease = errors.New("registry: an announcement must carry a release number")

	// ErrReleaseBackwards refuses a release older than the highest accepted
	// for that plugin id. Returning to an earlier release is an operator
	// act, never an effect of a received message (decision 98).
	ErrReleaseBackwards = errors.New("registry: that release is older than the one already accepted")
)

// Verifier answers one question and no more: did this key sign these bytes.
//
// Who the key belongs to, whether it may sign at all and what it is allowed
// to speak for are the trust table's business, not the verifier's. Keeping
// the split means the cryptography has no policy in it and the policy has no
// cryptography in it.
type Verifier interface {
	Verify(payload []byte, signature, publicKey string) error
}

// NKeyVerifier is the real anchor: Ed25519 through the same NKey encoding
// and tooling the rest of the repo uses (decision 69). Publisher keypairs are
// minted outside the nsc trust chain (gate answer 2), so a leaked signing key
// cannot connect to NATS as anything.
type NKeyVerifier struct{}

func (NKeyVerifier) Verify(payload []byte, signature, publicKey string) error {
	kp, err := nkeys.FromPublicKey(publicKey)
	if err != nil {
		return fmt.Errorf("key is not a public NKey: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("signature is not base64: %w", err)
	}
	return kp.Verify(payload, raw)
}

// NoVerifier verifies nothing and says so. It exists so that "this deployment
// has no trust anchor" is a configured state with a spec on it, rather than a
// nil check someone can forget on the one path where forgetting means
// injecting JavaScript into an operator's browser (decision 72).
type NoVerifier struct{}

func (NoVerifier) Verify([]byte, string, string) error { return ErrUnverified }

// Announcement is everything the gate needs, gathered by the caller so the
// gate itself reads no state and can be exercised without a database.
type Announcement struct {
	// PluginID is the id the manifest claims, which is the id ownership is
	// checked against.
	PluginID string
	// SigningKey names which key to check. It is a lookup hint, never a
	// grant: the signature still has to verify under it, the table still has
	// to trust it, and its holder still has to own the plugin.
	SigningKey string
	// Payload is the exact signed byte sequence, never a remarshal.
	Payload   []byte
	Signature string
	// Release is the counter inside the signed bytes.
	Release int64
	// Accepted is the highest release already accepted for this plugin id,
	// or zero if none has been.
	Accepted int64
}

// Admission is what the gate concluded about an announcement it let through.
type Admission struct {
	// PublisherID is the identity the signing key belongs to.
	PublisherID string
	// SigningKey is the key that actually signed, stored with the entry so a
	// later revocation can tell which entries it must touch (decision 103).
	SigningKey string
	// NoOp is an equal release: everything checked out and there is nothing
	// to write, so an ordinary retry after a timeout is safe (decision 98).
	NoOp bool
}

// AdmitAnnouncement runs the whole gate. A nil verifier is not "skip the
// check" — it is NoVerifier.
func AdmitAnnouncement(trust PublisherDocument, v Verifier, a Announcement) (Admission, error) {
	if a.PluginID == "" {
		return Admission{}, ErrNoEntryID
	}

	// 1. Ownership, ahead of everything (decision 97, BR-AS46).
	owner, owned := trust.OwnerOf(a.PluginID)
	if !owned {
		return Admission{}, ErrNotOwned
	}
	publisher, key, known := trust.KeyHolder(a.SigningKey)
	if !known {
		return Admission{}, ErrKeyNotTrusted
	}
	if publisher.ID != owner {
		return Admission{}, ErrNotOwned
	}

	// 2. What the key is allowed to do now (BR-AS38).
	switch key.State {
	case KeyEnabled:
	case KeyRetired:
		return Admission{}, ErrKeyRetired
	case KeyRevoked:
		return Admission{}, ErrKeyRevoked
	default:
		return Admission{}, ErrKeyNotTrusted
	}

	// 3. The signature, over the exact bytes (BR-AS35, BR-AS37).
	if a.Signature == "" {
		// Unsigned is malformed, not a verification failure, and the two
		// read differently to a publisher debugging an integration.
		return Admission{}, ErrUnsigned
	}
	if v == nil {
		v = NoVerifier{}
	}
	if err := v.Verify(a.Payload, a.Signature, a.SigningKey); err != nil {
		return Admission{}, fmt.Errorf("%w: %s", ErrUnverified, err)
	}

	// 4. The release counter (BR-AS47, decision 98).
	if a.Release <= 0 {
		return Admission{}, ErrNoRelease
	}
	if a.Release < a.Accepted {
		return Admission{}, ErrReleaseBackwards
	}
	return Admission{
		PublisherID: publisher.ID,
		SigningKey:  a.SigningKey,
		NoOp:        a.Release == a.Accepted,
	}, nil
}
