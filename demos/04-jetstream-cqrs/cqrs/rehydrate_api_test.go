package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Specs for the read-only rehydrate endpoint.
//
// There is no NATS here either. What is being proved is that the shim asks
// for the mode the caller named, and reports what the write side measured --
// unchanged. The number on the screen has to be the number the CLI would
// print, or the tab is a decoration rather than a demonstration.

// stubRehydrate records the mode it was asked for and answers with a fixed
// result. `asked` is the point of several specs below.
func stubRehydrate(out Rehydrated, err error, asked ...*[]bool) rehydrateRunner {
	return func(_ context.Context, _ Source, _ string, withSnapshot bool) (Rehydrated, error) {
		for _, a := range asked {
			*a = append(*a, withSnapshot)
		}
		return out, err
	}
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Origin", testOrigin)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// rehydrateAPI builds the handler with a stubbed command runner, mirroring
// what api() does for the command specs.
func rehydrateAPI(r rehydrateRunner) http.Handler {
	return newCommandAPI(apiDeps{
		run:          fakeRunner(registered(), 1),
		rehydrateOne: r,
		seedBench:    stubBench(BenchState{}, nil, nil),
		readBench:    readOnly(BenchState{}),
		origins:      []string{testOrigin},
	})
}

var _ = Describe("the rehydrate endpoint", func() {
	cold := Rehydrated{
		Vehicle:      Vehicle{Status: StatusRegistered, Plate: "ABC-123"},
		LastSeq:      10003,
		EventsRead:   10001,
		FromSeq:      1,
		UsedSnapshot: false,
		Elapsed:      25 * time.Millisecond,
	}

	Context("the mode the caller asked for", func() {
		It("replays from sequence 1 when snapshot=false", func() {
			var asked []bool
			h := rehydrateAPI(stubRehydrate(cold, nil, &asked))

			Expect(get(h, "/rehydrate?id=V1&snapshot=false").Code).To(Equal(http.StatusOK))
			Expect(asked).To(Equal([]bool{false}))
		})

		It("uses the snapshot when snapshot=true", func() {
			var asked []bool
			h := rehydrateAPI(stubRehydrate(cold, nil, &asked))

			Expect(get(h, "/rehydrate?id=V1&snapshot=true").Code).To(Equal(http.StatusOK))
			Expect(asked).To(Equal([]bool{true}))
		})

		// The expensive mode is never the accident. A missing flag must not
		// replay ten thousand events because somebody mistyped a query string.
		It("uses the snapshot when the flag is missing", func() {
			var asked []bool
			h := rehydrateAPI(stubRehydrate(cold, nil, &asked))

			Expect(get(h, "/rehydrate?id=V1").Code).To(Equal(http.StatusOK))
			Expect(asked).To(Equal([]bool{true}))
		})
	})

	Context("what it reports", func() {
		It("reports the cost the write side measured, not a rounded one", func() {
			h := rehydrateAPI(stubRehydrate(cold, nil))

			body := decodeBody(get(h, "/rehydrate?id=V1&snapshot=false"))

			Expect(body).To(HaveKeyWithValue("id", "V1"))
			Expect(body).To(HaveKeyWithValue("usedSnapshot", false))
			Expect(body).To(HaveKeyWithValue("fromSeq", float64(1)))
			Expect(body).To(HaveKeyWithValue("eventsRead", float64(10001)))
			Expect(body).To(HaveKeyWithValue("lastSeq", float64(10003)))
			Expect(body).To(HaveKeyWithValue("elapsedMs", float64(25)))
		})

		// The two sides must be shown to agree. A comparison whose halves
		// rebuilt different states is not a comparison, so the state travels
		// with every result and the screen prints it under both numbers.
		It("reports the state it rebuilt, so both sides can be compared", func() {
			h := rehydrateAPI(stubRehydrate(cold, nil))

			body := decodeBody(get(h, "/rehydrate?id=V1&snapshot=false"))

			Expect(body).To(HaveKeyWithValue("status", "registered"))
			Expect(body).To(HaveKeyWithValue("plate", "ABC-123"))
		})

		It("reports an empty vehicle without inventing a state", func() {
			h := rehydrateAPI(stubRehydrate(Rehydrated{UsedSnapshot: true, FromSeq: 1}, nil))

			body := decodeBody(get(h, "/rehydrate?id=ghost"))

			Expect(body).To(HaveKeyWithValue("status", ""))
			Expect(body).To(HaveKeyWithValue("eventsRead", float64(0)))
		})
	})

	Context("what it refuses", func() {
		It("needs an id", func() {
			h := rehydrateAPI(stubRehydrate(cold, nil))

			rec := get(h, "/rehydrate")

			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(decodeBody(rec)).To(HaveKeyWithValue("error", "BadRequest"))
		})

		// It reads and never writes, so it is a GET. A POST here would be a
		// lie about what the button does.
		It("is a GET, because it changes nothing", func() {
			h := rehydrateAPI(stubRehydrate(cold, nil))

			rec := post(h, "/rehydrate?id=V1", "{}")

			Expect(rec.Code).To(Equal(http.StatusMethodNotAllowed))
		})

		// A broken write side is never given a rule code. Rehydrating breaks
		// no business rule -- there is no command to refuse.
		It("reports a broken write side without a rule code", func() {
			h := rehydrateAPI(stubRehydrate(Rehydrated{}, errors.New("nats down")))

			rec := get(h, "/rehydrate?id=V1")
			body := decodeBody(rec)

			Expect(rec.Code).To(Equal(http.StatusBadGateway))
			Expect(body).To(HaveKeyWithValue("error", "Unavailable"))
			Expect(body).NotTo(HaveKey("rule"))
		})
	})

	Context("cross-origin", func() {
		It("answers the allowed origin", func() {
			h := rehydrateAPI(stubRehydrate(cold, nil))

			rec := get(h, "/rehydrate?id=V1")

			Expect(rec.Header().Get("Access-Control-Allow-Origin")).To(Equal(testOrigin))
		})

		It("allows GET in its preflight", func() {
			h := rehydrateAPI(stubRehydrate(cold, nil))

			req := httptest.NewRequest(http.MethodOptions, "/rehydrate?id=V1", nil)
			req.Header.Set("Origin", testOrigin)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusNoContent))
			Expect(rec.Header().Get("Access-Control-Allow-Methods")).To(ContainSubstring("GET"))
		})
	})
})
