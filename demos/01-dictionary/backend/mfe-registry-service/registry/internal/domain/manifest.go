package domain

import (
	"encoding/json"
	"reflect"
)

// Manifest is one entry exactly as its publisher emitted and signed it
// (BR-AS37, decisions 68 and 101).
//
// Bytes is the artifact. Everything else about an entry — the id, the remote,
// the contributions — is a *projection* of these bytes, derived for query and
// display and never the thing a signature is checked against. The distinction
// is not pedantry: JSONB reorders keys and strips whitespace, and an ordinary
// JSON round trip does the same, so an entry reassembled from columns can
// still parse, still render, and no longer verify.
//
// Bytes is []byte on purpose: encoding/json carries it as base64 in both
// directions, so the KV cache and every transport hop move it opaquely
// without any hop choosing an encoding of its own (BR-AS50).
type Manifest struct {
	Bytes     []byte `json:"bytes"`
	Signature string `json:"signature,omitempty"`
	// SigningKey is the key that actually signed these bytes, not merely the
	// publisher that holds it (decision 103). Without it a revocation cannot
	// tell which entries it must touch: a publisher rotating through several
	// keys would have every one of its entries re-evaluated, or none.
	SigningKey string `json:"signingKey,omitempty"`
}

// EntryFromManifest is the only way a signed entry is assembled. The bytes
// are parsed for the projection and then kept, unmodified, beside it.
func EntryFromManifest(manifest []byte, signature, signingKey string) (Entry, error) {
	var e Entry
	if err := json.Unmarshal(manifest, &e); err != nil {
		return Entry{}, err
	}
	// Copied, not aliased: a caller that later reuses its buffer must not be
	// able to change what the registry believes was signed.
	blob := make([]byte, len(manifest))
	copy(blob, manifest)
	e.Manifest = &Manifest{Bytes: blob, Signature: signature, SigningKey: signingKey}
	return e, nil
}

// Signed reports whether this entry carries manifest bytes at all. Curated and
// preload entries do not, and that is a legitimate state, not a defect
// (decision 102).
func (e Entry) Signed() bool { return e.Manifest != nil && len(e.Manifest.Bytes) > 0 }

// Attested reports whether the projection still says what the signed bytes
// say. An unsigned entry is never attested — there is nothing attesting it.
//
// Editing signed content is not an edit; it invalidates the attestation, and
// the entry must be re-signed or fall back to operator curation (BR-AS50).
// The facts the store owns — enabled, lifecycle and the announcement stamps —
// are excluded, because a write may legitimately change them without the
// publisher signing anything new.
func (e Entry) Attested() bool {
	if !e.Signed() {
		return false
	}
	projected, err := EntryFromManifest(e.Manifest.Bytes, e.Manifest.Signature, e.Manifest.SigningKey)
	if err != nil {
		return false
	}
	return reflect.DeepEqual(e.signedContent(), projected.signedContent())
}

// signedContent is the entry with everything the signature does not cover
// zeroed out, so two entries can be compared on what was actually signed.
func (e Entry) signedContent() Entry {
	e.Enabled = false
	e.Lifecycle = ""
	e.AnnouncedAt = ""
	e.LastAnnouncedAt = ""
	e.Manifest = nil
	return e
}
