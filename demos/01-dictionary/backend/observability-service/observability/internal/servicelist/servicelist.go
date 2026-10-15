// Package servicelist is the live service list (Phase 54, decision 17,
// BR-072).
//
// Operators ask for it over NATS on one api._platform.* subject. The reply
// is the lab's own versioned snapshot: every micro service instance that
// answered PING, INFO and STATS within one bounded discovery window, in
// PLATFORM and in each tenant account through BR-AC31's monitor.{tenant}.srv.>
// import. It does not read trace export, so it works with the Collector and
// Tempo down.
//
// A reply means responding, not dependency-ready. Readiness comes only from
// shared/natsready, where a service mounts it; otherwise it is unknown. No
// reply means not observed in the window, not dead. Failed or partial
// discovery is marked in the snapshot, never shown as an empty success.
//
// Clients cannot name a discovery subject. The subjects are built here.
//
// It extends the discovery in internal/rest/nats_services.go (the REST
// Services panel, retired in 54d) with INFO, readiness, account labels and
// explicit failure.
package servicelist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

// Subject is the one request subject (decision 17).
const Subject = "api._platform.observability.service.list.v1"

// SchemaVersion names the reply schema. A breaking change gets a new one.
const SchemaVersion = "lab.servicelist.v1"

// PlatformAccount labels services found on the bare $SRV.* subjects.
const PlatformAccount = "PLATFORM"

// Readiness values. Unknown means the service mounts no natsready
// responder, or it did not answer in time.
const (
	ReadinessReady    = "ready"
	ReadinessNotReady = "not-ready"
	ReadinessUnknown  = "unknown"
)

// Account status values.
const (
	AccountOK    = "ok"
	AccountError = "error"
)

// Defaults bound one discovery.
const (
	DefaultWindow       = 500 * time.Millisecond
	DefaultMaxInstances = 200
)

// Options bound one discovery. Zero values take the defaults.
type Options struct {
	// Window is how long discovery waits for PING, INFO and STATS replies.
	Window time.Duration
	// MaxInstances caps the instances in one reply. Over the cap the reply
	// is marked truncated.
	MaxInstances int
}

// TenantSource lists the tenant accounts to query. An error is reported in
// the snapshot; it never becomes an empty tenant list.
type TenantSource interface {
	Tenants(ctx context.Context) ([]string, error)
}

// Snapshot is one reply in the lab schema.
type Snapshot struct {
	Schema     string    `json:"schema"`
	ObservedAt time.Time `json:"observedAt"`
	WindowMs   int64     `json:"windowMs"`
	// Complete is false when any account failed, the tenant list failed, or
	// the reply was truncated.
	Complete  bool      `json:"complete"`
	Truncated bool      `json:"truncated"`
	Errors    []string  `json:"errors,omitempty"`
	Accounts  []Account `json:"accounts"`
	Services  []Service `json:"services"`
}

// Account is one queried account and how its discovery went.
type Account struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Service is one micro service name in one account.
type Service struct {
	Name      string     `json:"name"`
	Account   string     `json:"account"`
	Readiness string     `json:"readiness"`
	Instances []Instance `json:"instances"`
}

