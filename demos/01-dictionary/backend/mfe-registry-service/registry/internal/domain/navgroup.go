package domain

import (
	"encoding/json"
	"errors"
)

// NavGroup is the navigation group a navigation contribution belongs in
// (BR-AS83). The group is IDENTIFIED by the plugin and PLACED by the shell:
// two plugins land in one band by agreeing on ID, never by agreeing on Label,
// and there is deliberately no order or collapsed field here — under BR-AS07
// a plugin fills a target and never chooses where the target lives.
//
// A manifest may write the group in either of two forms:
//
//	"group": "Features"
//	"group": {"id": "jetstream", "label": "JetStream"}
//
// The registry carries a publisher's manifest, it does not author one, so the
// form is remembered and re-emitted as written. That is not cosmetic: drift
// detection re-marshals both the curated entry and the served manifest and
// compares the text (internal/application/drift.go), so promoting one form
// into the other on only one of the two paths would read as permanent,
// unexplainable drift on `contributions`.
type NavGroup struct {
	ID    string
	Label string

	// shorthand records that the publisher wrote the string form. Unexported
	// so no caller can set it inconsistently with ID and Label; compared by
	// reflect.DeepEqual in Attested and SameAnnouncedContent, which is safe
	// because both sides of those comparisons decode from the same bytes.
	shorthand bool
}

// ShorthandNavGroup builds the string form, where one word is both the
// identity and the label. Used by tests and by any caller that needs to state
// the legacy shape explicitly; decoding produces it on its own.
func ShorthandNavGroup(s string) *NavGroup {
	return &NavGroup{ID: s, Label: s, shorthand: true}
}

// ErrNavGroupShape is returned when a group is neither a string nor an object.
var ErrNavGroupShape = errors.New("navigation group is neither a string nor an object")

// UnmarshalJSON accepts both forms.
//
// Unrecognised keys inside the object are DROPPED, never refused, which is the
// one place this decoder is deliberately laxer than the enclosing
// Contribution: the shell drops them too (BR-AS83), and a registry stricter
// than the shell would refuse a plugin that works in `build` mode, which is
// exactly the split between the two catalogue sources BR-AS85 exists to close.
// Note this also means Entry's DisallowUnknownFields does not reach in here —
// intentional, and the reason the drop is spelled out rather than inherited.
func (g *NavGroup) UnmarshalJSON(data []byte) error {
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		*g = NavGroup{ID: asString, Label: asString, shorthand: true}
		return nil
	}
	var asObject struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	if err := json.Unmarshal(data, &asObject); err != nil {
		return ErrNavGroupShape
	}
	*g = NavGroup{ID: asObject.ID, Label: asObject.Label}
	return nil
}

// MarshalJSON re-emits the form that was decoded.
func (g NavGroup) MarshalJSON() ([]byte, error) {
	if g.shorthand {
		return json.Marshal(g.ID)
	}
	return json.Marshal(struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}{ID: g.ID, Label: g.Label})
}

// Shorthand reports whether the publisher wrote the string form. The write
// door needs to know, because the shorthand is exempt from the id pattern.
func (g NavGroup) Shorthand() bool { return g.shorthand }
