package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
func newCommandAPI(run commandRunner, allowedOrigins []string) http.Handler {
	mux := http.NewServeMux()
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
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
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

	srv := &http.Server{
		Addr:              addr,
		Handler:           newCommandAPI(run, origins),
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
