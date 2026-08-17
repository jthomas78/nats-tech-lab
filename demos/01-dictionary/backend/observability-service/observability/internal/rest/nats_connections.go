package rest

// Connections + Account Activity panels — lifted from shipping-service's
// dictionary/internal/rest/nats_ops.go (Phase 17c) and
// nats_ops.go's Account Activity section (Phase 27), Main-POC-Plan.md's
// Phase 30d. Behavior is unchanged except for tenant-label resolution: the
// original tenantLabelsByAccount matched /connz rows against the LocalAddr
// of connections shipping-service itself held (one per tenant); this
// service holds only PLATFORM, so labels come from AccountsClient.Labels
// instead (see accounts_client.go). Services (the third original panel in
// nats_ops.go) is Phase 30f, not lifted here.
import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// ─── Connections ──────────────────────────────────────────────────────────

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
	// TenantLabel is resolved via AccountsClient.Labels — "PLATFORM" or a
	// tenant's own name when accounts-service recognizes the account,
	// empty for anything it doesn't (a browser tab, the nats CLI, or
	// accounts-service's own SYS-account connection, which has no
	// accounts-service Postgres row to resolve from — see BR-028).
	TenantLabel string `json:"tenantLabel,omitempty"`
}

// connzPage is /connz's own paging envelope, passed through so the panel can
// state whether the snapshot it drew is the whole picture.
type connzPage struct {
	NumConnections int `json:"numConnections"`
	Total          int `json:"total"`
	Offset         int `json:"offset"`
	Limit          int `json:"limit"`
}

// natsServerLimits carries the server's own configured ceilings, read from
// /varz. Zero means /varz was unreachable or didn't report it.
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

// varzResponse is the sliver of /varz this panel reads.
type varzResponse struct {
	MaxConnections int `json:"max_connections"`
}

// listNatsConnections godoc
//
// @Summary      List NATS connections
// @Description  Every active connection on the NATS server, across all accounts — proxies the server's own /connz monitoring endpoint (subs=true&auth=true), reshaped to camelCase. tenantLabel is resolved against accounts-service's own name<->publicKey mapping (GET /api/accounts), not a live-connection-matching trick — see BR-028 and Phase 30's design note. The page object passes through /connz's paging envelope; the server object carries the real ceiling from /varz.
// @Tags         nats
// @Produce      json
// @Success      200  {object}  natsConnectionsResponse
// @Failure      502  {object}  errorResponse  "NATS monitoring endpoint unreachable or returned an unexpected body"
// @Router       /api/nats/connections [get]
func (h *Handlers) listNatsConnections(w http.ResponseWriter, r *http.Request) {
	deps := h.deps
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

	// /varz is a secondary read, not a dependency — same as accountLabels
	// below: neither should cost the caller the connection list it did get.
	var varz varzResponse
	if err := fetchMonitor(r.Context(), client, deps.NatsMonitorURL+"/varz", &varz); err != nil {
		deps.Log.Warn("nats varz probe", "err", err)
	}

	accountLabels := deps.Accounts.Labels(r.Context())
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

// ─── Account Activity ─────────────────────────────────────────────────────

type accstatzDataStats struct {
	Msgs  int64 `json:"msgs"`
	Bytes int64 `json:"bytes"`
}

// accstatzAccount is one entry from /accstatz's account_statz array
// (https://docs.nats.io/running-a-nats-service/nats_admin/monitoring#accstatz).
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

// accountActivity is one /accstatz row, reshaped to camelCase.
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
	// on — the Admin UI renders this one as an alarm, not a stat.
	SlowConsumers int64 `json:"slowConsumers"`
}

type natsAccountActivityResponse struct {
	Accounts []accountActivity `json:"accounts"`
}

// listNatsAccountActivity godoc
//
// @Summary      List NATS account activity
// @Description  Per-account traffic and health, proxying the server's own /accstatz monitoring endpoint, reshaped to camelCase. tenantLabel is resolved the same way Connections does (accounts-service's GET /api/accounts, BR-028) — present when accounts-service recognizes the account, empty otherwise, and never something a failed accounts-service probe costs the caller the activity rollup over.
// @Tags         nats
// @Produce      json
// @Success      200  {object}  natsAccountActivityResponse
// @Failure      502  {object}  errorResponse  "NATS monitoring endpoint unreachable or returned an unexpected body"
// @Router       /api/nats/account-activity [get]
func (h *Handlers) listNatsAccountActivity(w http.ResponseWriter, r *http.Request) {
	deps := h.deps
	client := http.Client{Timeout: 5 * time.Second}

	var stat accstatzResponse
	if err := fetchMonitor(r.Context(), client, deps.NatsMonitorURL+"/accstatz", &stat); err != nil {
		deps.Log.Error("nats accstatz proxy", "err", err)
		writeError(w, http.StatusBadGateway, "nats monitoring endpoint unreachable")
		return
	}

	accountLabels := deps.Accounts.Labels(r.Context())

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
