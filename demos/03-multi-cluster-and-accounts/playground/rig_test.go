package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeStreams struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeStreams) ensureStreams(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.err
}

func (f *fakeStreams) n() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// metaSize is a monitor that reads one meta size from every server.
func metaSize(size int) fetchFunc {
	return func(_ context.Context, url string) (int, []byte, error) {
		return 200, []byte(fmt.Sprintf(`{"meta_cluster":{"cluster_size":%d}}`, size)), nil
	}
}

func noMonitor(context.Context, string) (int, []byte, error) {
	return 0, nil, errors.New("connection refused")
}

type labRigUnderTest struct {
	r       *labRig
	host    *fakeHost
	streams *fakeStreams
	ups     int
}

func newLabRigUnderTest(fetch fetchFunc) *labRigUnderTest {
	t := &labRigUnderTest{host: newFakeHost(), streams: &fakeStreams{}}
	t.r = newLabRig(lab, t.host.deps(), fetch, t.streams)
	t.r.scripts = []string{"rig-t4.sh", "10-hub-meta-leader.sh", "run-all.sh"}
	t.r.waitEvery = 5 * time.Millisecond
	t.r.up = func(context.Context) (string, error) { t.ups++; return "rig up\n", nil }
	return t
}

func (t *labRigUnderTest) signalsTo(pid int) []syscall.Signal {
	var out []syscall.Signal
	for _, k := range t.host.signals() {
		if k.PID == pid {
			out = append(out, k.Sig)
		}
	}
	return out
}

var _ = Describe("the rig lifecycle (rig.go)", func() {
	ctx := context.Background()

	Describe("start", func() {
		It("refuses while a lab script runs, before it runs rig-t4.sh up", func() {
			t := newLabRigUnderTest(metaSize(9))
			t.host.psAll = "/bin/zsh -l\nbash ./10-hub-meta-leader.sh step2\n"
			_, err := t.r.start(ctx)
			Expect(err).To(MatchError(ContainSubstring("a lab script is running (bash ./10-hub-meta-leader.sh step2)")))
			Expect(t.ups).To(Equal(0))
		})

		It("verifies the nine, then creates the three streams", func() {
			t := newLabRigUnderTest(metaSize(9))
			ids, err := t.r.start(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(t.ups).To(Equal(1))
			Expect(allVerified(ids)).To(BeTrue())
			Expect(t.streams.n()).To(Equal(1))
			Expect(t.host.signals()).To(BeEmpty())
		})

		It("reports rig-t4.sh up's last lines when it fails, and creates nothing", func() {
			t := newLabRigUnderTest(metaSize(9))
			t.r.up = func(context.Context) (string, error) {
				return "x\na\nb\nrefusing: a t- server is already running\n", errors.New("exit status 1")
			}
			_, err := t.r.start(ctx)
			Expect(err).To(MatchError(ContainSubstring("rig-t4.sh up failed (exit status 1): a / b / refusing: a t- server is already running")))
			Expect(t.streams.n()).To(Equal(0))
		})

		It("returns an unverified rig as it is, with no streams and no signals", func() {
			t := newLabRigUnderTest(metaSize(9))
			t.host.gone[pidOf(servers[4])] = true
			ids, err := t.r.start(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(allVerified(ids)).To(BeFalse())
			Expect(t.streams.n()).To(Equal(0))
			Expect(t.host.signals()).To(BeEmpty())
		})

		It("stops the rig it just started when the streams cannot be created", func() {
			t := newLabRigUnderTest(metaSize(9))
			t.streams.err = errors.New("create ODOMETER_ZA: no reply")
			_, err := t.r.start(ctx)
			Expect(err).To(MatchError(ContainSubstring("the rig was stopped again")))
			for _, s := range servers {
				Expect(t.signalsTo(pidOf(s))).To(Equal([]syscall.Signal{syscall.SIGCONT, syscall.SIGTERM}), s.Name)
			}
		})
	})

	Describe("attach", func() {
		It("accepts nine verified servers and meta size 9, and changes nothing", func() {
			t := newLabRigUnderTest(metaSize(9))
			ids, err := t.r.attach(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(allVerified(ids)).To(BeTrue())
			Expect(t.ups).To(Equal(0))
			Expect(t.streams.n()).To(Equal(0))
			Expect(t.host.signals()).To(BeEmpty())
		})

		It("refuses a rig whose meta size is not 9", func() {
			t := newLabRigUnderTest(metaSize(6))
			_, err := t.r.attach(ctx)
			Expect(err).To(MatchError(ContainSubstring("meta size 6, not 9: this is not the T4 rig")))
		})

		It("refuses when no monitor answers", func() {
			t := newLabRigUnderTest(noMonitor)
			_, err := t.r.attach(ctx)
			Expect(err).To(MatchError(ContainSubstring("no monitor gave a meta size")))
		})

		It("refuses while a lab script runs", func() {
			t := newLabRigUnderTest(metaSize(9))
			t.host.psAll = "/usr/bin/python3 render-report.py\n/repo/lab/run-all.sh\n"
			_, err := t.r.attach(ctx)
			Expect(err).To(MatchError(ContainSubstring("a lab script is running (/repo/lab/run-all.sh)")))
		})
	})

	Describe("stop", func() {
		It("sends CONT then TERM to each verified PID and waits until all are gone", func() {
			t := newLabRigUnderTest(metaSize(9))
			ids, _ := t.r.attach(ctx)
			t.host.stopped[pidOf(servers[0])] = true
			Expect(t.r.stop(ctx, ids)).To(Succeed())
			for _, s := range servers {
				Expect(t.signalsTo(pidOf(s))).To(Equal([]syscall.Signal{syscall.SIGCONT, syscall.SIGTERM}), s.Name)
				Expect(readProcState(ctx, t.host.deps(), pidOf(s))).To(Equal(procGone))
			}
		})

		It("signals no process whose identity was not verified", func() {
			t := newLabRigUnderTest(metaSize(9))
			ids, _ := t.r.attach(ctx)
			ids[2].Verified = false
			Expect(t.r.stop(ctx, ids)).To(Succeed())
			Expect(t.signalsTo(pidOf(servers[2]))).To(BeEmpty())
		})

		It("names each process that did not end before the limit", func() {
			t := newLabRigUnderTest(metaSize(9))
			ids, _ := t.r.attach(ctx)
			t.host.ignoreTerm = true
			c, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			err := t.r.stop(c, ids)
			Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("za-1 (PID %d) did not end", pidOf(servers[0])))))
		})
	})

	Describe("release, on the service's own shutdown", func() {
		It("stops an owned rig", func() {
			r := newRigUnderTest(ownerOwned)
			Expect(r.c.release(ctx)).To(Equal("owned rig: nine processes ended"))
			r.rig.mu.Lock()
			defer r.rig.mu.Unlock()
			Expect(r.rig.stopped).To(HaveLen(9))
		})

		It("sends CONT only to what it stopped on an attached rig, and no TERM", func() {
			r := newRigUnderTest(ownerAttached)
			id, err := r.c.freeze(clusterZA)
			Expect(err).NotTo(HaveOccurred())
			r.waitEnded(id)
			before := len(r.host.signals())
			Expect(r.c.release(ctx)).To(ContainSubstring("CONT sent to the 3 processes"))
			after := r.host.signals()[before:]
			Expect(after).To(HaveLen(3))
			for _, k := range after {
				Expect(k.Sig).To(Equal(syscall.SIGCONT))
				s, _ := serverNamed(serverOfPID(k.PID))
				Expect(s.Cluster).To(Equal(clusterZA))
			}
			r.rig.mu.Lock()
			defer r.rig.mu.Unlock()
			Expect(r.rig.stopped).To(BeEmpty())
		})
	})
})

