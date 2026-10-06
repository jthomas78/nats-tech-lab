package main

// The HTTP layer. Transport only, in the shape of demo 04's `cqrs serve`:
// a plain ServeMux, the method checked per handler, JSON refusals.
//
// Local callers only (D03-R27). The listener is 127.0.0.1. Every POST must
// carry Content-Type application/json and an Origin from allowedOrigins,
// else 403, so another web page in the same browser cannot send a signal.
// A body holds enumerated values only, checked against fixed sets, else
// 400. No body carries a command, a path or a PID: fixed verbs on fixed
// names.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// route is one row of the route table in PLAYGROUND-PLAN.md. routes_test.go
// checks the two agree, both ways.
type route struct {
	Method string
	Path   string
}

var clusterVerbs = []string{"freeze", "resume", "leadership", "publish", "verify"}

func routeTable() []route {
	rs := []route{
		{http.MethodGet, "/state"},
		{http.MethodPost, "/rig/start"},
		{http.MethodPost, "/rig/attach"},
		{http.MethodPost, "/rig/stop"},
	}
	for _, v := range clusterVerbs {
		for _, cl := range clusters {
			rs = append(rs, route{http.MethodPost, "/clusters/" + cl + "/" + v})
		}
	}
	return append(rs,
		route{http.MethodPost, "/meta/probe"},
		route{http.MethodPost, "/restore"},
		route{http.MethodGet, "/readyz"},
	)
}

type refusal struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func setCORS(w http.ResponseWriter, r *http.Request, allowed []string) {
	origin := r.Header.Get("Origin")
	for _, a := range allowed {
		if origin == a {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Vary", "Origin")
			return
		}
	}
}

// reqBody is every field any POST may carry. Which ones a route accepts is
// the route's own list.
type reqBody struct {
	Via      *string `json:"via"`
	TimeoutS *int    `json:"timeoutS"`
	RetryOf  *string `json:"retryOf"`
}

const (
	fieldVia      = "via"
	fieldTimeoutS = "timeoutS"
	fieldRetryOf  = "retryOf"
)

// api holds the handler's collaborators.
type api struct {
	c       *controller
	origins []string
	now     func() time.Time
}

func newAPI(c *controller, origins []string) http.Handler {
	a := &api{c: c, origins: origins, now: c.d.now}
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", a.readyz)
	mux.HandleFunc("/state", a.state)
	mux.HandleFunc("/rig/", a.rig)
	mux.HandleFunc("/clusters/", a.clusters)
	mux.HandleFunc("/meta/probe", a.probe)
	mux.HandleFunc("/restore", a.restore)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, refusal{"NotFound", "no route " + r.URL.Path})
	})
	return mux
}

// readyz answers the lab shell's pre-mount check. Like demo 04 it is never
// CORS'd. It says the control service is up; whether a rig is running is a
// state on the page, not a reason to refuse to mount it — the page is where
// a person starts the rig.
func (a *api) readyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, refusal{"MethodNotAllowed", "readiness is read with GET"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ready":  true,
		"checks": []map[string]any{{"name": "control service", "ok": true}},
	})
}

func (a *api) state(w http.ResponseWriter, r *http.Request) {
	setCORS(w, r, a.origins)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, refusal{"MethodNotAllowed", "state is read with GET"})
		return
	}
	after := 0
	if q := r.URL.Query().Get("after"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, refusal{"BadRequest", "after must be a history sequence number"})
			return
		}
		after = n
	}
	writeJSON(w, http.StatusOK, a.c.stateView(a.now(), after))
}

func (a *api) rig(w http.ResponseWriter, r *http.Request) {
	verb := strings.TrimPrefix(r.URL.Path, "/rig/")
	run := map[string]func() (int, error){
		"start":  a.c.startRig,
		"attach": a.c.attachRig,
		"stop":   a.c.stopRig,
	}[verb]
	if run == nil {
		writeJSON(w, http.StatusNotFound, refusal{"NotFound", "no route " + r.URL.Path})
		return
	}
	if _, ok := a.post(w, r); !ok {
		return
	}
	a.accepted(w, run)
}

