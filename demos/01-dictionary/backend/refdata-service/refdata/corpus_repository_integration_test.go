package refdata_test

// Integration coverage for internal/postgres/context_repository.go and
// internal/postgres/corpus_repository.go — the SQL/transaction logic behind
// Phase 12 that corpus_test.go's pure in-memory specs never exercise. Spins
// up a disposable Postgres container via the docker CLI; if docker isn't
// available, every spec in this file Skips rather than failing the suite.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/postgres"
)

var (
	integrationDB          *sql.DB
	integrationUnavailable string
	integrationContainer   string
)

var _ = BeforeSuite(func() {
	db, containerID, err := startIntegrationPostgres()
	if err != nil {
		integrationUnavailable = err.Error()
		return
	}
	integrationDB, integrationContainer = db, containerID
})

var _ = AfterSuite(func() {
	if integrationDB != nil {
		integrationDB.Close()
	}
	if integrationContainer != "" {
		_ = exec.Command("docker", "rm", "-f", integrationContainer).Run()
	}
})

func startIntegrationPostgres() (*sql.DB, string, error) {
	out, err := exec.Command("docker", "run", "-d", "--rm",
		"-e", "POSTGRES_USER=dict", "-e", "POSTGRES_PASSWORD=dict", "-e", "POSTGRES_DB=dictionary",
		"-p", "0:5432", "postgres:16-alpine").Output()
	if err != nil {
		return nil, "", fmt.Errorf("docker run postgres: %w", err)
	}
	containerID := strings.TrimSpace(string(out))

	portOut, err := exec.Command("docker", "port", containerID, "5432/tcp").Output()
	if err != nil {
		_ = exec.Command("docker", "rm", "-f", containerID).Run()
		return nil, "", fmt.Errorf("docker port: %w", err)
	}
	firstLine := strings.TrimSpace(strings.Split(string(portOut), "\n")[0])
	hostPort := firstLine[strings.LastIndex(firstLine, ":")+1:]

	dsn := fmt.Sprintf("postgres://dict:dict@127.0.0.1:%s/dictionary?sslmode=disable", hostPort)

	var db *sql.DB
	deadline := time.Now().Add(30 * time.Second)
	for {
		db, err = sql.Open("pgx", dsn)
		if err == nil {
			if pingErr := db.Ping(); pingErr == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			_ = exec.Command("docker", "rm", "-f", containerID).Run()
			return nil, "", fmt.Errorf("postgres did not become ready in time: %w", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	if err := postgres.Migrate(context.Background(), db); err != nil {
		_ = exec.Command("docker", "rm", "-f", containerID).Run()
		return nil, "", fmt.Errorf("migrate: %w", err)
	}
	return db, containerID, nil
}

var testContextCounter int64

func uniqueContext(base string) string {
	return fmt.Sprintf("%s-%d", base, atomic.AddInt64(&testContextCounter, 1))
}

func seedWorkingItem(db *sql.DB, contextKey, typeKey, code string, attrs map[string]any) error {
	_, err := db.Exec(`INSERT INTO refdata.dictionary_types (type_key, name) VALUES ($1, $1) ON CONFLICT (type_key) DO NOTHING`, typeKey)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO refdata.dictionary_items (context, type_key, code, status, attrs) VALUES ($1, $2, $3, 'active', $4)
		ON CONFLICT (context, type_key, code) DO UPDATE SET attrs = EXCLUDED.attrs`,
		contextKey, typeKey, code, mustJSON(attrs))
	return err
}

func deleteWorkingItem(db *sql.DB, contextKey, typeKey, code string) error {
	_, err := db.Exec(`DELETE FROM refdata.dictionary_items WHERE context = $1 AND type_key = $2 AND code = $3`, contextKey, typeKey, code)
	return err
}

func seedWorkingLocalization(db *sql.DB, contextKey, typeKey, code, locale, label string) error {
	_, err := db.Exec(`INSERT INTO refdata.dictionary_localizations (context, type_key, code, locale, label, description, source)
		VALUES ($1, $2, $3, $4, $5, '', 'manual')
		ON CONFLICT (context, type_key, code, locale) DO UPDATE SET label = EXCLUDED.label`,
		contextKey, typeKey, code, locale, label)
	return err
}

func mustJSON(attrs map[string]any) []byte {
	data, err := json.Marshal(attrs)
	Expect(err).NotTo(HaveOccurred())
	return data
}

func namesOf(values []domain.Context) []string {
	names := make([]string, len(values))
	for i, v := range values {
		names[i] = v.Context
	}
	return names
}

func itemsByCode(items []domain.CorpusItem) map[string]domain.CorpusItem {
	byCode := map[string]domain.CorpusItem{}
	for _, item := range items {
		byCode[item.Code] = item
	}
	return byCode
}

func locsByLocale(locs []domain.CorpusLocalization) map[string]domain.CorpusLocalization {
	byLocale := map[string]domain.CorpusLocalization{}
	for _, loc := range locs {
		byLocale[loc.Locale] = loc
	}
	return byLocale
}

var _ = Describe("Corpus and context repositories (Postgres integration)", func() {
	var (
		contexts *postgres.ContextRepository
		corpus   *postgres.CorpusRepository
	)

	BeforeEach(func() {
		if integrationDB == nil {
			Skip("docker postgres unavailable: " + integrationUnavailable)
		}
		contexts = postgres.NewContextRepository(integrationDB)
		corpus = postgres.NewCorpusRepository(integrationDB)
	})

	registerChain := func(names ...string) {
		parent := ""
		for _, name := range names {
			Expect(contexts.Register(context.Background(), domain.Context{Context: name, Parent: parent, Name: name})).To(Succeed())
			parent = name
		}
	}

	It("returns a child-first ancestor chain and a root-first descendant list across 3+ levels", func() {
		root, mid, leaf := uniqueContext("root"), uniqueContext("mid"), uniqueContext("leaf")
		registerChain(root, mid, leaf)

		ancestors, err := contexts.Ancestors(context.Background(), leaf)
		Expect(err).NotTo(HaveOccurred())
		Expect(namesOf(ancestors)).To(Equal([]string{leaf, mid, root}))

		descendants, err := contexts.Descendants(context.Background(), root)
		Expect(err).NotTo(HaveOccurred())
		Expect(namesOf(descendants)).To(Equal([]string{root, mid, leaf}))
	})

	It("scopes ListByTenant to the requested tenant plus every untenanted (platform) context (Phase 16f)", func() {
		platformRoot := uniqueContext("platform")
		Expect(contexts.Register(context.Background(), domain.Context{Context: platformRoot, Name: platformRoot})).To(Succeed())

		acmeCo, acmeUnit := uniqueContext("acme-co"), uniqueContext("acme-unit")
		Expect(contexts.Register(context.Background(), domain.Context{Context: acmeCo, Parent: platformRoot, Name: acmeCo, Tenant: "acme"})).To(Succeed())
		Expect(contexts.Register(context.Background(), domain.Context{Context: acmeUnit, Parent: acmeCo, Name: acmeUnit, Tenant: "acme"})).To(Succeed())

		globexCo := uniqueContext("globex-co")
		Expect(contexts.Register(context.Background(), domain.Context{Context: globexCo, Parent: platformRoot, Name: globexCo, Tenant: "globex"})).To(Succeed())

		acmeView, err := contexts.ListByTenant(context.Background(), "acme")
		Expect(err).NotTo(HaveOccurred())
		acmeNames := namesOf(acmeView)
		Expect(acmeNames).To(ContainElements(platformRoot, acmeCo, acmeUnit))
		Expect(acmeNames).NotTo(ContainElement(globexCo))

		globexView, err := contexts.ListByTenant(context.Background(), "globex")
		Expect(err).NotTo(HaveOccurred())
		globexNames := namesOf(globexView)
		Expect(globexNames).To(ContainElements(platformRoot, globexCo))
		Expect(globexNames).NotTo(ContainElement(acmeCo))
		Expect(globexNames).NotTo(ContainElement(acmeUnit))
	})

	It("flattens a child's draft from its parent's latest published corpus, applying overrides without ever dropping an inherited item (BR-V06/V07)", func() {
		parent, child := uniqueContext("parent"), uniqueContext("child")
		registerChain(parent, child)

		Expect(seedWorkingItem(integrationDB, parent, "currency", "usd", map[string]any{"name": "US Dollar"})).To(Succeed())
		Expect(seedWorkingItem(integrationDB, parent, "currency", "eur", map[string]any{"name": "Euro"})).To(Succeed())
		_, err := corpus.CreateDraft(context.Background(), parent, "seed")
		Expect(err).NotTo(HaveOccurred())
		_, err = corpus.Publish(context.Background(), parent)
		Expect(err).NotTo(HaveOccurred())

		draft, err := corpus.CreateDraft(context.Background(), child, "child draft")
		Expect(err).NotTo(HaveOccurred())
		Expect(draft.BaseContextVersion).NotTo(BeNil())
		Expect(*draft.BaseContextVersion).To(Equal(1))

		items, err := corpus.ItemsAtVersion(context.Background(), child, draft.Version)
		Expect(err).NotTo(HaveOccurred())
		byCode := itemsByCode(items)
		Expect(byCode).To(HaveLen(2))
		Expect(byCode["usd"].SourceContext).To(Equal(parent))
		Expect(byCode["usd"].IsOverride).To(BeFalse())

		Expect(corpus.PutDraftItem(context.Background(), child, domain.CorpusItem{
			DictionaryItem: domain.DictionaryItem{TypeKey: "currency", Code: "usd", Status: domain.StatusActive, Attrs: map[string]any{"name": "US Dollar (child override)"}},
			SourceContext:  child, IsOverride: true,
		})).To(Succeed())
		Expect(corpus.PutDraftItem(context.Background(), child, domain.CorpusItem{
			DictionaryItem: domain.DictionaryItem{TypeKey: "currency", Code: "gbp", Status: domain.StatusActive, Attrs: map[string]any{"name": "Pound Sterling"}},
			SourceContext:  child, IsOverride: false,
		})).To(Succeed())

		items, err = corpus.ItemsAtVersion(context.Background(), child, draft.Version)
		Expect(err).NotTo(HaveOccurred())
		byCode = itemsByCode(items)
		Expect(byCode).To(HaveLen(3))
		Expect(byCode["usd"].SourceContext).To(Equal(child))
		Expect(byCode["usd"].IsOverride).To(BeTrue())
		Expect(byCode["eur"].SourceContext).To(Equal(parent)) // untouched, still inherited
		Expect(byCode["gbp"].SourceContext).To(Equal(child))

		_, err = corpus.Publish(context.Background(), child)
		Expect(err).NotTo(HaveOccurred())

		// Parent publishes v2 adding "jpy". BR-V08: this must not alter the
		// child's already-published v1.
		Expect(seedWorkingItem(integrationDB, parent, "currency", "jpy", map[string]any{"name": "Yen"})).To(Succeed())
		_, err = corpus.CreateDraft(context.Background(), parent, "add jpy")
		Expect(err).NotTo(HaveOccurred())
		_, err = corpus.Publish(context.Background(), parent)
		Expect(err).NotTo(HaveOccurred())

		unaffected, err := corpus.ItemsAtVersion(context.Background(), child, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(itemsByCode(unaffected)).NotTo(HaveKey("jpy"))

		// A NEW child draft picks up the parent's new item, and keeps the
		// child's own override across the re-flatten.
		childDraft2, err := corpus.CreateDraft(context.Background(), child, "pick up parent v2")
		Expect(err).NotTo(HaveOccurred())
		Expect(*childDraft2.BaseContextVersion).To(Equal(2))
		picked, err := corpus.ItemsAtVersion(context.Background(), child, childDraft2.Version)
		Expect(err).NotTo(HaveOccurred())
		byCode = itemsByCode(picked)
		Expect(byCode).To(HaveKey("jpy"))
		Expect(byCode["usd"].IsOverride).To(BeTrue())
		Expect(byCode["usd"].SourceContext).To(Equal(child))
	})

	It("inherits localizations with the item and lets a child override a single locale without overriding the item (resolved Q3)", func() {
		parent, child := uniqueContext("parent"), uniqueContext("child")
		registerChain(parent, child)

		Expect(seedWorkingItem(integrationDB, parent, "currency", "usd", map[string]any{"name": "US Dollar"})).To(Succeed())
		Expect(seedWorkingLocalization(integrationDB, parent, "currency", "usd", "en", "US Dollar")).To(Succeed())
		Expect(seedWorkingLocalization(integrationDB, parent, "currency", "usd", "es", "Dolar estadounidense")).To(Succeed())
		_, err := corpus.CreateDraft(context.Background(), parent, "seed")
		Expect(err).NotTo(HaveOccurred())
		_, err = corpus.Publish(context.Background(), parent)
		Expect(err).NotTo(HaveOccurred())

		draft, err := corpus.CreateDraft(context.Background(), child, "child draft")
		Expect(err).NotTo(HaveOccurred())
		locs, err := corpus.LocalizationsAtVersion(context.Background(), child, draft.Version)
		Expect(err).NotTo(HaveOccurred())
		Expect(locs).To(HaveLen(2))
		Expect(locsByLocale(locs)["es"].SourceContext).To(Equal(parent))

		Expect(corpus.PutDraftLocalization(context.Background(), child, domain.CorpusLocalization{
			Localization:  domain.Localization{TypeKey: "currency", Code: "usd", Locale: "es", Label: "Dolar (child)"},
			SourceContext: child,
		})).To(Succeed())

		locs, err = corpus.LocalizationsAtVersion(context.Background(), child, draft.Version)
		Expect(err).NotTo(HaveOccurred())
		byLocale := locsByLocale(locs)
		Expect(byLocale).To(HaveLen(2))
		Expect(byLocale["es"].Label).To(Equal("Dolar (child)"))
		Expect(byLocale["es"].SourceContext).To(Equal(child))
		Expect(byLocale["en"].Label).To(Equal("US Dollar")) // untouched
		Expect(byLocale["en"].SourceContext).To(Equal(parent))
	})

	It("publishes atomically and rejects publish without a draft (BR-V02/V03)", func() {
		ctxName := uniqueContext("solo")
		Expect(contexts.Register(context.Background(), domain.Context{Context: ctxName, Name: ctxName})).To(Succeed())

		_, err := corpus.Publish(context.Background(), ctxName)
		Expect(errors.Is(err, domain.ErrDraftNotFound)).To(BeTrue())

		_, err = corpus.CreateDraft(context.Background(), ctxName, "v1")
		Expect(err).NotTo(HaveOccurred())
		published, err := corpus.Publish(context.Background(), ctxName)
		Expect(err).NotTo(HaveOccurred())
		Expect(published.Status).To(Equal(domain.CorpusPublished))

		versions, err := corpus.Versions(context.Background(), ctxName)
		Expect(err).NotTo(HaveOccurred())
		Expect(versions).To(HaveLen(1))
		Expect(versions[0].Status).To(Equal(domain.CorpusPublished))
	})

	It("rolls back to a non-immediately-preceding version, creating a forward version and round-tripping audit fields (BR-V04/V05)", func() {
		ctxName := uniqueContext("rollback")
		Expect(contexts.Register(context.Background(), domain.Context{Context: ctxName, Name: ctxName})).To(Succeed())

		Expect(seedWorkingItem(integrationDB, ctxName, "currency", "usd", map[string]any{"name": "v1"})).To(Succeed())
		_, err := corpus.CreateDraft(context.Background(), ctxName, "v1")
		Expect(err).NotTo(HaveOccurred())
		v1, err := corpus.Publish(context.Background(), ctxName)
		Expect(err).NotTo(HaveOccurred())

		Expect(seedWorkingItem(integrationDB, ctxName, "currency", "usd", map[string]any{"name": "v2"})).To(Succeed())
		_, err = corpus.CreateDraft(context.Background(), ctxName, "v2")
		Expect(err).NotTo(HaveOccurred())
		_, err = corpus.Publish(context.Background(), ctxName)
		Expect(err).NotTo(HaveOccurred())

		Expect(seedWorkingItem(integrationDB, ctxName, "currency", "usd", map[string]any{"name": "v3"})).To(Succeed())
		_, err = corpus.CreateDraft(context.Background(), ctxName, "v3")
		Expect(err).NotTo(HaveOccurred())
		v3, err := corpus.Publish(context.Background(), ctxName)
		Expect(err).NotTo(HaveOccurred())
		Expect(v3.Version).To(Equal(3))

		rolled, err := corpus.Rollback(context.Background(), ctxName, v1.Version, "back to v1")
		Expect(err).NotTo(HaveOccurred())
		Expect(rolled.Version).To(Equal(4)) // forward-only, never renumbers history
		Expect(rolled.Status).To(Equal(domain.CorpusPublished))

		items, err := corpus.ItemsAtVersion(context.Background(), ctxName, rolled.Version)
		Expect(err).NotTo(HaveOccurred())
		Expect(itemsByCode(items)["usd"].Attrs["name"]).To(Equal("v1"))

		versions, err := corpus.Versions(context.Background(), ctxName)
		Expect(err).NotTo(HaveOccurred())
		byVersion := map[int]domain.CorpusVersion{}
		for _, v := range versions {
			byVersion[v.Version] = v
		}
		Expect(byVersion[3].Status).To(Equal(domain.CorpusRolledBack))
		Expect(byVersion[3].RolledBackBy).NotTo(BeNil())
		Expect(*byVersion[3].RolledBackBy).To(Equal(4))

		// The now rolled-back v3 is no longer a valid rollback target (BR-V04).
		_, err = corpus.Rollback(context.Background(), ctxName, 3, "invalid")
		Expect(errors.Is(err, domain.ErrRollbackTargetNotPublic)).To(BeTrue())
	})

	It("diffs two versions to a plain list of added/removed/changed keys", func() {
		ctxName := uniqueContext("diff")
		Expect(contexts.Register(context.Background(), domain.Context{Context: ctxName, Name: ctxName})).To(Succeed())

		Expect(seedWorkingItem(integrationDB, ctxName, "currency", "usd", map[string]any{"name": "US Dollar"})).To(Succeed())
		Expect(seedWorkingItem(integrationDB, ctxName, "currency", "eur", map[string]any{"name": "Euro"})).To(Succeed())
		_, err := corpus.CreateDraft(context.Background(), ctxName, "v1")
		Expect(err).NotTo(HaveOccurred())
		v1, err := corpus.Publish(context.Background(), ctxName)
		Expect(err).NotTo(HaveOccurred())

		Expect(deleteWorkingItem(integrationDB, ctxName, "currency", "eur")).To(Succeed())
		Expect(seedWorkingItem(integrationDB, ctxName, "currency", "usd", map[string]any{"name": "US Dollar (renamed)"})).To(Succeed())
		Expect(seedWorkingItem(integrationDB, ctxName, "currency", "gbp", map[string]any{"name": "Pound Sterling"})).To(Succeed())
		_, err = corpus.CreateDraft(context.Background(), ctxName, "v2")
		Expect(err).NotTo(HaveOccurred())
		v2, err := corpus.Publish(context.Background(), ctxName)
		Expect(err).NotTo(HaveOccurred())

		entries, err := corpus.Diff(context.Background(), ctxName, v1.Version, v2.Version)
		Expect(err).NotTo(HaveOccurred())

		byCode := map[string]domain.CorpusDiffChange{}
		for _, entry := range entries {
			byCode[entry.Code] = entry.Change
		}
		Expect(byCode["gbp"]).To(Equal(domain.DiffAdded))
		Expect(byCode["eur"]).To(Equal(domain.DiffRemoved))
		Expect(byCode["usd"]).To(Equal(domain.DiffChanged))
	})
})
