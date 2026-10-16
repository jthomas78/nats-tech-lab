package servicelist_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/observability-service/observability/internal/servicelist"
	"github.com/jthomas78/nats-tech-lab/shared/natsready"
)

// twoAccounts is PLATFORM and ACME, wired the way BR-AC31 wires a tenant:
// ACME exports $SRV.> with a streamed response (every instance replies), and
// PLATFORM imports it as monitor.acme.srv.>. The passwords are test values
// for this in-process server only.
const twoAccounts = `
listen: "127.0.0.1:-1"
accounts: {
  PLATFORM: {
    users: [{user: platform, password: platform-test}]
    imports: [{service: {account: ACME, subject: "$SRV.>"}, to: "monitor.acme.srv.>"}]
  }
  ACME: {
    users: [{user: acme, password: acme-test}]
    exports: [{service: "$SRV.>", response_type: stream}]
  }
}
`

type rig struct {
	platform *nats.Conn
	acme     *nats.Conn
}

func startRig() rig {
	path := filepath.Join(GinkgoT().TempDir(), "accounts.conf")
	Expect(os.WriteFile(path, []byte(twoAccounts), 0o600)).To(Succeed())
	opts, err := server.ProcessConfigFile(path)
	Expect(err).NotTo(HaveOccurred())
	opts.NoLog, opts.NoSigs = true, true
	srv, err := server.NewServer(opts)
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())
	DeferCleanup(srv.Shutdown)

	connect := func(user, pass string) *nats.Conn {
		nc, err := nats.Connect(srv.ClientURL(), nats.UserInfo(user, pass), nats.Name("servicelist-spec-"+user))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(nc.Close)
		return nc
	}
	return rig{platform: connect("platform", "platform-test"), acme: connect("acme", "acme-test")}
}

// addService registers a micro service with one endpoint. A request whose
// body is "fail" gets a micro error reply.
func addService(nc *nats.Conn, name, version string) micro.Service {
	svc, err := micro.AddService(nc, micro.Config{Name: name, Version: version, Description: name + " for the spec"})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = svc.Stop() })
	Expect(svc.AddEndpoint("item-get", micro.HandlerFunc(func(req micro.Request) {
		if string(req.Data()) == "fail" {
			_ = req.Error("500", "item lookup failed", nil)
			return
		}
		_ = req.Respond([]byte(`{}`))
	}), micro.WithEndpointSubject("rpc.acme."+name+".item.get.v1"))).To(Succeed())
	return svc
}

type tenants struct {
	names []string
	err   error
}

func (t tenants) Tenants(context.Context) ([]string, error) { return t.names, t.err }

// hangingTenants is accounts-service with its Postgres paused: the call
// returns only when its context ends.
type hangingTenants struct{}

