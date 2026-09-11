// The odometer -- demo 02's only JetStream + CQRS example.
//
// Write side : publish evt.odometer.vehicle.{vehicleID}.travelled to the
//
//	ODOMETER stream (LimitsPolicy, replicas 3).
//
// Read side  : a durable consumer folds each event into the KV bucket
//
//	`vehicles`, key {vehicleID}, value {"totalKm":..,"trips":..}.
//
// KV only. There is no Postgres in demo 02. In demo 01 the KV entry is a
// write-through cache and Postgres is the truth; here the KV entry IS the
// read model, because the thing being measured is whether the number is
// right after a message crosses a gateway.
//
// Runs on your Mac against the published host ports. That is deliberate and
// is NOT the same rule as the `nats` CLI, which must stay in the toolbox --
// the CLI has no pinned version on a host, this binary pins every dependency
// in go.mod.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	// One JetStream domain for the whole supercluster. A domain names a
	// JetStream system, and every server in a cluster AND a supercluster must
	// carry the same name -- it may only change across a leaf-node link. Per
	// region domains were tried and silently broke JetStream; see the comment
	// in nats/nats.conf. Regions are separated by PLACEMENT and by ACCOUNTS.
	jsDomain = "lb"

	streamName   = "ODOMETER"
	bucketName   = "vehicles"
	consumerName = "odometer-projector"

	// The subject. Read it token by token:
	//
	//   evt        fixed literal, never a wildcard -- an open first token
	//              textually overlaps $SYS.> and $JS.API.>, and JetStream
	//              refuses such a stream without NoAck.
	//   odometer   the service.
	//   vehicle    the entity.
	//   {id}       the entity id. Subject-safe characters only.
	//   travelled  the event.
	//
	// NOTE -- there is NO {context} token here, and that breaks
	// ARCHITECTURE-COMMUNICATIONS section 2, which requires
	// evt.{context}.{service}.{entity}.{entity-id}.{event}.
	//
	// Why it is left out on purpose: {context} is the company or business
	// unit. This demo has no companies. It has two REGIONS, and the one
	// thing section 2 is most emphatic about is that a region is never a
	// subject token. Putting a fake context in would either invite someone
	// to write `za` there -- the exact mistake the rule exists to stop -- or
	// add a constant that means nothing and teaches nothing.
	//
	// Demo 01 carries the full six-token form and is the reference for it.
	// Do not copy this shortened subject into a service.
	subjectPrefix = "evt.odometer.vehicle."
	subjectSuffix = ".travelled"
	streamFilter  = "evt.odometer.>"
)

// eventSubject builds the subject for one vehicle.
func eventSubject(vehicleID string) string {
	return subjectPrefix + vehicleID + subjectSuffix
}

// vehicleFromSubject reads the id back out by POSITION, not by splitting on
// something clever. Fixed arity is the whole reason the taxonomy is fixed.
func vehicleFromSubject(subject string) (string, error) {
	tokens := strings.Split(subject, ".")
	if len(tokens) != 5 {
		return "", fmt.Errorf("subject %q has %d tokens, want 5", subject, len(tokens))
	}
	return tokens[3], nil
}

// regionPorts maps a region to its published client ports. All three, never
// one -- a client that knows a single address cannot tell "the stream is
// gone" from "my one address is gone".
var regionPorts = map[string][]int{
	"za": {4621, 4622, 4623},
	"au": {4721, 4722, 4723},
}

type config struct {
	region  string
	account string
	vehicle string
	km      float64
	once    bool
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	command := os.Args[1]

	fs := flag.NewFlagSet(command, flag.ExitOnError)
	var c config
	fs.StringVar(&c.region, "region", "za", "za or au")
	fs.StringVar(&c.account, "account", "", "credential name, e.g. linebooker-za (default: linebooker-<region>)")
	fs.StringVar(&c.vehicle, "vehicle", "V1", "vehicle id")
	fs.Float64Var(&c.km, "km", 0, "kilometres travelled")
	fs.BoolVar(&c.once, "once", false, "project what is waiting, then exit")
	_ = fs.Parse(os.Args[2:])

	if c.account == "" {
		c.account = "linebooker-" + c.region
	}

	var err error
	switch command {
	case "setup":
		err = runSetup(c)
	case "travelled":
		err = runTravelled(c)
	case "project":
		err = runProject(c)
	case "show":
		err = runShow(c)
	case "reset":
		err = runReset(c)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `the odometer -- demo 02's JetStream + CQRS example

  setup      create the ODOMETER stream and the vehicles bucket
  travelled  publish one trip      -vehicle V1 -km 12.5
  project    fold events into KV   -once to drain and exit
  show       print the read model
  reset      delete the stream and the bucket

common flags: -region za|au   -account <creds name>
`)
}

