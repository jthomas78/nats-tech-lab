package registry_test

// BR-AS70 — the split between what a publisher asserts and what the platform
// owns is named once, and every field of Entry sits on one side of it.
//
// The specs below are as much about the SEAM as about the checks. Before it,
// three paths each carried their own list — the parser refused four names, the
// announcement zeroed three fields, the attestation comparison zeroed four —
// and the lists had already drifted apart from each other and from the struct.

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

// publisherAsserted is every Entry field a publisher may legitimately state.
// Held here rather than in the domain because it is the test's job to force a
// decision: a field added to Entry appears in neither list and fails below,
// which is what stops a new field defaulting to publisher-asserted by silence.
var publisherAsserted = []string{
	"id", "name", "description", "version", "schemaVersion", "shellApiVersion",
	"routePrefix", "release", "manifest", "remote", "extensionPoints", "contributions",
	// A plugin says what it depends on; it does not say whether the platform
	// may ask (BR-AS62). The approval is on the other list, in CuratedFields.
	"backendServices",
}

func jsonFieldNames(v any) []string {
	t := reflect.TypeOf(v)
	out := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			name = t.Field(i).Name
		}
		if name == "-" {
			continue
		}
		out = append(out, name)
	}
	return out
}

var _ = Describe("BR-AS70: publisher-asserted and platform-owned", func() {
	It("classifies every field of Entry as one or the other", func() {
		classified := map[string]bool{}
		for _, f := range append(append([]string{}, publisherAsserted...), domain.CuratedFields()...) {
			Expect(classified[f]).To(BeFalse(), "field %q is on both sides", f)
			classified[f] = true
		}
		for _, f := range jsonFieldNames(domain.Entry{}) {
			Expect(classified).To(HaveKey(f),
				"Entry field %q belongs to a publisher or to the platform; say which in curation.go or in this spec", f)
		}
	})

	It("keeps release publisher-asserted, because the signature covers it", func() {
		Expect(domain.CuratedFields()).NotTo(ContainElement("release"))
	})

	Context("a payload may not assert a platform-owned fact", func() {
		It("refuses each of them by name, rather than ignoring it", func() {
			for _, field := range domain.CuratedFields() {
				raw := []byte(`{"id":"a","name":"A","` + field + `":null}`)
				_, err := domain.ParseManifest(raw)
				Expect(errors.Is(err, domain.ErrSelfAssertedField)).To(BeTrue(), "field %q was accepted", field)
			}
		})

		It("refuses withheld, which reaches a shell as a forced reload", func() {
			_, err := domain.ParseManifest([]byte(`{"id":"a","name":"A","withheld":true}`))
			Expect(errors.Is(err, domain.ErrSelfAssertedField)).To(BeTrue())
		})

		It("accepts a payload that states only what a publisher may", func() {
			e, err := domain.ParseManifest([]byte(`{"id":"a","name":"A","release":3,` +
				`"remote":{"kind":"federated","url":"http://localhost:7110/r.js","module":"./p"}}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(e.Release).To(Equal(int64(3)))
		})
	})

	Context("an announcement clears the platform's half whatever the caller built", func() {
		It("drops every curated fact off an entry assembled outside the parser", func() {
			incoming := domain.Entry{
				ID: "a", Name: "A", Enabled: true, Withheld: true, Withdrawn: true,
				AnnouncedAt: "then", LastAnnouncedAt: "then",
				Remote: domain.Remote{Kind: domain.RemoteFederated, URL: "http://localhost:7110/r.js", Module: "./p"},
			}
			_, out := domain.DecideAnnounce(nil, incoming)
			Expect(out.Enabled).To(BeFalse())
			Expect(out.Withheld).To(BeFalse())
			Expect(out.Withdrawn).To(BeFalse())
			Expect(out.AnnouncedAt).To(BeEmpty())
			Expect(out.LastAnnouncedAt).To(BeEmpty())
			// The one it does decide: an announced entry is dynamic.
			Expect(out.Lifecycle).To(Equal(domain.LifecycleDynamic))
		})
	})

	Context("attestation compares the signed half only", func() {
		signed := func() domain.Entry {
			raw, err := json.Marshal(map[string]any{
				"id": "a", "name": "A",
				"remote": map[string]any{"kind": "federated", "url": "http://localhost:7110/r.js", "module": "./p"},
			})
			Expect(err).NotTo(HaveOccurred())
			e, err := domain.EntryFromManifest(raw, "sig", "key")
			Expect(err).NotTo(HaveOccurred())
			return e
		}

		It("still attests an entry the platform withdrew or withheld", func() {
			for _, mutate := range []func(*domain.Entry){
				func(e *domain.Entry) { e.Withdrawn = true },
				func(e *domain.Entry) { e.Withheld = true },
				func(e *domain.Entry) { e.Enabled = true },
				func(e *domain.Entry) { e.Lifecycle = domain.LifecycleDynamic },
			} {
				e := signed()
				mutate(&e)
				Expect(e.Attested()).To(BeTrue())
			}
		})

		It("stops attesting when a publisher-asserted field is edited", func() {
			e := signed()
			e.Remote.URL = "http://localhost:7110/other.js"
			Expect(e.Attested()).To(BeFalse())
		})
	})
})
