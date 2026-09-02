package application

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

/*
The operator's view of the catalogue, joined here rather than in a transport
(decision 80, BR-AS43).

Four things have to be brought together before an operator can act on a row:
the stored entry, whether it conforms to the allowlist, how it got here, and
what the last manifest fetch saw. Only the first is stored on the entry; the
other three come from three different places, and two of them have a rule
about what to say when the answer is missing — "unknown" rather than blank,
"awaiting-check" rather than agreement.

Those rules used to live in the NATS adapter, which meant they were reachable
only by asking the adapter and reading JSON back, and a second transport (an
HTTP admin surface, a CLI) would have had to restate them or drift from them.
The adapter now owns JSON tags and nothing else.
*/

// DriftSnapshotter exposes a completed observation only. Fetching is
// deliberately not part of this interface: nothing on a read path may wait on
// a plugin's HTTP.
type DriftSnapshotter interface {
	Snapshot(domain.Entry, string) domain.Drift
}

// CuratedEntry is one row of the operator surface. The three joined fields
// never travel on the shell's read and never go back into the stored entry —
// how a plugin arrived changes nothing about how it loads.
type CuratedEntry struct {
	Entry        domain.Entry
	Conforming   bool
	Source       string
	RegisteredBy string
	Drift        domain.Drift
}

type CuratedView struct {
	SchemaVersion  int
	Revision       int64
	AllowedOrigins []string
	Plugins        []CuratedEntry
}

// CurateDocument is the join itself, with no reads of its own, so every rule
// below is testable from plain values.
func CurateDocument(doc domain.Document, sources map[string]domain.Registration, allowed domain.Allowlist, drift DriftSnapshotter) CuratedView {
	out := CuratedView{
		SchemaVersion:  doc.SchemaVersion,
		Revision:       doc.Revision,
		AllowedOrigins: allowed.Origins(),
		Plugins:        []CuratedEntry{},
	}
	for _, entry := range doc.Entries {
		/* Absent means unknown, spelled out rather than left empty: a blank
		   badge on one row among many reads as a rendering fault, and the one
		   thing this field must never do is look like agreement. */
		reg, ok := sources[entry.ID]
		if !ok {
			reg = domain.Registration{Source: domain.SourceUnknown}
		}
		result := domain.UncheckedDrift("awaiting-check")
		if drift != nil {
			result = drift.Snapshot(entry, reg.Source)
		}
		out.Plugins = append(out.Plugins, CuratedEntry{
			Entry:        entry,
			Conforming:   allowed.Check(entry) == nil,
			Source:       reg.Source,
			RegisteredBy: reg.By,
			Drift:        result,
		})
	}
	return out
}

// WithDrift attaches the observer whose last readings the operator surface
// shows. A service without one still curates — every row reads
// "awaiting-check", which is what a deployment that runs no checker should
// say.
func (s *Service) WithDrift(drift DriftSnapshotter) *Service {
	s.drift = drift
	return s
}

// Curate joins a document the caller already has — the copy a write just
// returned — so a write's reply and a read's reply are assembled by the same
// code.
func (s *Service) Curate(ctx context.Context, doc domain.Document) CuratedView {
	return CurateDocument(doc, s.Sources(ctx), s.allowlist, s.drift)
}

// CuratedView is the whole operator read: the stored document, joined.
func (s *Service) CuratedView(ctx context.Context) (CuratedView, error) {
	doc, err := s.Curated(ctx)
	if err != nil {
		return CuratedView{}, err
	}
	return s.Curate(ctx, doc), nil
}