// connect opens a connection and a JetStream handle in the one shared domain.
//
// The domain does NOT keep the regions apart -- it cannot, both clusters are
// one supercluster and so one JetStream namespace. What keeps them apart is
// the ACCOUNT (stage B) and, inside one account, PLACEMENT. See the comment in
// nats/nats.conf for the measurement that settled this.
func connect(c config) (*nats.Conn, nats.JetStreamContext, error) {
	ports, ok := regionPorts[c.region]
	if !ok {
		return nil, nil, fmt.Errorf("unknown region %q -- want za or au", c.region)
	}
	urls := make([]string, 0, len(ports))
	for _, p := range ports {
		urls = append(urls, fmt.Sprintf("nats://127.0.0.1:%d", p))
	}

	creds := "../nats/creds/" + c.account + ".creds"
	if _, err := os.Stat(creds); err != nil {
		return nil, nil, fmt.Errorf("no credentials at %s -- run ../deploy/up.sh first", creds)
	}

	// nats.Name is mandatory in this repo: an anonymous connection cannot be
	// told apart in `nats server list connections`.
	nc, err := nats.Connect(strings.Join(urls, ","),
		nats.Name("odometer"),
		nats.UserCredentials(creds),
		nats.MaxReconnects(-1),
		nats.Timeout(5*time.Second),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to %s: %w", c.region, err)
	}

	js, err := nc.JetStream(nats.Domain(jsDomain))
	if err != nil {
		nc.Close()
		return nil, nil, err
	}
	return nc, js, nil
}

func runSetup(c config) error {
	nc, js, err := connect(c)
	if err != nil {
		return err
	}
	defer nc.Close()

	// LimitsPolicy, not InterestPolicy -- replay has to stay possible.
	// Replicas 3, because lab/02-replicas.sh already proved what R1 costs on
	// a three-node cluster.
	//
	// Placement pins the stream to THIS region's three servers. Without it the
	// supercluster's meta leader picks, and it picks its own cluster every
	// time -- so an "au" run would quietly build its stream in za.
	_, err = js.AddStream(&nats.StreamConfig{
		Name:      streamName,
		Subjects:  []string{streamFilter},
		Retention: nats.LimitsPolicy,
		Storage:   nats.FileStorage,
		Replicas:  3,
		MaxAge:    24 * time.Hour,
		Placement: &nats.Placement{Cluster: c.region},
	})
	if err != nil && !errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		return fmt.Errorf("create stream %s: %w", streamName, err)
	}

	_, err = js.CreateKeyValue(&nats.KeyValueConfig{
		Bucket:    bucketName,
		Replicas:  3,
		Placement: &nats.Placement{Cluster: c.region},
	})
	if err != nil && !errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		return fmt.Errorf("create bucket %s: %w", bucketName, err)
	}

	fmt.Printf("%s: stream %s and bucket %s are ready\n", c.region, streamName, bucketName)
	return nil
}

func runTravelled(c config) error {
	event := Travelled{Km: c.km}
	// The rule is checked here, on the write side, before anything is
	// stored. A bad event that reaches the log is permanent.
	if err := event.Validate(); err != nil {
		return err
	}

	nc, js, err := connect(c)
	if err != nil {
		return err
	}
	defer nc.Close()

	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	subject := eventSubject(c.vehicle)
	ack, err := js.Publish(subject, payload)
	if err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	fmt.Printf("%s: %s km=%v -> %s seq %d\n", c.region, subject, c.km, ack.Stream, ack.Sequence)
	return nil
}

