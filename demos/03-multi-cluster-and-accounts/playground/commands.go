package main

// The command runner (D03-R24, rule 9). A command gets an ID, enters the
// pending set, and the caller gets the ID at once. The result arrives in
// /state and in the history.
//
// Three lanes:
//
//	signal     freeze, resume, restore all. The signals are sent before the
//	           HTTP answer; only the ps confirmation stays pending. Never
//	           queued behind another lane.
//	nats       publish, verify, probe, request leadership. A goroutine each,
//	           at most one per kind per cluster; leadership one for the rig.
//	lifecycle  start, attach, stop. Exclusive: while one runs, only restore
//	           all and GET /state are accepted.
//
// Every command ends exactly once: result, error, timeout, or cancelled
// with its reason. The first ending wins and later ones are dropped. A
// watchdog closes a command that outlives its limit by watchdogGrace, so a
// body that never returns still ends.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"syscall"
	"time"
)

const (
	laneSignal    = "signal"
	laneNATS      = "nats"
	laneLifecycle = "lifecycle"

	endResult    = "result"
	endError     = "error"
	endTimeout   = "timeout"
	endCancelled = "cancelled"

	cmdFreeze     = "freeze"
	cmdResume     = "resume"
	cmdPublish    = "publish"
	cmdVerify     = "verify"
	cmdProbe      = "probe"
	cmdLeadership = "leadership"
	cmdStart      = "start"
	cmdAttach     = "attach"
	cmdStop       = "stop"
)

type rigStatus string

const (
	rigAbsent   rigStatus = "absent"
	rigStarting rigStatus = "starting"
	rigReady    rigStatus = "ready"
	rigStopping rigStatus = "stopping"
	rigPartial  rigStatus = "partial"
	rigRefused  rigStatus = "refused"

	ownerOwned    = "owned"
	ownerAttached = "attached"
)

// rigLifecycle is rig.go (step 5): start runs rig-t4.sh up, attach checks
// a running rig, stop sends CONT then TERM to the nine verified PIDs.
type rigLifecycle interface {
	start(ctx context.Context) ([]identity, error)
	attach(ctx context.Context) ([]identity, error)
	stop(ctx context.Context, ids []identity) error
}

// natsOps is natsops.go (step 5). Each call connects through `via`.
type natsOps interface {
	publish(ctx context.Context, via server, site, msgID string, body []byte, ackWait time.Duration) (pubAck, error)
	readStream(ctx context.Context, via server, site string, afterSeq uint64) (streamRead, error)
	probe(ctx context.Context, via server, n int) []probeStep
	stepDown(ctx context.Context, via server, cluster string) error
}

type pubAck struct {
	Seq       uint64
	Duplicate bool
}

type storedMsg struct {
	Seq   uint64
	MsgID string
}

type streamRead struct {
	LastSeq uint64
	Msgs    []storedMsg
}

type probeStep struct {
	Step string `json:"step"` // create, delete, gone
	OK   bool   `json:"ok"`
	Code int    `json:"code,omitempty"`
	Err  string `json:"error,omitempty"`
}

// errNoReply is how natsops says "no reply in time". It and a context
// deadline both mean the outcome is unknown (rule 6).
var errNoReply = errors.New("no reply")

// refusals the HTTP layer turns into status codes.
var (
	errBusy      = errors.New("Busy")
	errNoRig     = errors.New("NoRig")
	errNotOwned  = errors.New("NotOwned")
	errRigExists = errors.New("RigExists")
	errNoEntry   = errors.New("NoSuchMessage")
)

// refused is a refusal with its message.
type refused struct {
	kind error
	msg  string
}

func (r *refused) Error() string { return r.msg }
func (r *refused) Unwrap() error { return r.kind }

func refuse(kind error, format string, a ...any) error {
	return &refused{kind: kind, msg: fmt.Sprintf(format, a...)}
}

// timings are the limits, from names.go. Specs pass shorter ones.
type timings struct {
	grace         time.Duration
	confirmFor    time.Duration
	confirmEvery  time.Duration
	verifyLimit   time.Duration
	probeLimit    time.Duration
	leaderReply   time.Duration
	leaderObserve time.Duration
	startLimit    time.Duration
	attachLimit   time.Duration
	stopLimit     time.Duration
	tick          time.Duration
}

func defaultTimings() timings {
	return timings{
		grace:         watchdogGrace,
		confirmFor:    signalConfirmFor,
		confirmEvery:  signalConfirmEvery,
		verifyLimit:   verifyLimit,
		probeLimit:    probeLimit,
		leaderReply:   leaderReplyFor,
		leaderObserve: leaderObserveFor,
		startLimit:    startLimit,
		attachLimit:   attachLimit,
		stopLimit:     stopLimit,
		tick:          metaPollEvery,
	}
}

