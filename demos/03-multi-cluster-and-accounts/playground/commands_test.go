package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeHost is the nine T4 processes as ps and lsof see them. A SIGSTOP
// stops a process and a SIGCONT runs it, unless ignoreStop is set, so a
// freeze can be made to hang. It is safe for the runner's goroutines.
type fakeHost struct {
	mu         sync.Mutex
	stopped    map[int]bool
	ignoreStop bool
	ignoreCont bool
	kills      []killCall
}

func pidOf(s server) int {
	for i, x := range servers {
		if x.Name == s.Name {
			return 50001 + i
		}
	}
	return 0
}

func newFakeHost() *fakeHost { return &fakeHost{stopped: map[int]bool{}} }

func (h *fakeHost) deps() procDeps {
	return procDeps{
		run: func(_ context.Context, name string, args ...string) runResult {
			defer GinkgoRecover() // the runner calls this from its own goroutines
			h.mu.Lock()
			defer h.mu.Unlock()
			key := strings.Join(append([]string{name}, args...), " ")
			for _, s := range servers {
				p := strconv.Itoa(pidOf(s))
				switch key {
				case "ps -o args= -p " + p:
					return runResult{Stdout: "nats-server -c " + s.Name + ".conf\n"}
				case "lsof -a -p " + p + " -d cwd -Fn":
					return runResult{Stdout: "p" + p + "\nfcwd\nn" + lab + "/run\n"}
				case fmt.Sprintf("lsof -nP -iTCP:%d -sTCP:LISTEN -Fp", s.Monitor):
					return runResult{Stdout: "p" + p + "\n"}
				case "ps -o lstart= -p " + p:
					return runResult{Stdout: "Tue Oct  6 09:12:01 2026\n"}
				case "ps -o stat= -p " + p:
					if h.stopped[pidOf(s)] {
						return runResult{Stdout: "T\n"}
					}
					return runResult{Stdout: "S\n"}
				}
			}
			Fail("unexpected command: " + key)
			return runResult{}
		},
		readFile: func(path string) ([]byte, error) {
			for _, s := range servers {
				if path == pidFilePath(lab, s) {
					return []byte(strconv.Itoa(pidOf(s)) + "\n"), nil
				}
			}
			return nil, fmt.Errorf("no file %s", path)
		},
		kill: func(pid int, sig syscall.Signal) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.kills = append(h.kills, killCall{pid, sig})
			switch {
			case sig == syscall.SIGSTOP && !h.ignoreStop:
				h.stopped[pid] = true
			case sig == syscall.SIGCONT && !h.ignoreCont:
				delete(h.stopped, pid)
			}
			return nil
		},
	}
}

func (h *fakeHost) setIgnoreStop(v bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ignoreStop = v
}

func (h *fakeHost) setIgnoreCont(v bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ignoreCont = v
}

func (h *fakeHost) signals() []killCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]killCall(nil), h.kills...)
}

// fakeRig verifies the nine fake processes for start and attach, and
// records a stop.
type fakeRig struct {
	host    *fakeHost
	mu      sync.Mutex
	stopped []identity
}

func (r *fakeRig) verifyAll(ctx context.Context) ([]identity, error) {
	ids := []identity{}
	for _, s := range servers {
		ids = append(ids, verifyServer(ctx, r.host.deps(), lab, s))
	}
	return ids, nil
}

func (r *fakeRig) start(ctx context.Context) ([]identity, error)  { return r.verifyAll(ctx) }
func (r *fakeRig) attach(ctx context.Context) ([]identity, error) { return r.verifyAll(ctx) }
func (r *fakeRig) stop(_ context.Context, ids []identity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = ids
	return nil
}

// fakeNATS runs whatever each spec sets. An unset call fails the spec.
type fakeNATS struct {
	onPublish  func(ctx context.Context, via server, site, msgID string) (pubAck, error)
	onRead     func(ctx context.Context, via server, site string, after uint64) (streamRead, error)
	onProbe    func(ctx context.Context, via server, n int) []probeStep
	onStepDown func(ctx context.Context, via server, cluster string) error
}

func (f *fakeNATS) publish(ctx context.Context, via server, site, msgID string, _ []byte, _ time.Duration) (pubAck, error) {
	defer GinkgoRecover()
	if f.onPublish == nil {
		Fail("unexpected publish")
	}
	return f.onPublish(ctx, via, site, msgID)
}

