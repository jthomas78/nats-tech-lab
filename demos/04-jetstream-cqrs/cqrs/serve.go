package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// The HTTP shim. Transport only.
//
// This file exists so a browser can send a command. It holds no business
// rule and must never hold one: it reads JSON, calls the same decide function
// the CLI calls, and turns the domain's answer into a status code.
//
// The one piece of judgement here is the rule code. ErrNotRegistered is
// BR-OD02 when a vehicle tries to travel and BR-OD05 when it tries to retire,
// so the code is read from the ENDPOINT, not from the error alone. That is a
// naming decision about BUSINESS_RULES-ODOMETER.md, not a rule -- the domain
// still decides whether to refuse.

// commandRunner is the write side, narrowed to what the shim needs. The real
// one is handleCommand; the specs pass a fake, which is how they run without
// NATS while still calling the real rules.
type commandRunner func(ctx context.Context, id string, decide func(Vehicle) (Event, error)) (uint64, error)

// rehydrateRunner is the READ-ONLY side of the shim: rebuild one aggregate
// and report what it cost. The real one is rehydrate(); the specs pass a
// fake, which is how they measure nothing and still prove the wiring.
//
// It takes withSnapshot because that flag is the whole lesson. The screen
// calls this twice with the two values and puts the answers side by side.
type rehydrateRunner func(ctx context.Context, src Source, id string, withSnapshot bool) (Rehydrated, error)

// benchRunner builds the rehydrate fixture at one size. It is the WRITE half
// of the benchmark and the only thing on the Rehydrate tab that writes
// anything -- to ODOMETER_BENCH, never to ODOMETER (plan 04.7.16).
type benchRunner func(ctx context.Context, size int) (BenchState, error)

// benchReader reports what the fixture holds without changing it.
type benchReader func(ctx context.Context) (BenchState, error)

// rehydrateResult is one half of the comparison, as JSON.
//
// ElapsedMs is a float, not a rounded integer. The snapshot side of a small
// vehicle finishes in well under a millisecond, and an integer would print
// the demo's best number as 0.
type rehydrateResult struct {
	ID           string  `json:"id"`
	UsedSnapshot bool    `json:"usedSnapshot"`
	FromSeq      uint64  `json:"fromSeq"`
	EventsRead   int     `json:"eventsRead"`
	LastSeq      uint64  `json:"lastSeq"`
	ElapsedMs    float64 `json:"elapsedMs"`
	// The state both sides must agree on. A comparison whose halves
	// rebuilt different vehicles is not a comparison.
	Status string `json:"status"`
	Plate  string `json:"plate"`
}

// commandReq is the body of all three commands. Each field is used by one of
// them and ignored by the others.
type commandReq struct {
	ID     string  `json:"id"`
	Plate  string  `json:"plate"`
	Km     float64 `json:"km"`
	Reason string  `json:"reason"`
}

// refusal is what the screen renders when the domain says no.
type refusal struct {
	Rule    string `json:"rule,omitempty"`
	Error   string `json:"error"`
	Message string `json:"message"`
}

// endpoint is one command: how to decide it, and what each refusal is called.
type endpoint struct {
	decide func(commandReq) func(Vehicle) (Event, error)
	rules  map[error]string
}

var endpoints = map[string]endpoint{
	"register": {
		decide: func(r commandReq) func(Vehicle) (Event, error) {
			return func(v Vehicle) (Event, error) { return v.Register(RegisterVehicle{Plate: r.Plate}) }
		},
		rules: map[error]string{ErrAlreadyRegistered: "BR-OD03"},
	},
	"travel": {
		decide: func(r commandReq) func(Vehicle) (Event, error) {
			return func(v Vehicle) (Event, error) { return v.Travel(RecordTrip{Km: r.Km}) }
		},
		rules: map[error]string{
			ErrNonPositiveKm: "BR-OD01",
			ErrNotRegistered: "BR-OD02",
			ErrRetired:       "BR-OD04",
		},
	},
	"retire": {
		decide: func(r commandReq) func(Vehicle) (Event, error) {
			return func(v Vehicle) (Event, error) { return v.Retire(RetireVehicle{Reason: r.Reason}) }
		},
		// Both refusals of a retire are BR-OD05: "a vehicle cannot be retired
		// unless it is registered" covers never-registered and already-retired.
		rules: map[error]string{
			ErrNotRegistered: "BR-OD05",
			ErrRetired:       "BR-OD05",
		},
	},
}

