package rest

// Phase 51a (BR-062) — connection OUTCOME, read from /connz's closed ring.
//
// Phase 50 made NATS users enumerable; it did not make a `0/0` row legible.
// A roster row with no live connections is either a credential nothing has
// ever used or one that was refused thirty seconds ago, and the Users panel
// could not tell those apart. This endpoint supplies the missing half.
//
// Three things were established against the running stack on 2026-08-27 and
// they are why this file looks the way it does:
//
//   - A closed /connz entry carries `jwt`, `authorized_user`, `stop` and
//     `reason`, so the BR-058 join key (the user public NKey) and the BR-060
//     credential-name decode both work here unchanged. No new join, no new
//     identity.
//   - The ring is ~100% short-TTL session churn — 59 closed entries, 31
//     distinct NKeys, ZERO long-lived service credentials. Service
//     credentials do not disconnect. That is why `reason` is the signal this
//     endpoint exists to carry and a last-seen *timestamp* is not: a
//     timestamp would be blank on exactly the credential rows it was drafted
//     to explain, whereas a refusal — `Authentication Failure` — is
//     invisible in the Admin UI today and is precisely what a REVOKED
//     credential produces. 51a is therefore also 51b's verification surface.
//   - The ring is bounded and it is not a history. An absent entry means
//     "outside the retained window", never "never connected", and BR-062
//     requires the panel to present it as the former. The paging envelope
//     below is what lets it say so honestly, the same way the live path's
//     `partial · /connz paged` note already does.
//
// Deliberately a SEPARATE endpoint rather than another field on
// /api/nats/connections: the closed ring is far larger than the live list
// (the server's own default retains up to 10,000 entries), only the Users
// panel reads it, and folding it in would make every Connections-panel
// refresh pay for data that panel never draws.

import (
	"net/http"
	"sort"
	"time"
)

// closedConnection is one closed /connz entry, reshaped to camelCase.
//
// It is deliberately narrower than natsConnection. A closed connection's
// traffic counters and subscription list are history that nothing in the
// Users panel reads, and BR-060's join needs exactly three things: who it
// was (`userKey`), why it ended (`reason`) and when (`stop`).
type closedConnection struct {
	CID uint64 `json:"cid"`
	// Name is the client's own nats.Name(), carried through for the same
	// reason the live path carries it: a Name/Credential mismatch is the
	// signal that a process is holding someone else's .creds file.
	Name    string `json:"name,omitempty"`
	Account string `json:"account,omitempty"`
	// User and UserKey are the credential's `name` claim and its public
	// NKey, identical in meaning to the live path's fields — see
	// natsConnection's own comments. UserKey is the join key (BR-058).
	User    string `json:"user,omitempty"`
	UserKey string `json:"userKey,omitempty"`
	// Reason is the server's own words for how the connection ended:
	// "Authentication Expired", "Client Closed", "Authentication Failure",
	// and so on. Passed through VERBATIM and never mapped to a friendlier
	// vocabulary of our own — an operator correlating this against the
	// server log needs the string the server used, and a translation layer
	// would silently swallow any reason NATS adds in a future release.
	Reason string    `json:"reason,omitempty"`
	Start  time.Time `json:"start"`
	Stop   time.Time `json:"stop"`
}

type natsClosedConnectionsResponse struct {
	Page        connzPage          `json:"page"`
	Connections []closedConnection `json:"connections"`
}

// listNatsClosedConnections godoc
//
// @Summary      List recently closed NATS connections
// @Description  The server's closed-connection ring — proxies /connz?state=closed&auth=true, reshaped to camelCase — supplying the Users panel with the OUTCOME of a credential's most recent connection (BR-062). reason is the server's own verbatim wording ("Authentication Expired", "Client Closed", "Authentication Failure"), never remapped. userKey is authorized_user, the same BR-058 join key the live connections endpoint reports, and the raw JWT is decoded for its name claim and dropped exactly as it is there. The ring is bounded and is not a history: an entry's ABSENCE means the connection fell outside the retained window, never that a credential has never connected, and the page envelope is passed through so a caller can say which it is looking at.
// @Tags         nats
// @Produce      json
// @Success      200  {object}  natsClosedConnectionsResponse
// @Failure      502  {object}  errorResponse  "NATS monitoring endpoint unreachable or returned an unexpected body"
// @Router       /api/nats/connections/closed [get]
func (h *Handlers) listNatsClosedConnections(w http.ResponseWriter, r *http.Request) {
	deps := h.deps
	// subs=true is deliberately omitted — a closed connection's subscription
	// list is history no caller of this endpoint reads, and it is the single
	// largest field on a /connz row.
	url := deps.NatsMonitorURL + "/connz?state=closed&auth=true"
	client := http.Client{Timeout: 5 * time.Second}

	var connz connzResponse
	if err := fetchMonitor(r.Context(), client, url, &connz); err != nil {
		deps.Log.Error("nats connz closed proxy", "err", err)
		writeError(w, http.StatusBadGateway, "nats monitoring endpoint unreachable")
		return
	}

	out := make([]closedConnection, 0, len(connz.Connections))
	for _, c := range connz.Connections {
		user := credentialName(c.JWT)
		if user == "" {
			user = c.NameTag
		}
		out = append(out, closedConnection{
			CID:     c.CID,
			Name:    c.Name,
			Account: c.Account,
			User:    user,
			UserKey: c.AuthorizedUser,
			Reason:  c.Reason,
			Start:   c.Start,
			Stop:    c.Stop,
		})
	}
	// Most recently stopped first — the opposite of the live path's CID
	// ordering, and for a different job: a caller joining this onto a roster
	// row wants that row's LATEST outcome, so the first match wins and no
	// caller has to scan the whole ring to find it.
	sort.Slice(out, func(i, j int) bool { return out[i].Stop.After(out[j].Stop) })

	writeJSON(w, http.StatusOK, natsClosedConnectionsResponse{
		Page: connzPage{
			NumConnections: connz.NumConnections,
			Total:          connz.Total,
			Offset:         connz.Offset,
			Limit:          connz.Limit,
		},
		Connections: out,
	})
}
