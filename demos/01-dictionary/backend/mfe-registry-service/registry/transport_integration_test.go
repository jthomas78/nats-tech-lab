package registry_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/notify"
)

var _ = Describe("BR-AS27/28/31 — registry request/reply through the composed module", func() {
	It("curates from revision zero, publishes only its committed revision, and serves the authoritative read", func() {
		if pgUnavailable != "" {
			Skip(pgUnavailable)
		}
		ctx := context.Background()
		_, err := pgDB.ExecContext(ctx, `TRUNCATE registry.entries, registry.audit; UPDATE registry.revision SET revision = 0`)
		Expect(err).NotTo(HaveOccurred())
		srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1})
		Expect(err).NotTo(HaveOccurred())
		srv.Start()
		DeferCleanup(srv.Shutdown)
		Expect(srv.ReadyForConnections(5 * time.Second)).To(BeTrue())
		nc, err := nats.Connect(srv.ClientURL(), nats.Name("registry-integration-test"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(nc.Close)
		module, err := registry.Startup(ctx, pgDB, nil, nc, domain.NewAllowlist([]string{"http://localhost:7110"}), slog.Default())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(module.Stop)
		hints, err := nc.SubscribeSync(notify.Changed().Name)
		Expect(err).NotTo(HaveOccurred())
		Expect(nc.Flush()).To(Succeed())
		body := []byte(`{"ifRevision":0,"entryId":"fleet","entry":{"id":"fleet","enabled":true,"name":"fixture","contributions":[{"kind":"shell-footer","id":"status"}],"remote":{"kind":"federated","url":"http://localhost:7110/r.js","module":"./plugin"}}}`)
		msg, err := nc.Request(browserrpc.UpsertSubject, body, time.Second)
		Expect(err).NotTo(HaveOccurred())
		var accepted browserrpc.CuratedResponse
		Expect(json.Unmarshal(msg.Data, &accepted)).To(Succeed())
		Expect(accepted.Revision).To(Equal(int64(1)))
		Expect(accepted.Plugins).To(HaveLen(1))
		hint, err := hints.NextMsg(time.Second)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(hint.Data)).To(Equal(`{"revision":1}`))
		msg, err = nc.Request(browserrpc.ShellReadSubject, []byte(`{"heldRevision":null}`), time.Second)
		Expect(err).NotTo(HaveOccurred())
		var read browserrpc.ReadResponse
		Expect(json.Unmarshal(msg.Data, &read)).To(Succeed())
		Expect(read.Revision).To(Equal(accepted.Revision))
		Expect(read.Plugins).To(HaveLen(1))
		msg, err = nc.Request(browserrpc.UpsertSubject, body, time.Second)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(msg.Data)).To(ContainSubstring(`"conflict":true`))
		_, err = hints.NextMsg(30 * time.Millisecond)
		Expect(err).To(MatchError(nats.ErrTimeout))
		var auditCount int
		Expect(pgDB.QueryRowContext(ctx, `SELECT count(*) FROM registry.audit`).Scan(&auditCount)).To(Succeed())
		Expect(auditCount).To(Equal(2), "accepted and stale writes are both audited")
	})
})
