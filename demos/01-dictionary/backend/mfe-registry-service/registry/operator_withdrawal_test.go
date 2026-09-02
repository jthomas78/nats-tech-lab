package registry_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/postgres"
)

/*
An operator switching a plugin off (BR-AS53, BR-AS54).

The class decides what the shell is told, and the two answers are
genuinely different:

  - STATIC — the entry leaves the document. The shell keeps running what
    it has and offers a reload, because a static plugin's contributions
    were never designed to be taken away underneath it.
  - DYNAMIC — the entry is served as a withdrawal marker, and the shell
    takes it off screen live. That is the whole point of the class.

The marker is only ever served for an entry an operator had approved. A
pending entry has never been on anyone's screen, and naming it to every
browser would hand out the ids of plugins nobody has admitted yet.
*/
var _ = Describe("BR-AS54 — an operator disable withdraws a dynamic plugin", func() {
	var (
		ctx   context.Context
		store *postgres.Store
	)

	allowed := domain.NewAllowlist([]string{"http://localhost:7110"})

	BeforeEach(func() {
		if pgUnavailable != "" {
			Skip(pgUnavailable)
		}
		ctx = context.Background()
		_, err0 := pgDB.ExecContext(ctx, `TRUNCATE registry.entries, registry.audit; UPDATE registry.revision SET revision = 0`)
		Expect(err0).NotTo(HaveOccurred())
		store = postgres.NewStore(pgDB, allowed)
	})

	approved := func(id string, lifecycle string) domain.Entry {
		return domain.Entry{
			ID:        id,
			Name:      id,
			Enabled:   true,
			Lifecycle: lifecycle,
			Remote:    domain.Remote{Kind: "federated", URL: "http://localhost:7110/" + id + ".js", Module: "./plugin"},
			// An entry that contributes nothing is refused on write (BR-AS69),
			// so the fixture contributes the least a plugin can.
			Contributions: []domain.Contribution{{Kind: "shell-footer", ID: "status"}},
		}
	}

	seed := func(entries ...domain.Entry) domain.Document {
		doc, err := store.Current(ctx)
		Expect(err).NotTo(HaveOccurred())
		for _, e := range entries {
			entry := e
			doc, err = store.Apply(ctx, domain.Write{
				Op: domain.OpUpsert, EntryID: entry.ID, Actor: "operator", Entry: &entry, IfRevision: doc.Revision,
			})
			Expect(err).NotTo(HaveOccurred())
		}
		return doc
	}

	setEnabled := func(doc domain.Document, id string, enabled bool) domain.Document {
		next, err := store.Apply(ctx, domain.Write{
			Op: domain.OpSetEnabled, EntryID: id, Actor: "operator", Enabled: enabled, IfRevision: doc.Revision,
		})
		Expect(err).NotTo(HaveOccurred())
		return next
	}

	find := func(doc domain.Document, id string) *domain.Entry {
		for i := range doc.Entries {
			if doc.Entries[i].ID == id {
				return &doc.Entries[i]
			}
		}
		return nil
	}

	It("serves a disabled dynamic plugin as a withdrawal marker", func() {
		doc := seed(approved("fleet-ops", domain.LifecycleDynamic))

		doc = setEnabled(doc, "fleet-ops", false)

		served := find(doc.Readable(allowed), "fleet-ops")
		Expect(served).NotTo(BeNil(), "a running shell must be told, not left to infer it from absence")
		Expect(served.Withdrawn).To(BeTrue())
		Expect(served.Remote.URL).To(BeEmpty())
	})

	It("takes a disabled static plugin out of the document instead", func() {
		doc := seed(approved("fleet-ops", domain.LifecycleStatic))

		doc = setEnabled(doc, "fleet-ops", false)

		// BR-AS53: the shell keeps running it and offers a reload.
		Expect(find(doc.Readable(allowed), "fleet-ops")).To(BeNil())
	})

	It("says nothing about a dynamic entry no operator ever approved", func() {
		pending := approved("fleet-ops", domain.LifecycleDynamic)
		pending.Enabled = false
		doc := seed(pending)

		doc = setEnabled(doc, "fleet-ops", false)

		// Naming it would hand every browser the id of a plugin nobody has
		// admitted yet.
		Expect(find(doc.Readable(allowed), "fleet-ops")).To(BeNil())
		Expect(find(doc, "fleet-ops").Withdrawn).To(BeFalse())
	})

	It("clears the withdrawal when the operator switches it back on", func() {
		doc := seed(approved("fleet-ops", domain.LifecycleDynamic))
		doc = setEnabled(doc, "fleet-ops", false)

		doc = setEnabled(doc, "fleet-ops", true)

		Expect(find(doc, "fleet-ops").Withdrawn).To(BeFalse())
		served := find(doc.Readable(allowed), "fleet-ops")
		Expect(served).NotTo(BeNil())
		Expect(served.Withdrawn).To(BeFalse())
		Expect(served.Remote.URL).NotTo(BeEmpty(), "the whole entry is back, not a marker")
	})

	It("survives a restart, because the withdrawal is stored and not remembered", func() {
		doc := seed(approved("fleet-ops", domain.LifecycleDynamic))
		setEnabled(doc, "fleet-ops", false)

		after, err := postgres.NewStore(pgDB, allowed).Current(ctx)
		Expect(err).NotTo(HaveOccurred())

		Expect(find(after.Readable(allowed), "fleet-ops").Withdrawn).To(BeTrue())
	})
})