// command is one entry in the pending set, and later in the history.
type command struct {
	ID       int           `json:"id"`
	Kind     string        `json:"kind"`
	Cluster  string        `json:"cluster,omitempty"`
	Lane     string        `json:"lane"`
	Via      string        `json:"via,omitempty"`
	At       time.Time     `json:"at"`
	Limit    time.Duration `json:"limitNs"`
	Progress string        `json:"progress,omitempty"`
	End      string        `json:"end,omitempty"`
	Text     string        `json:"text,omitempty"`
	EndedAt  time.Time     `json:"endedAt,omitempty"`

	ctx      context.Context
	cancel   context.CancelFunc
	watchdog *time.Timer
	// onAbort runs, under the lock, when a cancellation or the watchdog
	// ends the command before its body does.
	onAbort func(end, text string)
}

// ending is what a body returns. apply runs under the lock, and only if
// this ending is the one that wins.
type ending struct {
	end   string
	text  string
	apply func()
}

// darkState is one cluster's wanted state and when it last changed.
type darkState struct {
	Wanted bool
	Since  time.Time
}

// ledgerEntry is one message ID this session published (D03-R22).
type ledgerEntry struct {
	ID       string        `json:"id"`
	Site     string        `json:"site"`
	Stream   string        `json:"stream"`
	Body     string        `json:"body"`
	Attempts []*pubAttempt `json:"attempts"`
	Storage  *storageCheck `json:"storage,omitempty"`
}

const (
	outcomePending      = "pending"
	outcomeAcked        = "acked"
	outcomeDuplicate    = "acked_duplicate"
	outcomeRefusedNot   = "refused_not_sent"
	outcomeTimedOut     = "timed_out_unknown"
	outcomeCancelledUnk = "cancelled_unknown"
	outcomeError        = "error"
)

type pubAttempt struct {
	Cmd      int       `json:"cmd"`
	At       time.Time `json:"at"`
	Via      string    `json:"via,omitempty"`
	TimeoutS int       `json:"timeoutS"`
	Outcome  string    `json:"outcome"`
	Seq      uint64    `json:"seq,omitempty"`
	Text     string    `json:"text"`
}

type storageCheck struct {
	Present bool      `json:"present"`
	Seq     uint64    `json:"seq,omitempty"`
	AsOf    time.Time `json:"asOf"`
}

// leadershipRecord is the last "Request leadership here" (D03-R21). The
// next request replaces it.
type leadershipRecord struct {
	Cmd      int            `json:"cmd"`
	Cluster  string         `json:"cluster"`
	Via      string         `json:"via,omitempty"`
	At       time.Time      `json:"at"`
	Before   *lastAgreement `json:"before,omitempty"`
	Reply    string         `json:"reply"`
	Observed *lastAgreement `json:"observed,omitempty"`
	// Took is from the reply to the summary that agreed on a higher term.
	Took time.Duration `json:"tookNs,omitempty"`
	Text string        `json:"text"`
}

type controllerDeps struct {
	lab     string
	proc    procDeps
	rig     rigLifecycle
	nats    natsOps
	now     func() time.Time
	t       timings
	session string
	// onReady, when set, runs (on its own goroutine) each time a rig
	// becomes ready. main.go opens the session file with it.
	onReady func(owner string)
}

type controller struct {
	d    controllerDeps
	base context.Context
	st   *store
	hist *history

	mu      sync.Mutex
	nextCmd int
	pending map[int]*command

	status    rigStatus
	owner     string
	rigErr    string
	ids       map[string]identity
	lifecycle *command

	dark        map[string]*darkState
	stoppedByUs map[string]bool
	// sigMu is held while a cluster's signals are sent, so a Resume's
	// SIGCONT always comes after the SIGSTOPs of the Freeze it cancels.
	sigMu map[string]*sync.Mutex

	last        *lastAgreement
	latest      summary
	transitions transitionLog
	leadership  *leadershipRecord

	ledger     map[string][]*ledgerEntry
	pubN       map[string]int
	verifiedTo map[string]uint64
	probeN     int
}

func newController(base context.Context, d controllerDeps, st *store, hist *history) *controller {
	c := &controller{
		d: d, base: base, st: st, hist: hist,
		pending:     map[int]*command{},
		status:      rigAbsent,
		ids:         map[string]identity{},
		dark:        map[string]*darkState{},
		stoppedByUs: map[string]bool{},
		ledger:      map[string][]*ledgerEntry{},
		pubN:        map[string]int{},
		verifiedTo:  map[string]uint64{},
		sigMu:       map[string]*sync.Mutex{},
	}
	for _, cl := range clusters {
		c.dark[cl] = &darkState{}
		c.sigMu[cl] = &sync.Mutex{}
	}
	return c
}