func runProject(c config) error {
	nc, js, err := connect(c)
	if err != nil {
		return err
	}
	defer nc.Close()

	kv, err := js.KeyValue(bucketName)
	if err != nil {
		return fmt.Errorf("open bucket %s: %w", bucketName, err)
	}

	// A durable pull consumer. Durable so a restart resumes where it stopped
	// instead of replaying the whole stream and doubling every total.
	sub, err := js.PullSubscribe(streamFilter, consumerName, nats.BindStream(streamName))
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	defer sub.Unsubscribe()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	for {
		msgs, err := sub.Fetch(16, nats.MaxWait(2*time.Second))
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) {
				if c.once {
					fmt.Printf("%s: nothing left to project\n", c.region)
					return nil
				}
			} else {
				return err
			}
		}
		for _, m := range msgs {
			if err := applyMessage(kv, m); err != nil {
				// Do NOT ack. An unacked message is redelivered, which is
				// what you want for a transient failure and what makes a
				// permanent one visible instead of silent.
				fmt.Fprintf(os.Stderr, "%s: %v\n", m.Subject, err)
				continue
			}
			_ = m.Ack()
		}

		select {
		case <-stop:
			fmt.Println()
			return nil
		default:
		}
	}
}

// applyMessage is the projection step: read the current entry, fold, write it
// back.
//
// The write uses Update with the revision we read, not Put. Two projectors on
// one bucket would otherwise both read 12.5, both write 25, and lose a trip.
// Update fails on a stale revision, the message is not acked, and it comes
// back.
func applyMessage(kv nats.KeyValue, m *nats.Msg) error {
	vehicleID, err := vehicleFromSubject(m.Subject)
	if err != nil {
		return err
	}

	var event Travelled
	if err := json.Unmarshal(m.Data, &event); err != nil {
		return fmt.Errorf("bad payload: %w", err)
	}
	if err := event.Validate(); err != nil {
		return err
	}

	current := Odometer{}
	var revision uint64
	entry, err := kv.Get(vehicleID)
	switch {
	case err == nil:
		if err := json.Unmarshal(entry.Value(), &current); err != nil {
			return fmt.Errorf("bad read model for %s: %w", vehicleID, err)
		}
		revision = entry.Revision()
	case errors.Is(err, nats.ErrKeyNotFound):
		// First trip for this vehicle. revision 0 means "create".
	default:
		return err
	}

	next := current.Apply(event)
	value, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if _, err := kv.Update(vehicleID, value, revision); err != nil {
		if revision == 0 {
			if _, createErr := kv.Create(vehicleID, value); createErr != nil {
				return createErr
			}
			return nil
		}
		return err
	}
	return nil
}

func runShow(c config) error {
	nc, js, err := connect(c)
	if err != nil {
		return err
	}
	defer nc.Close()

	kv, err := js.KeyValue(bucketName)
	if err != nil {
		return fmt.Errorf("open bucket %s: %w", bucketName, err)
	}

	keys, err := kv.Keys()
	if errors.Is(err, nats.ErrNoKeysFound) {
		fmt.Printf("%s: the bucket is empty\n", c.region)
		return nil
	}
	if err != nil {
		return err
	}

	for _, key := range keys {
		entry, err := kv.Get(key)
		if err != nil {
			return err
		}
		var o Odometer
		if err := json.Unmarshal(entry.Value(), &o); err != nil {
			return err
		}
		fmt.Printf("%s: %-8s totalKm=%-8v trips=%d\n", c.region, key, o.TotalKm, o.Trips)
	}
	return nil
}

// reset exists so a lab script starts from zero. A total that carries over
// from the last run means nothing.
func runReset(c config) error {
	nc, js, err := connect(c)
	if err != nil {
		return err
	}
	defer nc.Close()

	if err := js.DeleteStream(streamName); err != nil && !errors.Is(err, nats.ErrStreamNotFound) {
		return err
	}
	if err := js.DeleteKeyValue(bucketName); err != nil &&
		!errors.Is(err, nats.ErrStreamNotFound) && !errors.Is(err, nats.ErrBucketNotFound) {
		return err
	}
	fmt.Printf("%s: stream and bucket removed\n", c.region)
	return nil
}
