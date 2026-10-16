package tracequery_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/observability-service/observability/internal/tracequery"
)

// searchTempo serves Tempo's search and tag-values routes from testdata and
// records the last query it was sent.
//
// testdata/tempo-search.json: the first two traces are real replies,
// captured 2026-10-16 from the pinned Tempo (grafana/tempo 3.1.0) on au-1.
// The second has a 31-character trace ID: Tempo drops a leading zero. The
// third is hand-edited to show two cases a capture did not: a root span not
// yet received (no rootTraceName, no durationMs) and two services.
// testdata/tempo-tag-values.json is a real reply from the same day.
type searchTempo struct {
	*httptest.Server
	mu     sync.Mutex
	last   url.Values
	status int
}

func newSearchTempo() *searchTempo {
	search, err := os.ReadFile("testdata/tempo-search.json")
	Expect(err).NotTo(HaveOccurred())
	tags, err := os.ReadFile("testdata/tempo-tag-values.json")
	Expect(err).NotTo(HaveOccurred())
	st := &searchTempo{status: http.StatusOK}
	st.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		st.last = r.URL.Query()
		status := st.status
		st.mu.Unlock()
		if status != http.StatusOK {
			http.Error(w, "tempo internal detail", status)
			return
		}
		switch r.URL.Path {
		case "/api/search":
			_, _ = w.Write(search)
		case "/api/v2/search/tag/resource.service.name/values":
			_, _ = w.Write(tags)
		default:
			http.NotFound(w, r)
		}
	}))
	DeferCleanup(st.Close)
	return st
}

func (st *searchTempo) query() url.Values {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.last
}

func (st *searchTempo) fail(status int) {
	st.mu.Lock()
	st.status = status
	st.mu.Unlock()
}

func mountOn(backend tracequery.Backend) func(subject, body string) (map[string]any, string) {
	nc := startNATS()
	stop, err := tracequery.Mount(nc, backend)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(stop)
	return func(subject, body string) (map[string]any, string) {
		reply, err := nc.Request(subject, []byte(body), 5*time.Second)
		Expect(err).NotTo(HaveOccurred())
		if code := reply.Header.Get("Nats-Service-Error-Code"); code != "" {
			Expect(reply.Header.Get("Nats-Service-Error")).NotTo(BeEmpty())
			Expect(string(reply.Data)).NotTo(ContainSubstring("tempo internal detail"))
			return nil, code
		}
		var out map[string]any
		Expect(json.Unmarshal(reply.Data, &out)).To(Succeed())
		return out, ""
	}
}

