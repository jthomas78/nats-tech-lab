package registry_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/notify"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/postgres"
)

var _ = Describe("Phase 8c — background observations through the composed module", func() {
	Context("BR-AS45 and decisions 77/85 — no HTTP on reads and no curation from drift", func() {
		It("serves NATS reads during a hanging fetch, then reports drift without changing the catalogue", func() {
			if pgUnavailable != "" {
				Skip(pgUnavailable)
			}
			ctx := context.Background()
			GinkgoT().Setenv("REGISTRY_PRELOAD_FILE", "")
			_, err := pgDB.ExecContext(ctx, `TRUNCATE registry.entries, registry.audit; UPDATE registry.revision SET revision = 0`)
			Expect(err).NotTo(HaveOccurred())
			allowed := registry.ParseAllowedOrigins("http://localhost:7111")
			store := postgres.NewStore(pgDB, allowed)
			entry := federated("example", "http://localhost:7111/remoteEntry.js")
			entry.Lifecycle = domain.LifecycleStatic
			before, err := store.Apply(ctx, domain.Write{Op: domain.OpUpsert, EntryID: entry.ID, Entry: &entry, Actor: domain.PreloadActor})
			Expect(err).NotTo(HaveOccurred())
			served := entry
			served.Version = "redeployed"
			body, err := json.Marshal(served)
			Expect(err).NotTo(HaveOccurred())
			started, release := make(chan struct{}), make(chan struct{})
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-started:
				default:
					close(started)
				}
				select {
				case <-release:
					_, _ = w.Write(body)
				case <-r.Context().Done():
				}
			}))
			DeferCleanup(origin.Close)
			mapping, _ := json.Marshal(map[string]string{"http://localhost:7111": origin.URL})
			origins, warnings := registry.ParseFetchOrigins(string(mapping), allowed)
			Expect(warnings).To(BeEmpty())
			srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1})
			Expect(err).NotTo(HaveOccurred())
			srv.Start()
			DeferCleanup(srv.Shutdown)
			Expect(srv.ReadyForConnections(5 * time.Second)).To(BeTrue())
			nc, err := nats.Connect(srv.ClientURL(), nats.Name("registry-drift-integration-test"))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(nc.Close)
			module, err := registry.Startup(ctx, pgDB, nil, nc, allowed, slog.Default(), origins)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(module.Stop)
			Eventually(started).Should(BeClosed())
			hints, err := nc.SubscribeSync(notify.Changed().Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(nc.Flush()).To(Succeed())
			readAdmin := func() browserrpc.CuratedResponse {
				msg, err := nc.Request(browserrpc.CuratedSubject, []byte(`{}`), 500*time.Millisecond)
				Expect(err).NotTo(HaveOccurred())
				var out browserrpc.CuratedResponse
				Expect(json.Unmarshal(msg.Data, &out)).To(Succeed())
				return out
			}
			Expect(readAdmin().Plugins[0].Drift.State).To(Equal("not checked"))
			msg, err := nc.Request(browserrpc.ShellReadSubject, []byte(`{}`), 500*time.Millisecond)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(msg.Data)).NotTo(ContainSubstring(`"drift"`))
			close(release)
			Eventually(func() string { return readAdmin().Plugins[0].Drift.State }).Should(Equal("drift"))
			admin := readAdmin()
			Expect(admin.Revision).To(Equal(before.Revision))
			Expect(admin.Plugins[0].Enabled).To(BeTrue())
			Expect(admin.Plugins[0].Conforming).To(BeTrue())
			Expect(admin.Plugins[0].Drift.Fields).To(Equal([]string{"version"}))
			Expect(admin.Plugins[0].Drift.AttemptedAt.IsZero()).To(BeFalse())
			Expect(module.Service.Read(ctx).Entries).To(Equal(before.Entries))
			after, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(after).To(Equal(before))
			audit, err := store.Audit(ctx, 100)
			Expect(err).NotTo(HaveOccurred())
			Expect(audit).To(HaveLen(1))
			_, err = hints.NextMsg(30 * time.Millisecond)
			Expect(err).To(MatchError(nats.ErrTimeout))
		})
	})
})
