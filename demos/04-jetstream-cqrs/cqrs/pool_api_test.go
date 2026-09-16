package main

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Specs for POST /pool/run (plan 04.9.1).
//
// There is no NATS here. What these prove is the three things the shim is
// actually responsible for:
//
//   - the flags the screen sends are the flags runPool is given, unchanged;
//   - the run happens on lesson 02's own log and never on ODOMETER;
//   - exactly one run exists at a time, refused HERE and not by the screen.
//
// The last one is the reason this endpoint has specs at all. Running a pool
// is not a business rule -- domain.go decides nothing about it -- but "one at
// a time" is a promise the screen is allowed to rely on, and a promise the
// screen makes instead would be broken by a second browser tab.

// poolCall records one call into runPool, so a spec can read back what the
// handler actually asked for. Without this, the handler could ignore the
// whole request body and every status-code spec would still pass.
type poolCall struct {
	Src Source
	Cfg PoolConfig
}

// stubPool answers with a fixed result and records what it was asked for.
//
// hold, when non-nil, blocks the run until the spec closes it. That is how a
// run is held "in flight" while a second request is made, without a sleep.
func stubPool(out PoolResult, err error, calls *[]poolCall, hold <-chan struct{}) poolRunner {
	return func(ctx context.Context, src Source, cfg PoolConfig) (PoolResult, error) {
		if calls != nil {
			*calls = append(*calls, poolCall{Src: src, Cfg: cfg})
		}
		if hold != nil {
			select {
			case <-hold:
			case <-ctx.Done():
				return PoolResult{}, ctx.Err()
			}
		}
		return out, err
	}
}

// enteredOnce wraps a runner so a spec can wait for the FIRST call into it.
// Once, not every call: the later specs make a second run through the same
// handler, and a bare close() would panic on the second entry.
func enteredOnce(inner poolRunner) (poolRunner, chan struct{}) {
	entered := make(chan struct{})
	var once sync.Once
	return func(ctx context.Context, src Source, cfg PoolConfig) (PoolResult, error) {
		once.Do(func() { close(entered) })
		return inner(ctx, src, cfg)
	}, entered
}

func poolAPI(run poolRunner) http.Handler {
	return newCommandAPI(apiDeps{
		run:          fakeRunner(registered(), 1),
		rehydrateOne: stubRehydrate(Rehydrated{}, nil),
		seedBench:    stubBench(BenchState{}, nil, nil),
		readBench:    readOnly(BenchState{}),
		runPool:      run,
		origins:      []string{testOrigin},
	})
}

func drained() PoolResult {
	return PoolResult{
		Events: 10_000, Acked: 9_990, Dropped: 10,
		Elapsed: 5400 * time.Millisecond,
		Share:   PoolShare{Workers: 2, Busy: 2, Idle: 0, Acked: []int{5_000, 4_990}},
	}
}

