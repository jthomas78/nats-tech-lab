package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeOS answers fixed argv with canned output, the way `ps` and `lsof`
// print it on macOS. An argv it does not know is a spec bug, and fails.
type fakeOS struct {
	files   map[string]string
	answers map[string]runResult
	kills   []killCall
}

func (f *fakeOS) deps() procDeps {
	return procDeps{
		run: func(_ context.Context, name string, args ...string) runResult {
			key := strings.Join(append([]string{name}, args...), " ")
			r, ok := f.answers[key]
			if !ok {
				Fail("unexpected command: " + key)
			}
			return r
		},
		readFile: func(path string) ([]byte, error) {
			s, ok := f.files[path]
			if !ok {
				return nil, os.ErrNotExist
			}
			return []byte(s), nil
		},
		kill: func(pid int, sig syscall.Signal) error {
			f.kills = append(f.kills, killCall{pid, sig})
			return nil
		},
	}
}

// killCall is one signal the fake was asked to send.
type killCall struct {
	PID int
	Sig syscall.Signal
}

const lab = "/repo/demos/03-multi-cluster-and-accounts/lab"

// healthy is t-za-1 as rig-t4.sh up leaves it: PID 41231, started from
// lab/run, listening on 8231.
func healthy() *fakeOS {
	return &fakeOS{
		files: map[string]string{lab + "/run/pid/za-1.pid": "41231\n"},
		answers: map[string]runResult{
			"ps -o args= -p 41231":                 {Stdout: "nats-server -c t-za-1.conf\n"},
			"lsof -a -p 41231 -d cwd -Fn":          {Stdout: "p41231\nfcwd\nn" + lab + "/run\n"},
			"lsof -nP -iTCP:8231 -sTCP:LISTEN -Fp": {Stdout: "p41231\n"},
			"ps -o lstart= -p 41231":               {Stdout: "Tue Oct  6 09:12:01 2026\n"},
			"ps -o stat= -p 41231":                 {Stdout: "S\n"},
		},
	}
}

