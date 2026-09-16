package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Lesson 02, driven from the screen (plan 04.9, decisions D3, D8, D10).
//
// Transport only, like the rest of the shim. Nothing here decides anything a
// reader could call a business rule: it reads JSON, calls the same runPool()
// the CLI calls, and turns the answer into a status code.
//
// Two things ARE decided here, and both are about the shim rather than the
// domain:
//
//   - The run is always made against lesson 02's OWN log. The request cannot
//     name a stream. That is 04.8's whole point -- this consumer starves,
//     redelivers and, under -kill-at, abandons messages, and a body that
//     could point it at ODOMETER would put the demo's own log under it.
//   - One run at a time, refused here and not by the screen (D8). A screen
//     that policed this would be right until a second browser tab opened.

// poolRunner is the pool, narrowed to what the shim needs. The real one is
// runPool(); the specs pass a fake, which is how they run without NATS.
//
// It takes a Source because the specs have to be able to prove which log the
// handler reached for. A runner that named its own stream would pass every
// spec in the file and still be able to ruin ODOMETER.
type poolRunner func(ctx context.Context, src Source, cfg PoolConfig) (PoolResult, error)

// MaxPoolWorkers caps what the shim will start.
//
// 64, which is eight times the largest worker count any tab offers. It is not
// a domain rule -- it is the same kind of guard as BenchSizes, there so a
// typo in a request body costs nothing instead of ten thousand goroutines.
const MaxPoolWorkers = 64

// MaxPoolAckWait caps how long a run may hold the lock waiting for an ack
// that is never coming. The redelivery lesson uses 30s; five minutes is far
// past anything the screen offers.
const MaxPoolAckWait = 5 * time.Minute

// The CLI's defaults, named so the flag set and the shim cannot drift. A
// reader who presses the button and a reader who types the bare command have
// to get the same run, or the command printed under the button is a lie.
const (
	DefaultPoolWorkers    = 4
	DefaultPoolMaxPending = 1000
	DefaultPoolAckWait    = 30 * time.Second
)

// ErrPoolRunRequest is a request body the shim will not run. Named so the
// handler can answer 400 without matching on text.
var ErrPoolRunRequest = errors.New("bad pool run request")

// poolRunReq is the body of POST /pool/run.
//
// The three settings a reader can leave out are pointers, so "not mentioned"
// and "asked for zero" are different facts. Zero workers is a typo and is
// refused; an absent workers field is the CLI default.
//
// KillAt and Drain are plain values because their zeros mean something: no
// kill, and an open-ended run.
type poolRunReq struct {
	Workers    *int    `json:"workers"`
	MaxPending *int    `json:"maxPending"`
	AckWait    *string `json:"ackWait"`
	KillAt     uint64  `json:"killAt"`
	Drain      bool    `json:"drain"`
}

// poolConfigFrom turns a request into a config, or refuses it.
//
// Pure, so the refusals are specs rather than something a reviewer has to
// spot inside a handler.
func poolConfigFrom(req poolRunReq) (PoolConfig, error) {
	cfg := PoolConfig{
		Workers:    DefaultPoolWorkers,
		MaxPending: DefaultPoolMaxPending,
		AckWait:    DefaultPoolAckWait,
		KillAt:     req.KillAt,
		Drain:      req.Drain,
	}

	if req.Workers != nil {
		cfg.Workers = *req.Workers
	}
	if cfg.Workers < 1 {
		return PoolConfig{}, fmt.Errorf("%w: workers must be at least 1, got %d", ErrPoolRunRequest, cfg.Workers)
	}
	if cfg.Workers > MaxPoolWorkers {
		return PoolConfig{}, fmt.Errorf("%w: workers must be at most %d, got %d",
			ErrPoolRunRequest, MaxPoolWorkers, cfg.Workers)
	}

	if req.MaxPending != nil {
		cfg.MaxPending = *req.MaxPending
	}
	if cfg.MaxPending < 1 {
		return PoolConfig{}, fmt.Errorf("%w: max-pending must be at least 1, got %d",
			ErrPoolRunRequest, cfg.MaxPending)
	}

	if req.AckWait != nil {
		d, err := time.ParseDuration(*req.AckWait)
		if err != nil {
			return PoolConfig{}, fmt.Errorf("%w: ack-wait %q is not a duration", ErrPoolRunRequest, *req.AckWait)
		}
		cfg.AckWait = d
	}
	if cfg.AckWait <= 0 {
		return PoolConfig{}, fmt.Errorf("%w: ack-wait must be positive, got %s", ErrPoolRunRequest, cfg.AckWait)
	}
	if cfg.AckWait > MaxPoolAckWait {
		return PoolConfig{}, fmt.Errorf("%w: ack-wait must be at most %s, got %s",
			ErrPoolRunRequest, MaxPoolAckWait, cfg.AckWait)
	}

	return cfg, nil
}