func (f *fakeNATS) readStream(ctx context.Context, via server, site string, after uint64) (streamRead, error) {
	defer GinkgoRecover()
	if f.onRead == nil {
		Fail("unexpected readStream")
	}
	return f.onRead(ctx, via, site, after)
}

func (f *fakeNATS) probe(ctx context.Context, via server, n int) []probeStep {
	defer GinkgoRecover()
	if f.onProbe == nil {
		Fail("unexpected probe")
	}
	return f.onProbe(ctx, via, n)
}

func (f *fakeNATS) stepDown(ctx context.Context, via server, cluster string) error {
	defer GinkgoRecover()
	if f.onStepDown == nil {
		Fail("unexpected stepDown")
	}
	return f.onStepDown(ctx, via, cluster)
}

// blockUntilCancelled is a NATS call that waits out its context: a 5 s
// publish into a dark region.
func blockUntilCancelled(ctx context.Context, _ server, _, _ string) (pubAck, error) {
	<-ctx.Done()
	return pubAck{}, ctx.Err()
}

// testTimings are short limits, so the specs run in well under a second
// each. The rules are the same.
func testTimings() timings {
	return timings{
		grace:         50 * time.Millisecond,
		confirmFor:    time.Second,
		confirmEvery:  10 * time.Millisecond,
		verifyLimit:   200 * time.Millisecond,
		probeLimit:    150 * time.Millisecond,
		leaderReply:   100 * time.Millisecond,
		leaderObserve: 200 * time.Millisecond,
		startLimit:    time.Second,
		attachLimit:   time.Second,
		stopLimit:     time.Second,
		tick:          50 * time.Millisecond,
	}
}

type rigUnderTest struct {
	c    *controller
	host *fakeHost
	rig  *fakeRig
	nats *fakeNATS
	stop context.CancelFunc
}

// newRigUnderTest builds a controller on the fakes. ready brings the rig to
// ready through the real start or attach path.
func newRigUnderTest(owner string) *rigUnderTest {
	host := newFakeHost()
	r := &rigUnderTest{host: host, rig: &fakeRig{host: host}, nats: &fakeNATS{}}
	ctx, cancel := context.WithCancel(context.Background())
	r.stop = cancel
	r.c = newController(ctx, controllerDeps{
		lab: lab, proc: host.deps(), rig: r.rig, nats: r.nats,
		now: time.Now, t: testTimings(), session: "s1",
	}, newStore(), newHistory(time.Now))
	DeferCleanup(func() {
		r.c.cancelAll("the spec ended")
		cancel()
	})
	if owner == "" {
		return r
	}
	var id int
	var err error
	if owner == ownerOwned {
		id, err = r.c.startRig()
	} else {
		id, err = r.c.attachRig()
	}
	Expect(err).NotTo(HaveOccurred())
	r.waitEnded(id)
	Expect(r.status()).To(Equal(rigReady))
	return r
}

// ended returns a command's history result, or nil while it is pending.
func (r *rigUnderTest) ended(id int) *event {
	evs, _ := r.c.hist.after(0)
	for _, e := range evs {
		if e.Kind == eventResult && e.Cmd == id {
			e := e
			return &e
		}
	}
	return nil
}

func (r *rigUnderTest) waitEnded(id int) event {
	var got *event
	Eventually(func() *event { got = r.ended(id); return got }, 3*time.Second, 5*time.Millisecond).
		ShouldNot(BeNil(), "command %d never ended (rule 9)", id)
	return *got
}

func (r *rigUnderTest) status() rigStatus {
	r.c.mu.Lock()
	defer r.c.mu.Unlock()
	return r.c.status
}

func (r *rigUnderTest) pendingIDs() []int {
	r.c.mu.Lock()
	defer r.c.mu.Unlock()
	ids := []int{}
	for _, cmd := range r.c.sortedPendingLocked() {
		ids = append(ids, cmd.ID)
	}
	return ids
}

func (r *rigUnderTest) attempt(site string, i int) pubAttempt {
	r.c.mu.Lock()
	defer r.c.mu.Unlock()
	e := r.c.ledger[site][0]
	return *e.Attempts[i]
}

