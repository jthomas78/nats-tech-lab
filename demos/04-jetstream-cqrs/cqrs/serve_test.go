package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Specs for the HTTP shim.
//
// There is no NATS here on purpose. The shim's job is transport: read JSON,
// call the domain, turn the domain's answer into a status code and a rule
// code. Every one of those steps is testable with a fake runner, and a fake
// runner that calls the REAL decide function means these specs still fail if
// someone reimplements a rule inside serve.go.

const testOrigin = "http://localhost:20401"

// fakeRunner stands in for the write side. It hands the supplied aggregate to
// the real domain decision, so the rules under test are the ones in
// domain.go, not a copy.
func fakeRunner(v Vehicle, seq uint64) commandRunner {
	return func(_ context.Context, _ string, decide func(Vehicle) (Event, error)) (uint64, error) {
		if _, err := decide(v); err != nil {
			return 0, err
		}
		return seq, nil
	}
}

// failingRunner stands in for a broken write side — NATS down, for example.
func failingRunner(err error) commandRunner {
	return func(context.Context, string, func(Vehicle) (Event, error)) (uint64, error) {
		return 0, err
	}
}

// api builds the handler these specs drive.
//
// The rehydrate runner is a stub. This file is about commands; the read-only
// rehydrate endpoint has its own spec file, and a command spec that had to
// name a rehydration would be describing two things at once.
func api(run commandRunner) http.Handler {
	return newCommandAPI(run, stubRehydrate(Rehydrated{}, nil),
		stubBench(BenchState{}, nil, nil), readOnly(BenchState{}),
		[]string{testOrigin})
}

func post(h http.Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Origin", testOrigin)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeBody(rec *httptest.ResponseRecorder) map[string]any {
	out := map[string]any{}
	ExpectWithOffset(1, json.Unmarshal(rec.Body.Bytes(), &out)).To(Succeed())
	return out
}

// registered() and retired() come from domain_test.go. unregistered is the
// zero Vehicle: a vehicle the log has never seen.
func unregistered() Vehicle { return Vehicle{} }