// activeRun is the run that currently holds the lock.
//
// Not named poolRun: pool_cmd.go already has a poolRun, and it is a
// poolAction constant meaning "the command line asked for a run".
type activeRun struct {
	Config  PoolConfig
	Started time.Time
	// cancel stops an open-ended run. Live is the only tab that starts
	// one, and 04.9.8 is what gives it a button; it is held here from the
	// start because the gate is the only thing that knows which run to
	// stop.
	cancel context.CancelFunc
}

// poolGate is "one run at a time".
type poolGate struct {
	mu      sync.Mutex
	running *activeRun
}

// acquire takes the lock, or reports the run that already holds it.
func (g *poolGate) acquire(cfg PoolConfig, cancel context.CancelFunc, now time.Time) (*activeRun, *activeRun) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.running != nil {
		return nil, g.running
	}
	g.running = &activeRun{Config: cfg, Started: now, cancel: cancel}
	return g.running, nil
}

// release gives the lock back. It checks identity rather than clearing
// blindly: a late release from a run that has already been replaced would
// otherwise unlock somebody else's run.
func (g *poolGate) release(run *activeRun) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.running == run {
		g.running = nil
	}
}

// current reports the run in flight, if there is one.
func (g *poolGate) current() *activeRun {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.running
}

// poolRunDescription names the run holding the lock, in the reader's own
// units -- the flags they typed, not a struct dump.
func poolRunDescription(run *activeRun, now time.Time) string {
	return fmt.Sprintf("a run is already in progress: %d workers, max-pending %d, ack-wait %s, started %s ago",
		run.Config.Workers, run.Config.MaxPending, run.Config.AckWait,
		now.Sub(run.Started).Round(time.Second))
}

// poolRunResult is one run, as JSON.
//
// The settings are echoed back with the numbers. A row on the starvation
// table is labelled with the cap that produced it, and a row labelled from
// what the screen BELIEVES it asked for is a row that cannot be wrong.
//
// ElapsedMs is a float for the same reason rehydrateResult's is.
type poolRunResult struct {
	Workers    int     `json:"workers"`
	MaxPending int     `json:"maxPending"`
	AckWait    string  `json:"ackWait"`
	KillAt     uint64  `json:"killAt"`
	Drain      bool    `json:"drain"`
	Events     int     `json:"events"`
	Acked      int     `json:"acked"`
	Dropped    int     `json:"dropped"`
	ElapsedMs  float64 `json:"elapsedMs"`
	Share      struct {
		Workers int   `json:"workers"`
		Busy    int   `json:"busy"`
		Idle    int   `json:"idle"`
		Acked   []int `json:"acked"`
	} `json:"share"`
	// Absent unless the run injected a fault and got the event back. The
	// Redelivery tab draws this; before 04.9.9 it drew a row recorded in a
	// file, which could not be wrong and could not be current either.
	Redelivery *poolRedeliveryResult `json:"redelivery,omitempty"`
}

// poolRedeliveryResult is the fault the run cost, in the units the screen
// reads: milliseconds, and the AckWait as the string the flag was given.
//
// RanOn is computed here rather than on the screen. It is one subtraction,
// and one subtraction done in two places is two subtractions that can differ.
type poolRedeliveryResult struct {
	Seq          uint64  `json:"seq"`
	KilledWorker int     `json:"killedWorker"`
	ToWorker     int     `json:"toWorker"`
	Delivery     uint64  `json:"delivery"`
	WaitedMs     float64 `json:"waitedMs"`
	AckWait      string  `json:"ackWait"`
	FoldAt       uint64  `json:"foldAt"`
	RanOn        uint64  `json:"ranOn"`
	Outcome      string  `json:"outcome"`
}

func poolRunResultOf(cfg PoolConfig, r PoolResult) poolRunResult {
	out := poolRunResult{
		Workers:    cfg.Workers,
		MaxPending: cfg.MaxPending,
		AckWait:    cfg.AckWait.String(),
		KillAt:     cfg.KillAt,
		Drain:      cfg.Drain,
		Events:     r.Events,
		Acked:      r.Acked,
		Dropped:    r.Dropped,
		ElapsedMs:  float64(r.Elapsed) / float64(time.Millisecond),
	}
	out.Share.Workers = r.Share.Workers
	out.Share.Busy = r.Share.Busy
	out.Share.Idle = r.Share.Idle
	out.Share.Acked = r.Share.Acked
	if out.Share.Acked == nil {
		out.Share.Acked = []int{}
	}
	if r.Redelivery != nil {
		d := r.Redelivery
		// A fold that had NOT passed the event ran on no distance. The
		// subtraction is unsigned, so guarding it is not tidiness.
		var ranOn uint64
		if d.FoldAt > d.Seq {
			ranOn = d.FoldAt - d.Seq
		}
		out.Redelivery = &poolRedeliveryResult{
			Seq:          d.Seq,
			KilledWorker: d.KilledWorker,
			ToWorker:     d.ToWorker,
			Delivery:     d.Delivery,
			WaitedMs:     float64(d.Waited) / float64(time.Millisecond),
			AckWait:      d.AckWait.String(),
			FoldAt:       d.FoldAt,
			RanOn:        ranOn,
			Outcome:      d.Outcome,
		}
	}
	return out
}

