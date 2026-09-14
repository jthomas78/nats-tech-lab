package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
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

const usage = `cqrs — demo 04, JetStream as an event source

  register    -vehicle ID -plate P     put a vehicle into service
  travel      -vehicle ID -km N        record one trip
  retire      -vehicle ID -reason R    take a vehicle out of service
  rehydrate   -vehicle ID [-snapshot]  rebuild the aggregate and report the cost
  snapshotter                          run the write-side projector (blocks)
  projector                            run the read-side projector (blocks)
  query       -vehicle ID              read the read store — one KV get, no replay
  seed        -vehicle ID -n N [-km K]  write N trips, to make the replay worth timing

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
	snapshot := fs.Bool("snapshot", true, "rehydrate from the snapshot, then replay the tail")
	if err := fs.Parse(args); err != nil {
		return err
	}

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
		state, err := rehydrate(ctx, js, writeKV, *vehicle, *snapshot)
		if err != nil {
			return err
		}
		printRehydration(*vehicle, state)
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
