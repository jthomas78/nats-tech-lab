package main

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Specs for the benchmark endpoints (plan 04.7.16).
//
// The shim holds no rules. What these prove is the wiring: that the button
// reaches the same seeder the CLI does, that a size off the list is refused
// before anything is written, and that the panel is handed the bytes it is
// required to show.

// stubBench records what it was asked for and answers with a fixed state.
func stubBench(out BenchState, err error, asked *[]int) benchRunner {
	return func(_ context.Context, size int) (BenchState, error) {
		if asked != nil {
			*asked = append(*asked, size)
		}
		return out, err
	}
}

func benchAPI(seed benchRunner, read benchReader) http.Handler {
	return newCommandAPI(apiDeps{
		run:          fakeRunner(registered(), 1),
		rehydrateOne: stubRehydrate(Rehydrated{}, nil),
		seedBench:    seed,
		readBench:    read,
		origins:      []string{testOrigin},
	})
}

func seeded() BenchState {
	return BenchState{
		Stream: "ODOMETER_BENCH", Subject: "evt.odometer-bench.>",
		WriteKV: "odometer-bench-write",
		Exists:  true, Events: 10_000, Bytes: 840_000,
		Sizes: BenchSizes,
		Fixtures: []BenchFixture{
			{Size: 10_000, Vehicle: "bench-10k", Events: 10_000, SnapSeq: 9_950, TailLeft: 50},
		},
	}
}

func readOnly(s BenchState) benchReader {
	return func(_ context.Context) (BenchState, error) { return s, nil }
}

var _ = Describe("the benchmark endpoints", func() {
	Context("reading what the fixture holds", func() {
		It("answers a GET with the fixture state", func() {
			h := benchAPI(stubBench(seeded(), nil, nil), readOnly(seeded()))
			body := decodeBody(get(h, "/bench"))
			Expect(body["stream"]).To(Equal("ODOMETER_BENCH"))
			Expect(body["exists"]).To(BeTrue())
		})

		// The standing rule from 04.7.16: a length is never reported alone.
		// A reader choosing 1 000 000 is spending disk, and the screen has
		// to be able to say how much.
		It("always reports bytes beside the event count", func() {
			h := benchAPI(stubBench(seeded(), nil, nil), readOnly(seeded()))
			body := decodeBody(get(h, "/bench"))
			Expect(body).To(HaveKey("events"))
			Expect(body).To(HaveKey("bytes"))
			Expect(body["bytes"]).To(BeNumerically(">", 0))
		})

		// Nobody has to seed. An empty fixture is a state to report, not a
		// failure, and the panel offers the button on the strength of it.
		It("reports an absent fixture without failing", func() {
			empty := BenchState{Stream: "ODOMETER_BENCH", Sizes: BenchSizes}
			h := benchAPI(stubBench(empty, nil, nil), readOnly(empty))
			rec := get(h, "/bench")
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(decodeBody(rec)["exists"]).To(BeFalse())
		})

		// The panel draws its size buttons from this, so it can never offer
		// a size the server would refuse.
		It("tells the panel which sizes it may ask for", func() {
			h := benchAPI(stubBench(seeded(), nil, nil), readOnly(seeded()))
			Expect(decodeBody(get(h, "/bench"))["sizes"]).To(HaveLen(len(BenchSizes)))
		})
	})

	Context("seeding a fixture", func() {
		It("passes the requested size to the seeder", func() {
			var asked []int
			h := benchAPI(stubBench(seeded(), nil, &asked), readOnly(seeded()))
			Expect(postSeed(h, "/bench/seed?size=100000").Code).To(Equal(http.StatusOK))
			Expect(asked).To(Equal([]int{100_000}))
		})

		It("answers with the fixture's new state", func() {
			h := benchAPI(stubBench(seeded(), nil, nil), readOnly(seeded()))
			body := decodeBody(postSeed(h, "/bench/seed?size=10000"))
			Expect(body["events"]).To(BeNumerically("==", 10_000))
			Expect(body["bytes"]).To(BeNumerically("==", 840_000))
		})

		// The fixed list IS the cap. A size off it is refused before a
		// single event is written, which is what keeps a typo from costing
		// a gigabyte.
		It("refuses a size that is not on the list, writing nothing", func() {
			var asked []int
			h := benchAPI(stubBench(seeded(), nil, &asked), readOnly(seeded()))
			rec := postSeed(h, "/bench/seed?size=99")
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(asked).To(BeEmpty())
		})

		It("refuses a missing size, writing nothing", func() {
			var asked []int
			h := benchAPI(stubBench(seeded(), nil, &asked), readOnly(seeded()))
			Expect(postSeed(h, "/bench/seed").Code).To(Equal(http.StatusBadRequest))
			Expect(asked).To(BeEmpty())
		})

		// Seeding writes a million events. A GET that did that would be
		// fetched by a link preview or a browser prefetch.
		It("will not seed on a GET", func() {
			var asked []int
			h := benchAPI(stubBench(seeded(), nil, &asked), readOnly(seeded()))
			Expect(get(h, "/bench/seed?size=10000").Code).To(Equal(http.StatusMethodNotAllowed))
			Expect(asked).To(BeEmpty())
		})
	})

	Context("rehydrating the fixture instead of the demo's log", func() {
		// asked records which log the handler actually reached for. Without
		// this the query parameter could be ignored entirely and every spec
		// above would still pass.
		api := func(asked *[]string) http.Handler {
			return newCommandAPI(apiDeps{
				run: fakeRunner(registered(), 1),
				rehydrateOne: func(_ context.Context, src Source, _ string, _ bool) (Rehydrated, error) {
					*asked = append(*asked, src.Stream)
					return Rehydrated{}, nil
				},
				seedBench: stubBench(seeded(), nil, nil),
				readBench: readOnly(seeded()),
				origins:   []string{testOrigin},
			})
		}

		It("reads the benchmark when asked to", func() {
			var asked []string
			Expect(get(api(&asked), "/rehydrate?id=bench-10k&source=bench").Code).
				To(Equal(http.StatusOK))
			Expect(asked).To(Equal([]string{Bench.Stream}))
		})

		// The demo's own log is the default. A missing parameter must not
		// silently point the headline measurement at a fixture.
		It("reads the demo's log when no source is named", func() {
			var asked []string
			Expect(get(api(&asked), "/rehydrate?id=V1").Code).To(Equal(http.StatusOK))
			Expect(asked).To(Equal([]string{Live.Stream}))
		})

		It("refuses a source it does not have", func() {
			var asked []string
			Expect(get(api(&asked), "/rehydrate?id=V1&source=nonsense").Code).
				To(Equal(http.StatusBadRequest))
			Expect(asked).To(BeEmpty())
		})
	})
})

func postSeed(h http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Origin", testOrigin)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