func (a *api) clusters(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/clusters/"), "/")
	if len(parts) != 2 || !contains(clusters, parts[0]) || !contains(clusterVerbs, parts[1]) {
		writeJSON(w, http.StatusNotFound, refusal{"NotFound", "no route " + r.URL.Path + "; clusters are za, arb, au"})
		return
	}
	cl, verb := parts[0], parts[1]
	fields := []string{fieldVia}
	if verb == "publish" {
		fields = append(fields, fieldTimeoutS, fieldRetryOf)
	}
	b, ok := a.post(w, r, fields...)
	if !ok {
		return
	}
	via := "auto"
	if b.Via != nil {
		via = *b.Via
	}
	switch verb {
	case "freeze":
		a.accepted(w, func() (int, error) { return a.c.freeze(cl) })
	case "resume":
		a.accepted(w, func() (int, error) { return a.c.resume(cl) })
	case "leadership":
		a.accepted(w, func() (int, error) { return a.c.requestLeadership(cl, via) })
	case "verify":
		a.accepted(w, func() (int, error) { return a.c.verify(cl, via) })
	case "publish":
		timeoutS := publishDefaultS
		if b.TimeoutS != nil {
			timeoutS = *b.TimeoutS
		}
		retryOf := ""
		if b.RetryOf != nil {
			retryOf = *b.RetryOf
		}
		a.accepted(w, func() (int, error) { return a.c.publish(cl, via, timeoutS, retryOf) })
	}
}

func (a *api) probe(w http.ResponseWriter, r *http.Request) {
	b, ok := a.post(w, r, fieldVia)
	if !ok {
		return
	}
	via := "auto"
	if b.Via != nil {
		via = *b.Via
	}
	a.accepted(w, func() (int, error) { return a.c.probe(via) })
}