// verifiedPIDs is the monitor's pids func: the rig's verified processes.
func (c *controller) verifiedPIDs() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]int{}
	for name, id := range c.ids {
		if id.Verified {
			out[name] = id.PID
		}
	}
	return out
}

// --- the runner --------------------------------------------------------------

// beginLocked puts a new command in the pending set and arms its watchdog.
func (c *controller) beginLocked(kind, cluster, lane string, limit time.Duration) *command {
	c.nextCmd++
	cmd := &command{ID: c.nextCmd, Kind: kind, Cluster: cluster, Lane: lane, At: c.d.now(), Limit: limit}
	cmd.ctx, cmd.cancel = context.WithTimeout(c.base, limit)
	c.pending[cmd.ID] = cmd
	cmd.watchdog = time.AfterFunc(limit+c.d.t.grace, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.abortLocked(cmd, endTimeout, fmt.Sprintf("no completion within %s", secs(limit)))
	})
	what := kind
	if cluster != "" {
		what += " " + cluster
	}
	c.hist.add(eventAction, cmd.ID, "", what)
	return cmd
}

// finishLocked ends a command if nothing has ended it yet. It reports
// whether this ending won.
func (c *controller) finishLocked(cmd *command, end, text string) bool {
	if cmd.End != "" {
		return false
	}
	cmd.End, cmd.Text, cmd.EndedAt = end, text, c.d.now()
	cmd.Progress = ""
	delete(c.pending, cmd.ID)
	cmd.watchdog.Stop()
	cmd.cancel()
	if c.lifecycle == cmd {
		c.lifecycle = nil
	}
	c.hist.add(eventResult, cmd.ID, end, text)
	return true
}

// abortLocked ends a command from outside its body: a cancellation or the
// watchdog. The body's own result, if it ever comes, is dropped.
func (c *controller) abortLocked(cmd *command, end, text string) {
	if !c.finishLocked(cmd, end, text) {
		return
	}
	if cmd.onAbort != nil {
		cmd.onAbort(end, text)
	}
}

func (c *controller) cancelLocked(cmd *command, reason string) {
	c.abortLocked(cmd, endCancelled, reason)
}

// run runs body in its own goroutine and ends the command with its result.
func (c *controller) run(cmd *command, body func(ctx context.Context) ending) {
	go func() {
		e := body(cmd.ctx)
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.finishLocked(cmd, e.end, e.text) && e.apply != nil {
			e.apply()
		}
	}()
}

// pendingLocked finds a pending command by kind and cluster.
func (c *controller) pendingLocked(kind, cluster string) *command {
	for _, cmd := range c.pending {
		if cmd.Kind == kind && cmd.Cluster == cluster {
			return cmd
		}
	}
	return nil
}

// cancelAll ends every pending command with one reason: the rig is
// stopping, or the service is shutting down.
func (c *controller) cancelAll(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancelAllLocked(reason, nil)
}

func (c *controller) cancelAllLocked(reason string, except *command) {
	for _, cmd := range c.sortedPendingLocked() {
		if cmd != except {
			c.cancelLocked(cmd, reason)
		}
	}
}