// Instance is one responding process. Instances are never merged.
type Instance struct {
	ID          string            `json:"id"`
	Version     string            `json:"version"`
	Description string            `json:"description,omitempty"`
	Started     time.Time         `json:"started"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Endpoints   []Endpoint        `json:"endpoints"`
}

// Endpoint is one endpoint's identity and live statistics.
type Endpoint struct {
	Name                    string `json:"name"`
	Subject                 string `json:"subject"`
	QueueGroup              string `json:"queueGroup"`
	NumRequests             int    `json:"numRequests"`
	NumErrors               int    `json:"numErrors"`
	LastError               string `json:"lastError,omitempty"`
	AverageProcessingTimeMs int64  `json:"averageProcessingTimeMs"`
}

// Discover runs one bounded discovery on nc, the PLATFORM connection.
//
// Every account is asked for INFO and STATS at once, so the cost is one
// window whatever the number of accounts. Readiness is asked after, in a
// second window, and only in PLATFORM: natsready answers on an rpc._platform
// subject that no tenant account serves.
func Discover(ctx context.Context, nc *nats.Conn, tenants TenantSource, opts Options) Snapshot {
	opts = opts.withDefaults()
	snap := Snapshot{
		Schema:     SchemaVersion,
		ObservedAt: time.Now().UTC(),
		WindowMs:   opts.Window.Milliseconds(),
		Complete:   true,
		Accounts:   []Account{},
		Services:   []Service{},
	}

	accounts := []queried{{name: PlatformAccount, prefix: ""}}
	names, err := tenants.Tenants(ctx)
	if err != nil {
		// The cause stays out of the reply; the operator sees that the tenant
		// accounts were not asked, never an empty tenant list.
		snap.Complete = false
		snap.Errors = append(snap.Errors, "tenant list unavailable: tenant accounts were not queried")
	}
	for _, n := range names {
		accounts = append(accounts, queried{name: n, prefix: "monitor." + n + ".srv."})
	}

	deadline := time.Now().Add(opts.Window)
	results := make([]accountResult, len(accounts))
	var wg sync.WaitGroup
	for i, a := range accounts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = discoverAccount(ctx, nc, a, deadline)
		}()
	}
	wg.Wait()

	var all []Service
	for i, a := range accounts {
		res := results[i]
		if res.err != nil {
			snap.Complete = false
			snap.Accounts = append(snap.Accounts, Account{Name: a.name, Status: AccountError, Error: res.err.Error()})
			continue
		}
		snap.Accounts = append(snap.Accounts, Account{Name: a.name, Status: AccountOK})
		all = append(all, res.services...)
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].Account != all[j].Account {
			return all[i].Account < all[j].Account
		}
		return all[i].Name < all[j].Name
	})

	// The cap is on instances, the unit that grows with scale.
	remaining := opts.MaxInstances
	for _, svc := range all {
		if remaining <= 0 {
			snap.Truncated = true
			break
		}
		if len(svc.Instances) > remaining {
			svc.Instances = svc.Instances[:remaining]
			snap.Truncated = true
		}
		remaining -= len(svc.Instances)
		snap.Services = append(snap.Services, svc)
	}
	if snap.Truncated {
		snap.Complete = false
		snap.Errors = append(snap.Errors, fmt.Sprintf("truncated at %d instances", opts.MaxInstances))
	}

	askReadiness(nc, snap.Services, opts.Window)
	return snap
}

func (o Options) withDefaults() Options {
	if o.Window <= 0 {
		o.Window = DefaultWindow
	}
	if o.MaxInstances <= 0 {
		o.MaxInstances = DefaultMaxInstances
	}
	return o
}

// queried is one account and the prefix its $SRV verbs are reached on.
// PLATFORM uses the bare $SRV.* subjects; a tenant uses BR-AC31's import.
type queried struct {
	name   string
	prefix string
}

func (q queried) subject(verb micro.Verb) string {
	if q.prefix == "" {
		s, _ := micro.ControlSubject(verb, "", "")
		return s
	}
	return q.prefix + verb.String()
}

type accountResult struct {
	services []Service
	err      error
}

// discoverAccount asks one account for INFO and STATS and joins the replies
// by instance ID. An instance that answered only one of the two is still
// listed: it responded.
func discoverAccount(ctx context.Context, nc *nats.Conn, a queried, deadline time.Time) accountResult {
	var (
		wg               sync.WaitGroup
		infos, stats     [][]byte
		infoErr, statErr error
	)
	wg.Add(2)
	go func() { defer wg.Done(); infos, infoErr = collect(ctx, nc, a.subject(micro.InfoVerb), deadline) }()
	go func() { defer wg.Done(); stats, statErr = collect(ctx, nc, a.subject(micro.StatsVerb), deadline) }()
	wg.Wait()
	if infoErr != nil || statErr != nil {
		return accountResult{err: fmt.Errorf("discovery request failed")}
	}

	type key struct{ name, id string }
	instances := map[key]*Instance{}
	get := func(name, id string) *Instance {
		k := key{name, id}
		in, ok := instances[k]
		if !ok {
			in = &Instance{ID: id, Endpoints: []Endpoint{}}
			instances[k] = in
		}
		return in
	}

	for _, body := range infos {
		var info micro.Info
		if json.Unmarshal(body, &info) != nil || info.ID == "" {
			continue
		}
		in := get(info.Name, info.ID)
		in.Version, in.Description, in.Metadata = info.Version, info.Description, info.Metadata
		if len(in.Endpoints) == 0 {
			for _, ep := range info.Endpoints {
				in.Endpoints = append(in.Endpoints, Endpoint{Name: ep.Name, Subject: ep.Subject, QueueGroup: ep.QueueGroup})
			}
		}
	}
	for _, body := range stats {
		var st micro.Stats
		if json.Unmarshal(body, &st) != nil || st.ID == "" {
			continue
		}
		in := get(st.Name, st.ID)
		in.Started = st.Started
		if in.Version == "" {
			in.Version = st.Version
		}
		if in.Metadata == nil {
			in.Metadata = st.Metadata
		}
		in.Endpoints = in.Endpoints[:0]
		for _, ep := range st.Endpoints {
			in.Endpoints = append(in.Endpoints, Endpoint{
				Name: ep.Name, Subject: ep.Subject, QueueGroup: ep.QueueGroup,
				NumRequests: ep.NumRequests, NumErrors: ep.NumErrors, LastError: ep.LastError,
				AverageProcessingTimeMs: ep.AverageProcessingTime.Milliseconds(),
			})
		}
	}

	byName := map[string]*Service{}
	for k, in := range instances {
		svc, ok := byName[k.name]
		if !ok {
			svc = &Service{Name: k.name, Account: a.name, Readiness: ReadinessUnknown}
			byName[k.name] = svc
		}
		sort.Slice(in.Endpoints, func(i, j int) bool { return in.Endpoints[i].Name < in.Endpoints[j].Name })
		svc.Instances = append(svc.Instances, *in)
	}
	out := make([]Service, 0, len(byName))
	for _, svc := range byName {
		sort.Slice(svc.Instances, func(i, j int) bool { return svc.Instances[i].ID < svc.Instances[j].ID })
		out = append(out, *svc)
	}
	return accountResult{services: out}
}

// collect broadcasts one request on subject and gathers every reply until
// deadline. A broadcast has no "no more replies" signal, so it always waits
// for the deadline. Only a failed subscribe or publish is an error.
func collect(ctx context.Context, nc *nats.Conn, subject string, deadline time.Time) ([][]byte, error) {
	if nc == nil {
		return nil, fmt.Errorf("no connection")
	}
	inbox := nc.NewRespInbox()
	sub, err := nc.SubscribeSync(inbox)
	if err != nil {
		return nil, err
	}
	defer sub.Unsubscribe() //nolint:errcheck
	if err := nc.PublishRequest(subject, inbox, nil); err != nil {
		return nil, err
	}
	var out [][]byte
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 || ctx.Err() != nil {
			return out, nil
		}
		msg, err := sub.NextMsg(remaining)
		if err != nil {
			return out, nil
		}
		out = append(out, msg.Data)
	}
}

// askReadiness sets Readiness on each PLATFORM service from its natsready
// responder (BR-AS62). No responder, no reply in time, or a reply that is
// not natsready's shape all leave it unknown.
func askReadiness(nc *nats.Conn, services []Service, window time.Duration) {
	var wg sync.WaitGroup
	for i := range services {
		if services[i].Account != PlatformAccount {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			msg, err := nc.Request(mferegistry.ServiceReady(services[i].Name), nil, window)
			if err != nil {
				return
			}
			var r struct {
				Ready *bool `json:"ready"`
			}
			if json.Unmarshal(msg.Data, &r) != nil || r.Ready == nil {
				return
			}
			if *r.Ready {
				services[i].Readiness = ReadinessReady
			} else {
				services[i].Readiness = ReadinessNotReady
			}
		}()
	}
	wg.Wait()
}

// Request is the request body. It has no fields today. Unknown fields are
// refused, so a client cannot pass a discovery subject or any other input.
type Request struct{}

// Mount serves Subject on nc, the PLATFORM connection. Call stop on
// shutdown.
func Mount(nc *nats.Conn, tenants TenantSource, opts Options) (stop func() error, err error) {
	opts = opts.withDefaults()
	svc, err := micro.AddService(nc, micro.Config{
		Name:        "observability-service",
		Version:     "1.0.0",
		Description: "observability-service live service list (Phase 54, BR-072)",
	})
	if err != nil {
		return nil, err
	}
	handler := micro.HandlerFunc(func(req micro.Request) {
		body := bytes.TrimSpace(req.Data())
		if len(body) > 0 {
			dec := json.NewDecoder(bytes.NewReader(body))
			dec.DisallowUnknownFields()
			var in Request
			if err := dec.Decode(&in); err != nil {
				_ = req.Error("400", "request body must be an empty JSON object", nil)
				return
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*opts.Window)
		defer cancel()
		_ = req.RespondJSON(Discover(ctx, nc, tenants, opts))
	})
	if err := svc.AddEndpoint("service-list", handler, micro.WithEndpointSubject(Subject)); err != nil {
		_ = svc.Stop()
		return nil, err
	}
	return svc.Stop, nil
}