// errorNames turns a domain error into the name a Go reader would recognise,
// so the screen can print "BR-OD04 ErrRetired" and a viewer can grep for it.
var errorNames = map[error]string{
	ErrNonPositiveKm:     "ErrNonPositiveKm",
	ErrNotRegistered:     "ErrNotRegistered",
	ErrAlreadyRegistered: "ErrAlreadyRegistered",
	ErrRetired:           "ErrRetired",
	ErrConflict:          "ErrConflict",
}

// newCommandAPI builds the handler. allowedOrigins is an exact-match list --
// the page is served from another port, so every real request is
// cross-origin, but a wildcard would let any page on the machine drive the
// demo.
func newCommandAPI(run commandRunner, rehydrateOne rehydrateRunner, seedBenchFixture benchRunner, readBench benchReader, allowedOrigins []string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/rehydrate", rehydrateHandler(rehydrateOne, allowedOrigins))
	mux.HandleFunc("/bench", benchStateHandler(readBench, allowedOrigins))
	mux.HandleFunc("/bench/seed", benchSeedHandler(seedBenchFixture, allowedOrigins))
	mux.HandleFunc("/commands/", func(w http.ResponseWriter, r *http.Request) {
		setCORS(w, r, allowedOrigins)

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, refusal{
				Error: "MethodNotAllowed", Message: "commands are sent with POST",
			})
			return
		}

		// The pattern is a prefix, not a wildcard, so that a GET or an
		// unknown command still reaches this handler and gets a 405 or a
		// 404 rather than Go's bare default.
		name := strings.TrimPrefix(r.URL.Path, "/commands/")
		ep, ok := endpoints[name]
		if !ok {
			writeJSON(w, http.StatusNotFound, refusal{
				Error:   "UnknownCommand",
				Message: fmt.Sprintf("no command %q; try register, travel or retire", name),
			})
			return
		}

		var req commandReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, refusal{
				Error: "BadRequest", Message: "body must be a JSON object",
			})
			return
		}
		if req.ID == "" {
			writeJSON(w, http.StatusBadRequest, refusal{
				Error: "BadRequest", Message: "id is required",
			})
			return
		}

		seq, err := run(r.Context(), req.ID, ep.decide(req))
		if err != nil {
			status, body := describe(err, ep.rules)
			writeJSON(w, status, body)
			return
		}
		writeJSON(w, http.StatusOK, map[string]uint64{"seq": seq})
	})
	return mux
}

// rehydrateHandler answers "how much does a snapshot buy you" for one vehicle.
//
// It is a GET because it changes nothing: it reads the log and the snapshot
// bucket and appends neither. A POST here would be a lie about what the
// button does.
//
// The default is snapshot=true. A missing flag must not replay ten thousand
// events because somebody mistyped a query string -- the expensive mode is
// always the one you asked for on purpose.
func rehydrateHandler(rehydrateOne rehydrateRunner, allowedOrigins []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w, r, allowedOrigins)

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, refusal{
				Error: "MethodNotAllowed", Message: "rehydrate reads; use GET",
			})
			return
		}

		id := r.URL.Query().Get("id")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, refusal{
				Error: "BadRequest", Message: "id is required",
			})
			return
		}

		// Which log. The demo's own is the default: a missing parameter
		// must never silently point the headline measurement at a fixture.
		src, ok := sourceNamed(r.URL.Query().Get("source"))
		if !ok {
			writeJSON(w, http.StatusBadRequest, refusal{
				Error: "BadRequest", Message: "source must be live or bench",
			})
			return
		}

		out, err := rehydrateOne(r.Context(), src, id, r.URL.Query().Get("snapshot") != "false")
		if err != nil {
			// No rule code. Rehydrating refuses no command, so there is no
			// business rule here to name -- see describe().
			writeJSON(w, http.StatusBadGateway, refusal{
				Error: "Unavailable", Message: err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, rehydrateResult{
			ID:           id,
			UsedSnapshot: out.UsedSnapshot,
			FromSeq:      out.FromSeq,
			EventsRead:   out.EventsRead,
			LastSeq:      out.LastSeq,
			ElapsedMs:    float64(out.Elapsed) / float64(time.Millisecond),
			Status:       string(out.Vehicle.Status),
			Plate:        out.Vehicle.Plate,
		})
	}
}