func (c *controller) sortedPendingLocked() []*command {
	out := make([]*command, 0, len(c.pending))
	for _, cmd := range c.pending {
		out = append(out, cmd)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// gateLocked is the lane rule every command but restore passes first.
func (c *controller) gateLocked(needRig bool) error {
	if c.lifecycle != nil {
		return refuse(errBusy, "the rig is %s; only Restore all is accepted until it ends", c.lifecycle.Kind+"ing")
	}
	if needRig && c.status != rigReady {
		return refuse(errNoRig, "no ready rig: start one or attach to one first")
	}
	return nil
}

// --- the signal lane ---------------------------------------------------------

// freeze sends SIGSTOP to a cluster's three verified processes, then
// confirms them stopped with ps (D03-R20).
func (c *controller) freeze(cluster string) (int, error) {
	c.mu.Lock()
	if err := c.gateLocked(true); err != nil {
		c.mu.Unlock()
		return 0, err
	}
	if c.pendingLocked(cmdResume, cluster) != nil {
		c.mu.Unlock()
		return 0, refuse(errBusy, "%s is coming back; Resume always wins", cluster)
	}
	if c.pendingLocked(cmdFreeze, cluster) != nil {
		c.mu.Unlock()
		return 0, refuse(errBusy, "%s is already going dark", cluster)
	}
	cmd := c.beginLocked(cmdFreeze, cluster, laneSignal, c.d.t.confirmFor)
	c.dark[cluster] = &darkState{Wanted: true, Since: cmd.At}
	ids := c.idsLocked()
	c.mu.Unlock()

	c.sigMu[cluster].Lock()
	sent, errs := c.signalCluster(cmd.ctx, cluster, ids, syscall.SIGSTOP)
	c.sigMu[cluster].Unlock()
	c.mu.Lock()
	for _, s := range sent {
		c.stoppedByUs[s] = true
	}
	c.mu.Unlock()
	c.confirm(cmd, cluster, ids, sent, errs, procStopped)
	return cmd.ID, nil
}

// resume cancels a pending freeze of the cluster, then sends SIGCONT to
// all three and confirms them running. Resume always wins.
func (c *controller) resume(cluster string) (int, error) {
	c.mu.Lock()
	if err := c.gateLocked(true); err != nil {
		c.mu.Unlock()
		return 0, err
	}
	id, err := c.resumeLocked(cluster)
	c.mu.Unlock()
	return id, err
}

// resumeLocked is resume's locked half. It drops the lock while it signals
// and takes it again before it returns.
func (c *controller) resumeLocked(cluster string) (int, error) {
	if c.pendingLocked(cmdResume, cluster) != nil {
		return 0, refuse(errBusy, "%s is already coming back", cluster)
	}
	now := c.d.now()
	if f := c.pendingLocked(cmdFreeze, cluster); f != nil {
		c.cancelLocked(f, fmt.Sprintf("superseded by Resume after %.1f s", now.Sub(f.At).Seconds()))
	}
	cmd := c.beginLocked(cmdResume, cluster, laneSignal, c.d.t.confirmFor)
	c.dark[cluster] = &darkState{Wanted: false, Since: cmd.At}
	ids := c.idsLocked()
	c.mu.Unlock()

	c.sigMu[cluster].Lock()
	sent, errs := c.signalCluster(cmd.ctx, cluster, ids, syscall.SIGCONT)
	c.sigMu[cluster].Unlock()

	c.mu.Lock()
	for _, s := range sent {
		delete(c.stoppedByUs, s)
	}
	c.mu.Unlock()
	c.confirm(cmd, cluster, ids, sent, errs, procRunning)
	c.mu.Lock()
	return cmd.ID, nil
}

// restoreAll is a Resume for every cluster that is wanted dark, is going
// dark, or has a stopped process. It is accepted during a lifecycle
// command (D03-R24); it needs only verified identities.
func (c *controller) restoreAll() ([]int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.verifiedLocked()) == 0 {
		return nil, refuse(errNoRig, "no verified rig processes to resume")
	}
	c.hist.add(eventAction, 0, "", "restore all")
	ids := []int{}
	for _, cl := range clusters {
		need := c.dark[cl].Wanted || c.pendingLocked(cmdFreeze, cl) != nil
		for _, s := range serversIn(cl) {
			if c.st.procOf(s.Name) == procStopped {
				need = true
			}
		}
		if !need || c.pendingLocked(cmdResume, cl) != nil {
			continue
		}
		id, err := c.resumeLocked(cl)
		if err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (c *controller) idsLocked() map[string]identity {
	out := make(map[string]identity, len(c.ids))
	for k, v := range c.ids {
		out[k] = v
	}
	return out
}

func (c *controller) verifiedLocked() []string {
	var out []string
	for name, id := range c.ids {
		if id.Verified {
			out = append(out, name)
		}
	}
	return out
}

// signalCluster sends sig to each of the cluster's verified processes,
// re-verifying each first (rule 7). It returns who got it and why the
// others did not. A cancelled command sends nothing more.
func (c *controller) signalCluster(ctx context.Context, cluster string, ids map[string]identity, sig syscall.Signal) ([]string, []string) {
	var sent, errs []string
	for _, s := range serversIn(cluster) {
		if ctx.Err() != nil {
			errs = append(errs, s.Name+": not sent, the command ended")
			continue
		}
		if err := signalVerified(ctx, c.d.proc, c.d.lab, s, ids[s.Name], sig); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		sent = append(sent, s.Name)
	}
	return sent, errs
}

// confirm reads ps every confirmEvery until every signalled process shows
// `want`, then ends the command and opens a transition (D03-R29). It reads
// the PIDs it signalled, from ids, never the rig's current set: a Stop may
// clear that while a cancelled confirm finishes its last pass.
func (c *controller) confirm(cmd *command, cluster string, ids map[string]identity, sent, errs []string, want procState) {
	verb := map[procState]string{procStopped: "stopped", procRunning: "running"}[want]
	if len(sent) == 0 {
		c.mu.Lock()
		c.finishLocked(cmd, endError, fmt.Sprintf("no process signalled: %s", joinErrs(errs)))
		c.mu.Unlock()
		return
	}
	c.run(cmd, func(ctx context.Context) ending {
		tick := time.NewTicker(c.d.t.confirmEvery)
		defer tick.Stop()
		for {
			n := 0
			for _, name := range sent {
				if ctx.Err() != nil {
					break
				}
				s, _ := serverNamed(name)
				p := readProcState(ctx, c.d.proc, ids[name].PID)
				for _, ch := range c.st.putProc(s.Name, p, c.d.now()) {
					c.hist.add(eventObservation, 0, "", ch)
				}
				if p == want {
					n++
				}
			}
			c.setProgress(cmd, fmt.Sprintf("%d of 3 confirmed %s", n, verb))
			if n == len(sent) {
				at := c.d.now()
				took := at.Sub(cmd.At)
				text := fmt.Sprintf("%d of 3 confirmed %s after %.1f s", n, verb, took.Seconds())
				end := endResult
				if len(errs) > 0 {
					end = endError
					text += "; not signalled: " + joinErrs(errs)
				}
				return ending{end: end, text: text, apply: func() {
					kind := changeFreeze
					if want == procRunning {
						kind = changeResume
					}
					c.transitions.start(change{Kind: kind, Cluster: cluster, At: at}, c.last)
				}}
			}
			select {
			case <-ctx.Done():
				return ending{end: endTimeout, text: fmt.Sprintf("%d of 3 confirmed %s in %s", n, verb, secs(cmd.Limit))}
			case <-tick.C:
			}
		}
	})
}

func (c *controller) setProgress(cmd *command, p string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cmd.End == "" {
		cmd.Progress = p
	}
}

// --- the nats lane -----------------------------------------------------------

// pickVia chooses the server a NATS command connects through (D03-R30):
// a running, verified server, from the clusters in `order`. The process
// state is the OS's, from the store; a connection to a stopped server hangs.
func (c *controller) pickViaLocked(order []string) (server, bool) {
	for _, cl := range order {
		for _, s := range serversIn(cl) {
			if c.ids[s.Name].Verified && c.st.procOf(s.Name) == procRunning {
				return s, true
			}
		}
	}
	return server{}, false
}

// viaOrder turns a via choice into clusters to try. Auto is the
// destination's own cluster, then arb, then the other region.
func viaOrder(via, dest string) []string {
	if via != "auto" {
		return []string{via}
	}
	order := []string{dest}
	for _, cl := range []string{clusterArb, clusterZA, clusterAU} {
		if cl != dest {
			order = append(order, cl)
		}
	}
	return order
}

// publish sends one message to the site's stream (D03-R22). retryOf names
// a ledger entry to send again with its own ID and body; "" makes a new one.
func (c *controller) publish(site, via string, timeoutS int, retryOf string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.gateLocked(true); err != nil {
		return 0, err
	}
	var entry *ledgerEntry
	if retryOf != "" {
		for _, e := range c.ledger[site] {
			if e.ID == retryOf {
				entry = e
			}
		}
		if entry == nil {
			return 0, refuse(errNoEntry, "%s is not in this session's ledger for %s", retryOf, streamOf(site))
		}
	}
	if c.pendingLocked(cmdPublish, site) != nil {
		return 0, refuse(errBusy, "a publish to %s is pending", streamOf(site))
	}
	if entry == nil {
		c.pubN[site]++
		n := c.pubN[site]
		id := fmt.Sprintf("pg-%s-%s-%d", c.d.session, site, n)
		entry = &ledgerEntry{
			ID: id, Site: site, Stream: streamOf(site),
			Body: fmt.Sprintf(`{"id":"%s","vehicle":"v-1","km":%d}`, id, 2000+n),
		}
		c.ledger[site] = append(c.ledger[site], entry)
	}

	limit := time.Duration(timeoutS) * time.Second
	cmd := c.beginLocked(cmdPublish, site, laneNATS, limit)
	att := &pubAttempt{Cmd: cmd.ID, At: cmd.At, TimeoutS: timeoutS, Outcome: outcomePending, Text: "pending"}
	entry.Attempts = append(entry.Attempts, att)

	s, ok := c.pickViaLocked(viaOrder(via, site))
	if !ok {
		att.Outcome = outcomeRefusedNot
		att.Text = fmt.Sprintf("refused, not sent: no running server in %s", viaLabel(via, site))
		c.finishLocked(cmd, endError, att.Text)
		return cmd.ID, nil
	}
	cmd.Via, att.Via = s.Name, s.Name
	cmd.onAbort = func(end, text string) {
		att.Outcome = outcomeCancelledUnk
		att.Text = "cancelled (" + text + ") — outcome unknown"
		if end == endTimeout {
			att.Outcome = outcomeTimedOut
			att.Text = "timed out — outcome unknown (" + text + ")"
		}
	}
	id, body := entry.ID, []byte(entry.Body)
	c.run(cmd, func(ctx context.Context) ending {
		ack, err := c.d.nats.publish(ctx, s, site, id, body, limit)
		switch {
		case err == nil && ack.Duplicate:
			text := fmt.Sprintf("%s acked as duplicate (seq %d) via %s: JetStream already held this ID", id, ack.Seq, s.Name)
			return ending{endResult, text, func() { att.Outcome, att.Seq, att.Text = outcomeDuplicate, ack.Seq, text }}
		case err == nil:
			text := fmt.Sprintf("%s acked at seq %d via %s", id, ack.Seq, s.Name)
			return ending{endResult, text, func() { att.Outcome, att.Seq, att.Text = outcomeAcked, ack.Seq, text }}
		case errors.Is(err, errNoReply) || errors.Is(err, context.DeadlineExceeded):
			text := fmt.Sprintf("%s timed out after %d s via %s — outcome unknown", id, timeoutS, s.Name)
			return ending{endTimeout, text, func() { att.Outcome, att.Text = outcomeTimedOut, text }}
		default:
			text := fmt.Sprintf("%s failed via %s: %v", id, s.Name, err)
			return ending{endError, text, func() { att.Outcome, att.Text = outcomeError, text }}
		}
	})
	return cmd.ID, nil
}

func viaLabel(via, dest string) string {
	if via == "auto" {
		return "any cluster"
	}
	return via
}

// verify reads the site's stream above the last sequence verified and
// marks each unverified ledger entry present at seq N or absent, as of now.
// An absent entry can turn present later.
func (c *controller) verify(site, via string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.gateLocked(true); err != nil {
		return 0, err
	}
	if c.pendingLocked(cmdVerify, site) != nil {
		return 0, refuse(errBusy, "a verify of %s is pending", streamOf(site))
	}
	cmd := c.beginLocked(cmdVerify, site, laneNATS, c.d.t.verifyLimit)
	s, ok := c.pickViaLocked(viaOrder(via, site))
	if !ok {
		c.finishLocked(cmd, endError, fmt.Sprintf("refused, not sent: no running server in %s", viaLabel(via, site)))
		return cmd.ID, nil
	}
	cmd.Via = s.Name
	from := c.verifiedTo[site]
	c.run(cmd, func(ctx context.Context) ending {
		got, err := c.d.nats.readStream(ctx, s, site, from)
		if err != nil {
			end := endError
			if errors.Is(err, errNoReply) || errors.Is(err, context.DeadlineExceeded) {
				end = endTimeout
			}
			return ending{end: end, text: fmt.Sprintf("verify %s via %s: %v", streamOf(site), s.Name, err)}
		}
		asOf := c.d.now()
		seqOf := map[string]uint64{}
		for _, m := range got.Msgs {
			seqOf[m.MsgID] = m.Seq
		}
		var apply []func()
		present, absent := 0, 0
		for _, e := range c.ledgerOf(site) {
			if e.Storage != nil && e.Storage.Present {
				continue
			}
			e := e
			if seq, ok := seqOf[e.ID]; ok {
				present++
				apply = append(apply, func() { e.Storage = &storageCheck{Present: true, Seq: seq, AsOf: asOf} })
			} else {
				absent++
				apply = append(apply, func() { e.Storage = &storageCheck{AsOf: asOf} })
			}
		}
		text := fmt.Sprintf("%s via %s: %d present, %d absent, as of %s (last seq %d)",
			streamOf(site), s.Name, present, absent, asOf.Format("15:04:05"), got.LastSeq)
		return ending{endResult, text, func() {
			for _, f := range apply {
				f()
			}
			if got.LastSeq > c.verifiedTo[site] {
				c.verifiedTo[site] = got.LastSeq
			}
		}}
	})
	return cmd.ID, nil
}

func (c *controller) ledgerOf(site string) []*ledgerEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*ledgerEntry(nil), c.ledger[site]...)
}

// probe is the exercise 10 metadata probe: create PG_PROBE_<n>, delete it,
// check it is gone (D03-R23). Auto connects through arb first.
func (c *controller) probe(via string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.gateLocked(true); err != nil {
		return 0, err
	}
	if c.pendingLocked(cmdProbe, "") != nil {
		return 0, refuse(errBusy, "a metadata probe is pending")
	}
	cmd := c.beginLocked(cmdProbe, "", laneNATS, c.d.t.probeLimit)
	s, ok := c.pickViaLocked(viaOrder(via, clusterArb))
	if !ok {
		c.finishLocked(cmd, endError, fmt.Sprintf("refused, not sent: no running server in %s", viaLabel(via, clusterArb)))
		return cmd.ID, nil
	}
	cmd.Via = s.Name
	c.probeN++
	n := c.probeN
	c.run(cmd, func(ctx context.Context) ending {
		steps := c.d.nats.probe(ctx, s, n)
		end, parts := endResult, []string{}
		for _, st := range steps {
			if st.OK {
				parts = append(parts, st.Step+" ok")
				continue
			}
			end = endError
			p := st.Step + " failed"
			if st.Code != 0 {
				p += fmt.Sprintf(" %d", st.Code)
			}
			parts = append(parts, p+": "+st.Err)
		}
		if len(steps) == 0 {
			end, parts = endError, []string{"no step ran"}
		}
		return ending{end: end, text: fmt.Sprintf("PG_PROBE_%d via %s: %s", n, s.Name, joinErrs(parts))}
	})
	return cmd.ID, nil
}

// requestLeadership sends the meta step-down with a placement cluster,
// then watches the summary for an agreement with a higher term than before
// (D03-R21). It never says the leader moved: it says what it then observed.
func (c *controller) requestLeadership(cluster, via string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.gateLocked(true); err != nil {
		return 0, err
	}
	for _, cmd := range c.pending {
		if cmd.Kind == cmdLeadership {
			return 0, refuse(errBusy, "a leadership request for %s is pending; one at a time for the rig", cmd.Cluster)
		}
	}
	cmd := c.beginLocked(cmdLeadership, cluster, laneNATS, c.d.t.leaderReply+c.d.t.leaderObserve)
	rec := &leadershipRecord{Cmd: cmd.ID, Cluster: cluster, At: cmd.At}
	if c.last != nil {
		b := *c.last
		rec.Before = &b
	}
	c.leadership = rec
	s, ok := c.pickViaLocked(viaOrder(via, cluster))
	if !ok {
		rec.Reply = "not sent"
		rec.Text = fmt.Sprintf("refused, not sent: no running server in %s", viaLabel(via, cluster))
		c.finishLocked(cmd, endError, rec.Text)
		return cmd.ID, nil
	}
	cmd.Via, rec.Via = s.Name, s.Name
	cmd.onAbort = func(end, text string) {
		rec.Text = text
	}
	c.run(cmd, func(ctx context.Context) ending {
		rctx, cancel := context.WithTimeout(ctx, c.d.t.leaderReply)
		err := c.d.nats.stepDown(rctx, s, cluster)
		cancel()
		if err != nil {
			reply, end := "error: "+err.Error(), endError
			if errors.Is(err, errNoReply) || errors.Is(err, context.DeadlineExceeded) {
				reply, end = fmt.Sprintf("no reply in %s", secs(c.d.t.leaderReply)), endTimeout
			}
			text := fmt.Sprintf("Requested %s. %s.", cluster, reply)
			return ending{end, text, func() { rec.Reply, rec.Text = reply, text }}
		}
		replyAt := c.d.now()
		c.mu.Lock()
		rec.Reply = "accepted"
		c.transitions.start(change{Kind: changeLeadership, Cluster: cluster, At: replyAt}, c.last)
		c.mu.Unlock()

		beforeTerm := -1
		if rec.Before != nil {
			beforeTerm = rec.Before.Term
		}
		tick := time.NewTicker(c.d.t.tick / 5)
		defer tick.Stop()
		obs := time.NewTimer(c.d.t.leaderObserve)
		defer obs.Stop()
		for {
			c.mu.Lock()
			sum := c.latest
			c.mu.Unlock()
			if sum.State == stateAgreed && sum.Term > beforeTerm && !sum.At.Before(replyAt) {
				got := &lastAgreement{Leader: sum.Leader, Term: sum.Term, At: sum.At}
				text := fmt.Sprintf("Requested %s. Observed %s in %s, term %d.", cluster, sum.Leader, clusterOf(sum.Leader), sum.Term)
				took := sum.At.Sub(replyAt)
				return ending{endResult, text, func() { rec.Observed, rec.Took, rec.Text = got, took, text }}
			}
			select {
			case <-ctx.Done():
				return ending{end: endTimeout, text: fmt.Sprintf("Requested %s. Accepted; %v.", cluster, ctx.Err())}
			case <-obs.C:
				text := fmt.Sprintf("Requested %s. Accepted; no new agreed leader within %s.", cluster, secs(c.d.t.leaderObserve))
				return ending{endResult, text, func() { rec.Text = text }}
			case <-tick.C:
			}
		}
	})
	return cmd.ID, nil
}

// --- the lifecycle lane ------------------------------------------------------

func (c *controller) startRig() (int, error) {
	return c.lifecycleCmd(cmdStart, c.d.t.startLimit, ownerOwned, c.d.rig.start)
}

func (c *controller) attachRig() (int, error) {
	return c.lifecycleCmd(cmdAttach, c.d.t.attachLimit, ownerAttached, c.d.rig.attach)
}

func (c *controller) lifecycleCmd(kind string, limit time.Duration, owner string, f func(context.Context) ([]identity, error)) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.gateLocked(false); err != nil {
		return 0, err
	}
	if c.status == rigReady {
		return 0, refuse(errRigExists, "a rig is already %s (%s)", c.status, c.owner)
	}
	cmd := c.beginLocked(kind, "", laneLifecycle, limit)
	c.lifecycle = cmd
	if kind == cmdStart {
		c.status = rigStarting
	}
	c.rigErr = ""
	c.run(cmd, func(ctx context.Context) ending {
		ids, err := f(ctx)
		if err != nil {
			return ending{endError, err.Error(), func() {
				c.status, c.rigErr = rigAbsent, err.Error()
				if kind == cmdAttach {
					c.status = rigRefused
				}
			}}
		}
		byName := map[string]identity{}
		bad := []string{}
		for _, id := range ids {
			byName[id.Server] = id
			if !id.Verified {
				bad = append(bad, id.Server+": "+id.Reason)
			}
		}
		for _, s := range servers {
			if _, ok := byName[s.Name]; !ok {
				bad = append(bad, s.Name+": not checked")
			}
		}
		if len(bad) > 0 {
			text := "identity check failed: " + joinErrs(bad)
			return ending{endError, text, func() {
				c.status, c.rigErr, c.ids = rigRefused, text, byName
			}}
		}
		// Read every process state now, so the first command after this
		// one can pick a running server.
		for _, s := range servers {
			p := readProcState(ctx, c.d.proc, byName[s.Name].PID)
			c.st.putProc(s.Name, p, c.d.now())
		}
		text := fmt.Sprintf("rig %s: nine processes verified", map[string]string{ownerOwned: "started", ownerAttached: "attached"}[owner])
		return ending{endResult, text, func() {
			c.status, c.owner, c.ids = rigReady, owner, byName
			c.stoppedByUs = map[string]bool{}
			for _, cl := range clusters {
				c.dark[cl] = &darkState{}
			}
			if c.d.onReady != nil {
				go c.d.onReady(owner)
			}
		}}
	})
	return cmd.ID, nil
}

