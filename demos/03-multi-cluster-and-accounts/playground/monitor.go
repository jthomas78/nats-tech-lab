package main

// Monitoring (D03-R25). Nine servers, each read on its own: /raftz for the
// meta reading every 0.5 s, /jsz?meta=1 for the meta size and /gatewayz for
// the arrows once a second, and the process state from ps once a second.
//
// Every poll writes into the store, and only into the store. Commands never
// wait for a poller and a poller never waits for a command; they share the
// store and nothing else. A failed poll never erases the last answer: the
// answer ages, and the summary decides what an old answer is worth (rule 3).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// fetchFunc reads one monitor URL. A non-nil err means no HTTP answer at all.
type fetchFunc func(ctx context.Context, url string) (status int, body []byte, err error)

func raftzURL(s server) string {
	return fmt.Sprintf("http://127.0.0.1:%d/raftz?group=_meta_", s.Monitor)
}

func jszURL(s server) string {
	return fmt.Sprintf("http://127.0.0.1:%d/jsz?meta=1", s.Monitor)
}

func gatewayzURL(s server) string {
	return fmt.Sprintf("http://127.0.0.1:%d/gatewayz", s.Monitor)
}

// httpFetch is the real fetchFunc: one GET, with monitorFetchFor as the
// whole limit.
func httpFetch(client *http.Client) fetchFunc {
	return func(ctx context.Context, url string) (int, []byte, error) {
		ctx, cancel := context.WithTimeout(ctx, monitorFetchFor)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return 0, nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return 0, nil, fmt.Errorf("no answer in %.1f s", monitorFetchFor.Seconds())
			}
			return 0, nil, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			return 0, nil, err
		}
		return resp.StatusCode, body, nil
	}
}

// parseRaftz is classify-10.py's parse_reading, rule for rule:
//
//	ok           a leader name and a numeric term
//	no_leader    leader null or empty, with a numeric term
//	unreachable  no HTTP answer at all (timeout, refused)
//	invalid      an answer that cannot be trusted: an HTTP error, not JSON,
//	             no meta group, no numeric term, a leader id with no name, or
//	             the server's own name among its peers (the wrong source)
func parseRaftz(srv string, at time.Time, status int, body []byte, fetchErr error) metaReading {
	r := metaReading{Server: srv, At: at, Kind: kindInvalid}
	if fetchErr != nil {
		r.Kind, r.Why = kindUnreachable, fetchErr.Error()
		return r
	}
	if status != http.StatusOK {
		r.Why = fmt.Sprintf("HTTP %d", status)
		return r
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var top map[string]any
	if err := dec.Decode(&top); err != nil {
		r.Why = "malformed: no $SYS._meta_ in the answer"
		return r
	}
	sys, _ := top["$SYS"].(map[string]any)
	raw, ok := sys["_meta_"]
	if !ok {
		r.Why = "malformed: no $SYS._meta_ in the answer"
		return r
	}
	g, ok := raw.(map[string]any)
	if !ok {
		r.Why = "malformed: _meta_ is not an object"
		return r
	}
	num, ok := g["term"].(json.Number)
	term, err := num.Int64()
	if !ok || err != nil {
		r.Why = fmt.Sprintf("malformed: term %v is not a number", g["term"])
		return r
	}
	r.Term = int(term)
	r.State, _ = g["state"].(string)

	names := map[string]string{}
	if peers, ok := g["peers"].(map[string]any); ok {
		for id, p := range peers {
			if pm, ok := p.(map[string]any); ok {
				name, _ := pm["name"].(string)
				names[id] = name
			}
		}
	}
	for _, name := range names {
		if name == srv {
			r.Why = fmt.Sprintf("source mismatch: %s is listed among its own peers", srv)
			return r
		}
	}

	lid, isString := g["leader"].(string)
	switch {
	case g["leader"] == nil || (isString && lid == ""):
		r.Kind = kindNoLeader
	case !isString:
		r.Why = fmt.Sprintf("leader id %v is not a string", g["leader"])
	case lid == g["id"]:
		r.Kind, r.Leader = kindOK, srv
	case names[lid] != "":
		r.Kind, r.Leader = kindOK, names[lid]
	default:
		r.Why = fmt.Sprintf("leader id %q has no name in the peer list", lid)
	}
	return r
}

// parseMetaSize reads cluster_size from /jsz?meta=1.
func parseMetaSize(body []byte) (int, error) {
	var j struct {
		Meta *struct {
			Size *int `json:"cluster_size"`
		} `json:"meta_cluster"`
	}
	if err := json.Unmarshal(body, &j); err != nil {
		return 0, err
	}
	if j.Meta == nil || j.Meta.Size == nil {
		return 0, errors.New("no meta_cluster.cluster_size")
	}
	return *j.Meta.Size, nil
}

// slot is everything the store knows about one server.
type slot struct {
	answer        *metaReading
	attemptFailed bool
	answering     bool // the newest /raftz poll got an HTTP answer
	seen          bool // any /raftz poll has finished

	metaSize   int
	metaSizeAt time.Time

	gateway *gatewayReading

	proc   procState
	procAt time.Time
}

// store holds the newest readings. It is the only thing pollers and
// commands share.
type store struct {
	mu    sync.Mutex
	slots map[string]*slot
}

func newStore() *store {
	st := &store{slots: map[string]*slot{}}
	for _, s := range servers {
		st.slots[s.Name] = &slot{proc: procUnknown}
	}
	return st
}

// putMeta records one /raftz result and returns what changed, as history
// text: a monitor starting or stopping to answer, or a new leader, term or
// role. Nothing is returned for a poll that changed nothing.
func (st *store) putMeta(r metaReading) []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	sl := st.slots[r.Server]
	var changes []string
	firstPoll := !sl.seen
	sl.seen = true

	if r.Kind == kindUnreachable {
		sl.attemptFailed = true
		if sl.answering || firstPoll {
			changes = append(changes, fmt.Sprintf("%s monitor gives no answer: %s", r.Server, r.Why))
		}
		sl.answering = false
		return changes
	}

	prev := sl.answer
	if !sl.answering {
		changes = append(changes, fmt.Sprintf("%s monitor answers", r.Server))
	}
	sl.answering, sl.attemptFailed = true, false
	cp := r
	sl.answer = &cp

	switch {
	case r.Kind == kindInvalid:
		if prev == nil || prev.Kind != kindInvalid || prev.Why != r.Why {
			changes = append(changes, fmt.Sprintf("%s answer is invalid: %s", r.Server, r.Why))
		}
	case prev == nil || prev.Kind != r.Kind || prev.Leader != r.Leader || prev.Term != r.Term || prev.State != r.State:
		changes = append(changes, fmt.Sprintf("%s reads %s", r.Server, describeReading(r)))
	}
	return changes
}

