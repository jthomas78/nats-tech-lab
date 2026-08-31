package domain

import "sort"

// Revocation — what withdrawing trust from a key does to the entries that key
// already signed (BR-AS38, decisions 70 and 104).
//
// The two halves of the rule pull opposite ways, and both are deliberate:
//
//   - Withdrawal is bulk and automatic. An operator revoking a key is
//     answering an incident, and making them find every entry that key signed
//     is making them get it right under pressure. One act, every entry, one
//     revision.
//   - Restoration is one entry at a time and manual. Re-enabling the key says
//     "we trust this team again"; it does not say "we have re-checked this
//     code". Nothing comes back on its own.
//
// Note what is *not* here: retiring a key withholds nothing. A retired key
// signs nothing new and everything it already signed stays valid, which is the
// whole reason retired and revoked are separate states (decision 103).

// RevocationEffect names the entries a revocation of publicKey must withhold,
// in id order.
//
// Only signed entries can be selected, and only by the key that actually
// signed them — Manifest.SigningKey, not the publisher who happens to hold it
// today. An operator-curated entry has no signing key and is never swept up:
// the publisher never touched it, so withdrawing trust from the publisher says
// nothing about it (decision 102).
func RevocationEffect(entries []Entry, publicKey string) []string {
	out := []string{}
	if publicKey == "" {
		return out
	}
	for _, e := range entries {
		if e.Manifest != nil && e.Manifest.SigningKey == publicKey {
			out = append(out, e.ID)
		}
	}
	sort.Strings(out)
	return out
}
