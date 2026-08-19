package rest

// Account Activity history (BR-043, Phase 45) — /accstatz is a stateless
// snapshot (see accstatzResponse in nats_connections.go); the Admin UI's
// Overview tab needs real trend history for its charts and duration
// selector, so this file adds a 60-minute in-memory ring buffer of samples
// plus a query that buckets them into one of three fixed windows.
//
// Not persisted to Postgres or NATS KV — deliberately transient telemetry,
// not source-of-truth data (same reasoning this repo already applies to
// what does vs. doesn't get event-sourced). The buffer starts empty at
// process boot and fills in real time; a freshly-restarted service
// legitimately has less than 60 minutes of history until it's been up that
// long.

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"
)

const accstatzHistoryRetention = time.Hour

// accstatzSample is one poll tick's full /accstatz snapshot, keyed by
// account so bucketSeries can look up "the sample for account X at or
// before time T" without a linear scan of every account on every lookup.
type accstatzSample struct {
	at   time.Time
	accs map[string]accstatzAccount
}

// accountHistoryBuffer is samples in ascending time order, trimmed to the
// last `retain` on every append. Safe for concurrent use — appended by the
// poller goroutine, read by request-handling goroutines.
type accountHistoryBuffer struct {
	mu      sync.Mutex
	retain  time.Duration
	samples []accstatzSample
}

func (b *accountHistoryBuffer) append(at time.Time, accs []accstatzAccount) {
	m := make(map[string]accstatzAccount, len(accs))
	for _, a := range accs {
		m[a.Account] = a
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.samples = append(b.samples, accstatzSample{at: at, accs: m})
	cutoff := at.Add(-b.retain)
	i := 0
	for i < len(b.samples) && b.samples[i].at.Before(cutoff) {
		i++
	}
	if i > 0 {
		b.samples = b.samples[i:]
	}
}

// snapshot copies the current sample list so callers can bucket it without
// holding the lock for the duration of that work.
func (b *accountHistoryBuffer) snapshot() []accstatzSample {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]accstatzSample, len(b.samples))
	copy(out, b.samples)
	return out
}

// durationSpec pairs a supported window with the bucket size that keeps its
// chart legible — shorter windows use smaller buckets rather than a fixed
// size that would collapse a 5-minute view to one or two fat bars.
type durationSpec struct {
	window time.Duration
	bucket time.Duration
}

var durationSpecs = map[string]durationSpec{
	"5m":  {window: 5 * time.Minute, bucket: 30 * time.Second},
	"30m": {window: 30 * time.Minute, bucket: 2 * time.Minute},
	"1h":  {window: time.Hour, bucket: 5 * time.Minute},
}

// historyBucket is one bucket in an account's trend series. Connections and
// Subscriptions are point samples (the value as of this bucket's end);
// every other field is a delta against the previous bucket, because
// /accstatz's own sent/received counters are cumulative since server start,
// not per-interval — charting the raw values would draw an ever-climbing
// line instead of a throughput bar.
type historyBucket struct {
	Ts            time.Time `json:"ts"`
	Connections   int       `json:"connections"`
	Subscriptions int       `json:"subscriptions"`
	InBytesDelta  int64     `json:"inBytesDelta"`
	OutBytesDelta int64     `json:"outBytesDelta"`
	InMsgsDelta   int64     `json:"inMsgsDelta"`
	OutMsgsDelta  int64     `json:"outMsgsDelta"`
}

type accountHistorySeries struct {
	Account     string          `json:"account"`
	TenantLabel string          `json:"tenantLabel,omitempty"`
	Buckets     []historyBucket `json:"buckets"`
}

type natsAccountActivityHistoryResponse struct {
	Duration      string                 `json:"duration"`
	BucketSeconds int                    `json:"bucketSeconds"`
	Accounts      []accountHistorySeries `json:"accounts"`
}

// lastSampleAtOrBefore returns the most recent sample for account at or
// before t, nil if there is none. samples must be in ascending time order
// (accountHistoryBuffer.append's invariant).
func lastSampleAtOrBefore(samples []accstatzSample, account string, t time.Time) *accstatzAccount {
	var found *accstatzAccount
	for _, s := range samples {
		if s.at.After(t) {
			break
		}
		if a, ok := s.accs[account]; ok {
			aCopy := a
			found = &aCopy
		}
	}
	return found
}

