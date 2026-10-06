package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The route table spec, in the shape of demo 04's docs_test.go: the plan's
// "### Routes" table is read, each brace is expanded, and every row is
// checked against the handler — its method is accepted, the other method is
// a 405 — and against routeTable(), both ways.

var planRouteRow = regexp.MustCompile("(?m)^\\| (GET|POST) \\| `([^`]+)` \\|")

// expandBraces turns /clusters/{za,arb,au}/freeze into three paths.
func expandBraces(path string) []string {
	open := strings.Index(path, "{")
	if open < 0 {
		return []string{path}
	}
	end := strings.Index(path, "}")
	var out []string
	for _, alt := range strings.Split(path[open+1:end], ",") {
		out = append(out, expandBraces(path[:open]+alt+path[end+1:])...)
	}
	return out
}

func plannedRoutes() []route {
	// This runs while the spec tree is built, where Expect cannot; a
	// missing plan or section leaves no routes, and the count spec fails.
	raw, err := os.ReadFile("../PLAYGROUND-PLAN.md")
	if err != nil {
		return nil
	}
	text := string(raw)
	start := strings.Index(text, "### Routes")
	if start < 0 {
		return nil
	}
	section := text[start:]
	if next := strings.Index(section[3:], "\n### "); next >= 0 {
		section = section[:next+3]
	}
	var out []route
	for _, m := range planRouteRow.FindAllStringSubmatch(section, -1) {
		for _, p := range expandBraces(m[2]) {
			out = append(out, route{m[1], p})
		}
	}
	return out
}

const goodOrigin = "http://localhost:7110"

