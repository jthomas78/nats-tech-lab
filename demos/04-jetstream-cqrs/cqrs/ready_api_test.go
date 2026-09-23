package main

// Specs for /readyz — the lab shell's pre-mount check (app-shell BR-AS79).
//
// No NATS here either. The endpoint's job is to turn a list of answers into
// a status code and a body, and the one judgement it makes is "all of them or
// not". The real checks are three JetStream lookups wired in runServe; what
// is worth pinning down is the CONTRACT the shell reads.
//
// The two things these specs exist to stop:
//
//   - A shim that is up with no stream answering 200. "A port answered" is
//     not readiness, and BR-AS79 says so in as many words.
//   - A CORS header appearing on this route. The shell reaches it through a
//     proxy on its own origin; a grant here would be the cross-origin
//     exception F-3 forbids, and nobody would notice it for months.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// readyAPI builds the shim with only the readiness collaborator supplied.
func readyAPI(ready readinessChecker) http.Handler {
	return newCommandAPI(apiDeps{
		run:          fakeRunner(registered(), 1),
		rehydrateOne: stubRehydrate(Rehydrated{}, nil),
		seedBench:    stubBench(BenchState{}, nil, nil),
		readBench:    readOnly(BenchState{}),
		ready:        ready,
		origins:      []string{testOrigin},
	})
}

func fixedChecks(checks ...readyCheck) readinessChecker {
	return func(context.Context) []readyCheck { return checks }
}

// getReady sends one GET and decodes the body.
func getReady(h http.Handler, origin string) (*httptest.ResponseRecorder, map[string]any) {
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec, body
}

var _ = Describe("the readiness endpoint", func() {
	It("answers 200 and ready when every check passed", func() {
		rec, body := getReady(readyAPI(fixedChecks(
			readyCheck{Name: "stream ODOMETER", OK: true},
			readyCheck{Name: "kv odometer-write", OK: true},
		)), testOrigin)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(body["ready"]).To(BeTrue())
	})

	It("answers 503 when one check failed, and names which", func() {
		rec, body := getReady(readyAPI(fixedChecks(
			readyCheck{Name: "stream ODOMETER", OK: true},
			readyCheck{Name: "kv odometer-write", Code: "missing"},
		)), testOrigin)

		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))
		Expect(body["ready"]).To(BeFalse())
		Expect(rec.Body.String()).To(ContainSubstring(`"code":"missing"`))
	})

	// The distinction BR-AS79 is built on. A process answering this route is
	// proof of nothing; the demo is ready only when its NATS objects exist.
	It("is not satisfied by the process merely being up", func() {
		rec, body := getReady(readyAPI(fixedChecks(
			readyCheck{Name: "stream ODOMETER", Code: "missing"},
		)), testOrigin)

		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))
		Expect(body["ready"]).To(BeFalse())
	})

	// An unwired probe is a deployment mistake, not a healthy demo.
	It("reports not ready rather than panicking when nothing is wired", func() {
		rec, body := getReady(readyAPI(nil), testOrigin)

		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))
		Expect(body["ready"]).To(BeFalse())
	})

	It("refuses a POST", func() {
		req := httptest.NewRequest(http.MethodPost, "/readyz", strings.NewReader("{}"))
		req.Header.Set("Origin", testOrigin)
		rec := httptest.NewRecorder()
		readyAPI(fixedChecks(readyCheck{Name: "x", OK: true})).ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusMethodNotAllowed))
	})

	// F-3: readiness is same-origin through the shell's own proxy. No grant
	// is issued here, not even to the origin the command routes allow.
	It("sets no CORS header, for any origin", func() {
		ready := fixedChecks(readyCheck{Name: "x", OK: true})

		for _, origin := range []string{testOrigin, "http://localhost:7110", ""} {
			rec, _ := getReady(readyAPI(ready), origin)
			Expect(rec.Header().Get("Access-Control-Allow-Origin")).To(BeEmpty())
		}
	})
})