func serverOfPID(pid int) string {
	for _, s := range servers {
		if pidOf(s) == pid {
			return s.Name
		}
	}
	return ""
}

var _ = Describe("the session file (D03-R28)", func() {
	readLines := func(path string) []map[string]any {
		f, err := os.Open(path)
		Expect(err).NotTo(HaveOccurred())
		defer f.Close()
		var out []map[string]any
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			var m map[string]any
			Expect(json.Unmarshal(sc.Bytes(), &m)).To(Succeed())
			out = append(out, m)
		}
		return out
	}
	files := func(dir string) []string {
		m, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		return m
	}

	It("opens with a not-evidence header, replays the events so far, then appends", func() {
		dir := GinkgoT().TempDir()
		h := newHistory(time.Now)
		s := &sessionFiles{dir: dir, session: "s1", hist: h, commit: "abc1234",
			fetch: func(context.Context, string) (int, []byte, error) {
				return 200, []byte(`{"version":"2.14.6"}`), nil
			}}
		h.add(eventAction, 1, "", "start rig")
		h.add(eventResult, 1, endResult, "rig started")
		s.open(ownerOwned)
		h.add(eventObservation, 0, "", "summary: agreed")
		s.close()

		Expect(files(dir)).To(HaveLen(1))
		lines := readLines(files(dir)[0])
		Expect(lines).To(HaveLen(4))
		Expect(lines[0]).To(Equal(map[string]any{
			"kind": "playground", "evidence": false, "session": "s1",
			"opened": lines[0]["opened"], "owner": "owned",
			"serverVersion": "2.14.6", "commit": "abc1234",
		}))
		Expect([]any{lines[1]["seq"], lines[2]["seq"], lines[3]["seq"]}).To(Equal([]any{1.0, 2.0, 3.0}))
	})

	It("starts a new file for the next rig, with no event repeated or lost", func() {
		dir := GinkgoT().TempDir()
		h := newHistory(time.Now)
		s := &sessionFiles{dir: dir, session: "s1", hist: h, commit: "x", fetch: noMonitor}
		h.add(eventAction, 1, "", "one")
		s.open(ownerOwned)
		h.add(eventAction, 2, "", "two")
		time.Sleep(1100 * time.Millisecond) // the file name is to the second
		s.open(ownerAttached)
		h.add(eventAction, 3, "", "three")
		s.close()

		fs := files(dir)
		Expect(fs).To(HaveLen(2))
		first, second := readLines(fs[0]), readLines(fs[1])
		Expect(first[0]["serverVersion"]).To(Equal("unknown"))
		Expect(second[0]["owner"]).To(Equal("attached"))
		Expect([]any{first[1]["seq"], first[2]["seq"]}).To(Equal([]any{1.0, 2.0}))
		Expect(second).To(HaveLen(2))
		Expect(second[1]["seq"]).To(Equal(3.0))
	})

	It("never lives under lab/run/", func() {
		Expect(filepath.Join(filepath.Dir(lab), "playground", ".run", "sessions")).
			NotTo(HavePrefix(filepath.Join(lab, "run")))
	})
})
