package rest

// Services panel — lifted from shipping-service's listNatsServices
// (dictionary/internal/rest/nats_ops.go, Phase 17c), Phase 30f.
//
// The original queried two live connections it happened to hold — deps.NC
// (PLATFORM) and deps.TenantNC (whichever tenant's session was currently
// active) — with the bare $SRV.STATS broadcast on each, explicitly
// documented as unable to see any other tenant's services. This service
// holds one connection, but BR-AC31 gives it a per-tenant remapped
// discovery subject (monitor.{tenant}.srv.STATS -> that tenant's own
// $SRV.STATS) for every tenant accounts-service knows about — so instead of
// querying N connections with one subject, this queries one connection with
// N+1 subjects (PLATFORM's bare $SRV.STATS, plus one per tenant), all
// concurrently for the same reason the original ran its N connections
// concurrently: collectStats always blocks for the full srvDiscoveryWindow
// (a broadcast/fan-in protocol has no "no more replies" signal), so
// querying M subjects sequentially would cost M*srvDiscoveryWindow.
// Net effect: this panel now sees every tenant's services at once, not just
// whichever one was active — a capability improvement, not just a port.
//
// Whether monitor.{tenant}.srv.STATS's reply actually routes back across
// the account boundary for every replying instance (not just the first) is
// the same not-yet-proven-live mechanism BR-AC31's design note flags —
// exercised at Phase 30i's live verification, not by this file's tests.
import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
)

// srvDiscoveryWindow is how long listNatsServices waits for $SRV.STATS
// replies to arrive after the broadcast — long enough for every
// same-datacenter instance to reply, short enough to keep the endpoint
// feeling synchronous.
const srvDiscoveryWindow = 500 * time.Millisecond

type natsEndpoint struct {
	Name                    string `json:"name"`
	Subject                 string `json:"subject"`
	QueueGroup              string `json:"queueGroup"`
	NumRequests             int    `json:"numRequests"`
	NumErrors               int    `json:"numErrors"`
	LastError               string `json:"lastError,omitempty"`
	AverageProcessingTimeMs int64  `json:"averageProcessingTimeMs"`
}

type natsServiceInstance struct {
	ID        string            `json:"id"`
	Started   time.Time         `json:"started"`
	Endpoints []natsEndpoint    `json:"endpoints"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type natsService struct {
	Name      string                `json:"name"`
	Version   string                `json:"version"`
	Instances []natsServiceInstance `json:"instances"`
}

type natsServicesResponse struct {
	Services []natsService `json:"services"`
}

// listNatsServices godoc
//
// @Summary      List NATS micro services
// @Description  Every service registered via nats.go/micro, discovered by broadcasting $SRV.STATS and collecting replies for a short window — the same protocol `nats micro stats` uses. Queried on PLATFORM's own $SRV.STATS plus, for every tenant accounts-service currently knows about, that tenant's $SRV.STATS via BR-AC31's monitor.{tenant}.srv.> import — every known tenant at once, not just one active session.
// @Tags         nats
// @Produce      json
// @Success      200  {object}  natsServicesResponse
// @Router       /api/nats/services [get]
func (h *Handlers) listNatsServices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	subjects := discoverySubjects(h.deps.Accounts.TenantNames(ctx))

	// collectStats always blocks for the full srvDiscoveryWindow, so query
	// every subject concurrently — the wall-clock cost stays one window
	// regardless of how many tenants are being queried.
	perSubject := make([][]micro.Stats, len(subjects))
	var wg sync.WaitGroup
	for i, subject := range subjects {
		wg.Add(1)
		go func(i int, subject string) {
			defer wg.Done()
			perSubject[i] = collectStats(ctx, h.deps.NC, subject)
		}(i, subject)
	}
	wg.Wait()

	type instanceKey struct{ name, id string }
	seen := map[instanceKey]micro.Stats{}
	for _, results := range perSubject {
		for _, s := range results {
			seen[instanceKey{s.Name, s.ID}] = s
		}
	}

	byName := map[string]*natsService{}
	for key, s := range seen {
		svc, ok := byName[key.name]
		if !ok {
			svc = &natsService{Name: key.name, Version: s.Version}
			byName[key.name] = svc
		}
		endpoints := make([]natsEndpoint, 0, len(s.Endpoints))
		for _, ep := range s.Endpoints {
			endpoints = append(endpoints, natsEndpoint{
				Name: ep.Name, Subject: ep.Subject, QueueGroup: ep.QueueGroup,
				NumRequests: ep.NumRequests, NumErrors: ep.NumErrors, LastError: ep.LastError,
				AverageProcessingTimeMs: ep.AverageProcessingTime.Milliseconds(),
			})
		}
		sort.Slice(endpoints, func(i, j int) bool { return endpoints[i].Name < endpoints[j].Name })
		svc.Instances = append(svc.Instances, natsServiceInstance{ID: key.id, Started: s.Started, Endpoints: endpoints, Metadata: s.Metadata})
	}

	out := make([]natsService, 0, len(byName))
	for _, svc := range byName {
		sort.Slice(svc.Instances, func(i, j int) bool { return svc.Instances[i].ID < svc.Instances[j].ID })
		out = append(out, *svc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, natsServicesResponse{Services: out})
}

// discoverySubjects builds one $SRV.STATS broadcast subject for PLATFORM
// (the bare, unprefixed control subject, via micro.ControlSubject — same
// call the pre-lift code used) plus one BR-AC31-remapped
// monitor.{tenant}.srv.STATS per tenant name.
func discoverySubjects(tenantNames []string) []string {
	platformSubject, err := micro.ControlSubject(micro.StatsVerb, "", "")
	if err != nil {
		return nil
	}
	subjects := make([]string, 0, 1+len(tenantNames))
	subjects = append(subjects, platformSubject)
	for _, name := range tenantNames {
		subjects = append(subjects, fmt.Sprintf("monitor.%s.srv.STATS", name))
	}
	return subjects
}

// collectStats broadcasts a $SRV.STATS discovery request on subject over nc
// (the bare, name-less control subject for PLATFORM, or a BR-AC31-remapped
// tenant subject) and gathers every reply that arrives within
// srvDiscoveryWindow.
func collectStats(ctx context.Context, nc *nats.Conn, subject string) []micro.Stats {
	if nc == nil {
		return nil
	}
	inbox := nats.NewInbox()
	sub, err := nc.SubscribeSync(inbox)
	if err != nil {
		return nil
	}
	defer sub.Unsubscribe() //nolint:errcheck
	if err := nc.PublishRequest(subject, inbox, nil); err != nil {
		return nil
	}

	deadline := time.Now().Add(srvDiscoveryWindow)
	var results []micro.Stats
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return results
		}
		msg, err := sub.NextMsg(remaining)
		if err != nil {
			return results // timeout — no more replies within the window
		}
		var s micro.Stats
		if err := json.Unmarshal(msg.Data, &s); err == nil {
			results = append(results, s)
		}
		select {
		case <-ctx.Done():
			return results
		default:
		}
	}
}
