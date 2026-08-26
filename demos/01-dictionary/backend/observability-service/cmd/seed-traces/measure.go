package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"

	"github.com/nats-io/nats.go/jetstream"
)

// measure answers the one question BR-053 refuses to let the plan guess: how
// big is a real trace record? It scans every key currently in the bucket and
// reports the distribution of stored value sizes alongside the span count
// that drives them, because a trace record's size is span count × per-span
// payload and a single summary number hides which of the two is moving.
//
// It runs after the OK/ERROR runs rather than instead of them, so the sample
// always includes at least one of each shape this harness knows how to
// produce. Everything else in the bucket is whatever the running stack has
// been doing — browser api.* calls, notify traffic, the demo UIs — which is
// exactly the "representative traffic" the rule asks to be sized against
// rather than a synthetic corpus of the harness's own making.
func measure(ctx context.Context, kv jetstream.KeyValue) error {
	lister, err := kv.ListKeys(ctx)
	if err != nil {
		return fmt.Errorf("list keys: %w", err)
	}

	type record struct {
		bytes int
		spans int
	}
	var records []record
	var total int
	bySpanCount := map[int]int{}

	for key := range lister.Keys() {
		entry, err := kv.Get(ctx, key)
		if err != nil {
			// A key that vanished between the listing and the read is an
			// ordinary race against the projector, not a measurement
			// failure — skip it rather than abandoning the sample.
			continue
		}
		var rec traceRecord
		if err := json.Unmarshal(entry.Value(), &rec); err != nil {
			continue
		}
		n := len(entry.Value())
		records = append(records, record{bytes: n, spans: len(rec.Spans)})
		bySpanCount[len(rec.Spans)]++
		total += n
	}

	if len(records) == 0 {
		return fmt.Errorf("no trace records in %s to measure", bucketName)
	}

	sort.Slice(records, func(i, j int) bool { return records[i].bytes < records[j].bytes })
	at := func(q float64) record {
		idx := int(q * float64(len(records)-1))
		return records[idx]
	}

	log.Printf("measured %d trace records in %s, %s stored in total", len(records), bucketName, humanBytes(total))
	log.Printf("      size    min %s   p50 %s   p90 %s   p99 %s   max %s",
		humanBytes(at(0).bytes), humanBytes(at(0.50).bytes), humanBytes(at(0.90).bytes),
		humanBytes(at(0.99).bytes), humanBytes(at(1).bytes))
	log.Printf("      mean    %s per record over %d records", humanBytes(total/len(records)), len(records))

	// Broken down by span count, because that is the variable: a bound sized
	// against a bucket dominated by single-span records would be sized
	// against the wrong shape the moment real multi-hop traffic arrives.
	counts := make([]int, 0, len(bySpanCount))
	for n := range bySpanCount {
		counts = append(counts, n)
	}
	sort.Ints(counts)
	for _, n := range counts {
		var sizes []int
		for _, r := range records {
			if r.spans == n {
				sizes = append(sizes, r.bytes)
			}
		}
		sort.Ints(sizes)
		p90 := sizes[int(0.90*float64(len(sizes)-1))]
		log.Printf("      spans  %2d  ×%-5d p50 %-9s p90 %-9s max %-9s (%s per span at p90)",
			n, len(sizes), humanBytes(sizes[len(sizes)/2]), humanBytes(p90),
			humanBytes(sizes[len(sizes)-1]), humanBytes(p90/n))
	}

	// What the measurement is FOR: a MaxBytes is only meaningful as "how many
	// records does this hold", so state that rather than leaving the reader
	// to divide. p90 is the right divisor, not the mean — the bound has to
	// survive the large end of the distribution, which is where a deep trace
	// carrying redact-then-truncate payloads lands.
	for _, cap := range []int{4 << 20, 8 << 20, 16 << 20} {
		log.Printf("      budget  %s holds ~%d records at the p90 size",
			humanBytes(cap), cap/max(at(0.90).bytes, 1))
	}
	return nil
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