func (a *api) restore(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.post(w, r); !ok {
		return
	}
	ids, err := a.c.restoreAll()
	if err != nil {
		a.refused(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ids": ids})
}

// accepted runs one command call and answers 202 with its ID, or the
// refusal.
func (a *api) accepted(w http.ResponseWriter, run func() (int, error)) {
	id, err := run()
	if err != nil {
		a.refused(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int{"id": id})
}

func (a *api) refused(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, errBusy), errors.Is(err, errNoRig), errors.Is(err, errNotOwned), errors.Is(err, errRigExists):
		status = http.StatusConflict
	case errors.Is(err, errNoEntry):
		status = http.StatusNotFound
	}
	name := "Error"
	var r *refused
	if errors.As(err, &r) {
		name = r.kind.Error()
	}
	writeJSON(w, status, refusal{name, err.Error()})
}

// post runs every check a POST must pass, in order: method, origin,
// content type, then the body. allowed names the fields this route
// accepts; any other field is 400.
func (a *api) post(w http.ResponseWriter, r *http.Request, allowed ...string) (reqBody, bool) {
	var b reqBody
	setCORS(w, r, a.origins)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return b, false
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, refusal{"MethodNotAllowed", "commands are sent with POST"})
		return b, false
	}
	if !contains(a.origins, r.Header.Get("Origin")) {
		writeJSON(w, http.StatusForbidden, refusal{"Forbidden", "a command needs an Origin from the allow-list"})
		return b, false
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeJSON(w, http.StatusForbidden, refusal{"Forbidden", "a command needs Content-Type: application/json"})
		return b, false
	}
	bad := func(msg string) (reqBody, bool) {
		writeJSON(w, http.StatusBadRequest, refusal{"BadRequest", msg})
		return reqBody{}, false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		return bad("could not read the body")
	}
	// The field names first, so an unknown or unaccepted one is named.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return bad("the body must be one JSON object, {} when there is nothing to say")
	}
	for k := range fields {
		if !contains(allowed, k) {
			return bad(fmt.Sprintf("field %q is not accepted here; accepted: %v", k, allowed))
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return bad("a field has the wrong type: " + err.Error())
	}
	if b.Via != nil && !contains(viaChoices, *b.Via) {
		return bad(fmt.Sprintf("via must be one of %v", viaChoices))
	}
	if b.TimeoutS != nil && !containsInt(publishTimeoutsS, *b.TimeoutS) {
		return bad(fmt.Sprintf("timeoutS must be one of %v", publishTimeoutsS))
	}
	if b.RetryOf != nil && *b.RetryOf == "" {
		return bad("retryOf must name a message ID from this session's ledger")
	}
	return b, true
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// --- /state ------------------------------------------------------------------

type rigView struct {
	Status  rigStatus  `json:"status"`
	Owner   string     `json:"owner,omitempty"`
	Error   string     `json:"error,omitempty"`
	Servers []identity `json:"servers"`
}

const (
	phaseGoingDark  = "going_dark"
	phaseDark       = "dark"
	phaseComingBack = "coming_back"
	phaseOff        = "off"
	phaseMixed      = "mixed"
)

// darkView is one cluster's Dark control (D03-R20): the wanted state, the
// state ps confirms, and the time since the change.
type darkView struct {
	Cluster string        `json:"cluster"`
	Wanted  bool          `json:"wanted"`
	Phase   string        `json:"phase"`
	Stopped int           `json:"stopped"`
	Running int           `json:"running"`
	Since   time.Duration `json:"sinceNs,omitempty"`
	Pending int           `json:"pending,omitempty"`
}

type stateView struct {
	At          time.Time                 `json:"at"`
	Session     string                    `json:"session"`
	PollEvery   time.Duration             `json:"pollEveryNs"`
	Rig         rigView                   `json:"rig"`
	Servers     []serverView              `json:"servers"`
	Summary     summary                   `json:"summary"`
	Arrows      []arrow                   `json:"arrows"`
	Clusters    []darkView                `json:"clusters"`
	Transitions transitionLog             `json:"transitions"`
	Pending     []command                 `json:"pending"`
	Leadership  *leadershipRecord         `json:"leadership,omitempty"`
	Ledger      map[string][]*ledgerEntry `json:"ledger"`
	History     []event                   `json:"history"`
	HistorySeq  int                       `json:"historySeq"`
}

// stateView copies everything /state shows, under the lock, so the JSON
// encoder never reads what a command is writing.
func (c *controller) stateView(now time.Time, after int) stateView {
	v := c.st.view(now)
	hist, seq := c.hist.after(after)

	c.mu.Lock()
	defer c.mu.Unlock()
	sv := stateView{
		At: now, Session: c.d.session, PollEvery: metaPollEvery,
		Rig:        rigView{Status: c.status, Owner: c.owner, Error: c.rigErr, Servers: []identity{}},
		Servers:    v.servers,
		Summary:    c.latest,
		Arrows:     gatewayArrows(v.gateways, now),
		Pending:    []command{},
		Ledger:     map[string][]*ledgerEntry{},
		History:    hist,
		HistorySeq: seq,
	}
	for _, s := range servers {
		if id, ok := c.ids[s.Name]; ok {
			sv.Rig.Servers = append(sv.Rig.Servers, id)
		}
	}
	for _, cmd := range c.sortedPendingLocked() {
		sv.Pending = append(sv.Pending, *cmd)
	}
	sv.Transitions.Closed = append([]transition{}, c.transitions.Closed...)
	if c.transitions.Open != nil {
		t := *c.transitions.Open
		sv.Transitions.Open = &t
	}
	if c.leadership != nil {
		l := *c.leadership
		sv.Leadership = &l
	}
	for _, site := range clusters {
		entries := []*ledgerEntry{}
		for _, e := range c.ledger[site] {
			cp := *e
			cp.Attempts = make([]*pubAttempt, len(e.Attempts))
			for i, at := range e.Attempts {
				a := *at
				cp.Attempts[i] = &a
			}
			if e.Storage != nil {
				s := *e.Storage
				cp.Storage = &s
			}
			entries = append(entries, &cp)
		}
		sv.Ledger[site] = entries
	}
	for _, cl := range clusters {
		sv.Clusters = append(sv.Clusters, c.darkViewLocked(cl, now))
	}
	return sv
}

func (c *controller) darkViewLocked(cl string, now time.Time) darkView {
	d := c.dark[cl]
	dv := darkView{Cluster: cl, Wanted: d.Wanted}
	if !d.Since.IsZero() {
		dv.Since = now.Sub(d.Since)
	}
	for _, s := range serversIn(cl) {
		switch c.st.procOf(s.Name) {
		case procStopped:
			dv.Stopped++
		case procRunning:
			dv.Running++
		}
	}
	freeze, resume := c.pendingLocked(cmdFreeze, cl), c.pendingLocked(cmdResume, cl)
	switch {
	case resume != nil:
		dv.Phase, dv.Pending = phaseComingBack, resume.ID
	case freeze != nil:
		dv.Phase, dv.Pending = phaseGoingDark, freeze.ID
	case d.Wanted && dv.Stopped == 3:
		dv.Phase = phaseDark
	case !d.Wanted && dv.Running == 3:
		dv.Phase = phaseOff
	default:
		dv.Phase = phaseMixed
	}
	return dv
}