var _ = Describe("BR-074: trace.search returns capped trace summaries in the lab's own schema", func() {
	var (
		tempo *searchTempo
		call  func(subject, body string) (map[string]any, string)
	)

	BeforeEach(func() {
		tempo = newSearchTempo()
		call = mountOn(tracequery.NewTempo(tempo.URL, http.DefaultClient))
	})

	Context("BR-074: mapping a Tempo search", func() {
		It("names the search schema, the limit and the window", func() {
			before := time.Now()
			got, code := call(tracequery.SearchSubject, `{}`)
			Expect(code).To(BeEmpty())
			Expect(got["schema"]).To(Equal(tracequery.SearchSchemaVersion))
			Expect(got["limit"]).To(BeNumerically("==", tracequery.DefaultSearchLimit))
			end := int64(got["windowEndUnixNano"].(float64))
			start := int64(got["windowStartUnixNano"].(float64))
			Expect(end).To(BeNumerically(">=", before.Add(-time.Second).UnixNano()))
			Expect(time.Duration(end - start)).To(BeNumerically("~", tracequery.DefaultSearchWindow, time.Second))
		})

		It("pads trace IDs to 32 hex characters and orders newest first", func() {
			got, _ := call(tracequery.SearchSubject, `{}`)
			traces := got["traces"].([]any)
			Expect(traces).To(HaveLen(3))
			ids := []string{}
			for _, t := range traces {
				ids = append(ids, t.(map[string]any)["traceId"].(string))
			}
			Expect(ids).To(Equal([]string{
				"2d4800fba9809b538db890d1da1ac9ee",
				"9ce968fc29b9e77140d2404a6a8834b8",
				"0fe67960cdc0b783582ea4689f37879c",
			}))
		})

		It("keeps root, start, duration, span and error counts, and the services", func() {
			got, _ := call(tracequery.SearchSubject, `{}`)
			byID := map[string]map[string]any{}
			for _, t := range got["traces"].([]any) {
				m := t.(map[string]any)
				byID[m["traceId"].(string)] = m
			}
			arrive := byID["9ce968fc29b9e77140d2404a6a8834b8"]
			Expect(arrive["rootService"]).To(Equal("shipping-service"))
			Expect(arrive["rootName"]).To(Equal("api.acme.shipping.ship.arrive.v1"))
			Expect(arrive["startUnixNano"]).To(BeNumerically("==", 1792170166109209968))
			Expect(arrive["durationMs"]).To(BeNumerically("==", 23))
			Expect(arrive["spanCount"]).To(BeNumerically("==", 6))
			Expect(arrive["errorCount"]).To(BeNumerically("==", 0))
			Expect(arrive["services"]).To(Equal([]any{"shipping-service"}))

			Expect(byID["0fe67960cdc0b783582ea4689f37879c"]["errorCount"]).To(BeNumerically("==", 1))

			late := byID["2d4800fba9809b538db890d1da1ac9ee"]
			Expect(late["rootService"]).To(BeEmpty(), "Tempo's placeholder is not a service name")
			Expect(late["rootName"]).To(BeEmpty())
			Expect(late["spanCount"]).To(BeNumerically("==", 3))
			Expect(late["errorCount"]).To(BeNumerically("==", 1))
			Expect(late["services"]).To(Equal([]any{"organizations-service", "refdata-service"}))
		})

		It("carries none of Tempo's own keys", func() {
			got, _ := call(tracequery.SearchSubject, `{}`)
			keys := map[string]bool{}
			keysAnywhere(got, keys)
			for _, tempoKey := range []string{"traceID", "rootServiceName", "rootTraceName", "spanSet", "spanSets", "serviceStats", "metrics", "stringValue"} {
				Expect(keys).NotTo(HaveKey(tempoKey))
			}
		})
	})

	Context("BR-074: the query sent to Tempo", func() {
		It("asks for every trace, newest first, one span per set, with no filter", func() {
			_, code := call(tracequery.SearchSubject, `{}`)
			Expect(code).To(BeEmpty())
			q := tempo.query()
			Expect(q.Get("q")).To(Equal(`{ } with (most_recent=true)`))
			Expect(q.Get("limit")).To(Equal("50"))
			Expect(q.Get("spss")).To(Equal("1"))
			Expect(q.Get("start")).NotTo(BeEmpty())
			Expect(q.Get("end")).NotTo(BeEmpty())
		})

		It("adds each filter as its own span set, so the spans may differ", func() {
			_, code := call(tracequery.SearchSubject,
				`{"service":"shipping-service","name":"api.acme.shipping.ship.arrive.v1","tenant":"acme","errorsOnly":true,"limit":7,"sinceMs":60000}`)
			Expect(code).To(BeEmpty())
			q := tempo.query()
			Expect(q.Get("q")).To(Equal(`{ resource.service.name = "shipping-service" } && ` +
				`{ name = "api.acme.shipping.ship.arrive.v1" } && ` +
				`{ span.lab.tenant.originating = "acme" } && ` +
				`{ status = error } with (most_recent=true)`))
			Expect(q.Get("limit")).To(Equal("7"))
		})
	})

	Context("BR-074: a capped result is never a total", func() {
		It("says limited when the reply fills the limit", func() {
			got, _ := call(tracequery.SearchSubject, `{"limit":3}`)
			Expect(got["limited"]).To(BeTrue())
			Expect(got).NotTo(HaveKey("total"))
		})

		It("says not limited when the reply is under the limit", func() {
			got, _ := call(tracequery.SearchSubject, `{"limit":4}`)
			Expect(got["limited"]).To(BeFalse())
		})

		It("keeps only the limit when the store sends more", func() {
			got, _ := call(tracequery.SearchSubject, `{"limit":2}`)
			Expect(got["traces"]).To(HaveLen(2))
			Expect(got["limited"]).To(BeTrue())
		})
	})

	DescribeTable("BR-074: refuses a request that could break the query or the bounds",
		func(body, code string) {
			_, got := call(tracequery.SearchSubject, body)
			Expect(got).To(Equal(code))
			Expect(tempo.query()).To(BeNil(), "a refused request never reaches Tempo")
		},
		Entry("a body that is not JSON", `nope`, "400"),
		Entry("an unknown field", `{"q":"{ }"}`, "400"),
		Entry("a quote in a filter value", `{"service":"x\" } || { "}`, "400"),
		Entry("a backslash in a filter value", `{"name":"a\\b"}`, "400"),
		Entry("a filter value that is too long", `{"tenant":"`+longValue()+`"}`, "400"),
		Entry("a limit over the maximum", `{"limit":201}`, "400"),
		Entry("a negative limit", `{"limit":-1}`, "400"),
		Entry("a window over the maximum", `{"sinceMs":86400001}`, "400"),
		Entry("a negative window", `{"sinceMs":-1}`, "400"),
	)

	It("BR-074: answers 503, without Tempo's text, when Tempo fails", func() {
		tempo.fail(http.StatusInternalServerError)
		_, code := call(tracequery.SearchSubject, `{}`)
		Expect(code).To(Equal("503"))
	})

	It("BR-074: an empty body is the same as {}", func() {
		_, code := call(tracequery.SearchSubject, ``)
		Expect(code).To(BeEmpty())
	})
})

var _ = Describe("BR-075: trace.services returns the service names seen in the store", func() {
	var (
		tempo *searchTempo
		call  func(subject, body string) (map[string]any, string)
	)

	BeforeEach(func() {
		tempo = newSearchTempo()
		call = mountOn(tracequery.NewTempo(tempo.URL, http.DefaultClient))
	})

	It("BR-075: lists the names sorted, in the lab's own schema", func() {
		got, code := call(tracequery.ServicesSubject, `{}`)
		Expect(code).To(BeEmpty())
		Expect(got["schema"]).To(Equal(tracequery.ServicesSchemaVersion))
		Expect(got["services"]).To(Equal([]any{"accounts-service", "mfe-registry-service", "shipping-service"}))
		Expect(got).NotTo(HaveKey("tagValues"))
		Expect(tempo.query().Get("start")).NotTo(BeEmpty())
	})

	It("BR-075: refuses a body with fields", func() {
		_, code := call(tracequery.ServicesSubject, `{"tag":"span.secret"}`)
		Expect(code).To(Equal("400"))
	})

	It("BR-075: answers 503, without Tempo's text, when Tempo fails", func() {
		tempo.fail(http.StatusBadGateway)
		_, code := call(tracequery.ServicesSubject, `{}`)
		Expect(code).To(Equal("503"))
	})
})

func longValue() string {
	b := make([]byte, tracequery.MaxFilterLength+1)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

// Backend must stay small enough for a second store to implement.
var _ tracequery.Backend = (*tracequery.Tempo)(nil)