func send(h http.Handler, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if method == http.MethodPost {
		req.Header.Set("Origin", goodOrigin)
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func refusalOf(w *httptest.ResponseRecorder) refusal {
	var r refusal
	Expect(json.Unmarshal(w.Body.Bytes(), &r)).To(Succeed(), w.Body.String())
	return r
}

func theOtherMethod(m string) string {
	if m == http.MethodGet {
		return http.MethodPost
	}
	return http.MethodGet
}

var _ = Describe("the HTTP layer", func() {
	Describe("the route table (PLAYGROUND-PLAN.md, ### Routes)", func() {
		planned := plannedRoutes()

		It("found the routes to check, rather than checking none", func() {
			Expect(len(planned)).To(Equal(22))
		})

		It("serves exactly the planned routes, no more and no fewer", func() {
			Expect(routeTable()).To(ConsistOf(planned))
		})

		for _, rt := range planned {
			rt := rt
			It("accepts "+rt.Method+" on "+rt.Path, func() {
				h := newAPI(newRigUnderTest("").c, allowedOrigins)
				w := send(h, rt.Method, rt.Path, "{}", nil)
				Expect(w.Code).NotTo(Equal(http.StatusMethodNotAllowed), w.Body.String())
				Expect(w.Code).NotTo(Equal(http.StatusNotFound), w.Body.String())
			})

			It("refuses "+theOtherMethod(rt.Method)+" on "+rt.Path+" with a JSON 405", func() {
				h := newAPI(newRigUnderTest("").c, allowedOrigins)
				w := send(h, theOtherMethod(rt.Method), rt.Path, "{}", nil)
				Expect(w.Code).To(Equal(http.StatusMethodNotAllowed))
				Expect(refusalOf(w).Error).To(Equal("MethodNotAllowed"))
			})
		}

		DescribeTable("404s a name outside the fixed sets",
			func(path string) {
				h := newAPI(newRigUnderTest(ownerOwned).c, allowedOrigins)
				Expect(send(h, http.MethodPost, path, "{}", nil).Code).To(Equal(http.StatusNotFound))
			},
			Entry("an unknown cluster", "/clusters/eu/freeze"),
			Entry("an unknown verb", "/clusters/za/kill"),
			Entry("a server name, not a cluster", "/clusters/t-za-1/freeze"),
			Entry("a deeper path", "/clusters/za/freeze/now"),
			Entry("an unknown rig verb", "/rig/down"),
		)
	})

	Describe("local callers only (D03-R27)", func() {
		DescribeTable("403s a POST without the right Origin and Content-Type",
			func(hdr map[string]string) {
				r := newRigUnderTest(ownerOwned)
				w := send(newAPI(r.c, allowedOrigins), http.MethodPost, "/clusters/za/freeze", "{}", hdr)

				Expect(w.Code).To(Equal(http.StatusForbidden))
				Expect(r.host.signals()).To(BeEmpty(), "nothing was signalled")
			},
			Entry("no Origin", map[string]string{"Origin": ""}),
			Entry("another site's Origin", map[string]string{"Origin": "https://example.com"}),
			Entry("an allowed host on another port", map[string]string{"Origin": "http://localhost:7111"}),
			Entry("a form post", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}),
			Entry("text/plain, which a page may send with no preflight", map[string]string{"Content-Type": "text/plain"}),
			Entry("no Content-Type", map[string]string{"Content-Type": ""}),
		)

		It("accepts application/json with a charset", func() {
			r := newRigUnderTest(ownerOwned)
			w := send(newAPI(r.c, allowedOrigins), http.MethodPost, "/clusters/za/freeze", "{}",
				map[string]string{"Content-Type": "application/json; charset=utf-8"})
			Expect(w.Code).To(Equal(http.StatusAccepted), w.Body.String())
		})

		It("echoes an allowed Origin for CORS, and no other", func() {
			h := newAPI(newRigUnderTest("").c, allowedOrigins)
			w := send(h, http.MethodGet, "/state", "", map[string]string{"Origin": "http://127.0.0.1:20301"})
			Expect(w.Header().Get("Access-Control-Allow-Origin")).To(Equal("http://127.0.0.1:20301"))

			w = send(h, http.MethodGet, "/state", "", map[string]string{"Origin": "https://example.com"})
			Expect(w.Header().Get("Access-Control-Allow-Origin")).To(BeEmpty())
		})

		It("never CORS's /readyz, as demo 04", func() {
			h := newAPI(newRigUnderTest("").c, allowedOrigins)
			w := send(h, http.MethodGet, "/readyz", "", map[string]string{"Origin": goodOrigin})
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Header().Get("Access-Control-Allow-Origin")).To(BeEmpty())
		})
	})

	Describe("request bodies: enumerated values only", func() {
		DescribeTable("400s a value outside its set, and sends nothing",
			func(path, body string) {
				r := newRigUnderTest(ownerOwned)
				w := send(newAPI(r.c, allowedOrigins), http.MethodPost, path, body, nil)

				Expect(w.Code).To(Equal(http.StatusBadRequest), w.Body.String())
				Expect(refusalOf(w).Error).To(Equal("BadRequest"))
				Expect(r.host.signals()).To(BeEmpty())
				Expect(r.pendingIDs()).To(BeEmpty())
			},
			Entry("via a server, not a cluster", "/clusters/za/publish", `{"via":"t-za-1"}`),
			Entry("via an empty string", "/clusters/za/publish", `{"via":""}`),
			Entry("timeoutS 3", "/clusters/za/publish", `{"timeoutS":3}`),
			Entry("timeoutS 0", "/clusters/za/publish", `{"timeoutS":0}`),
			Entry("timeoutS as a string", "/clusters/za/publish", `{"timeoutS":"5"}`),
			Entry("timeoutS on freeze", "/clusters/za/freeze", `{"timeoutS":5}`),
			Entry("retryOf on verify", "/clusters/za/verify", `{"retryOf":"pg-s1-za-1"}`),
			Entry("an empty retryOf", "/clusters/za/publish", `{"retryOf":""}`),
			Entry("a field nobody accepts", "/clusters/za/freeze", `{"cmd":"kill -9 1"}`),
			Entry("a pid", "/clusters/za/freeze", `{"pid":50001}`),
			Entry("via on restore", "/restore", `{"via":"za"}`),
			Entry("via on start", "/rig/start", `{"via":"za"}`),
			Entry("not JSON", "/clusters/za/freeze", `via=za`),
			Entry("a JSON array", "/clusters/za/freeze", `["za"]`),
			Entry("JSON null", "/clusters/za/freeze", `null`),
			Entry("an empty body", "/clusters/za/freeze", ``),
		)

		It("404s a retryOf that is not in this session's ledger for that stream", func() {
			r := newRigUnderTest(ownerOwned)
			w := send(newAPI(r.c, allowedOrigins), http.MethodPost, "/clusters/za/publish",
				`{"via":"auto","timeoutS":5,"retryOf":"pg-s1-za-1"}`, nil)

			Expect(w.Code).To(Equal(http.StatusNotFound))
			Expect(refusalOf(w).Error).To(Equal("NoSuchMessage"))
		})

		It("answers 202 with the command ID for a good body", func() {
			r := newRigUnderTest(ownerOwned)
			r.nats.onPublish = blockUntilCancelled
			w := send(newAPI(r.c, allowedOrigins), http.MethodPost, "/clusters/au/publish",
				`{"via":"arb","timeoutS":10}`, nil)

			Expect(w.Code).To(Equal(http.StatusAccepted), w.Body.String())
			var got struct{ ID int }
			Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
			Expect(r.pendingIDs()).To(ConsistOf(got.ID))
		})
	})

	Describe("refusals", func() {
		It("409s a command with no ready rig", func() {
			h := newAPI(newRigUnderTest("").c, allowedOrigins)
			w := send(h, http.MethodPost, "/clusters/za/freeze", "{}", nil)
			Expect(w.Code).To(Equal(http.StatusConflict))
			Expect(refusalOf(w).Error).To(Equal("NoRig"))
		})

		It("409s Stop on an attached rig", func() {
			h := newAPI(newRigUnderTest(ownerAttached).c, allowedOrigins)
			w := send(h, http.MethodPost, "/rig/stop", "{}", nil)
			Expect(w.Code).To(Equal(http.StatusConflict))
			Expect(refusalOf(w).Error).To(Equal("NotOwned"))
		})

		It("409s Start when a rig is ready", func() {
			h := newAPI(newRigUnderTest(ownerAttached).c, allowedOrigins)
			w := send(h, http.MethodPost, "/rig/start", "{}", nil)
			Expect(w.Code).To(Equal(http.StatusConflict))
			Expect(refusalOf(w).Error).To(Equal("RigExists"))
		})
	})

	Describe("GET /state", func() {
		It("returns the rig, the clusters and only the history after ?after=", func() {
			r := newRigUnderTest(ownerOwned)
			h := newAPI(r.c, allowedOrigins)
			var first stateView
			w := send(h, http.MethodGet, "/state", "", nil)
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(json.Unmarshal(w.Body.Bytes(), &first)).To(Succeed())
			Expect(first.Rig.Status).To(Equal(rigReady))
			Expect(first.Rig.Owner).To(Equal(ownerOwned))
			Expect(first.Clusters).To(HaveLen(3))
			Expect(first.Clusters[0].Phase).To(Equal(phaseOff))
			Expect(first.History).NotTo(BeEmpty())

			r.waitEnded(must(r.c.freeze(clusterZA)))
			var next stateView
			w = send(h, http.MethodGet, "/state?after="+itoa(first.HistorySeq), "", nil)
			Expect(json.Unmarshal(w.Body.Bytes(), &next)).To(Succeed())
			for _, e := range next.History {
				Expect(e.Seq).To(BeNumerically(">", first.HistorySeq))
			}
			Expect(next.History).NotTo(BeEmpty())
			Expect(next.Clusters[0].Phase).To(Equal(phaseDark))
		})

		It("400s an ?after= that is not a sequence number", func() {
			h := newAPI(newRigUnderTest("").c, allowedOrigins)
			Expect(send(h, http.MethodGet, "/state?after=x", "", nil).Code).To(Equal(http.StatusBadRequest))
		})
	})
})

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