var _ = Describe("the command API", func() {

	Describe("a command the domain allows", func() {
		It("answers 200 with the sequence the event landed on", func() {
			h := api(fakeRunner(registered(), 129))
			rec := post(h, "/commands/travel", `{"id":"truck-7","km":42}`)

			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(decodeBody(rec)).To(HaveKeyWithValue("seq", float64(129)))
		})

		It("registers an unknown vehicle", func() {
			h := api(fakeRunner(unregistered(), 1))
			rec := post(h, "/commands/register", `{"id":"truck-7","plate":"CA 41-208"}`)

			Expect(rec.Code).To(Equal(http.StatusOK))
		})

		It("retires a registered vehicle", func() {
			h := api(fakeRunner(registered(), 130))
			rec := post(h, "/commands/retire", `{"id":"truck-7","reason":"scrapped"}`)

			Expect(rec.Code).To(Equal(http.StatusOK))
		})
	})

	// The reason the shim exists. A refusal must name the rule, because the
	// screen shows the rule code and a demo that says only "rejected" teaches
	// nothing.
	Describe("a command the domain refuses", func() {
		DescribeTable("answers 409 and names the rule",
			func(path, body string, state Vehicle, rule, errName string) {
				h := api(fakeRunner(state, 0))
				rec := post(h, path, body)

				Expect(rec.Code).To(Equal(http.StatusConflict))
				out := decodeBody(rec)
				Expect(out).To(HaveKeyWithValue("rule", rule))
				Expect(out).To(HaveKeyWithValue("error", errName))
				Expect(out["message"]).ToNot(BeEmpty())
				Expect(out).ToNot(HaveKey("seq"))
			},
			Entry("BR-OD01 — a trip of zero km",
				"/commands/travel", `{"id":"truck-7","km":0}`, registered(),
				"BR-OD01", "ErrNonPositiveKm"),
			Entry("BR-OD01 — a trip of negative km",
				"/commands/travel", `{"id":"truck-7","km":-3}`, registered(),
				"BR-OD01", "ErrNonPositiveKm"),
			Entry("BR-OD02 — an unregistered vehicle cannot travel",
				"/commands/travel", `{"id":"ghost","km":10}`, unregistered(),
				"BR-OD02", "ErrNotRegistered"),
			Entry("BR-OD03 — a vehicle cannot be registered twice",
				"/commands/register", `{"id":"truck-7","plate":"X"}`, registered(),
				"BR-OD03", "ErrAlreadyRegistered"),
			Entry("BR-OD04 — a retired vehicle refuses trips",
				"/commands/travel", `{"id":"bus-1","km":10}`, retired(),
				"BR-OD04", "ErrRetired"),
			Entry("BR-OD05 — an unregistered vehicle cannot be retired",
				"/commands/retire", `{"id":"ghost"}`, unregistered(),
				"BR-OD05", "ErrNotRegistered"),
			Entry("BR-OD05 — a retired vehicle cannot be retired again",
				"/commands/retire", `{"id":"bus-1"}`, retired(),
				"BR-OD05", "ErrRetired"),
		)

		// ErrNotRegistered and ErrRetired are each raised by two commands
		// under two different rules. The shim must read the rule from the
		// endpoint, not from the error alone.
		It("calls the same error BR-OD02 on travel and BR-OD05 on retire", func() {
			h := api(fakeRunner(unregistered(), 0))

			Expect(decodeBody(post(h, "/commands/travel", `{"id":"g","km":1}`))).
				To(HaveKeyWithValue("rule", "BR-OD02"))
			Expect(decodeBody(post(h, "/commands/retire", `{"id":"g"}`))).
				To(HaveKeyWithValue("rule", "BR-OD05"))
		})
	})

	Describe("a request the shim itself cannot use", func() {
		It("answers 400 when the body is not JSON", func() {
			h := api(fakeRunner(registered(), 1))
			Expect(post(h, "/commands/travel", `not json`).Code).To(Equal(http.StatusBadRequest))
		})

		It("answers 400 when id is missing", func() {
			h := api(fakeRunner(registered(), 1))
			rec := post(h, "/commands/travel", `{"km":42}`)

			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(decodeBody(rec)["message"]).ToNot(BeEmpty())
		})

		It("answers 405 for a GET", func() {
			h := api(fakeRunner(registered(), 1))
			req := httptest.NewRequest(http.MethodGet, "/commands/travel", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusMethodNotAllowed))
		})

		It("answers 404 for an unknown command", func() {
			h := api(fakeRunner(registered(), 1))
			Expect(post(h, "/commands/explode", `{"id":"x"}`).Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("a write side that is not working", func() {
		It("answers 502, and does not dress the failure up as a rule", func() {
			h := api(failingRunner(context.DeadlineExceeded))
			rec := post(h, "/commands/travel", `{"id":"truck-7","km":42}`)

			Expect(rec.Code).To(Equal(http.StatusBadGateway))
			Expect(decodeBody(rec)).ToNot(HaveKey("rule"))
		})

		It("answers 503 when optimistic concurrency ran out of retries", func() {
			h := api(failingRunner(ErrConflict))
			rec := post(h, "/commands/travel", `{"id":"truck-7","km":42}`)

			Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))
			Expect(decodeBody(rec)).To(HaveKeyWithValue("error", "ErrConflict"))
		})
	})

	// The browser is on 20401 and the API is on 20402, so every real request
	// is cross-origin. Without these headers the UI gets nothing.
	Describe("CORS", func() {
		It("allows the configured origin", func() {
			h := api(fakeRunner(registered(), 1))
			rec := post(h, "/commands/travel", `{"id":"truck-7","km":42}`)

			Expect(rec.Header().Get("Access-Control-Allow-Origin")).To(Equal(testOrigin))
		})

		It("stays silent for an origin that is not configured", func() {
			h := api(fakeRunner(registered(), 1))
			req := httptest.NewRequest(http.MethodPost, "/commands/travel", strings.NewReader(`{"id":"x","km":1}`))
			req.Header.Set("Origin", "http://evil.example")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			Expect(rec.Header().Get("Access-Control-Allow-Origin")).To(BeEmpty())
		})

		It("answers the preflight", func() {
			h := api(fakeRunner(registered(), 1))
			req := httptest.NewRequest(http.MethodOptions, "/commands/travel", nil)
			req.Header.Set("Origin", testOrigin)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusNoContent))
			Expect(rec.Header().Get("Access-Control-Allow-Origin")).To(Equal(testOrigin))
			Expect(rec.Header().Get("Access-Control-Allow-Methods")).To(ContainSubstring("POST"))
			Expect(rec.Header().Get("Access-Control-Allow-Headers")).To(ContainSubstring("Content-Type"))
		})
	})
})