// poolRunHandler runs one pool and blocks until it ends.
//
// A plain POST, not a stream. The progress bar is driven by the KV bucket the
// page is already watching (D3), so there is nothing to push down this
// connection and no second protocol to keep in step with the first.
func poolRunHandler(gate *poolGate, run poolRunner, allowedOrigins []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w, r, allowedOrigins)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, refusal{
				Error: "MethodNotAllowed", Message: "running a pool writes; use POST",
			})
			return
		}

		var req poolRunReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, refusal{
				Error: "BadRequest", Message: "body must be a JSON object",
			})
			return
		}
		cfg, err := poolConfigFrom(req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, refusal{
				Error: "BadRequest", Message: err.Error(),
			})
			return
		}

		// The run outlives the request on purpose (D11). A reader who
		// closes the tab in the middle of a ninety-second measurement has
		// not asked for it to be thrown away, and the next page load picks
		// the workers up from the bucket it is already watching.
		runCtx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
		held, busy := gate.acquire(cfg, cancel, time.Now())
		if held == nil {
			cancel()
			writeJSON(w, http.StatusConflict, refusal{
				Error:   "PoolRunning",
				Message: poolRunDescription(busy, time.Now()),
			})
			return
		}
		defer func() {
			gate.release(held)
			cancel()
		}()

		// Pool, always. The body cannot name a log.
		res, err := run(runCtx, Pool, cfg)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, refusal{
				Error: "Unavailable", Message: err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, poolRunResultOf(cfg, res))
	}
}

// poolStopResult is the answer to Stop.
//
// `stopped` is false when nothing was running, and that is a 200, not a 4xx.
// Pressing Stop on a run that has just finished by itself is the same request
// as pressing it a moment earlier; answering with an error would make the
// screen show a fault where nothing went wrong.
type poolStopResult struct {
	Stopped bool   `json:"stopped"`
	Message string `json:"message"`
}

// poolStopHandler cancels the run in flight (plan 04.9.8, decision D6).
//
// It takes no body. The gate is the only thing that knows which run holds the
// lock, and a stop that named a run in its body could name the wrong one --
// the run it described may have ended and been replaced between the read and
// the press.
//
// Only Live starts an open-ended run, so only Live shows this button. The
// multi-run tabs finish by themselves, and offering them a Stop would suggest
// they might not.
func poolStopHandler(gate *poolGate, allowedOrigins []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w, r, allowedOrigins)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, refusal{
				Error: "MethodNotAllowed", Message: "stopping a pool changes something; use POST",
			})
			return
		}

		run := gate.current()
		if run == nil {
			writeJSON(w, http.StatusOK, poolStopResult{
				Stopped: false, Message: "no pool is running",
			})
			return
		}

		// Cancel only. The lock is released by the request that took it, so
		// two Stops in a row cannot unlock somebody else's run.
		run.cancel()
		writeJSON(w, http.StatusOK, poolStopResult{
			Stopped: true,
			Message: fmt.Sprintf("stopped a run of %d workers, max-pending %d, ack-wait %s",
				run.Config.Workers, run.Config.MaxPending, run.Config.AckWait),
		})
	}
}

// The fixture half: seed, drop, report (plan 04.9.2).
//
// Same shape as the benchmark's endpoints, and for the same reason: the
// button reaches the function the CLI reaches, so the command printed under
// it is the thing the button does rather than a description of it.

// poolSeeder builds lesson 02's log at one size. The real one is seedPool().
type poolSeeder func(ctx context.Context, size int) (PoolState, error)

// poolDropper deletes lesson 02 entirely. The real one is dropPool().
type poolDropper func(ctx context.Context) error

// poolReader reports what the log holds without changing it.
type poolReader func(ctx context.Context) (PoolState, error)