// describe classifies one failure.
//
// A rule refusal is 409: the request was well formed and the domain said no,
// and it will say no again until something changes. Everything else is the
// demo being broken, and is never given a rule code -- a screen that shows
// "BR-OD04" when NATS is down teaches the wrong lesson.
func describe(err error, rules map[error]string) (int, refusal) {
	for domainErr, rule := range rules {
		if errors.Is(err, domainErr) {
			return http.StatusConflict, refusal{
				Rule: rule, Error: errorNames[domainErr], Message: err.Error(),
			}
		}
	}
	if errors.Is(err, ErrConflict) {
		return http.StatusServiceUnavailable, refusal{
			Error:   errorNames[ErrConflict],
			Message: "too many writers on this vehicle at once; try again",
		}
	}
	return http.StatusBadGateway, refusal{
		Error: "Unavailable", Message: err.Error(),
	}
}

func setCORS(w http.ResponseWriter, r *http.Request, allowed []string) {
	origin := r.Header.Get("Origin")
	for _, a := range allowed {
		if origin == a {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Vary", "Origin")
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// runServe wires the real write side to the handler and blocks.
//
// Reads are not served here. The browser watches NATS over its own WebSocket,
// so commands go one way and reads come the other -- the CQRS split is
// visible in the network traffic, not just in the code.
func runServe(ctx context.Context, js jetstream.JetStream, kv jetstream.KeyValue, addr string, origins []string) error {
	run := func(ctx context.Context, id string, decide func(Vehicle) (Event, error)) (uint64, error) {
		_, seq, err := handleCommand(ctx, js, kv, id, true, decide)
		return seq, err
	}

	// The read-only half. This is the demo's headline question wired to the
	// same function the CLI calls, so the tab and `cqrs rehydrate` cannot
	// drift apart.
	rehydrateOne := func(ctx context.Context, src Source, id string, withSnapshot bool) (Rehydrated, error) {
		snapKV := kv
		if src.Stream == Bench.Stream {
			// Opened per call, not at startup. The fixture may not exist
			// yet -- nobody has to seed -- and a shim that refused to
			// start without one would be broken by an empty server.
			opened, err := ensureBench(ctx, js)
			if err != nil {
				return Rehydrated{}, err
			}
			snapKV = opened
		}
		return rehydrate(ctx, js, snapKV, src, id, withSnapshot)
	}

	// The benchmark's two halves, wired to the same functions `cqrs bench`
	// calls. The button on the panel and the command printed under it do
	// the same thing, because they ARE the same thing.
	seedFixture := func(ctx context.Context, size int) (BenchState, error) {
		benchKV, err := ensureBench(ctx, js)
		if err != nil {
			return BenchState{}, err
		}
		return seedBench(ctx, js, benchKV, size)
	}
	readFixture := func(ctx context.Context) (BenchState, error) {
		benchKV, err := ensureBench(ctx, js)
		if err != nil {
			return BenchState{}, err
		}
		return benchState(ctx, js, benchKV)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           newCommandAPI(run, rehydrateOne, seedFixture, readFixture, origins),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// sourceNamed turns the query parameter into a Source. An empty value is the
// demo's own log, which is what every tab but Rehydrate means.
func sourceNamed(name string) (Source, bool) {
	switch name {
	case "", "live":
		return Live, true
	case "bench":
		return Bench, true
	default:
		return Source{}, false
	}
}

// benchStateHandler reports what the fixture holds. A GET, because it reads.
//
// An absent fixture is a state, not a failure: nobody has to seed, and the
// panel offers the button on the strength of exists=false.
func benchStateHandler(read benchReader, allowedOrigins []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w, r, allowedOrigins)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, refusal{
				Error: "MethodNotAllowed", Message: "the fixture is read with GET",
			})
			return
		}
		state, err := read(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, refusal{
				Error: "Unavailable", Message: err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, state)
	}
}

// benchSeedHandler builds one fixture.
//
// POST, never GET. This writes up to a million events, and a GET that did
// that would be fired by a link preview, a browser prefetch or a reload.
//
// The size is checked against BenchSizes HERE, before the seeder is called.
// The fixed list is the cap, so a typo costs nothing instead of a gigabyte.
func benchSeedHandler(seedFixture benchRunner, allowedOrigins []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w, r, allowedOrigins)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, refusal{
				Error: "MethodNotAllowed", Message: "seeding writes; use POST",
			})
			return
		}

		size, err := strconv.Atoi(r.URL.Query().Get("size"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, refusal{
				Error: "BadRequest", Message: "size is required",
			})
			return
		}
		if _, err := benchPlanFor(size); err != nil {
			writeJSON(w, http.StatusBadRequest, refusal{
				Error: "BadRequest", Message: err.Error(),
			})
			return
		}

		state, err := seedFixture(r.Context(), size)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, refusal{
				Error: "Unavailable", Message: err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, state)
	}
}
