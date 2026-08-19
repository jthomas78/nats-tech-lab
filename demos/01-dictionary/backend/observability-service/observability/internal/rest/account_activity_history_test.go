package rest

// BR-043 — /accstatz history: a 60-minute ring buffer sampled every 10s,
// queried over a fixed 5m/30m/1h window with correctly delta'd counters.

import (
	"testing"
	"time"
)

func sample(at time.Time, acc string, conns, subs int, inBytes, outBytes int64) accstatzSample {
	return accstatzSample{
		at: at,
		accs: map[string]accstatzAccount{
			acc: {
				Account:  acc,
				Conns:    conns,
				NumSubs:  subs,
				Received: accstatzDataStats{Bytes: inBytes},
				Sent:     accstatzDataStats{Bytes: outBytes},
			},
		},
	}
}

func TestBucketSeriesProducesCorrectBucketCountAndSizePerDuration(t *testing.T) {
	asOf := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	samples := []accstatzSample{sample(asOf.Add(-time.Minute), "acme", 1, 1, 0, 0)}

	cases := []struct {
		duration   string
		wantCount  int
		wantBucket time.Duration
	}{
		{"5m", 10, 30 * time.Second},
		{"30m", 15, 2 * time.Minute},
		{"1h", 12, 5 * time.Minute},
	}
	for _, c := range cases {
		series := bucketSeries(samples, asOf, durationSpecs[c.duration], nil)
		if len(series) != 1 {
			t.Fatalf("%s: expected one account series, got %d", c.duration, len(series))
		}
		buckets := series[0].Buckets
		if len(buckets) != c.wantCount {
			t.Fatalf("%s: expected %d buckets, got %d", c.duration, c.wantCount, len(buckets))
		}
		for i := 1; i < len(buckets); i++ {
			if got := buckets[i].Ts.Sub(buckets[i-1].Ts); got != c.wantBucket {
				t.Fatalf("%s: expected %s spacing between buckets, got %s", c.duration, c.wantBucket, got)
			}
		}
	}
}

func TestBucketSeriesComputesDeltasNotRawCumulativeCounters(t *testing.T) {
	asOf := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	start := asOf.Add(-5 * time.Minute)
	samples := []accstatzSample{
		sample(start, "acme", 3, 5, 1000, 500),
		sample(start.Add(2*time.Minute), "acme", 3, 5, 1800, 900),
		sample(start.Add(4*time.Minute), "acme", 3, 5, 3000, 1200),
	}

	series := bucketSeries(samples, asOf, durationSpecs["5m"], nil)
	if len(series) != 1 {
		t.Fatalf("expected one account series, got %d", len(series))
	}
	buckets := series[0].Buckets

	var totalIn, totalOut int64
	for _, b := range buckets {
		totalIn += b.InBytesDelta
		totalOut += b.OutBytesDelta
		// A correct delta implementation never re-reports the full
		// cumulative counter in a single 30s bucket — that would only
		// happen if raw values leaked through instead of deltas.
		if b.InBytesDelta > 3000 || b.OutBytesDelta > 1200 {
			t.Fatalf("bucket %s reports a raw cumulative value, not a delta: %+v", b.Ts, b)
		}
	}
	if totalIn != 2000 {
		t.Fatalf("expected total inbound delta 3000-1000=2000 across all buckets, got %d", totalIn)
	}
	if totalOut != 700 {
		t.Fatalf("expected total outbound delta 1200-500=700 across all buckets, got %d", totalOut)
	}
}

func TestAccstatzHistoryQueryRejectsInvalidDuration(t *testing.T) {
	h := NewAccstatzHistory("", discardLogger())
	if _, ok := h.Query(time.Now(), "2h", nil); ok {
		t.Fatal("expected ok=false for an unsupported duration")
	}
	if _, ok := h.Query(time.Now(), "", nil); ok {
		t.Fatal("expected ok=false for a missing duration")
	}
}

func TestAccstatzHistoryQueryIsNilSafe(t *testing.T) {
	var h *AccstatzHistory
	resp, ok := h.Query(time.Now(), "30m", nil)
	if !ok {
		t.Fatal("expected a nil *AccstatzHistory to still validate duration and return ok=true")
	}
	if len(resp.Accounts) != 0 {
		t.Fatalf("expected no accounts from a nil history buffer, got %+v", resp.Accounts)
	}
}

func TestAccountHistoryBufferEvictsSamplesOlderThanRetention(t *testing.T) {
	buf := &accountHistoryBuffer{retain: accstatzHistoryRetention}
	t0 := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

	buf.append(t0, []accstatzAccount{{Account: "acme", Conns: 1}})
	buf.append(t0.Add(70*time.Minute), []accstatzAccount{{Account: "acme", Conns: 2}})

	got := buf.snapshot()
	if len(got) != 1 {
		t.Fatalf("expected the 70-minute-old sample evicted, leaving 1, got %d", len(got))
	}
	if got[0].accs["acme"].Conns != 2 {
		t.Fatalf("expected only the recent sample to survive, got %+v", got[0])
	}
}

func TestAccountHistoryBufferRetainsSamplesWithinWindow(t *testing.T) {
	buf := &accountHistoryBuffer{retain: accstatzHistoryRetention}
	t0 := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

	buf.append(t0, []accstatzAccount{{Account: "acme", Conns: 1}})
	buf.append(t0.Add(30*time.Minute), []accstatzAccount{{Account: "acme", Conns: 2}})

	if got := buf.snapshot(); len(got) != 2 {
		t.Fatalf("expected both samples within the 60-minute retention window, got %d", len(got))
	}
}