// poolStateResult is PoolState plus what the gate knows.
//
// The run in flight is part of the answer because of D11: a page reloaded in
// the middle of a ninety-second set has to find out that the set is still
// going, and the bucket it watches cannot tell it whether the SHIM is busy.
type poolStateResult struct {
	PoolState
	Running        bool   `json:"running"`
	RunningWorkers int    `json:"runningWorkers,omitempty"`
	RunningSince   string `json:"runningSince,omitempty"`
	RunningNote    string `json:"runningNote,omitempty"`
}

// poolStateHandler reports the log. A GET, because it reads.
func poolStateHandler(read poolReader, gate *poolGate, allowedOrigins []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w, r, allowedOrigins)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, refusal{
				Error: "MethodNotAllowed", Message: "the log is read with GET",
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
		writeJSON(w, http.StatusOK, withRunState(state, gate.current(), time.Now()))
	}
}

// withRunState folds the gate's answer into the report. Pure, so "what does
// the screen see while a run is going" is a spec and not an inspection.
func withRunState(state PoolState, run *activeRun, now time.Time) poolStateResult {
	out := poolStateResult{PoolState: state}
	if run == nil {
		return out
	}
	out.Running = true
	out.RunningWorkers = run.Config.Workers
	out.RunningSince = run.Started.UTC().Format(time.RFC3339)
	out.RunningNote = poolRunDescription(run, now)
	return out
}

// poolSeedHandler builds the log. POST, never GET: this writes up to a
// million events, and a GET that did that would be fired by a reload.
// poolSeedReq is the JSON form of a seed request.
//
// A pointer, so "no size mentioned" and "size 0" are different requests --
// the first takes the CLI's default, the second is refused by the list.
type poolSeedReq struct {
	Size *int `json:"size"`
}

func poolSeedHandler(gate *poolGate, seed poolSeeder, allowedOrigins []string) http.HandlerFunc {
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
		if busy := gate.current(); busy != nil {
			// The same clash `cqrs pool` refuses on the command line. A
			// seed under a live pool has the pool folding a log that is
			// still being written, and the run measures the seed.
			writeJSON(w, http.StatusConflict, refusal{
				Error: "PoolRunning", Message: poolRunDescription(busy, time.Now()),
			})
			return
		}

		// An absent size is the CLI's own default, so the button and the
		// bare `cqrs pool -seed` build the same log.
		size := DefaultPoolSize
		if raw := r.URL.Query().Get("size"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, refusal{
					Error: "BadRequest", Message: fmt.Sprintf("size %q is not a number", raw),
				})
				return
			}
			size = n
		}
		// A body may name it as well. /pool/run takes JSON, and a caller
		// seeding the same way was answered 200 having quietly been given
		// the default -- found against the live server, not by a spec.
		// A stated size is honoured whichever way it arrived; a body that
		// cannot be read is refused rather than ignored.
		if body, err := io.ReadAll(r.Body); err == nil && len(bytes.TrimSpace(body)) > 0 {
			var req poolSeedReq
			if err := json.Unmarshal(body, &req); err != nil {
				writeJSON(w, http.StatusBadRequest, refusal{
					Error: "BadRequest", Message: "the seed request is not JSON",
				})
				return
			}
			if req.Size != nil {
				size = *req.Size
			}
		}
		// The fixed list is the cap, checked before anything is written.
		if _, err := poolPlanFor(size); err != nil {
			writeJSON(w, http.StatusBadRequest, refusal{
				Error: "BadRequest", Message: err.Error(),
			})
			return
		}

		state, err := seed(r.Context(), size)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, refusal{
				Error: "Unavailable", Message: err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, withRunState(state, gate.current(), time.Now()))
	}
}

// poolRemoveHandler deletes lesson 02 and leaves the demo standing.
//
// Refused under a run for the same reason a seed is, only more so: deleting
// the stream out from under a live consumer is the worst version of it.
func poolRemoveHandler(gate *poolGate, drop poolDropper, allowedOrigins []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w, r, allowedOrigins)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, refusal{
				Error: "MethodNotAllowed", Message: "removing writes; use POST",
			})
			return
		}
		if busy := gate.current(); busy != nil {
			writeJSON(w, http.StatusConflict, refusal{
				Error: "PoolRunning", Message: poolRunDescription(busy, time.Now()),
			})
			return
		}
		if err := drop(r.Context()); err != nil {
			writeJSON(w, http.StatusBadGateway, refusal{
				Error: "Unavailable", Message: err.Error(),
			})
			return
		}
		// Reported as a state, not as "ok". The screen redraws the seed
		// group from this, and an empty log is a state it can draw.
		writeJSON(w, http.StatusOK, poolStateResult{PoolState: PoolState{
			Stream: Pool.Stream, Subject: Pool.StreamSubject, TruthKV: PoolTruthKV,
			Sizes: PoolSizes, Vehicles: PoolVehicles,
		}})
	}
}