func describeReading(r metaReading) string {
	leader := "no leader"
	if r.Kind == kindOK {
		leader = "leader " + r.Leader
	}
	return fmt.Sprintf("%s, term %d, role %s", leader, r.Term, r.State)
}

func (st *store) putMetaSize(srv string, size int, at time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	sl := st.slots[srv]
	sl.metaSize, sl.metaSizeAt = size, at
}

func (st *store) putGateway(r gatewayReading) {
	st.mu.Lock()
	defer st.mu.Unlock()
	cp := r
	st.slots[r.Server].gateway = &cp
}

// putProc records one ps reading and returns the change, if any.
func (st *store) putProc(srv string, p procState, at time.Time) []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	sl := st.slots[srv]
	old := sl.proc
	sl.proc, sl.procAt = p, at
	if old == p {
		return nil
	}
	return []string{fmt.Sprintf("%s process %s (ps), was %s", srv, p, old)}
}

// forgetProcs sets every process state back to unknown, when the rig is
// gone or a new one is attached.
func (st *store) forgetProcs() {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, sl := range st.slots {
		sl.proc, sl.procAt = procUnknown, time.Time{}
	}
}

func (st *store) procOf(srv string) procState {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.slots[srv].proc
}

// readingsView is a copy of the store, in server order.
type readingsView struct {
	obs      []serverObs
	procs    []procState
	gateways map[string]gatewayReading
	servers  []serverView
}

