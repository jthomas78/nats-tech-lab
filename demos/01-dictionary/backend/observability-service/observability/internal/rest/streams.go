package rest

// Streams panel — lifted from shipping-service's listStreams handler
// (dictionary/internal/rest/handlers.go), Phase 30e. Uses the same
// introspectableAccounts as the KV Buckets panel (kv.go).
import (
	"net/http"
	"sort"
	"strings"
)

type jsStream struct {
	Stream string `json:"stream"`
	// Kind is "stream" | "kv" | "objstore", derived from NATS' own
	// KV_/OBJ_ backing-stream prefixes. Reported rather than filtered
	// (this endpoint dropped KV_ entirely until 38e): the Streams panel is
	// the only view of a tenant's JetStream stream count, and ADR-048
	// budgets against MaxStreams: 10 — a view that hides two of the three
	// kinds cannot answer "how close to the cap is this tenant". The
	// Stream field keeps the raw backing-stream name so it stays the
	// selection key and the thing $JS.API is addressed by; the UI strips
	// the prefix for display once the kind tag carries it.
	Kind      string `json:"kind"`
	Account   string `json:"account"`
	Subjects  int    `json:"subjects"`
	Messages  uint64 `json:"messages"`
	Bytes     uint64 `json:"bytes"`
	FirstSeq  uint64 `json:"firstSeq"`
	LastSeq   uint64 `json:"lastSeq"`
	Consumers int    `json:"consumers"`
}

// accountStatusEntry is one account this backend knows about, independent
// of whether its resources (streams, KV buckets) could actually be listed —
// a suspended tenant's cross-account $JS.API access always fails, but the
// account itself is still a real, permanent entry the Streams/KV Buckets
// panels should show (dimmed via Status), not silently drop. Listed
// separately from the per-resource rows rather than repeated on each one,
// since it's the authoritative source of "which accounts exist" — a
// suspended account with zero listable streams/buckets still needs a group
// header. Shared by jsStreamsResponse (streams.go) and kvBucketsResponse
// (kv.go).
type accountStatusEntry struct {
	Name   string `json:"name"`
	Status string `json:"status"` // accounts.StatusActive ("active") | accounts.StatusSuspended ("suspended")
}

type jsStreamsResponse struct {
	Accounts []accountStatusEntry `json:"accounts"`
	Streams  []jsStream           `json:"streams"`
}

// streamKind classifies a stream by the prefix NATS itself puts on a
// bucket's backing stream: a KV bucket "ships" is the stream "KV_ships",
// an Object Store "organizations-docs" is "OBJ_organizations-docs". These
// prefixes are part of NATS' own on-the-wire naming, not a convention this
// repo chose, so matching on them is stable.
func streamKind(name string) string {
	switch {
	case strings.HasPrefix(name, "KV_"):
		return "kv"
	case strings.HasPrefix(name, "OBJ_"):
		return "objstore"
	default:
		return "stream"
	}
}

// listStreams godoc
//
// @Summary      List registered streams
// @Description  Every JetStream stream registered across every NATS account this backend can introspect — PLATFORM plus every tenant accounts-service currently knows about. Each row carries a Kind: "stream" for an event stream, "kv" for a KV bucket's KV_* backing stream, "objstore" for an Object Store's OBJ_* backing stream. KV and Object Store rows are included so this endpoint can answer "how many of the account's MaxStreams are in use" (ADR-048); /api/kv/buckets remains the richer, bucket-shaped view of the KV ones. Accounts is the authoritative account list (every account, including ones whose streams couldn't be listed, e.g. a suspended tenant) — Streams may have zero rows for an account present in Accounts.
// @Tags         streams
// @Produce      json
// @Success      200  {object}  jsStreamsResponse
// @Failure      500  {object}  errorResponse
// @Router       /api/jetstream/streams [get]
func (h *Handlers) listStreams(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	accounts := []accountStatusEntry{}
	streams := []jsStream{}
	for _, acct := range h.introspectableAccounts(ctx) {
		accounts = append(accounts, accountStatusEntry{Name: acct.name, Status: acct.status})

		lister := acct.js.ListStreams(ctx)
		for info := range lister.Info() {
			streams = append(streams, jsStream{
				Stream:    info.Config.Name,
				Kind:      streamKind(info.Config.Name),
				Account:   acct.name,
				Subjects:  len(info.Config.Subjects),
				Messages:  info.State.Msgs,
				Bytes:     info.State.Bytes,
				FirstSeq:  info.State.FirstSeq,
				LastSeq:   info.State.LastSeq,
				Consumers: info.State.Consumers,
			})
		}
		if err := lister.Err(); err != nil {
			// Best-effort per account, not fatal to the whole response: a
			// suspended tenant's cross-account $JS.API access reliably
			// fails "no responders" (its account no longer accepts
			// traffic), and that must not blank out every other account's
			// already-gathered streams — same degrade-not-fail philosophy
			// as AccountsClient.list (accounts_client.go). The account
			// itself still appears in Accounts above with its real status;
			// only its stream rows are missing.
			h.deps.Log.Warn("list streams: account unreachable, skipping", "account", acct.name, "err", err)
			continue
		}
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Name < accounts[j].Name })
	sort.Slice(streams, func(i, j int) bool {
		if streams[i].Account != streams[j].Account {
			return streams[i].Account < streams[j].Account
		}
		return streams[i].Stream < streams[j].Stream
	})
	writeJSON(w, http.StatusOK, jsStreamsResponse{Accounts: accounts, Streams: streams})
}
