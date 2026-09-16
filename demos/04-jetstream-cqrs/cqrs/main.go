package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// The CLI. It parses flags, calls the write side, and prints. Nothing is
// decided here.
//
//	cqrs register  -vehicle V1 -plate ABC123
//	cqrs travel    -vehicle V1 -km 12.5
//	cqrs retire    -vehicle V1 -reason scrapped
//	cqrs rehydrate -vehicle V1 -snapshot=false
//	cqrs snapshotter
//	cqrs projector
//	cqrs query      -vehicle V1
//	cqrs seed       -vehicle V1 -n 10000
//	cqrs bench      -size 1000000
//	cqrs serve      -addr :20402
//	cqrs pool       -workers 4 -max-pending 1000 -ack-wait 30s
//	cqrs pool       -seed 10000

const usage = `cqrs — demo 04, JetStream as an event source

  register    -vehicle ID -plate P     put a vehicle into service
  travel      -vehicle ID -km N        record one trip
  retire      -vehicle ID -reason R    take a vehicle out of service
  rehydrate   -vehicle ID [-snapshot]  rebuild the aggregate and report the cost
              [-source bench]           read the benchmark fixture, not the demo's log
  snapshotter                          run the write-side projector (blocks)
  projector                            run the read-side projector (blocks)
  query       -vehicle ID              read the read store — one KV get, no replay
  seed        -vehicle ID -n N [-km K]  write N trips, to make the replay worth timing
  bench       [-size N]                 seed the rehydrate fixture on ODOMETER_BENCH (no size = report)
              [-rm]                     delete the fixture stream and its bucket
  serve       [-addr A] [-origin O]    the command API the browser UI posts to (blocks)
  pool        [no flags]                report what ODOMETER_POOL holds, in events and bytes
              [-seed N]                 build lesson 02's own log and fold it correctly
              [-rm]                     delete the log, both consumers and all three buckets
              -workers N                N workers on ONE durable consumer (blocks)
              [-max-pending N]          MaxAckPending, SHARED by every worker
              [-ack-wait D]             how long the server waits for an ack
              [-kill-at SEQ]            the worker holding SEQ goes silent — a real kill
              [-drain]                  rebuild from seq 1, stop when empty, print the time

  -url  NATS url (default ` + defaultURL + `)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	url := fs.String("url", defaultURL, "NATS url")
	vehicle := fs.String("vehicle", "", "vehicle id")
	plate := fs.String("plate", "", "number plate (register)")
	reason := fs.String("reason", "", "why it was retired (retire)")
	km := fs.Float64("km", 0, "kilometres travelled (travel)")
	n := fs.Int("n", 0, "how many trips to seed")
	size := fs.Int("size", 0, "benchmark fixture size in events (bench)")
	rm := fs.Bool("rm", false, "delete the benchmark fixture (bench)")
	source := fs.String("source", "live", "which log to rehydrate from: live or bench")
	snapshot := fs.Bool("snapshot", true, "rehydrate from the snapshot, then replay the tail")
	addr := fs.String("addr", defaultServeAddr, "address the command API listens on")
	workers := fs.Int("workers", 4, "how many workers bind to the pool consumer")
	maxPending := fs.Int("max-pending", 1000, "MaxAckPending on the pool consumer — shared by every worker")
	ackWait := fs.Duration("ack-wait", 30*time.Second, "AckWait on the pool consumer")
	killAt := fs.Uint64("kill-at", 0, "the worker that fetches this sequence stops fetching and never acks")
	drain := fs.Bool("drain", false, "rebuild the pool projection from seq 1, stop when drained, print the elapsed time")
	poolSize := fs.Int("seed", 0, "build lesson 02's own log, ODOMETER_POOL, with N events (pool)")
	origin := fs.String("origin", defaultOrigin, "comma-separated list of browser origins allowed to send commands")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// WHICH flags were typed, not what they hold. `-workers 4` is the
	// default and still means "run the pool", because a reader who typed it
	// asked for a run. Comparing against defaults cannot tell the two
	// apart, and the whole `pool` dispatch turns on that difference.
	typed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { typed[f.Name] = true })

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	nc, js, err := connect("lab4-cqrs-"+cmd, *url)
	if err != nil {
		return err
	}
	defer nc.Close()

	setupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := ensureStream(setupCtx, js); err != nil {
		return err
	}
	writeKV, err := ensureKV(setupCtx, js, WriteKV)
	if err != nil {
		return err
	}
	readKV, err := ensureKV(setupCtx, js, ReadKV)
	if err != nil {
		return err
	}

	switch cmd {
	case "snapshotter":
		fmt.Printf("snapshotter running — folding %s into KV %s. ctrl-c to stop.\n", StreamName, WriteKV)
		return runSnapshotter(ctx, js, writeKV)

	case "projector":
		fmt.Printf("projector running — folding %s into KV %s. ctrl-c to stop.\n", StreamName, ReadKV)
		return runProjector(ctx, js, readKV)

	// Four jobs behind one word. poolActionFor decides which, and it is a
	// pure function with its own specs -- see pool_cmd.go.
	case "pool":
		action, err := poolActionFor(typed)
		if err != nil {
			return err
		}
		switch action {
		case poolRemove:
			return dropPool(ctx, js)

		case poolSeed:
			// Refuse the size BEFORE announcing the work. A bad
			// size that printed "seeding" first would say it had
			// started something it never started.
			if _, err := poolPlanFor(*poolSize); err != nil {
				return err
			}
			// The fold runs inside the seed and is most of the
			// wait, so say what is happening before it starts.
			fmt.Printf("seeding     %s with %d events, then folding it correctly into %s\n",
				Pool.Stream, *poolSize, PoolTruthKV)
			state, err := seedPool(ctx, js, *poolSize)
			if err != nil {
				return err
			}
			printPoolState(state)
			return nil

		case poolReport:
			// A question, not an instruction. It creates no
			// consumer and writes no key.
			state, err := poolState(ctx, js)
			if err != nil {
				return err
			}
			printPoolState(state)
			return nil
		}

		if *workers < 1 {
			return fmt.Errorf("-workers must be at least 1")
		}
		poolKV, err := ensureKV(setupCtx, js, PoolKV)
		if err != nil {
			return err
		}
		workersKV, err := ensureExpiringKV(setupCtx, js, PoolWorkersKV, PoolWorkerTTL)
		if err != nil {
			return err
		}
		cfg := PoolConfig{
			Workers:    *workers,
			MaxPending: *maxPending,
			AckWait:    *ackWait,
			KillAt:     *killAt,
			Drain:      *drain,
		}
		fmt.Printf("%d workers bound to ONE durable consumer %s — MaxAckPending %d (shared), AckWait %s\n",
			cfg.Workers, PoolConsumer, cfg.MaxPending, cfg.AckWait)
		fmt.Printf("folding into KV %s — heartbeats in KV %s\n", PoolKV, PoolWorkersKV)
		if cfg.KillAt != 0 {
			fmt.Printf("the worker that fetches #%d will stop fetching and never ack\n", cfg.KillAt)
		}
		res, err := runPool(ctx, js, poolKV, workersKV, cfg)
		if err != nil {
			return err
		}
		printPool(cfg, res)
		return nil

	case "serve":
		fmt.Printf("command API on %s — accepting register, travel, retire from %s.\n", *addr, *origin)
		fmt.Printf("reads do NOT come through here; the browser watches NATS directly.\n")
		return runServe(ctx, js, writeKV, *addr, strings.Split(*origin, ","))

	case "query":
		if *vehicle == "" {
			return fmt.Errorf("-vehicle is required")
		}
		entry, elapsed, err := queryVehicle(ctx, readKV, *vehicle)
		if err != nil {
			return err
		}
		printQuery(*vehicle, entry, elapsed)
		return nil

	case "seed":
		if *vehicle == "" {
			return fmt.Errorf("-vehicle is required")
		}
		trip := *km
		if trip == 0 {
			trip = 1
		}
		elapsed, err := seed(ctx, js, writeKV, *vehicle, *n, trip)
		if err != nil {
			return err
		}
		fmt.Printf("seeded      %d trips of %.1f km in %s\n", *n, trip, elapsed)
		return nil

	case "rehydrate":
		if *vehicle == "" {
			return fmt.Errorf("-vehicle is required")
		}
		src, kv := Live, writeKV
		if *source == "bench" {
			src = Bench
			kv, err = ensureBench(setupCtx, js)
			if err != nil {
				return err
			}
		} else if *source != "live" {
			return fmt.Errorf("-source must be live or bench")
		}
		state, err := rehydrate(ctx, js, kv, src, *vehicle, *snapshot)
		if err != nil {
			return err
		}
		printRehydration(*vehicle, state)
		return nil

	// bench builds the fixture the Rehydrate panel measures against. It is
	// the SAME code the panel's button calls, so the screen and the
	// terminal cannot report different fixtures.
	case "bench":
		benchKV, err := ensureBench(setupCtx, js)
		if err != nil {
			return err
		}
		if *rm {
			return dropBench(ctx, js)
		}
		if *size == 0 {
			state, err := benchState(ctx, js, benchKV)
			if err != nil {
				return err
			}
			printBench(state)
			return nil
		}
		state, err := seedBench(ctx, js, benchKV, *size)
		if err != nil {
			return err
		}
		fmt.Printf("seeded      %d events in %.0f ms\n", *size, state.ElapsedMs)
		printBench(state)
		return nil

	case "register":
		return command(ctx, js, writeKV, *vehicle, *snapshot, func(v Vehicle) (Event, error) {
			return v.Register(RegisterVehicle{Plate: *plate})
		})

	case "travel":
		return command(ctx, js, writeKV, *vehicle, *snapshot, func(v Vehicle) (Event, error) {
			return v.Travel(RecordTrip{Km: *km})
		})

	case "retire":
		return command(ctx, js, writeKV, *vehicle, *snapshot, func(v Vehicle) (Event, error) {
			return v.Retire(RetireVehicle{Reason: *reason})
		})

	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func command(ctx context.Context, js jetstream.JetStream, kv jetstream.KeyValue, id string, snapshot bool, decide func(Vehicle) (Event, error)) error {
	if id == "" {
		return fmt.Errorf("-vehicle is required")
	}
	state, seq, err := handleCommand(ctx, js, kv, id, snapshot, decide)
	if err != nil {
		// A rule error is the expected outcome half the time in a demo, so
		// it is printed as a refusal, not as a crash.
		return err
	}
	printRehydration(id, state)
	fmt.Printf("appended    seq %d\n", seq)
	return nil
}

// printRehydration is the honest part of the demo: it says how much work the
// rehydration actually did, so "snapshots are faster" stays a measurement.
func printRehydration(id string, r Rehydrated) {
	mode := "no snapshot"
	if r.UsedSnapshot {
		mode = "snapshot"
	}
	fmt.Printf("vehicle     %s\n", id)
	fmt.Printf("mode        %s\n", mode)
	fmt.Printf("state       %s plate=%q\n", nonEmpty(string(r.Vehicle.Status)), r.Vehicle.Plate)
	fmt.Printf("replayed    from seq %d, %d events, last seq %d\n", r.FromSeq, r.EventsRead, r.LastSeq)
	fmt.Printf("took        %s\n", r.Elapsed)
}

// printQuery shows what the read side costs: one key, no events.
func printQuery(id string, e ReadEntry, elapsed time.Duration) {
	fmt.Printf("vehicle     %s\n", id)
	fmt.Printf("mode        read store — one KV get\n")
	fmt.Printf("state       %s plate=%q\n", nonEmpty(string(e.Status)), e.Plate)
	fmt.Printf("odometer    %.1f km over %d trips\n", e.TotalKm, e.Trips)
	fmt.Printf("last trip   %s\n", tripTime(e.LastTripAt))
	fmt.Printf("projected   up to seq %d\n", e.LastSeq)
	fmt.Printf("took        %s\n", elapsed)
}

// printPool reports what the pool did and what it cost.
//
// Dropped events are the number this whole phase exists to produce. Before
// BR-OD08 the same run printed nothing at all, because the old watermark
// acked an event it never applied.
func printPool(cfg PoolConfig, r PoolResult) {
	fmt.Printf("\nworkers     %d on one durable consumer %s\n", cfg.Workers, PoolConsumer)
	fmt.Printf("max pending %d — shared by every worker\n", cfg.MaxPending)
	fmt.Printf("ack wait    %s\n", cfg.AckWait)
	fmt.Printf("events      %d handed out\n", r.Events)
	fmt.Printf("folded      %d acked\n", r.Acked)
	fmt.Printf("dropped     %d refused by BR-OD08 — out of order, terminated, gone\n", r.Dropped)
	if r.Elapsed > 0 {
		fmt.Printf("took        %s\n", r.Elapsed.Round(time.Millisecond))
		if secs := r.Elapsed.Seconds(); secs > 0 && r.Events > 0 {
			fmt.Printf("rate        %.0f events/s\n", float64(r.Events)/secs)
		}
	}
	if sh := r.Share; sh.Workers > 0 {
		// The starvation answer. Totals cannot give it: one worker doing
		// everything and eight sharing it evenly print the same Events.
		fmt.Printf("busy        %d of %d workers acked anything\n", sh.Busy, sh.Workers)
		if sh.Idle > 0 {
			fmt.Printf("idle        %d never acked — the cap is on the consumer, not on each worker\n", sh.Idle)
		}
		parts := make([]string, 0, len(sh.Acked))
		for i, n := range sh.Acked {
			parts = append(parts, fmt.Sprintf("w%d:%d", i+1, n))
		}
		fmt.Printf("spread      %s\n", strings.Join(parts, "  "))
	}
	if r.Dropped > 0 {
		fmt.Printf("\nthe pool's total is SHORT by those %d events. A dropped event is a fact\n", r.Dropped)
		fmt.Printf("that is gone: the fold's position never moves back, so nothing repairs it.\n")
	}
}

func tripTime(t time.Time) string {
	if t.IsZero() {
		return "(never)"
	}
	return t.UTC().Format(time.RFC3339)
}

func nonEmpty(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}