var _ = Describe("the command runner (D03-R24, rule 9)", func() {
	Describe("the signal lane", func() {
		It("freezes a cluster: SIGSTOP to its three verified processes, confirmed by ps", func() {
			r := newRigUnderTest(ownerOwned)

			id, err := r.c.freeze(clusterAU)
			Expect(err).NotTo(HaveOccurred())
			e := r.waitEnded(id)

			Expect(e.End).To(Equal(endResult))
			Expect(e.Text).To(HavePrefix("3 of 3 confirmed stopped after"))
			Expect(r.host.signals()).To(ConsistOf(
				killCall{pidOf(servers[6]), syscall.SIGSTOP},
				killCall{pidOf(servers[7]), syscall.SIGSTOP},
				killCall{pidOf(servers[8]), syscall.SIGSTOP}))
		})

		It("does not delay a Resume behind a pending 5 s publish", func() {
			r := newRigUnderTest(ownerOwned)
			r.nats.onPublish = blockUntilCancelled
			pub, err := r.c.publish(clusterAU, "auto", 5, "")
			Expect(err).NotTo(HaveOccurred())

			began := time.Now()
			id, err := r.c.resume(clusterAU)
			Expect(err).NotTo(HaveOccurred())
			e := r.waitEnded(id)

			Expect(e.End).To(Equal(endResult))
			Expect(time.Since(began)).To(BeNumerically("<", 500*time.Millisecond))
			Expect(r.ended(pub)).To(BeNil(), "the publish is still pending; nothing waited for it")
		})

		It("cancels a pending Freeze when Resume arrives, and Resume wins", func() {
			r := newRigUnderTest(ownerOwned)
			r.host.setIgnoreStop(true) // the freeze never confirms
			fz, err := r.c.freeze(clusterZA)
			Expect(err).NotTo(HaveOccurred())
			Expect(r.ended(fz)).To(BeNil())

			_, err = r.c.freeze(clusterZA)
			Expect(err).To(MatchError(errBusy), "one freeze per cluster")

			r.host.setIgnoreStop(false)
			rs, err := r.c.resume(clusterZA)
			Expect(err).NotTo(HaveOccurred())

			f := r.waitEnded(fz)
			Expect(f.End).To(Equal(endCancelled))
			Expect(f.Text).To(HavePrefix("superseded by Resume after"))
			Expect(r.waitEnded(rs).End).To(Equal(endResult))

			// Every SIGCONT comes after every SIGSTOP: no process is left
			// stopped by a late freeze.
			sigs := r.host.signals()
			last := sigs[len(sigs)-3:]
			for _, k := range last {
				Expect(k.Sig).To(Equal(syscall.SIGCONT))
			}
		})

		It("refuses a Freeze while a Resume of that cluster is pending", func() {
			r := newRigUnderTest(ownerOwned)
			r.waitEnded(must(r.c.freeze(clusterZA)))
			r.host.setIgnoreCont(true) // the resume never confirms
			_, err := r.c.resume(clusterZA)
			Expect(err).NotTo(HaveOccurred())

			_, err = r.c.freeze(clusterZA)
			Expect(err).To(MatchError(errBusy))
			Expect(err.Error()).To(ContainSubstring("Resume always wins"))
		})
	})

	Describe("the lifecycle lane", func() {
		It("Stop cancels every pending command with one reason, then stops the rig", func() {
			r := newRigUnderTest(ownerOwned)
			r.nats.onPublish = blockUntilCancelled
			r.host.setIgnoreStop(true)
			pub, err := r.c.publish(clusterZA, "auto", 30, "")
			Expect(err).NotTo(HaveOccurred())
			fz, err := r.c.freeze(clusterAU)
			Expect(err).NotTo(HaveOccurred())
			Expect(r.pendingIDs()).To(ConsistOf(pub, fz))

			st, err := r.c.stopRig()
			Expect(err).NotTo(HaveOccurred())

			for _, id := range []int{pub, fz} {
				e := r.ended(id)
				Expect(e).NotTo(BeNil(), "cancelled before Stop returns")
				Expect(e.End).To(Equal(endCancelled))
				Expect(e.Text).To(Equal("the rig is stopping"))
			}
			Expect(r.attempt(clusterZA, 0).Outcome).To(Equal(outcomeCancelledUnk))
			Expect(r.waitEnded(st).End).To(Equal(endResult))
			Expect(r.status()).To(Equal(rigAbsent))
			Expect(r.rig.stopped).To(HaveLen(9))
		})

		It("refuses Stop on a rig the service only attached to", func() {
			r := newRigUnderTest(ownerAttached)

			_, err := r.c.stopRig()

			Expect(err).To(MatchError(errNotOwned))
		})

		It("refuses every command but Restore all while a lifecycle command runs", func() {
			r := newRigUnderTest(ownerOwned)
			block := make(chan struct{})
			DeferCleanup(func() { close(block) })
			r.c.mu.Lock()
			r.c.d.rig = blockingStop{block}
			r.c.mu.Unlock()
			_, err := r.c.stopRig()
			Expect(err).NotTo(HaveOccurred())

			_, err = r.c.freeze(clusterZA)
			Expect(err).To(MatchError(errBusy))
			_, err = r.c.publish(clusterZA, "auto", 5, "")
			Expect(err).To(MatchError(errBusy))
			_, err = r.c.restoreAll()
			Expect(err).NotTo(HaveOccurred())
		})

		It("refuses a command with no ready rig", func() {
			r := newRigUnderTest("")

			_, err := r.c.freeze(clusterZA)

			Expect(err).To(MatchError(errNoRig))
		})
	})

	Describe("the nats lane", func() {
		It("refuses a second publish to the same stream while one is pending", func() {
			r := newRigUnderTest(ownerOwned)
			r.nats.onPublish = blockUntilCancelled
			_, err := r.c.publish(clusterZA, "auto", 5, "")
			Expect(err).NotTo(HaveOccurred())

			_, err = r.c.publish(clusterZA, "auto", 5, "")
			Expect(err).To(MatchError(errBusy))
			_, err = r.c.publish(clusterAU, "auto", 5, "")
			Expect(err).NotTo(HaveOccurred(), "another stream is another lane slot")
		})

		It("says refused, not sent, when no server it may use is running (D03-R30)", func() {
			r := newRigUnderTest(ownerOwned)
			fz, _ := r.c.freeze(clusterZA)
			r.waitEnded(fz)

			id, err := r.c.publish(clusterZA, clusterZA, 5, "")
			Expect(err).NotTo(HaveOccurred(), "the command exists and ends at once")

			e := r.ended(id)
			Expect(e).NotTo(BeNil())
			Expect(e.End).To(Equal(endError))
			Expect(e.Text).To(Equal("refused, not sent: no running server in za"))
			Expect(r.attempt(clusterZA, 0).Outcome).To(Equal(outcomeRefusedNot))
		})

		It("picks the destination's cluster first under Auto, then arb", func() {
			r := newRigUnderTest(ownerOwned)
			via := make(chan string, 2)
			r.nats.onPublish = func(_ context.Context, s server, _, _ string) (pubAck, error) {
				via <- s.Name
				return pubAck{Seq: 1}, nil
			}
			id, _ := r.c.publish(clusterZA, "auto", 5, "")
			r.waitEnded(id)
			Expect(<-via).To(Equal("t-za-1"))

			fz, _ := r.c.freeze(clusterZA)
			r.waitEnded(fz)
			id, _ = r.c.publish(clusterZA, "auto", 5, "")
			r.waitEnded(id)
			Expect(<-via).To(Equal("t-arb-1"))
		})

		It("calls a publish timeout an unknown outcome, never a failure (rule 6)", func() {
			r := newRigUnderTest(ownerOwned)
			r.nats.onPublish = func(context.Context, server, string, string) (pubAck, error) {
				return pubAck{}, errNoReply
			}

			id, _ := r.c.publish(clusterAU, "auto", 1, "")
			e := r.waitEnded(id)

			Expect(e.End).To(Equal(endTimeout))
			Expect(e.Text).To(ContainSubstring("outcome unknown"))
			Expect(r.attempt(clusterAU, 0).Outcome).To(Equal(outcomeTimedOut))
		})

		It("sends a retry with the same ID, and 404s an ID not in the ledger", func() {
			r := newRigUnderTest(ownerOwned)
			ids := make(chan string, 2)
			r.nats.onPublish = func(_ context.Context, _ server, _, msgID string) (pubAck, error) {
				ids <- msgID
				return pubAck{Seq: 7, Duplicate: len(ids) > 1}, nil
			}
			id, _ := r.c.publish(clusterAU, "auto", 5, "")
			r.waitEnded(id)
			id, err := r.c.publish(clusterAU, "auto", 5, "pg-s1-au-1")
			Expect(err).NotTo(HaveOccurred())
			r.waitEnded(id)

			Expect(<-ids).To(Equal("pg-s1-au-1"))
			Expect(<-ids).To(Equal("pg-s1-au-1"))
			_, err = r.c.publish(clusterAU, "auto", 5, "pg-s1-au-9")
			Expect(err).To(MatchError(errNoEntry))
		})

		It("marks ledger entries present or absent, as of the readback", func() {
			r := newRigUnderTest(ownerOwned)
			r.nats.onPublish = func(context.Context, server, string, string) (pubAck, error) {
				return pubAck{}, errNoReply
			}
			for i := 0; i < 2; i++ {
				id, _ := r.c.publish(clusterAU, "auto", 1, "")
				r.waitEnded(id)
			}
			r.nats.onRead = func(context.Context, server, string, uint64) (streamRead, error) {
				return streamRead{LastSeq: 4, Msgs: []storedMsg{{Seq: 4, MsgID: "pg-s1-au-2"}}}, nil
			}

			e := r.waitEnded(must(r.c.verify(clusterAU, "auto")))

			Expect(e.Text).To(ContainSubstring("1 present, 1 absent, as of"))
			l := r.c.ledgerOf(clusterAU)
			Expect(l[0].Storage.Present).To(BeFalse())
			Expect(l[1].Storage.Present).To(BeTrue())
			Expect(l[1].Storage.Seq).To(Equal(uint64(4)))
		})
	})

	Describe("the watchdog", func() {
		It("closes a command whose body never returns", func() {
			r := newRigUnderTest(ownerOwned)
			never := make(chan struct{})
			DeferCleanup(func() { close(never) })
			r.nats.onRead = func(context.Context, server, string, uint64) (streamRead, error) {
				<-never // ignores its context
				return streamRead{}, nil
			}

			id, err := r.c.verify(clusterZA, "auto")
			Expect(err).NotTo(HaveOccurred())
			e := r.waitEnded(id)

			Expect(e.End).To(Equal(endTimeout))
			Expect(e.Text).To(Equal("no completion within 0.2 s"))
			Expect(r.pendingIDs()).To(BeEmpty())
		})

		It("closes a publish whose fake never returns, and leaves its outcome unknown", func() {
			r := newRigUnderTest(ownerOwned)
			never := make(chan struct{})
			DeferCleanup(func() { close(never) })
			r.nats.onPublish = func(context.Context, server, string, string) (pubAck, error) {
				<-never
				return pubAck{Seq: 1}, nil
			}

			id, _ := r.c.publish(clusterZA, "auto", 1, "")
			e := r.waitEnded(id)

			Expect(e.End).To(Equal(endTimeout))
			Expect(e.Text).To(Equal("no completion within 1 s"))
			Expect(r.attempt(clusterZA, 0).Outcome).To(Equal(outcomeTimedOut))
		})

		It("keeps the first ending: a body that returns late changes nothing", func() {
			r := newRigUnderTest(ownerOwned)
			late := make(chan struct{})
			r.nats.onRead = func(context.Context, server, string, uint64) (streamRead, error) {
				<-late
				return streamRead{LastSeq: 9}, nil
			}
			id, _ := r.c.verify(clusterZA, "auto")
			r.waitEnded(id)

			close(late)
			Consistently(func() int {
				n := 0
				evs, _ := r.c.hist.after(0)
				for _, e := range evs {
					if e.Kind == eventResult && e.Cmd == id {
						n++
					}
				}
				return n
			}, 100*time.Millisecond, 10*time.Millisecond).Should(Equal(1))
			r.c.mu.Lock()
			defer r.c.mu.Unlock()
			Expect(r.c.verifiedTo[clusterZA]).To(BeZero())
		})
	})
})

// blockingStop is a rig whose stop waits until the spec lets it go.
type blockingStop struct{ release chan struct{} }

func (blockingStop) start(context.Context) ([]identity, error)  { return nil, nil }
func (blockingStop) attach(context.Context) ([]identity, error) { return nil, nil }
func (b blockingStop) stop(context.Context, []identity) error {
	<-b.release
	return nil
}

func must(id int, err error) int {
	Expect(err).NotTo(HaveOccurred())
	return id
}