// bucketSeries buckets samples into spec.window/spec.bucket buckets ending
// at asOf, one series per account seen anywhere in samples — not just
// within the window — so an account doesn't disappear from the response the
// moment it's briefly quiet, it just reports zeroed buckets.
func bucketSeries(samples []accstatzSample, asOf time.Time, spec durationSpec, labels map[string]string) []accountHistorySeries {
	accountSet := map[string]bool{}
	for _, s := range samples {
		for acc := range s.accs {
			accountSet[acc] = true
		}
	}
	accounts := make([]string, 0, len(accountSet))
	for acc := range accountSet {
		accounts = append(accounts, acc)
	}
	sort.Strings(accounts)

	n := int(spec.window / spec.bucket)
	start := asOf.Add(-spec.window)

	result := make([]accountHistorySeries, 0, len(accounts))
	for _, acc := range accounts {
		buckets := make([]historyBucket, 0, n)
		prev := lastSampleAtOrBefore(samples, acc, start)
		for i := 1; i <= n; i++ {
			bucketEnd := start.Add(time.Duration(i) * spec.bucket)
			cur := lastSampleAtOrBefore(samples, acc, bucketEnd)
			b := historyBucket{Ts: bucketEnd}
			if cur != nil {
				b.Connections = cur.Conns
				b.Subscriptions = cur.NumSubs
				if prev != nil {
					b.InBytesDelta = cur.Received.Bytes - prev.Received.Bytes
					b.OutBytesDelta = cur.Sent.Bytes - prev.Sent.Bytes
					b.InMsgsDelta = cur.Received.Msgs - prev.Received.Msgs
					b.OutMsgsDelta = cur.Sent.Msgs - prev.Sent.Msgs
				}
				prev = cur
			}
			buckets = append(buckets, b)
		}
		result = append(result, accountHistorySeries{Account: acc, TenantLabel: labels[acc], Buckets: buckets})
	}
	return result
}

// AccstatzHistory owns the ring buffer and the background poller that fills
// it. The zero value is not usable — construct with NewAccstatzHistory — but
// a nil *AccstatzHistory is valid to call Query on (same nil-safe convention
// AccountsClient.Labels uses), so handler tests that don't care about
// history don't need to wire one up.
type AccstatzHistory struct {
	monitorURL string
	client     http.Client
	log        *slog.Logger
	buf        *accountHistoryBuffer
}

func NewAccstatzHistory(monitorURL string, log *slog.Logger) *AccstatzHistory {
	return &AccstatzHistory{
		monitorURL: monitorURL,
		client:     http.Client{Timeout: 5 * time.Second},
		log:        log,
		buf:        &accountHistoryBuffer{retain: accstatzHistoryRetention},
	}
}

// Run polls /accstatz every interval until ctx is done. A empty monitorURL
// is a no-op — same "not configured outside Docker" degrade every other
// panel this service proxies already has.
func (a *AccstatzHistory) Run(ctx context.Context, interval time.Duration) {
	if a == nil || a.monitorURL == "" {
		return
	}
	a.pollOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.pollOnce(ctx)
		}
	}
}

func (a *AccstatzHistory) pollOnce(ctx context.Context) {
	var stat accstatzResponse
	if err := fetchMonitor(ctx, a.client, a.monitorURL+"/accstatz", &stat); err != nil {
		if a.log != nil {
			a.log.Warn("accstatz history poll", "err", err)
		}
		return
	}
	a.buf.append(time.Now(), stat.AccountStatz)
}

// Query returns duration's bucketed series for every account currently in
// the buffer. ok is false when duration isn't one of the three supported
// values — the handler turns that into a 400, not a silently-empty 200.
func (a *AccstatzHistory) Query(asOf time.Time, duration string, labels map[string]string) (natsAccountActivityHistoryResponse, bool) {
	spec, ok := durationSpecs[duration]
	if !ok {
		return natsAccountActivityHistoryResponse{}, false
	}
	resp := natsAccountActivityHistoryResponse{Duration: duration, BucketSeconds: int(spec.bucket.Seconds())}
	if a == nil {
		return resp, true
	}
	resp.Accounts = bucketSeries(a.buf.snapshot(), asOf, spec, labels)
	return resp, true
}
