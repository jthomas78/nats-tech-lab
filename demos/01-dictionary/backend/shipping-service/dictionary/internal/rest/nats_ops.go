package rest

// Connections + Services panels (Phase 17c) — two read-only NATS
// observability surfaces, distinct from the RPC panel: that one traces
// individual rpc.*/api.* calls; these show what's actually attached to the
// server right now.
//
//   - Connections proxies the NATS server's own /connz monitoring endpoint
//     (port 8222 by default — see monolith.Monolith.NatsMonitorURL). It sees
//     every connection on every account (auth=true is passed so the account
//     identifier is included), independent of which tenant is active.
//   - Services broadcasts a $SRV.STATS discovery request (the nats.go/micro
//     control protocol — see ARCHITECTURE-COMMUNICATIONS.md §4) and collects
//     every reply within a short window, the same mechanism `nats micro
//     stats` uses. $SRV subjects don't cross NATS account boundaries, so
//     this only discovers services reachable on deps.NC (PLATFORM — where
//     refdata-service registers) and deps.TenantNC (the active tenant —
//     where shipping-service's browserrpc adapter registers). A service
//     registered on a different account (accounts-service registers on the
//     SYS account it already holds for JWT operations) won't appear here
//     without a third, SYS-scoped query connection — a deliberate scope cut
//     for this phase (Main-POC-Plan.md Phase 17c), not an oversight.
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

// ─── Connections ──────────────────────────────────────────────────────────

// natsConnection is one entry from the server's /connz, reshaped to
// camelCase. Only the fields the panel renders are declared — /connz
// carries more (pending_bytes, tls_*, jwt, issuer_key) that this panel has
// no use for.
type natsConnection struct {
	CID               uint64    `json:"cid"`
	Name              string    `json:"name"`
	Type              string    `json:"type"` // "nats" | "websocket"
	Lang              string    `json:"lang"`
	Version           string    `json:"version"`
	IP                string    `json:"ip"`
	Port              int       `json:"port"`
	Account           string    `json:"account"`
	RTT               string    `json:"rtt"`
	Uptime            string    `json:"uptime"`
	Idle              string    `json:"idle"`
	Start             time.Time `json:"start"`
	LastActivity      time.Time `json:"lastActivity"`
	InMsgs            int64     `json:"inMsgs"`
	OutMsgs           int64     `json:"outMsgs"`
	InBytes           int64     `json:"inBytes"`
	OutBytes          int64     `json:"outBytes"`
	Subscriptions     int       `json:"subscriptions"`
	SubscriptionsList []string  `json:"subscriptionsList,omitempty"`
	// TenantLabel is set when this connection is one shipping-service holds
	// itself (matched by local socket address — see tenantLabelsByLocalAddr)
	// — "PLATFORM" for deps.NC, or the friendly tenant name (e.g. "acme") for
	// a TenantResources entry. Empty for every connection this process
	// doesn't own (browser tabs, the nats CLI, refdata-service, ...): the
	// raw Account NKey above is all that's available for those.
	TenantLabel string `json:"tenantLabel,omitempty"`
}

// connzPage is /connz's own paging envelope, passed through so the panel can
// state whether the snapshot it drew is the whole picture. Limit is the page
// SIZE the server applied to this request (its default is 1024) — it is NOT
// max_connections, and a server holding 1024 connections is not "full". The
// snapshot is complete when Offset+NumConnections == Total; anything less
// means /connz paged and the panel is showing one page of several.
type connzPage struct {
	NumConnections int `json:"numConnections"`
	Total          int `json:"total"`
	Offset         int `json:"offset"`
	Limit          int `json:"limit"`
}

// natsServerLimits carries the server's own configured ceilings, read from
// /varz — a different fact from connzPage above, which is only about how one
// /connz response was paged. MaxConnections IS a cap: the server refuses
// connections past it, so "N of MaxConnections" is a real capacity ratio.
// Zero means /varz was unreachable or didn't report it (the panel then draws
// no capacity rail rather than implying an unlimited server).
type natsServerLimits struct {
	MaxConnections int `json:"maxConnections"`
}