var _ = Describe("the pool run endpoint", func() {

	Context("the flags the screen sent", func() {
		It("hands every one of them to runPool unchanged", func() {
			var calls []poolCall
			h := poolAPI(stubPool(drained(), nil, &calls, nil))

			rec := post(h, "/pool/run",
				`{"workers":8,"maxPending":3,"ackWait":"12s","killAt":250,"drain":true}`)

			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(calls).To(HaveLen(1))
			Expect(calls[0].Cfg).To(Equal(PoolConfig{
				Workers: 8, MaxPending: 3, AckWait: 12 * time.Second,
				KillAt: 250, Drain: true,
			}))
		})

		// A parameterised handler is only proved parameterised by a second,
		// different input. One call cannot tell "passed it through" from
		// "hardcoded the answer the first spec wanted".
		It("hands over a different set just as faithfully", func() {
			var calls []poolCall
			h := poolAPI(stubPool(drained(), nil, &calls, nil))

			Expect(post(h, "/pool/run",
				`{"workers":1,"maxPending":64,"ackWait":"1m","drain":false}`).Code).
				To(Equal(http.StatusOK))
			Expect(calls[0].Cfg).To(Equal(PoolConfig{
				Workers: 1, MaxPending: 64, AckWait: time.Minute,
			}))
		})

		// An omitted field is the CLI's own default, so a reader who
		// presses the button and a reader who types the bare command get
		// the same run.
		It("falls back to the same defaults `cqrs pool` uses", func() {
			var calls []poolCall
			h := poolAPI(stubPool(drained(), nil, &calls, nil))

			Expect(post(h, "/pool/run", `{}`).Code).To(Equal(http.StatusOK))
			Expect(calls[0].Cfg).To(Equal(PoolConfig{
				Workers:    DefaultPoolWorkers,
				MaxPending: DefaultPoolMaxPending,
				AckWait:    DefaultPoolAckWait,
			}))
		})
	})

	// The whole reason 04.8 gave lesson 02 its own log. A Run button that
	// reached ODOMETER would put the demo's own stream under a consumer
	// built to starve, redeliver and abandon.
	Context("which log it runs on", func() {
		It("always runs on lesson 02's own log", func() {
			var calls []poolCall
			h := poolAPI(stubPool(drained(), nil, &calls, nil))

			Expect(post(h, "/pool/run", `{"drain":true}`).Code).To(Equal(http.StatusOK))
			Expect(calls[0].Src.Stream).To(Equal(Pool.Stream))
			Expect(calls[0].Src.StreamSubject).To(Equal(Pool.StreamSubject))
		})

		// ODOMETER is a PREFIX of ODOMETER_POOL, so this is checked on the
		// whole name and not with a substring.
		It("never runs on ODOMETER, whatever the body says", func() {
			var calls []poolCall
			h := poolAPI(stubPool(drained(), nil, &calls, nil))

			Expect(post(h, "/pool/run",
				`{"source":"live","stream":"ODOMETER","drain":true}`).Code).
				To(Equal(http.StatusOK))
			Expect(calls[0].Src.Stream).NotTo(Equal(Live.Stream))
			Expect(calls[0].Src.Stream).To(Equal(Pool.Stream))
		})
	})

	Context("what it answers with", func() {
		It("reports what the run cost", func() {
			h := poolAPI(stubPool(drained(), nil, nil, nil))
			body := decodeBody(post(h, "/pool/run", `{"workers":2,"drain":true}`))

			Expect(body["events"]).To(BeNumerically("==", 10_000))
			Expect(body["acked"]).To(BeNumerically("==", 9_990))
			Expect(body["dropped"]).To(BeNumerically("==", 10))
			Expect(body["elapsedMs"]).To(BeNumerically("==", 5400))
		})

		// Totals cannot show starvation: one worker doing everything and
		// eight sharing it evenly produce the same Acked.
		It("reports where the work landed, per worker", func() {
			h := poolAPI(stubPool(drained(), nil, nil, nil))
			body := decodeBody(post(h, "/pool/run", `{"workers":2,"drain":true}`))

			share, ok := body["share"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(share["busy"]).To(BeNumerically("==", 2))
			Expect(share["idle"]).To(BeNumerically("==", 0))
			Expect(share["acked"]).To(HaveLen(2))
		})

		// The row on screen is labelled with the cap that produced it, not
		// with the cap the screen believes it asked for.
		It("echoes the settings the run was made with", func() {
			h := poolAPI(stubPool(drained(), nil, nil, nil))
			body := decodeBody(post(h, "/pool/run",
				`{"workers":8,"maxPending":3,"ackWait":"12s","drain":true}`))

			Expect(body["workers"]).To(BeNumerically("==", 8))
			Expect(body["maxPending"]).To(BeNumerically("==", 3))
			Expect(body["ackWait"]).To(Equal("12s"))
			Expect(body["drain"]).To(BeTrue())
		})
	})

	Context("one run at a time", func() {
		// held starts a run and leaves it in flight. The handler runs in a
		// goroutine; the spec waits until runPool has actually been
		// entered, so the second request cannot win a race with the first.
		held := func() (http.Handler, chan struct{}, chan struct{}, *[]poolCall) {
			hold := make(chan struct{})
			calls := []poolCall{}
			runner, entered := enteredOnce(stubPool(drained(), nil, &calls, hold))
			h := poolAPI(runner)
			done := make(chan struct{})
			go func() {
				defer close(done)
				post(h, "/pool/run", `{"workers":8,"maxPending":3,"ackWait":"30s","drain":true}`)
			}()
			Eventually(entered).Should(BeClosed())
			return h, hold, done, &calls
		}

		It("refuses a second run while one is in flight", func() {
			h, hold, done, _ := held()
			defer func() { close(hold); Eventually(done).Should(BeClosed()) }()

			rec := post(h, "/pool/run", `{"workers":1,"drain":true}`)
			Expect(rec.Code).To(Equal(http.StatusConflict))
			Expect(decodeBody(rec)["error"]).To(Equal("PoolRunning"))
		})

		// D8: the screen is told which run is holding the lock, so it can
		// say so instead of just going grey.
		It("names the run that is holding the lock", func() {
			h, hold, done, _ := held()
			defer func() { close(hold); Eventually(done).Should(BeClosed()) }()

			msg, _ := decodeBody(post(h, "/pool/run", `{"workers":1,"drain":true}`))["message"].(string)
			Expect(msg).To(ContainSubstring("8 workers"))
			Expect(msg).To(ContainSubstring("max-pending 3"))
			Expect(msg).To(ContainSubstring("ack-wait 30s"))
		})

		It("does not start the second run at all", func() {
			h, hold, done, calls := held()
			defer func() { close(hold); Eventually(done).Should(BeClosed()) }()

			Expect(post(h, "/pool/run", `{"workers":1,"drain":true}`).Code).
				To(Equal(http.StatusConflict))
			Expect(*calls).To(HaveLen(1))
		})

		It("lets the next run through once the first has ended", func() {
			h, hold, done, calls := held()
			close(hold)
			Eventually(done).Should(BeClosed())

			Expect(post(h, "/pool/run", `{"workers":1,"drain":true}`).Code).
				To(Equal(http.StatusOK))
			Expect(*calls).To(HaveLen(2))
		})

		// A run that failed still has to give the lock back. Otherwise one
		// broken run jams lesson 02 until somebody restarts the shim.
		It("releases the lock when a run fails", func() {
			var calls []poolCall
			h := poolAPI(stubPool(PoolResult{}, errors.New("nats down"), &calls, nil))

			Expect(post(h, "/pool/run", `{"drain":true}`).Code).To(Equal(http.StatusBadGateway))
			Expect(post(h, "/pool/run", `{"drain":true}`).Code).To(Equal(http.StatusBadGateway))
			Expect(calls).To(HaveLen(2))
		})
	})

	Context("a request that makes no sense", func() {
		refused := func(body string) {
			var calls []poolCall
			h := poolAPI(stubPool(drained(), nil, &calls, nil))
			rec := post(h, "/pool/run", body)
			ExpectWithOffset(1, rec.Code).To(Equal(http.StatusBadRequest))
			ExpectWithOffset(1, calls).To(BeEmpty())
		}

		It("refuses fewer than one worker, running nothing", func() {
			refused(`{"workers":0}`)
			refused(`{"workers":-3}`)
		})

		// A browser asking for ten thousand goroutines is a typo, not a
		// lesson. The cap is the shim's, the same way BenchSizes caps the
		// fixture before a gigabyte is written.
		It("refuses more workers than the cap, running nothing", func() {
			refused(`{"workers":100000}`)
		})

		It("refuses a max-pending below one, running nothing", func() {
			refused(`{"maxPending":0}`)
		})

		It("refuses an ack-wait it cannot read, running nothing", func() {
			refused(`{"ackWait":"soon"}`)
			refused(`{"ackWait":"0s"}`)
		})

		It("refuses a body that is not JSON, running nothing", func() {
			refused(`not json`)
		})
	})

	Context("the method", func() {
		// Running a pool creates a durable consumer and rewrites a KV
		// bucket. A GET that did that would be fired by a reload.
		It("will not run on a GET", func() {
			var calls []poolCall
			h := poolAPI(stubPool(drained(), nil, &calls, nil))
			Expect(get(h, "/pool/run").Code).To(Equal(http.StatusMethodNotAllowed))
			Expect(calls).To(BeEmpty())
		})
	})
})

// Specs for the seed / drop / report endpoints (plan 04.9.2).
//
// Same shape as the benchmark's: the button reaches the same function the CLI
// calls, a size off the fixed list is refused before anything is written, and
// the report never hands the screen a count it cannot price.

// stubPoolSeeder records the sizes it was asked for.
func stubPoolSeeder(out PoolState, err error, asked *[]int) poolSeeder {
	return func(_ context.Context, size int) (PoolState, error) {
		if asked != nil {
			*asked = append(*asked, size)
		}
		return out, err
	}
}

func stubPoolDropper(err error, calls *int) poolDropper {
	return func(context.Context) error {
		if calls != nil {
			*calls++
		}
		return err
	}
}

func poolReadOnly(s PoolState) poolReader {
	return func(context.Context) (PoolState, error) { return s, nil }
}

// poolFixtureAPI is the whole pool surface, so one spec can hold a run and
// then press Seed -- which is the interaction 04.9.2 has to get right.
func poolFixtureAPI(run poolRunner, seed poolSeeder, drop poolDropper, read poolReader) http.Handler {
	return newCommandAPI(apiDeps{
		run:          fakeRunner(registered(), 1),
		rehydrateOne: stubRehydrate(Rehydrated{}, nil),
		seedBench:    stubBench(BenchState{}, nil, nil),
		readBench:    readOnly(BenchState{}),
		runPool:      run,
		seedPool:     seed,
		dropPool:     drop,
		readPool:     read,
		origins:      []string{testOrigin},
	})
}

func poolSeeded() PoolState {
	return PoolState{
		Stream: PoolStream, Subject: PoolStreamSubject, TruthKV: PoolTruthKV,
		Exists: true, Events: 10_000, Bytes: 810_120,
		Sizes: PoolSizes, Vehicles: PoolVehicles,
	}
}

var _ = Describe("the pool fixture endpoints", func() {

	Context("reporting what the log holds", func() {
		It("answers a GET with lesson 02's own log", func() {
			h := poolFixtureAPI(stubPool(drained(), nil, nil, nil), nil, nil, poolReadOnly(poolSeeded()))
			body := decodeBody(get(h, "/pool"))
			Expect(body["stream"]).To(Equal(PoolStream))
			Expect(body["subject"]).To(Equal(PoolStreamSubject))
		})

		// The standing rule, set by the user 2026-09-16. A length is a
		// number nobody can price.
		It("never returns a count without its bytes", func() {
			h := poolFixtureAPI(stubPool(drained(), nil, nil, nil), nil, nil, poolReadOnly(poolSeeded()))
			body := decodeBody(get(h, "/pool"))
			Expect(body).To(HaveKey("events"))
			Expect(body).To(HaveKey("bytes"))
			Expect(body["bytes"]).To(BeNumerically(">", 0))
		})

		It("reports an unseeded log without failing", func() {
			empty := PoolState{Stream: PoolStream, Sizes: PoolSizes}
			h := poolFixtureAPI(stubPool(drained(), nil, nil, nil), nil, nil, poolReadOnly(empty))
			rec := get(h, "/pool")
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(decodeBody(rec)["exists"]).To(BeFalse())
		})

		It("tells the screen which sizes it may ask for", func() {
			h := poolFixtureAPI(stubPool(drained(), nil, nil, nil), nil, nil, poolReadOnly(poolSeeded()))
			Expect(decodeBody(get(h, "/pool"))["sizes"]).To(HaveLen(len(PoolSizes)))
		})

		// D11: a page that was reloaded in the middle of a run has to find
		// out that a run is still going, or it offers a button the shim
		// will refuse.
		It("says nothing is running when nothing is", func() {
			h := poolFixtureAPI(stubPool(drained(), nil, nil, nil), nil, nil, poolReadOnly(poolSeeded()))
			Expect(decodeBody(get(h, "/pool"))["running"]).To(BeFalse())
		})

		It("says a run is in flight while one is", func() {
			hold := make(chan struct{})
			runner, entered := enteredOnce(stubPool(drained(), nil, nil, hold))
			h := poolFixtureAPI(runner, nil, nil, poolReadOnly(poolSeeded()))

			done := make(chan struct{})
			go func() { defer close(done); post(h, "/pool/run", `{"workers":8,"drain":true}`) }()
			Eventually(entered).Should(BeClosed())
			defer func() { close(hold); Eventually(done).Should(BeClosed()) }()

			body := decodeBody(get(h, "/pool"))
			Expect(body["running"]).To(BeTrue())
			Expect(body["runningWorkers"]).To(BeNumerically("==", 8))
		})
	})

	Context("seeding the log", func() {
		It("passes the requested size to the seeder", func() {
			var asked []int
			h := poolFixtureAPI(stubPool(drained(), nil, nil, nil),
				stubPoolSeeder(poolSeeded(), nil, &asked), nil, poolReadOnly(poolSeeded()))
			Expect(postSeed(h, "/pool/seed?size=100000").Code).To(Equal(http.StatusOK))
			Expect(asked).To(Equal([]int{100_000}))
		})

		It("defaults to the size `cqrs pool -seed` defaults to", func() {
			var asked []int
			h := poolFixtureAPI(stubPool(drained(), nil, nil, nil),
				stubPoolSeeder(poolSeeded(), nil, &asked), nil, poolReadOnly(poolSeeded()))
			Expect(postSeed(h, "/pool/seed").Code).To(Equal(http.StatusOK))
			Expect(asked).To(Equal([]int{DefaultPoolSize}))
		})

		It("answers with the log's new state, bytes included", func() {
			h := poolFixtureAPI(stubPool(drained(), nil, nil, nil),
				stubPoolSeeder(poolSeeded(), nil, nil), nil, poolReadOnly(poolSeeded()))
			body := decodeBody(postSeed(h, "/pool/seed?size=10000"))
			Expect(body["events"]).To(BeNumerically("==", 10_000))
			Expect(body["bytes"]).To(BeNumerically("==", 810_120))
		})

		// The fixed list IS the cap, checked before a single event is
		// written -- 100 000 is ten minutes of watching a bar, and
		// 1 000 000 is a different product again.
		It("refuses a size that is not on the list, writing nothing", func() {
			var asked []int
			h := poolFixtureAPI(stubPool(drained(), nil, nil, nil),
				stubPoolSeeder(poolSeeded(), nil, &asked), nil, poolReadOnly(poolSeeded()))
			Expect(postSeed(h, "/pool/seed?size=99").Code).To(Equal(http.StatusBadRequest))
			Expect(asked).To(BeEmpty())
		})

		// Found against the live server, not by a spec: /pool/run takes a
		// JSON body, so a caller seeding the same way sent {"size":...}
		// and was answered 200 having silently been given the default.
		// A size the caller stated is honoured, whichever way it arrived.
		It("takes the size from a JSON body too", func() {
			var asked []int
			h := poolFixtureAPI(stubPool(drained(), nil, nil, nil),
				stubPoolSeeder(poolSeeded(), nil, &asked), nil, poolReadOnly(poolSeeded()))
			Expect(post(h, "/pool/seed", `{"size":100000}`).Code).To(Equal(http.StatusOK))
			Expect(asked).To(Equal([]int{100_000}))
		})

		It("refuses an off-list size in a JSON body, writing nothing", func() {
			var asked []int
			h := poolFixtureAPI(stubPool(drained(), nil, nil, nil),
				stubPoolSeeder(poolSeeded(), nil, &asked), nil, poolReadOnly(poolSeeded()))
			Expect(post(h, "/pool/seed", `{"size":99}`).Code).To(Equal(http.StatusBadRequest))
			Expect(asked).To(BeEmpty())
		})

		It("refuses a body it cannot read, writing nothing", func() {
			var asked []int
			h := poolFixtureAPI(stubPool(drained(), nil, nil, nil),
				stubPoolSeeder(poolSeeded(), nil, &asked), nil, poolReadOnly(poolSeeded()))
			Expect(post(h, "/pool/seed", `not json`).Code).To(Equal(http.StatusBadRequest))
			Expect(asked).To(BeEmpty())
		})

		It("will not seed on a GET", func() {
			var asked []int
			h := poolFixtureAPI(stubPool(drained(), nil, nil, nil),
				stubPoolSeeder(poolSeeded(), nil, &asked), nil, poolReadOnly(poolSeeded()))
			Expect(get(h, "/pool/seed?size=10000").Code).To(Equal(http.StatusMethodNotAllowed))
			Expect(asked).To(BeEmpty())
		})

		// The same clash `cqrs pool` refuses on the command line: a seed
		// that ran under a live pool would have the pool folding a log
		// still being written, and the run would be measuring the seed.
		It("refuses to seed under a run that is in flight", func() {
			hold := make(chan struct{})
			var asked []int
			runner, entered := enteredOnce(stubPool(drained(), nil, nil, hold))
			h := poolFixtureAPI(runner, stubPoolSeeder(poolSeeded(), nil, &asked), nil,
				poolReadOnly(poolSeeded()))

			done := make(chan struct{})
			go func() { defer close(done); post(h, "/pool/run", `{"workers":8,"drain":true}`) }()
			Eventually(entered).Should(BeClosed())
			defer func() { close(hold); Eventually(done).Should(BeClosed()) }()

			rec := postSeed(h, "/pool/seed?size=10000")
			Expect(rec.Code).To(Equal(http.StatusConflict))
			Expect(decodeBody(rec)["error"]).To(Equal("PoolRunning"))
			Expect(asked).To(BeEmpty())
		})
	})

	Context("dropping the log", func() {
		It("drops it and says so", func() {
			calls := 0
			h := poolFixtureAPI(stubPool(drained(), nil, nil, nil),
				stubPoolSeeder(poolSeeded(), nil, nil), stubPoolDropper(nil, &calls),
				poolReadOnly(PoolState{Stream: PoolStream, Sizes: PoolSizes}))
			rec := postSeed(h, "/pool/rm")
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(calls).To(Equal(1))
			Expect(decodeBody(rec)["exists"]).To(BeFalse())
		})

		It("will not drop on a GET", func() {
			calls := 0
			h := poolFixtureAPI(stubPool(drained(), nil, nil, nil),
				stubPoolSeeder(poolSeeded(), nil, nil), stubPoolDropper(nil, &calls),
				poolReadOnly(poolSeeded()))
			Expect(get(h, "/pool/rm").Code).To(Equal(http.StatusMethodNotAllowed))
			Expect(calls).To(BeZero())
		})

		// Deleting the stream out from under a running consumer is the
		// worst version of the clash, so it gets the same refusal.
		It("refuses to drop under a run that is in flight", func() {
			hold := make(chan struct{})
			calls := 0
			runner, entered := enteredOnce(stubPool(drained(), nil, nil, hold))
			h := poolFixtureAPI(runner, stubPoolSeeder(poolSeeded(), nil, nil),
				stubPoolDropper(nil, &calls), poolReadOnly(poolSeeded()))

			done := make(chan struct{})
			go func() { defer close(done); post(h, "/pool/run", `{"workers":8,"drain":true}`) }()
			Eventually(entered).Should(BeClosed())
			defer func() { close(hold); Eventually(done).Should(BeClosed()) }()

			Expect(postSeed(h, "/pool/rm").Code).To(Equal(http.StatusConflict))
			Expect(calls).To(BeZero())
		})
	})
})

// Stopping the run in flight (plan 04.9.8, decision D6).
//
// Live is the only tab that starts an open-ended run, so it is the only tab
// with a Stop button. The gate has held the cancel func since 04.9.1 for
// exactly this: the gate is the only thing that knows WHICH run to stop, and
// a stop that took a run description in its body could stop the wrong one.
var _ = Describe("stopping the run in flight", func() {

	held := func() (http.Handler, chan struct{}, chan struct{}) {
		hold := make(chan struct{})
		runner, entered := enteredOnce(stubPool(drained(), nil, nil, hold))
		h := poolAPI(runner)
		done := make(chan struct{})
		go func() {
			defer close(done)
			post(h, "/pool/run", `{"workers":4,"maxPending":1000,"ackWait":"30s"}`)
		}()
		Eventually(entered).Should(BeClosed())
		return h, hold, done
	}

	It("cancels the run, so the runner returns without the hold being released", func() {
		h, hold, done := held()
		defer close(hold)

		rec := post(h, "/pool/stop", `{}`)
		Expect(rec.Code).To(Equal(http.StatusOK))
		// The proof is the run ENDING. The hold is still shut, so only the
		// cancelled context can have let stubPool return.
		Eventually(done).Should(BeClosed())
	})

	It("names the run it stopped, in the flags the reader typed", func() {
		h, hold, done := held()
		defer func() { close(hold); Eventually(done).Should(BeClosed()) }()

		body := decodeBody(post(h, "/pool/stop", `{}`))
		Expect(body["stopped"]).To(BeTrue())
		Expect(body["message"]).To(ContainSubstring("4 workers"))
	})

	// Not an error. Pressing Stop on a run that has just finished by itself
	// is the same request as pressing it a moment earlier, and a 4xx would
	// make the screen show a fault where nothing went wrong.
	It("says so plainly when there is nothing to stop", func() {
		rec := post(poolAPI(stubPool(drained(), nil, nil, nil)), "/pool/stop", `{}`)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(decodeBody(rec)["stopped"]).To(BeFalse())
	})

	It("refuses a GET, because stopping changes something", func() {
		rec := get(poolAPI(stubPool(drained(), nil, nil, nil)), "/pool/stop")
		Expect(rec.Code).To(Equal(http.StatusMethodNotAllowed))
	})
})

// 04.9.9. The Redelivery tab draws what the run measured, so the run body has
// to carry it. Absent is a real answer: a run with no kill in it did not
// redeliver anything, and a zero-filled record would draw a redelivery that
// never happened.
var _ = Describe("the run body and its redelivery", func() {

	It("leaves the redelivery out when the run had no kill in it", func() {
		out := poolRunResultOf(PoolConfig{Workers: 4}, PoolResult{Acked: 10})
		Expect(out.Redelivery).To(BeNil())
	})

	It("reports what the run measured, in the units the screen reads", func() {
		res := PoolResult{Acked: 9999, Dropped: 1, Redelivery: &PoolRedelivery{
			Seq: 94, KilledWorker: 1, ToWorker: 3, Delivery: 2,
			Waited: 30_005 * time.Millisecond, AckWait: 30 * time.Second,
			FoldAt: 124, Outcome: "dropped",
		}}
		out := poolRunResultOf(PoolConfig{Workers: 4, KillAt: 94}, res)

		Expect(out.Redelivery).NotTo(BeNil())
		Expect(out.Redelivery.Seq).To(Equal(uint64(94)))
		Expect(out.Redelivery.KilledWorker).To(Equal(1))
		Expect(out.Redelivery.ToWorker).To(Equal(3))
		Expect(out.Redelivery.Delivery).To(Equal(uint64(2)))
		Expect(out.Redelivery.WaitedMs).To(BeNumerically("~", 30005, 0.5))
		Expect(out.Redelivery.AckWait).To(Equal("30s"))
		Expect(out.Redelivery.FoldAt).To(Equal(uint64(124)))
		Expect(out.Redelivery.Outcome).To(Equal("dropped"))
	})

	// The distance the fold travelled while the worker was silent. The screen
	// could subtract it, but then two screens could subtract it differently.
	It("says how far the fold ran on during the silence", func() {
		res := PoolResult{Redelivery: &PoolRedelivery{Seq: 94, FoldAt: 124, Outcome: "dropped"}}
		out := poolRunResultOf(PoolConfig{Workers: 4}, res)
		Expect(out.Redelivery.RanOn).To(Equal(uint64(30)))
	})

	// A redelivery that arrived before the fold passed it was RECOVERED, and
	// a negative distance is not a thing. Zero, not a wrapped-around number.
	It("reports no distance when the fold had not passed it", func() {
		res := PoolResult{Redelivery: &PoolRedelivery{Seq: 94, FoldAt: 90, Outcome: "folded"}}
		out := poolRunResultOf(PoolConfig{Workers: 4}, res)
		Expect(out.Redelivery.RanOn).To(Equal(uint64(0)))
	})
})
