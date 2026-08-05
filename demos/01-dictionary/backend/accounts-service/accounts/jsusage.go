package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// JSUsage holds the live JetStream usage for one account, sourced from the
// NATS server's /jsz monitoring endpoint, joined with the Postgres-stored
// limits so callers can see both at once.
type JSUsage struct {
	Name      string    `json:"name"`
	Streams   JSCounter `json:"streams"`
	Consumers JSCounter `json:"consumers"`
	Mem       JSCounter `json:"mem"`
	File      JSCounter `json:"file"`
}

// JSCounter is a used/limit pair for one JetStream resource dimension.
type JSCounter struct {
	Used  int64 `json:"used"`
	Limit int64 `json:"limit"`
}

// UsageFetcher calls the NATS server's /jsz?accounts=true monitoring endpoint
// and joins the per-account live stats with the Postgres-stored limits.
type UsageFetcher struct {
	monitorURL string
	store      *Store
}

func NewUsageFetcher(monitorURL string, store *Store) *UsageFetcher {
	return &UsageFetcher{monitorURL: monitorURL, store: store}
}

// jszAccount is the per-account shape inside /jsz?accounts=true&streams=true.
// AccountDetail embeds JetStreamStats (memory/storage) and has a stream_detail
// array populated only when ?streams=true is set — there is no bare numeric
// stream count in the response, so stream count = len(stream_detail). Each
// stream_detail element's state.consumer_count is already included at this
// verbosity level (no separate ?consumers=true needed), so a per-account
// consumer total is the sum of consumer_count across stream_detail.
// Field names match the NATS server's snake_case wire format exactly
// (JetStreamStats.Store serialises as "storage", not "store").
type jszAccount struct {
	ID            string            `json:"id"`
	StreamDetails []jszStreamDetail `json:"stream_detail"` // one element per stream; count = len
	Memory        int64             `json:"memory"`
	Store         int64             `json:"storage"` // was "store" — NATS uses "storage"
}

type jszStreamDetail struct {
	State jszStreamState `json:"state"`
}

type jszStreamState struct {
	Consumers int `json:"consumer_count"`
}

type jszResponse struct {
	AccountDetails []jszAccount `json:"account_details"`
}

// FetchAll returns live JetStream usage joined with Postgres limits for every
// known account. Accounts not yet seen by the NATS server (zero JetStream
// activity) appear with zero usage values.
func (f *UsageFetcher) FetchAll(ctx context.Context) ([]JSUsage, error) {
	// ?streams=true is required: AccountDetail has no bare numeric stream count —
	// it exposes streams as a stream_detail array (one object per stream).
	url := f.monitorURL + "/jsz?accounts=true&account-details=true&streams=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build jsz request: %w", err)
	}
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jsz request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	var jsz jszResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsz); err != nil {
		return nil, fmt.Errorf("decode jsz response: %w", err)
	}

	// Index live stats by public key.
	live := make(map[string]jszAccount, len(jsz.AccountDetails))
	for _, a := range jsz.AccountDetails {
		live[a.ID] = a
	}

	accs, err := f.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}

	out := make([]JSUsage, 0, len(accs))
	for _, a := range accs {
		if !jsEnabled(a) {
			// SYS (and any future account seeded without JetStream limits)
			// never appears in /jsz — it has no JetStream context to report
			// on, not merely idle usage of one. Omitting it here (not from
			// the account list itself — Store.List still returns it) lets
			// the Admin UI's existing "no usage record" branch render N/A
			// instead of a misleading 0/0, without the frontend needing to
			// special-case reserved accounts.
			continue
		}
		out = append(out, usageFor(a, live[a.PublicKey]))
	}
	return out, nil
}

// jsEnabled reports whether an account has any JetStream limit configured.
// An account seeded with all four limits at zero (SYS today — see
// cmd/main.go's seedSysAccountForDisplay) was never granted JetStream at
// all, as opposed to an account that has JetStream but happens to be at
// zero usage.
func jsEnabled(a Account) bool {
	return a.JSMaxMem != 0 || a.JSMaxFile != 0 || a.JSMaxStreams != 0 || a.JSMaxConsumers != 0
}

// usageFor joins one account's Postgres-stored limits with its live /jsz
// stats. Split out from FetchAll so the join/aggregation logic (in
// particular, summing consumer_count across stream_detail) is unit-testable
// without a live NATS server or Postgres.
func usageFor(a Account, stats jszAccount) JSUsage {
	var consumers int64
	for _, sd := range stats.StreamDetails {
		consumers += int64(sd.State.Consumers)
	}
	return JSUsage{
		Name:      a.Name,
		Streams:   JSCounter{Used: int64(len(stats.StreamDetails)), Limit: a.JSMaxStreams},
		Consumers: JSCounter{Used: consumers, Limit: a.JSMaxConsumers},
		Mem:       JSCounter{Used: stats.Memory, Limit: a.JSMaxMem},
		File:      JSCounter{Used: stats.Store, Limit: a.JSMaxFile},
	}
}