func (hangingTenants) Tenants(ctx context.Context) ([]string, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

var acmeOnly = tenants{names: []string{"acme"}}

var short = servicelist.Options{Window: 200 * time.Millisecond}

func find(snap servicelist.Snapshot, account, name string) (servicelist.Service, bool) {
	for _, s := range snap.Services {
		if s.Account == account && s.Name == name {
			return s, true
		}
	}
	return servicelist.Service{}, false
}

func account(snap servicelist.Snapshot, name string) servicelist.Account {
	for _, a := range snap.Accounts {
		if a.Name == name {
			return a
		}
	}
	Fail("account " + name + " was not queried")
	return servicelist.Account{}
}

var _ = Describe("BR-072: the live service list", func() {
	var r rig

	BeforeEach(func() { r = startRig() })

	Context("BR-072: discovery across PLATFORM and the tenant imports", func() {
		It("finds a PLATFORM service and a tenant service, each labelled with the account it was queried in", func() {
			addService(r.platform, "refdata-service", "1.0.0")
			addService(r.acme, "organizations-service", "2.0.0")

			snap := servicelist.Discover(context.Background(), r.platform, acmeOnly, short)

			_, ok := find(snap, servicelist.PlatformAccount, "refdata-service")
			Expect(ok).To(BeTrue(), "refdata-service in PLATFORM")
			_, ok = find(snap, "acme", "organizations-service")
			Expect(ok).To(BeTrue(), "organizations-service in acme")
			Expect(account(snap, servicelist.PlatformAccount).Status).To(Equal(servicelist.AccountOK))
			Expect(account(snap, "acme").Status).To(Equal(servicelist.AccountOK))
			Expect(snap.Complete).To(BeTrue())
		})

		It("keeps two instances of one service apart (BR-027)", func() {
			a := addService(r.platform, "refdata-service", "1.0.0")
			b := addService(r.platform, "refdata-service", "1.0.0")

			snap := servicelist.Discover(context.Background(), r.platform, acmeOnly, short)

			svc, ok := find(snap, servicelist.PlatformAccount, "refdata-service")
			Expect(ok).To(BeTrue())
			var ids []string
			for _, in := range svc.Instances {
				ids = append(ids, in.ID)
			}
			Expect(ids).To(ConsistOf(a.Info().ID, b.Info().ID))
		})

		It("carries INFO identity: version, description, endpoint subject and queue group", func() {
			addService(r.platform, "refdata-service", "1.4.2")

			snap := servicelist.Discover(context.Background(), r.platform, acmeOnly, short)

			svc, _ := find(snap, servicelist.PlatformAccount, "refdata-service")
			Expect(svc.Instances).To(HaveLen(1))
			in := svc.Instances[0]
			Expect(in.Version).To(Equal("1.4.2"))
			Expect(in.Description).To(Equal("refdata-service for the spec"))
			Expect(in.Endpoints).To(HaveLen(1))
			Expect(in.Endpoints[0].Subject).To(Equal("rpc.acme.refdata-service.item.get.v1"))
			Expect(in.Endpoints[0].QueueGroup).To(Equal("q"))
		})

		It("carries STATS: requests, errors and the last error after real calls", func() {
			addService(r.platform, "refdata-service", "1.0.0")
			for _, body := range []string{"ok", "ok", "fail"} {
				_, err := r.platform.Request("rpc.acme.refdata-service.item.get.v1", []byte(body), time.Second)
				Expect(err).NotTo(HaveOccurred())
			}

			snap := servicelist.Discover(context.Background(), r.platform, acmeOnly, short)

			svc, _ := find(snap, servicelist.PlatformAccount, "refdata-service")
			Expect(svc.Instances).To(HaveLen(1))
			ep := svc.Instances[0].Endpoints[0]
			Expect(ep.NumRequests).To(Equal(3))
			Expect(ep.NumErrors).To(Equal(1))
			Expect(ep.LastError).To(ContainSubstring("item lookup failed"))
		})

		It("stops listing a stopped instance, and lists its replacement after a restart", func() {
			first := addService(r.platform, "refdata-service", "1.0.0")
			firstID := first.Info().ID
			Expect(first.Stop()).To(Succeed())

			snap := servicelist.Discover(context.Background(), r.platform, acmeOnly, short)
			_, ok := find(snap, servicelist.PlatformAccount, "refdata-service")
			Expect(ok).To(BeFalse(), "a stopped instance is not observed")

			second := addService(r.platform, "refdata-service", "1.0.0")
			snap = servicelist.Discover(context.Background(), r.platform, acmeOnly, short)
			svc, ok := find(snap, servicelist.PlatformAccount, "refdata-service")
			Expect(ok).To(BeTrue())
			Expect(svc.Instances).To(HaveLen(1))
			Expect(svc.Instances[0].ID).To(Equal(second.Info().ID))
			Expect(svc.Instances[0].ID).NotTo(Equal(firstID))
		})
	})

	Context("BR-072: responding is not ready (BR-AS62)", func() {
		mountReady := func(check natsready.Check) {
			resp, err := natsready.Mount(r.platform, "refdata-service", check, slog.New(slog.DiscardHandler))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(resp.Stop)
		}

		It("shows ready when the service's own readiness check passes", func() {
			addService(r.platform, "refdata-service", "1.0.0")
			mountReady(func(context.Context) error { return nil })

			snap := servicelist.Discover(context.Background(), r.platform, acmeOnly, short)

			svc, _ := find(snap, servicelist.PlatformAccount, "refdata-service")
			Expect(svc.Readiness).To(Equal(servicelist.ReadinessReady))
		})

		It("shows not-ready while micro still responds", func() {
			addService(r.platform, "refdata-service", "1.0.0")
			mountReady(func(context.Context) error { return errors.New("database gone") })

			snap := servicelist.Discover(context.Background(), r.platform, acmeOnly, short)

			svc, ok := find(snap, servicelist.PlatformAccount, "refdata-service")
			Expect(ok).To(BeTrue(), "still listed: it responds")
			Expect(svc.Readiness).To(Equal(servicelist.ReadinessNotReady))
		})

		It("shows unknown for a service with no readiness responder", func() {
			addService(r.platform, "pricing-service", "1.0.0")

			snap := servicelist.Discover(context.Background(), r.platform, acmeOnly, short)

			svc, ok := find(snap, servicelist.PlatformAccount, "pricing-service")
			Expect(ok).To(BeTrue())
			Expect(svc.Readiness).To(Equal(servicelist.ReadinessUnknown))
		})
	})

	Context("BR-072: failed and partial discovery is explicit", func() {
		It("names the snapshot schema, the observation time and the window", func() {
			before := time.Now()

			snap := servicelist.Discover(context.Background(), r.platform, acmeOnly, short)

			Expect(snap.Schema).To(Equal(servicelist.SchemaVersion))
			Expect(snap.ObservedAt).To(BeTemporally(">=", before.Truncate(time.Second)))
			Expect(snap.WindowMs).To(Equal(int64(200)))
		})

		It("marks the snapshot incomplete when the tenant list fails, and still lists PLATFORM", func() {
			addService(r.platform, "refdata-service", "1.0.0")

			snap := servicelist.Discover(context.Background(), r.platform, tenants{err: errors.New("accounts-service down")}, short)

			Expect(snap.Complete).To(BeFalse())
			Expect(snap.Errors).NotTo(BeEmpty())
			_, ok := find(snap, servicelist.PlatformAccount, "refdata-service")
			Expect(ok).To(BeTrue())
		})

		It("still lists PLATFORM when the tenant list hangs for the whole request budget", func() {
			addService(r.platform, "refdata-service", "1.0.0")
			ctx, cancel := context.WithTimeout(context.Background(), servicelist.RequestBudget(short))
			defer cancel()

			snap := servicelist.Discover(ctx, r.platform, hangingTenants{}, short)

			Expect(snap.Complete).To(BeFalse())
			Expect(snap.Errors).NotTo(BeEmpty())
			Expect(account(snap, servicelist.PlatformAccount).Status).To(Equal(servicelist.AccountOK))
			_, ok := find(snap, servicelist.PlatformAccount, "refdata-service")
			Expect(ok).To(BeTrue(), "the hung tenant list must not eat PLATFORM's window")
		})

		It("marks an account failed, never an empty success, when the request ends before its window", func() {
			addService(r.platform, "refdata-service", "1.0.0")
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			snap := servicelist.Discover(ctx, r.platform, acmeOnly, short)

			Expect(snap.Complete).To(BeFalse())
			Expect(account(snap, servicelist.PlatformAccount).Status).To(Equal(servicelist.AccountError))
			Expect(account(snap, "acme").Status).To(Equal(servicelist.AccountError))
		})

		It("marks every account failed on a closed connection, never an empty success", func() {
			r.platform.Close()

			snap := servicelist.Discover(context.Background(), r.platform, acmeOnly, short)

			Expect(snap.Complete).To(BeFalse())
			Expect(account(snap, servicelist.PlatformAccount).Status).To(Equal(servicelist.AccountError))
			Expect(account(snap, "acme").Status).To(Equal(servicelist.AccountError))
		})

		It("truncates over the instance cap, and says so", func() {
			addService(r.platform, "refdata-service", "1.0.0")
			addService(r.platform, "refdata-service", "1.0.0")

			snap := servicelist.Discover(context.Background(), r.platform, acmeOnly,
				servicelist.Options{Window: 200 * time.Millisecond, MaxInstances: 1})

			Expect(snap.Truncated).To(BeTrue())
			Expect(snap.Complete).To(BeFalse())
			var n int
			for _, s := range snap.Services {
				n += len(s.Instances)
			}
			Expect(n).To(Equal(1))
		})

		It("returns within one bounded window, whatever the number of accounts", func() {
			start := time.Now()

			servicelist.Discover(context.Background(), r.platform, tenants{names: []string{"acme", "globex", "initech"}}, short)

			Expect(time.Since(start)).To(BeNumerically("<", 3*short.Window))
		})
	})

	Context("BR-072: the NATS adapter", func() {
		BeforeEach(func() {
			addService(r.platform, "refdata-service", "1.0.0")
			stop, err := servicelist.Mount(r.platform, acmeOnly, short)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(stop)
		})

		It("replies on the versioned subject with the lab's snapshot", func() {
			msg, err := r.platform.Request(servicelist.Subject, []byte(`{}`), 5*time.Second)
			Expect(err).NotTo(HaveOccurred())
			Expect(msg.Header.Get(micro.ErrorCodeHeader)).To(BeEmpty())

			var snap servicelist.Snapshot
			Expect(json.Unmarshal(msg.Data, &snap)).To(Succeed())
			Expect(snap.Schema).To(Equal(servicelist.SchemaVersion))
			_, ok := find(snap, servicelist.PlatformAccount, "refdata-service")
			Expect(ok).To(BeTrue())
		})

		It("refuses a request that names a discovery subject", func() {
			msg, err := r.platform.Request(servicelist.Subject, []byte(`{"subject":"$SRV.PING"}`), 5*time.Second)
			Expect(err).NotTo(HaveOccurred())
			Expect(msg.Header.Get(micro.ErrorCodeHeader)).To(Equal("400"))
		})

		It("refuses a body that is not JSON", func() {
			msg, err := r.platform.Request(servicelist.Subject, []byte(`not json`), 5*time.Second)
			Expect(err).NotTo(HaveOccurred())
			Expect(msg.Header.Get(micro.ErrorCodeHeader)).To(Equal("400"))
		})
	})
})
