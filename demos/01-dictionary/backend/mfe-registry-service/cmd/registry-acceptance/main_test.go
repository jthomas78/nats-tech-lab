package main

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
)

// A manifest in the shape the fixtures carry: the fields the requeue step
// must leave alone, plus the one it changes.
const fixtureManifest = `{
  "id": "example-plugin",
  "name": "Example Plugin",
  "schemaVersion": 1,
  "shellApiVersion": 1,
  "routePrefix": "/example",
  "remote": {"kind": "module-federation", "url": "http://localhost:7111/assets/remoteEntry.js", "name": "examplePlugin", "module": "./plugin"},
  "contributions": [{"kind": "nav-item", "id": "example"}]
}`

// --- the origin rewrite the requeue step depends on ---

func TestReOriginChangesTheOriginAndKeepsThePath(t *testing.T) {
	g := NewWithT(t)

	out, err := reOrigin([]byte(fixtureManifest), "http://localhost:7113")
	g.Expect(err).NotTo(HaveOccurred())

	var got map[string]any
	g.Expect(json.Unmarshal(out, &got)).To(Succeed())
	remote := got["remote"].(map[string]any)
	g.Expect(remote["url"]).To(Equal("http://localhost:7113/assets/remoteEntry.js"))
}

// The requeue assertion means "the publisher moved origin" and nothing else.
// If the rewrite also touched a name, a module or a contribution, a requeued
// outcome would no longer tell us which change caused it.
func TestReOriginChangesNothingElse(t *testing.T) {
	g := NewWithT(t)

	out, err := reOrigin([]byte(fixtureManifest), "http://localhost:7113")
	g.Expect(err).NotTo(HaveOccurred())

	var before, after map[string]any
	g.Expect(json.Unmarshal([]byte(fixtureManifest), &before)).To(Succeed())
	g.Expect(json.Unmarshal(out, &after)).To(Succeed())

	g.Expect(after).To(HaveLen(len(before)))
	for key, want := range before {
		if key == "remote" {
			continue
		}
		g.Expect(after[key]).To(Equal(want), "field %q changed", key)
	}
	remoteBefore := before["remote"].(map[string]any)
	remoteAfter := after["remote"].(map[string]any)
	for key, want := range remoteBefore {
		if key == "url" {
			continue
		}
		g.Expect(remoteAfter[key]).To(Equal(want), "remote.%s changed", key)
	}
}

func TestReOriginRefusesAManifestItCannotRewrite(t *testing.T) {
	g := NewWithT(t)

	_, err := reOrigin([]byte(`{"id":"x"}`), "http://localhost:7113")
	g.Expect(err).To(MatchError(ContainSubstring("no remote")))

	_, err = reOrigin([]byte(`{"remote":{"url":"localhost:7111/x.js"}}`), "http://localhost:7113")
	g.Expect(err).To(MatchError(ContainSubstring("no scheme")))

	_, err = reOrigin([]byte(`{"remote":{"url":"http://localhost:7111"}}`), "http://localhost:7113")
	g.Expect(err).To(MatchError(ContainSubstring("no path")))
}

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