type natsConnectionsResponse struct {
	Page        connzPage        `json:"page"`
	Server      natsServerLimits `json:"server"`
	Connections []natsConnection `json:"connections"`
}

// connzConnection is the wire shape of one /connz entry
// (https://docs.nats.io/running-a-nats-service/nats_admin/monitoring#connz).
// snake_case field names are the server's, not this codebase's convention —
// deliberately not reused past this decode step.
type connzConnection struct {
	CID               uint64    `json:"cid"`
	Type              string    `json:"type"`
	IP                string    `json:"ip"`
	Port              int       `json:"port"`
	Start             time.Time `json:"start"`
	LastActivity      time.Time `json:"last_activity"`
	RTT               string    `json:"rtt"`
	Uptime            string    `json:"uptime"`
	Idle              string    `json:"idle"`
	InMsgs            int64     `json:"in_msgs"`
	OutMsgs           int64     `json:"out_msgs"`
	InBytes           int64     `json:"in_bytes"`
	OutBytes          int64     `json:"out_bytes"`
	Subscriptions     int       `json:"subscriptions"`
	Name              string    `json:"name"`
	Lang              string    `json:"lang"`
	Version           string    `json:"version"`
	Account           string    `json:"account"`
	SubscriptionsList []string  `json:"subscriptions_list"`
}

type connzResponse struct {
	NumConnections int               `json:"num_connections"`
	Total          int               `json:"total"`
	Offset         int               `json:"offset"`
	Limit          int               `json:"limit"`
	Connections    []connzConnection `json:"connections"`
}

// varzResponse is the sliver of /varz this panel reads. The server publishes
// far more there (memory, JetStream config, cluster, gateways); only the
// connection ceiling is declared, since that's the one number the Connections
// panel can act on.
type varzResponse struct {
	MaxConnections int `json:"max_connections"`
}

// listNatsConnections godoc
//
// @Summary      List NATS connections
// @Description  Every active connection on the NATS server, across all accounts — proxies the server's own /connz monitoring endpoint (subs=true&auth=true), reshaped to camelCase. Independent of the active tenant: this is server-wide, not fleet-context-scoped. The page object passes through /connz's paging envelope (total/offset/limit/numConnections) so a caller can tell a complete snapshot from one page of several; limit there is the server's page size (default 1024), not max_connections. The server object carries the real ceiling, maxConnections, read from /varz — 0 when /varz was unreachable, which is a secondary read this endpoint absorbs rather than failing on.
// @Tags         nats
// @Produce      json
// @Success      200  {object}  natsConnectionsResponse
// @Failure      502  {object}  errorResponse  "NATS monitoring endpoint unreachable or returned an unexpected body"
// @Router       /api/nats/connections [get]
func (h *Handlers) listNatsConnections(w http.ResponseWriter, r *http.Request) {
	deps := h.deps()
	url := deps.NatsMonitorURL + "/connz?subs=true&auth=true"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		deps.Log.Error("nats connz proxy", "err", err)
		writeError(w, http.StatusBadGateway, "nats monitoring endpoint unreachable")
		return
	}
	defer resp.Body.Close() //nolint:errcheck
	var connz connzResponse
	if err := json.NewDecoder(resp.Body).Decode(&connz); err != nil {
		deps.Log.Error("nats connz decode", "err", err)
		writeError(w, http.StatusBadGateway, "invalid response from nats monitoring endpoint")
		return
	}

	// /varz is a secondary read, not a dependency: it supplies the connection
	// ceiling for the panel's capacity rail, and a server that won't answer it
	// shouldn't cost the caller the connection list it did get. A zero ceiling
	// reaches the panel as "don't draw the rail".
	var varz varzResponse
	if err := fetchMonitor(r.Context(), client, deps.NatsMonitorURL+"/varz", &varz); err != nil {
		deps.Log.Warn("nats varz probe", "err", err)
	}

	accountLabels := tenantLabelsByAccount(deps, connz.Connections)
	out := make([]natsConnection, 0, len(connz.Connections))
	for _, c := range connz.Connections {
		out = append(out, natsConnection{
			CID: c.CID, Name: c.Name, Type: c.Type, Lang: c.Lang, Version: c.Version,
			IP: c.IP, Port: c.Port, Account: c.Account,
			RTT: c.RTT, Uptime: c.Uptime, Idle: c.Idle,
			Start: c.Start, LastActivity: c.LastActivity,
			InMsgs: c.InMsgs, OutMsgs: c.OutMsgs, InBytes: c.InBytes, OutBytes: c.OutBytes,
			Subscriptions: c.Subscriptions, SubscriptionsList: c.SubscriptionsList,
			TenantLabel: accountLabels[c.Account],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CID < out[j].CID })
	writeJSON(w, http.StatusOK, natsConnectionsResponse{
		Page: connzPage{
			NumConnections: connz.NumConnections,
			Total:          connz.Total,
			Offset:         connz.Offset,
			Limit:          connz.Limit,
		},
		Server:      natsServerLimits{MaxConnections: varz.MaxConnections},
		Connections: out,
	})
}