var _ = Describe("process identity (D03-R27, rule 7)", func() {
	za1 := servers[0]
	ctx := context.Background()

	Describe("verifyServer", func() {
		It("verifies a server that passes all four checks, and records its start time", func() {
			id := verifyServer(ctx, healthy().deps(), lab, za1)

			Expect(id).To(Equal(identity{Server: "t-za-1", PID: 41231, Start: "Tue Oct  6 09:12:01 2026", Verified: true}))
		})

		It("accepts the debug flag rig adds under LAB_DEBUG=1", func() {
			f := healthy()
			f.answers["ps -o args= -p 41231"] = runResult{Stdout: "nats-server -c t-za-1.conf -D\n"}

			Expect(verifyServer(ctx, f.deps(), lab, za1).Verified).To(BeTrue())
		})

		DescribeTable("refuses, saying why",
			func(change func(*fakeOS), reason string) {
				f := healthy()
				change(f)
				id := verifyServer(ctx, f.deps(), lab, za1)

				Expect(id.Verified).To(BeFalse())
				Expect(id.Reason).To(ContainSubstring(reason))
			},
			Entry("no PID file", func(f *fakeOS) {
				delete(f.files, lab+"/run/pid/za-1.pid")
			}, "no PID file at "+lab+"/run/pid/za-1.pid"),
			Entry("a PID file with two numbers", func(f *fakeOS) {
				f.files[lab+"/run/pid/za-1.pid"] = "41231 41232"
			}, "does not hold one positive integer"),
			Entry("a PID that no longer runs", func(f *fakeOS) {
				f.answers["ps -o args= -p 41231"] = runResult{Exit: 1}
			}, "no process 41231"),
			Entry("one of the six committed T2 configs, not a t- config", func(f *fakeOS) {
				f.answers["ps -o args= -p 41231"] = runResult{Stdout: "nats-server -c za-1.conf\n"}
			}, `runs "nats-server -c za-1.conf"`),
			Entry("another t- server's process", func(f *fakeOS) {
				f.answers["ps -o args= -p 41231"] = runResult{Stdout: "nats-server -c t-za-2.conf\n"}
			}, `not "nats-server -c t-za-1.conf"`),
			Entry("the right command started somewhere else", func(f *fakeOS) {
				f.answers["lsof -a -p 41231 -d cwd -Fn"] = runResult{Stdout: "p41231\nfcwd\nn/tmp/elsewhere\n"}
			}, "works in [/tmp/elsewhere]"),
			Entry("the monitor port held by another process", func(f *fakeOS) {
				f.answers["lsof -nP -iTCP:8231 -sTCP:LISTEN -Fp"] = runResult{Stdout: "p50001\n"}
			}, "monitor port 8231 is held by [50001]"),
			Entry("nobody on the monitor port", func(f *fakeOS) {
				f.answers["lsof -nP -iTCP:8231 -sTCP:LISTEN -Fp"] = runResult{Exit: 1}
			}, "monitor port 8231 is held by []"),
			Entry("ps that will not run", func(f *fakeOS) {
				f.answers["ps -o args= -p 41231"] = runResult{Err: errors.New("exec: not found")}
			}, "could not run ps"),
		)
	})

	Describe("signalVerified", func() {
		var was identity

		BeforeEach(func() {
			was = verifyServer(ctx, healthy().deps(), lab, za1)
			Expect(was.Verified).To(BeTrue())
		})

		It("checks again, then sends the one signal to the one PID", func() {
			f := healthy()
			Expect(signalVerified(ctx, f.deps(), lab, za1, was, syscall.SIGSTOP)).To(Succeed())
			Expect(f.kills).To(Equal([]killCall{{41231, syscall.SIGSTOP}}))
		})

		It("refuses a reused PID: same number, different start time", func() {
			f := healthy()
			f.answers["ps -o lstart= -p 41231"] = runResult{Stdout: "Tue Oct  6 10:00:00 2026\n"}

			err := signalVerified(ctx, f.deps(), lab, za1, was, syscall.SIGSTOP)
			Expect(err).To(MatchError(ContainSubstring("the PID was reused")))
			Expect(f.kills).To(BeEmpty())
		})

		It("refuses when the PID file now names another process", func() {
			f := healthy()
			f.files[lab+"/run/pid/za-1.pid"] = "41300"
			f.answers["ps -o args= -p 41300"] = runResult{Stdout: "nats-server -c t-za-1.conf\n"}
			f.answers["lsof -a -p 41300 -d cwd -Fn"] = runResult{Stdout: "p41300\nfcwd\nn" + lab + "/run\n"}
			f.answers["lsof -nP -iTCP:8231 -sTCP:LISTEN -Fp"] = runResult{Stdout: "p41300\n"}
			f.answers["ps -o lstart= -p 41300"] = runResult{Stdout: "Tue Oct  6 10:00:00 2026\n"}

			err := signalVerified(ctx, f.deps(), lab, za1, was, syscall.SIGCONT)
			Expect(err).To(MatchError(ContainSubstring("PID changed from 41231 to 41300")))
			Expect(f.kills).To(BeEmpty())
		})

		It("refuses when a check now fails", func() {
			f := healthy()
			f.answers["ps -o args= -p 41231"] = runResult{Stdout: "sleep 100\n"}

			err := signalVerified(ctx, f.deps(), lab, za1, was, syscall.SIGTERM)
			Expect(err).To(MatchError(ContainSubstring("failed its identity check")))
			Expect(f.kills).To(BeEmpty())
		})

		It("refuses a server that was never verified, without looking again", func() {
			f := &fakeOS{}
			err := signalVerified(ctx, f.deps(), lab, za1, identity{Server: "t-za-1", Reason: "no PID file"}, syscall.SIGSTOP)
			Expect(err).To(MatchError(ContainSubstring("never verified")))
			Expect(f.kills).To(BeEmpty())
		})
	})

	Describe("readProcState (rule 2: the OS, never the monitor)", func() {
		DescribeTable("reads ps -o stat=",
			func(r runResult, want procState) {
				f := &fakeOS{answers: map[string]runResult{"ps -o stat= -p 41231": r}}
				Expect(readProcState(ctx, f.deps(), 41231)).To(Equal(want))
			},
			Entry("T is stopped (SIGSTOP)", runResult{Stdout: "T\n"}, procStopped),
			Entry("T+ is stopped", runResult{Stdout: "T+\n"}, procStopped),
			Entry("S is running", runResult{Stdout: "S\n"}, procRunning),
			Entry("Ss is running", runResult{Stdout: "Ss\n"}, procRunning),
			Entry("R is running", runResult{Stdout: "R\n"}, procRunning),
			Entry("no line and exit 1 is gone", runResult{Exit: 1}, procGone),
			Entry("no line and exit 0 is unknown", runResult{}, procUnknown),
			Entry("ps that will not run is unknown", runResult{Err: errors.New("boom")}, procUnknown),
		)
	})

	Describe("labScriptRunning", func() {
		scripts := []string{"run-all.sh", "10-hub-meta-leader.sh", "rig-t4.sh"}

		DescribeTable("finds a lab script by argv[0], or as a shell's first argument",
			func(ps string, want bool) {
				_, got := labScriptRunning(ps, scripts)
				Expect(got).To(Equal(want))
			},
			Entry("bash running run-all.sh", "/bin/zsh -l\nbash ./lab/run-all.sh\n", true),
			Entry("bash running 10 by full path", "/bin/bash /repo/lab/10-hub-meta-leader.sh step3\n", true),
			Entry("the script as argv[0]", "./lab/rig-t4.sh up\n", true),
			Entry("an editor with the script open", "vim lab/run-all.sh\n", false),
			Entry("a t- server", "nats-server -c t-za-1.conf\n", false),
			Entry("nothing", "", false),
		)
	})
})
