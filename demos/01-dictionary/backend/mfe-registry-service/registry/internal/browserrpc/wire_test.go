package browserrpc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	shared "github.com/jthomas78/nats-tech-lab/shared/browserrpc"
)

func TestExplicitZeroRevisionIsNotAMissingPrecondition(t *testing.T) {
	for _, raw := range []string{`{}`, `{"ifRevision":null}`, `{"ifRevision":0}`} {
		t.Run(raw, func(t *testing.T) {
			g := NewWithT(t)
			svc := &stubService{doc: doc(1)}
			e := endpoints(svc)
			var up UpsertRequest
			g.Expect(json.Unmarshal([]byte(raw), &up)).To(Succeed())
			up.EntryID, up.Entry = "first", &domain.Entry{ID: "first"}
			_, err := e.Upsert(context.Background(), up)
			if raw == `{"ifRevision":0}` {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(svc.applied.EntryID).To(Equal("first"))
				g.Expect(svc.applied.IfRevision).To(BeZero())
			} else {
				g.Expect(err).To(MatchError(ErrRevisionRequired))
			}
			var enabled SetEnabledRequest
			g.Expect(json.Unmarshal([]byte(raw), &enabled)).To(Succeed())
			_, err = e.SetEnabled(context.Background(), enabled)
			if raw == `{"ifRevision":0}` {
				g.Expect(err).NotTo(HaveOccurred())
			} else {
				g.Expect(err).To(MatchError(ErrRevisionRequired))
			}
		})
	}
}

func TestRegistrySubjectsServeTheWireContract(t *testing.T) {
	g := NewWithT(t)
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1})
	g.Expect(err).NotTo(HaveOccurred())
	srv.Start()
	t.Cleanup(srv.Shutdown)
	g.Expect(srv.ReadyForConnections(5 * time.Second)).To(BeTrue())
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("registry-wire-test"))
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(nc.Close)
	svc := &stubService{doc: doc(12, entry("fleet", "http://localhost:7110/r.js")), origins: []string{"http://localhost:7110"}}
	a, err := Mount(nc, endpoints(svc), nil)
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = a.Stop() })
	request := func(subject, payload string) (*nats.Msg, map[string]any) {
		msg, err := nc.Request(subject, []byte(payload), 2*time.Second)
		g.Expect(err).NotTo(HaveOccurred())
		var body map[string]any
		g.Expect(json.Unmarshal(msg.Data, &body)).To(Succeed())
		g.Expect(msg.Header.Get(shared.ResponderHeader)).NotTo(BeEmpty())
		return msg, body
	}
	_, read := request(ShellReadSubject, `{"heldRevision":null}`)
	g.Expect(read["entries"]).To(HaveLen(1))
	g.Expect(read["unchanged"]).To(BeFalse())
	_, read = request(ShellReadSubject, `{"heldRevision":12}`)
	g.Expect(read["unchanged"]).To(BeTrue())
	_, curated := request(CuratedSubject, `{}`)
	g.Expect(curated["plugins"]).To(HaveLen(1))
	g.Expect(curated["allowedOrigins"]).To(HaveLen(1))
	_, accepted := request(UpsertSubject, `{"ifRevision":0,"entryId":"first","entry":{"id":"first"}}`)
	g.Expect(accepted["revision"]).To(Equal(float64(12)))
	g.Expect(svc.applied.IfRevision).To(BeZero())
	for _, subject := range []string{UpsertSubject, SetEnabledSubject} {
		msg, refusal := request(subject, `{"entryId":"fleet"}`)
		g.Expect(refusal["code"]).To(Equal("revision-required"))
		g.Expect(msg.Header.Get(micro.ErrorCodeHeader)).To(Equal("428"))
	}
	svc.err = domain.StaleRevisionError{Current: 12, Supplied: 4}
	msg, stale := request(SetEnabledSubject, `{"ifRevision":4,"entryId":"fleet","enabled":false}`)
	g.Expect(stale["conflict"]).To(BeTrue())
	g.Expect(stale["merged"]).To(BeFalse())
	g.Expect(stale["currentRevision"]).To(Equal(float64(12)))
	g.Expect(stale["yourRevision"]).To(Equal(float64(4)))
	g.Expect(msg.Header.Get(micro.ErrorCodeHeader)).To(Equal("409"))
	svc.err = domain.ErrOriginNotAllowed
	msg, refused := request(UpsertSubject, `{"ifRevision":12,"entryId":"fleet","entry":{"id":"fleet","remote":{"url":"http://evil.example"}}}`)
	g.Expect(refused["code"]).To(Equal("origin-not-allowed"))
	g.Expect(string(msg.Data)).NotTo(ContainSubstring("evil.example"))
	g.Expect(msg.Header.Get(micro.ErrorCodeHeader)).To(Equal("422"))
	msg, _ = request(UpsertSubject, `{broken`)
	g.Expect(msg.Header.Get(micro.ErrorCodeHeader)).To(Equal("400"))
	msg, err = nc.Request(AuditSubject, []byte(`{"limit":200}`), time.Second)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(msg.Data)).To(Equal("[]"))
	svc.doc = domain.Degraded()
	_, degraded := request(ShellReadSubject, `{"heldRevision":0}`)
	g.Expect(degraded["degraded"]).To(BeTrue())
	g.Expect(degraded["unchanged"]).To(BeFalse())
	g.Expect(degraded["entries"]).To(BeEmpty())
}
