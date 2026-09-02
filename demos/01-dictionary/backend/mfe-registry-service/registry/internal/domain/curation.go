package domain

// Which half of an Entry is whose (BR-AS43, BR-AS70).
//
// An Entry is two things in one struct: what a publisher asserted about its
// plugin, and what the platform decided about it. The two have different
// authorities and different threat models — a publisher may say where its code
// lives, and may never say that its code is approved — so every path that
// crosses between them has to know the split.
//
// Before this file each path knew it separately: ParseManifest refused four
// field names, DecideAnnounce zeroed three fields, signedContent zeroed four,
// and the struct's own comments named a fifth and a sixth. They had already
// drifted. ParseManifest accepted a payload asserting `"withheld": true`,
// which reaches a shell as a tombstone and forces a reload on every browser
// running that plugin; and signedContent left Withdrawn in the comparison, so
// a withdrawn signed entry reported itself un-attested although nothing signed
// had changed.
//
// So the set is named once, here, and the paths ask. The type still carries
// both halves — flattening them into a nested struct would rename every JSON
// field and every stored row — but there is one definition, and a test pins
// every field of Entry to one side of it or the other, so a field added
// without a decision fails a spec rather than defaulting to publisher-asserted.

// CuratedFields names the JSON fields of Entry the platform owns. A payload
// asserting one of these is refused rather than ignored: ignoring it would
// let a publisher believe it had said something.
//
// Release is deliberately NOT here. It is self-asserted and signed — a
// publisher cannot move it without the key, which is the whole of BR-AS47.
func CuratedFields() []string {
	return []string{"enabled", "lifecycle", "withheld", "withdrawn", "announcedAt", "lastAnnouncedAt"}
}

// WithoutCuration returns the entry with every platform-owned fact cleared,
// leaving only what a publisher may assert. Used to strip an incoming payload
// and to compare two entries on what was actually signed.
func (e Entry) WithoutCuration() Entry {
	e.Enabled = false
	e.Lifecycle = ""
	e.Withheld = false
	e.Withdrawn = false
	e.AnnouncedAt = ""
	e.LastAnnouncedAt = ""
	return e
}