// fetchMonitor GETs a NATS monitoring endpoint and decodes it into target.
// Used for the reads whose failure the caller absorbs (currently /varz) — the
// /connz read above stays inline because its failure is the endpoint's own
// 502, with a distinct message per stage (unreachable vs. undecodable).
func fetchMonitor(ctx context.Context, client http.Client, url string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: unexpected status %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

// tenantLabelsByAccount resolves a friendly label ("PLATFORM", or a tenant
// name like "acme") for every /connz row that shares an account with a
// connection shipping-service itself holds — not just the rows that ARE
// shipping-service's own connections. Two stages:
//
//  1. Find which /connz rows are OUR connections, by matching local socket
//     address (nc.LocalAddr() is exactly what the server reports back as
//     that connection's ip:port — same TCP socket, both ends). This is the
//     only way to learn our own account's NKey at all: the client library
//     doesn't expose it, and decoding it out of the credentials JWT is
//     avoidable.
//  2. Read THOSE rows' Account field — now we know "this NKey means
//     PLATFORM" / "this NKey means acme" — and apply that mapping to every
//     row in the full list, not just our own.
//
// That second stage is what makes refdata-service and the nats CLI (both on
// PLATFORM, same as shipping-service's own PLATFORM connection) resolve to
// "PLATFORM" too, and would resolve a browser tab authenticated against a
// known tenant account the same way. accounts-service is the one connection
// nothing here can label: it authenticates on the SYS account, which
// shipping-service holds no connection on — same account-boundary gap
// documented on the Services panel's $SRV discovery (see the package doc
// above), just showing up here as "stays unresolved" instead of "doesn't
// appear."
func tenantLabelsByAccount(deps Deps, connz []connzConnection) map[string]string {
	ownAddr := map[string]string{} // local socket address -> friendly label
	if deps.NC != nil {
		ownAddr[deps.NC.LocalAddr()] = "PLATFORM"
	}
	for name, tr := range deps.TenantResources {
		if tr != nil && tr.nc != nil {
			ownAddr[tr.nc.LocalAddr()] = name
		}
	}
	if len(ownAddr) == 0 {
		return nil
	}

	byAccount := map[string]string{}
	for _, c := range connz {
		if label, ok := ownAddr[fmt.Sprintf("%s:%d", c.IP, c.Port)]; ok {
			byAccount[c.Account] = label
		}
	}
	return byAccount
}

// ─── Services ─────────────────────────────────────────────────────────────

// srvDiscoveryWindow is how long listNatsServices waits for $SRV.STATS
// replies to arrive after the broadcast — long enough for every
// same-datacenter instance to reply, short enough to keep the endpoint feeling
// synchronous. Matches the order of magnitude `nats micro` CLI commands use
// for the same discovery protocol.
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
// @Description  Every service registered via nats.go/micro (see ARCHITECTURE-COMMUNICATIONS.md §4), discovered by broadcasting $SRV.STATS and collecting replies for a short window — the same protocol `nats micro stats` uses. Queried on both the PLATFORM account (deps.NC, where refdata-service registers) and the active tenant's account (deps.TenantNC, where shipping-service's browserrpc adapter registers); $SRV subjects don't cross account boundaries, so a service registered on a different account (e.g. accounts-service, which registers on the SYS account) will not appear.
// @Tags         nats
// @Produce      json
// @Success      200  {object}  natsServicesResponse
// @Router       /api/nats/services [get]
func (h *Handlers) listNatsServices(w http.ResponseWriter, r *http.Request) {
	deps := h.deps()
	conns := []*nats.Conn{deps.NC}
	if deps.TenantNC != nil && deps.TenantNC != deps.NC {
		conns = append(conns, deps.TenantNC)
	}

	// collectStats always blocks for the full srvDiscoveryWindow — a
	// broadcast/fan-in protocol has no "no more replies coming" signal, so
	// it can't return early even once every instance has already answered.
	// Querying every connection sequentially would cost
	// len(conns)*srvDiscoveryWindow (visibly slow with 2 connections); run
	// them concurrently instead — PLATFORM and the active tenant are
	// independent accounts with no ordering dependency between them, so
	// the wall-clock cost stays a single srvDiscoveryWindow regardless of
	// how many connections are queried.
	perConn := make([][]micro.Stats, len(conns))
	var wg sync.WaitGroup
	for i, nc := range conns {
		wg.Add(1)
		go func(i int, nc *nats.Conn) {
			defer wg.Done()
			perConn[i] = collectStats(r.Context(), nc)
		}(i, nc)
	}
	wg.Wait()

	type instanceKey struct{ name, id string }
	seen := map[instanceKey]micro.Stats{}
	for _, results := range perConn {
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

// ─── Account Activity ─────────────────────────────────────────────────────

// accstatzDataStats is /accstatz's message/byte counter shape, shared by its
// sent and received fields.
type accstatzDataStats struct {
	Msgs  int64 `json:"msgs"`
	Bytes int64 `json:"bytes"`
}

// accstatzAccount is one entry from /accstatz's account_statz array
// (https://docs.nats.io/running-a-nats-service/nats_admin/monitoring#accstatz).
// snake_case field names are the server's, not this codebase's convention —
// deliberately not reused past this decode step, same as connzConnection above.
type accstatzAccount struct {
	Account       string            `json:"acc"`
	Conns         int               `json:"conns"`
	LeafNodes     int               `json:"leafnodes"`
	TotalConns    int               `json:"total_conns"`
	NumSubs       int               `json:"num_subscriptions"`
	Sent          accstatzDataStats `json:"sent"`
	Received      accstatzDataStats `json:"received"`
	SlowConsumers int64             `json:"slow_consumers"`
}

type accstatzResponse struct {
	AccountStatz []accstatzAccount `json:"account_statz"`
}

// accountActivity is one /accstatz row, reshaped to camelCase. InMsgs/InBytes
// is what /accstatz calls "received" (traffic the server took in from that
// account's clients); OutMsgs/OutBytes is "sent" (traffic delivered to them)
// — matching the In/Out naming natsConnection already uses above, rather than
// carrying the server's own sent/received wording into this API.
type accountActivity struct {
	Account          string `json:"account"`
	TenantLabel      string `json:"tenantLabel,omitempty"`
	Connections      int    `json:"connections"`
	LeafNodes        int    `json:"leafNodes"`
	TotalConnections int    `json:"totalConnections"`
	Subscriptions    int    `json:"subscriptions"`
	InMsgs           int64  `json:"inMsgs"`
	InBytes          int64  `json:"inBytes"`
	OutMsgs          int64  `json:"outMsgs"`
	OutBytes         int64  `json:"outBytes"`
	// SlowConsumers is the one field on this endpoint an operator has to act
	// on — a subscriber on this account isn't draining fast enough and the
	// server is dropping messages to it. Every other field here is routine
	// traffic; the Admin UI renders this one as an alarm, not a stat (see
	// AccountActivityPanel.vue).
	SlowConsumers int64 `json:"slowConsumers"`
}

type natsAccountActivityResponse struct {
	Accounts []accountActivity `json:"accounts"`
}

// listNatsAccountActivity godoc
//
// @Summary      List NATS account activity
// @Description  Per-account traffic and health, proxying the server's own /accstatz monitoring endpoint, reshaped to camelCase. Complements Connections (every raw socket) and Services (every registered endpoint) with the account-level rollup: connection/subscription counts, message/byte volume, and slowConsumers, the one field here that means a subscriber on that account is not draining fast enough and the server is dropping messages to it. tenantLabel is resolved the same best-effort way Connections/Services do (BR-028) from a secondary /connz read: present when this process can identify the account, empty otherwise, and never something a failed /connz probe costs the caller the activity rollup over.
// @Tags         nats
// @Produce      json
// @Success      200  {object}  natsAccountActivityResponse
// @Failure      502  {object}  errorResponse  "NATS monitoring endpoint unreachable or returned an unexpected body"
// @Router       /api/nats/account-activity [get]
func (h *Handlers) listNatsAccountActivity(w http.ResponseWriter, r *http.Request) {
	deps := h.deps()
	client := http.Client{Timeout: 5 * time.Second}

	var stat accstatzResponse
	if err := fetchMonitor(r.Context(), client, deps.NatsMonitorURL+"/accstatz", &stat); err != nil {
		deps.Log.Error("nats accstatz proxy", "err", err)
		writeError(w, http.StatusBadGateway, "nats monitoring endpoint unreachable")
		return
	}

	// Account-label resolution is a secondary read here too, same as /varz is
	// for Connections: a caller shouldn't lose the activity rollup just
	// because the /connz probe backing tenantLabelsByAccount failed.
	var accountLabels map[string]string
	var connz connzResponse
	if err := fetchMonitor(r.Context(), client, deps.NatsMonitorURL+"/connz?auth=true", &connz); err != nil {
		deps.Log.Warn("nats connz probe for account labels", "err", err)
	} else {
		accountLabels = tenantLabelsByAccount(deps, connz.Connections)
	}

	out := make([]accountActivity, 0, len(stat.AccountStatz))
	for _, a := range stat.AccountStatz {
		out = append(out, accountActivity{
			Account:          a.Account,
			TenantLabel:      accountLabels[a.Account],
			Connections:      a.Conns,
			LeafNodes:        a.LeafNodes,
			TotalConnections: a.TotalConns,
			Subscriptions:    a.NumSubs,
			InMsgs:           a.Received.Msgs,
			InBytes:          a.Received.Bytes,
			OutMsgs:          a.Sent.Msgs,
			OutBytes:         a.Sent.Bytes,
			SlowConsumers:    a.SlowConsumers,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Account < out[j].Account })
	writeJSON(w, http.StatusOK, natsAccountActivityResponse{Accounts: out})
}

// collectStats broadcasts a $SRV.STATS discovery request on nc (the bare,
// name-less control subject — every registered instance replies, not just
// one; micro's internal discovery subscriptions are unqueued specifically
// for this fan-out, unlike its queued business endpoints) and gathers every
// reply that arrives within srvDiscoveryWindow.
func collectStats(ctx context.Context, nc *nats.Conn) []micro.Stats {
	if nc == nil {
		return nil
	}
	subject, err := micro.ControlSubject(micro.StatsVerb, "", "")
	if err != nil {
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
