package main

import (
	"testing"

	. "github.com/onsi/gomega"
)

// The requeue step used to rewrite the fixture manifest's origin here, and
// three tests pinned that rewrite to the origin and nothing else. Since
// BR-AS71 there is no origin in the manifest to rewrite: the publisher stamps
// PLUGIN_PUBLIC_ORIGIN in immediately before signing, so step 6 moves the
// plugin by overriding that one deployment value and leaves the manifest
// untouched. "The requeue turns on the move and nothing else" is now true by
// construction rather than by assertion, and the stamping itself is pinned in
// shared/mferegistry/announcer/announcer_test.go.

// --- the origin comparison ---

func TestHasOriginMatchesOnlyOnAnOriginBoundary(t *testing.T) {
	g := NewWithT(t)

	g.Expect(hasOrigin("http://localhost:7113/assets/remoteEntry.js", "http://localhost:7113")).To(BeTrue())
	// The near-miss a prefix test would accept: a different port that starts
	// with the digits of the one we are looking for.
	g.Expect(hasOrigin("http://localhost:71130/assets/remoteEntry.js", "http://localhost:7113")).To(BeFalse())
	g.Expect(hasOrigin("http://localhost:7111/assets/remoteEntry.js", "http://localhost:7113")).To(BeFalse())
	// An origin with no path is not a remote entry, so it is not a match.
	g.Expect(hasOrigin("http://localhost:7113", "http://localhost:7113")).To(BeFalse())
}