// serverView is one server as /state shows it: the process state and the
// monitor reading side by side, never one standing in for the other (rule 2).
type serverView struct {
	Server        string        `json:"server"`
	Cluster       string        `json:"cluster"`
	Proc          procState     `json:"process"`
	ProcAge       time.Duration `json:"processAgeNs,omitempty"`
	Reading       *metaReading  `json:"reading,omitempty"`
	ReadingAge    time.Duration `json:"readingAgeNs,omitempty"`
	Fresh         bool          `json:"fresh"`
	AttemptFailed bool          `json:"attemptFailed"`
	MetaSize      int           `json:"metaSize,omitempty"`
	MetaSizeAge   time.Duration `json:"metaSizeAgeNs,omitempty"`
}

func (st *store) view(now time.Time) readingsView {
	st.mu.Lock()
	defer st.mu.Unlock()
	v := readingsView{gateways: map[string]gatewayReading{}}
	for _, s := range servers {
		sl := st.slots[s.Name]
		o := serverObs{Server: s.Name, AttemptFailed: sl.attemptFailed}
		sv := serverView{Server: s.Name, Cluster: s.Cluster, Proc: sl.proc, AttemptFailed: sl.attemptFailed}
		if sl.answer != nil {
			cp := *sl.answer
			o.Answer = &cp
			sv.Reading = &cp
			sv.ReadingAge = now.Sub(cp.At)
			sv.Fresh = sv.ReadingAge < metaFreshFor
		}
		if !sl.procAt.IsZero() {
			sv.ProcAge = now.Sub(sl.procAt)
		}
		if !sl.metaSizeAt.IsZero() {
			sv.MetaSize, sv.MetaSizeAge = sl.metaSize, now.Sub(sl.metaSizeAt)
		}
		if sl.gateway != nil {
			v.gateways[s.Name] = *sl.gateway
		}
		v.obs = append(v.obs, o)
		v.procs = append(v.procs, sl.proc)
		v.servers = append(v.servers, sv)
	}
	return v
}

// monitor runs the pollers. pids names the processes to read with ps: the
// verified rig's, or none.
type monitor struct {
	fetch fetchFunc
	proc  procDeps
	pids  func() map[string]int
	st    *store
	hist  *history
	now   func() time.Time
}

func (m *monitor) observe(changes []string) {
	for _, c := range changes {
		m.hist.add(eventObservation, 0, "", c)
	}
}

// pollMeta reads one server's /raftz once. The reading's time is when the
// answer arrived, as classify-10.py's poll: a frozen server answers only
// after its CONT, and that answer describes the state after the thaw.
func (m *monitor) pollMeta(ctx context.Context, s server) {
	status, body, err := m.fetch(ctx, raftzURL(s))
	if ctx.Err() != nil {
		return
	}
	m.observe(m.st.putMeta(parseRaftz(s.Name, m.now(), status, body, err)))
}

func (m *monitor) pollJsz(ctx context.Context, s server) {
	status, body, err := m.fetch(ctx, jszURL(s))
	if err != nil || status != http.StatusOK {
		return
	}
	if size, err := parseMetaSize(body); err == nil {
		m.st.putMetaSize(s.Name, size, m.now())
	}
}

func (m *monitor) pollGateway(ctx context.Context, s server) {
	status, body, err := m.fetch(ctx, gatewayzURL(s))
	if err != nil || status != http.StatusOK {
		return
	}
	if r, err := parseGatewayz(s.Name, s.Cluster, m.now(), body); err == nil {
		m.st.putGateway(r)
	}
}

// pollProcs reads ps for every verified PID. A server with no verified PID
// is left as it is: unknown, or forgotten by the rig lifecycle.
func (m *monitor) pollProcs(ctx context.Context) {
	for name, pid := range m.pids() {
		p := readProcState(ctx, m.proc, pid)
		m.observe(m.st.putProc(name, p, m.now()))
	}
}

// run starts every poller and returns when ctx ends. Each server's /raftz
// poll is its own goroutine, so one frozen server's 1 s timeout never
// delays another's reading.
func (m *monitor) run(ctx context.Context) {
	var wg sync.WaitGroup
	every := func(d time.Duration, f func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTicker(d)
			defer t.Stop()
			for {
				f()
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
		}()
	}
	for _, s := range servers {
		s := s
		every(metaPollEvery, func() { m.pollMeta(ctx, s) })
		every(jszPollEvery, func() { m.pollJsz(ctx, s) })
		every(gatewayPollEvery, func() { m.pollGateway(ctx, s) })
	}
	every(procPollEvery, func() { m.pollProcs(ctx) })
	wg.Wait()
}
