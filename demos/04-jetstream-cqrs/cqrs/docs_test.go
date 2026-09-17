package main

// 04.9.10 — the documents catch up. Strengthened 2026-09-17.
//
// A live guard, not a unit test. It reads the ROUTE TABLE OUT OF this demo's
// CLAUDE.md and checks it against the shim itself — the route names against
// serve.go, and the METHOD column against the handler's real behaviour.
//
// The first version only asked whether each route STRING appeared somewhere
// in the document. That was weaker than it read, in two ways:
//
//   - `/pool` is a prefix of `/pool/run`, so deleting the `/pool` row left
//     the check passing on a different row's text.
//   - A route registration carries no method. `mux.HandleFunc("/rehydrate",
//     ...)` says nothing about GET or POST; the method lives inside the
//     handler body. So no amount of regexp over serve.go can verify the
//     document's second column. Only a request can.
//
// The document said `/rehydrate` was POST. It is GET, and had been for as
// long as the frontend had called it.
//
// It still deliberately does NOT check the wording of the third column. A
// document that describes the right routes in its own words is fine.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// demoRoot walks up from the test's working directory (cqrs/) until it finds
// the demo's own CLAUDE.md. The suite's working directory is the package
// directory, but that is a promise of the tool, not of the repository, so
// the walk is cheap insurance.
func demoRoot() string {
	dir, err := os.Getwd()
	Expect(err).NotTo(HaveOccurred())
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "BUSINESS_RULES-ODOMETER.md")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	Fail("could not find the demo root above the test's working directory")
	return ""
}

var routeLine = regexp.MustCompile(`mux\.HandleFunc\("([^"]+)"`)

// docRow reads one row of the shim's route table: the route in backticks,
// then the method. Anchored at the start of a line so prose that mentions a
// route in passing is not mistaken for a row.
var docRow = regexp.MustCompile("(?m)^\\| `(/[^`]*)` \\| ([A-Z]+) \\|")

// contractAPI builds the whole shim with every collaborator stubbed. No NATS.
// The specs below never look at a body — only at whether the method was
// allowed — so a stub that answers instantly is enough.
func contractAPI() http.Handler {
	return newCommandAPI(apiDeps{
		run:          fakeRunner(registered(), 1),
		rehydrateOne: stubRehydrate(Rehydrated{}, nil),
		seedBench:    stubBench(BenchState{}, nil, nil),
		readBench:    readOnly(BenchState{}),
		runPool:      func(context.Context, Source, PoolConfig) (PoolResult, error) { return PoolResult{}, nil },
		seedPool:     func(context.Context, int) (PoolState, error) { return PoolState{}, nil },
		dropPool:     func(context.Context) error { return nil },
		readPool:     func(context.Context) (PoolState, error) { return PoolState{}, nil },
		origins:      []string{testOrigin},
	})
}

// probe sends one request to a FRESH handler and answers with the status.
// Fresh, because the pool gate allows one run at a time and a probe must not
// be refused for something the previous probe left behind.
func probe(method, path string) int {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Origin", testOrigin)
	rec := httptest.NewRecorder()
	contractAPI().ServeHTTP(rec, req)
	return rec.Code
}

// theOther is the method the document did NOT claim. Every route in this
// shim is GET or POST, so one probe each way is the whole contract.
func theOther(method string) string {
	if method == http.MethodGet {
		return http.MethodPost
	}
	return http.MethodGet
}

var _ = Describe("the documents and the shim", func() {
	served, err := os.ReadFile("serve.go")
	Expect(err).NotTo(HaveOccurred())
	guide, err := os.ReadFile(filepath.Join(demoRoot(), "CLAUDE.md"))
	Expect(err).NotTo(HaveOccurred())

	var routes []string
	for _, m := range routeLine.FindAllStringSubmatch(string(served), -1) {
		routes = append(routes, m[1])
	}

	documented := map[string]string{}
	var documentedRoutes []string
	for _, m := range docRow.FindAllStringSubmatch(string(guide), -1) {
		documented[m[1]] = m[2]
		documentedRoutes = append(documentedRoutes, m[1])
	}

	It("found the routes to check, rather than checking none", func() {
		// A regexp that matched nothing would pass every assertion below.
		Expect(len(routes)).To(BeNumerically(">=", 8))
		Expect(len(documentedRoutes)).To(BeNumerically(">=", 8))
	})

	// Set equality, not substring. A row deleted from the table fails here
	// even when another row's text still contains its path.
	It("lists exactly the routes the shim registers, no more and no fewer", func() {
		Expect(documentedRoutes).To(ConsistOf(routes))
	})

	for _, r := range routes {
		route := r
		It("names "+route+" in CLAUDE.md", func() {
			Expect(documented).To(HaveKey(route))
		})
	}

	// The behavioural half. The method column is a claim about what the shim
	// answers, so it is checked by asking the shim.
	for _, r := range documentedRoutes {
		route, method := r, documented[r]

		It("really accepts "+method+" on "+route, func() {
			Expect(probe(method, route)).NotTo(Equal(http.StatusMethodNotAllowed),
				"CLAUDE.md says "+route+" is "+method+", and the shim refused it")
		})

		It("really refuses "+theOther(method)+" on "+route, func() {
			Expect(probe(theOther(method), route)).To(Equal(http.StatusMethodNotAllowed),
				"CLAUDE.md says "+route+" is "+method+" only, and the shim allowed "+theOther(method))
		})
	}

	It("says lesson 02 is driven from the screen", func() {
		Expect(strings.ToLower(string(guide))).To(ContainSubstring("lesson 02 runs itself"))
	})
})