// stopRig is owned rigs only. It cancels every pending command first, with
// one reason, then sends CONT and TERM to the nine verified PIDs.
func (c *controller) stopRig() (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lifecycle != nil {
		return 0, refuse(errBusy, "the rig is %s", c.lifecycle.Kind+"ing")
	}
	if c.status != rigReady || c.owner != ownerOwned {
		return 0, refuse(errNotOwned, "Stop is for a rig this service started; this one is %s", ownerOr(c.status, c.owner))
	}
	c.cancelAllLocked("the rig is stopping", nil)
	cmd := c.beginLocked(cmdStop, "", laneLifecycle, c.d.t.stopLimit)
	c.lifecycle = cmd
	c.status = rigStopping
	ids := make([]identity, 0, len(c.ids))
	for _, s := range servers {
		ids = append(ids, c.ids[s.Name])
	}
	c.run(cmd, func(ctx context.Context) ending {
		if err := c.d.rig.stop(ctx, ids); err != nil {
			return ending{endError, err.Error(), func() { c.status, c.rigErr = rigPartial, err.Error() }}
		}
		return ending{endResult, "rig stopped: nine processes ended", func() {
			c.status, c.owner, c.ids = rigAbsent, "", map[string]identity{}
			c.stoppedByUs = map[string]bool{}
			c.st.forgetProcs()
			for _, cl := range clusters {
				c.dark[cl] = &darkState{}
			}
		}}
	})
	return cmd.ID, nil
}

func ownerOr(st rigStatus, owner string) string {
	if owner == "" {
		return string(st)
	}
	return owner
}

// --- the summary tick --------------------------------------------------------

// tick computes the summary from the store, remembers the last agreement,
// and feeds the transition timer. It runs every metaPollEvery.
func (c *controller) tick(now time.Time) {
	v := c.st.view(now)
	c.mu.Lock()
	defer c.mu.Unlock()
	s := summarize(v.obs, v.procs, c.last, now)
	prev := c.latest
	c.last, c.latest = s.Last, s
	c.transitions.observe(s)
	if prev.State != s.State || prev.Leader != s.Leader || prev.Term != s.Term {
		c.hist.add(eventObservation, 0, "", "summary: "+string(s.State)+" — "+s.Text)
	}
}

func (c *controller) runTicks(ctx context.Context) {
	t := time.NewTicker(c.d.t.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			c.tick(now)
		}
	}
}

// --- small helpers -----------------------------------------------------------

func secs(d time.Duration) string {
	if d%time.Second == 0 {
		return fmt.Sprintf("%d s", int(d/time.Second))
	}
	return fmt.Sprintf("%.1f s", d.Seconds())
}

func joinErrs(errs []string) string {
	out := ""
	for i, e := range errs {
		if i > 0 {
			out += "; "
		}
		out += e
	}
	return out
}
