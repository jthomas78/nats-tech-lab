package main

// Process identity, process state and the one guarded signal (D03-R27,
// rule 7). A PID from a PID file is a claim, not a fact: before every signal,
// and when attaching, all four checks run again, and any mismatch refuses.
//
// Everything that touches the OS goes through procDeps, so the specs pass
// fake `ps` / `lsof` output. Commands are a fixed argv — never a shell, never
// text from the browser.

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// runResult is one finished command. Exit is the process exit code; Err is
// set only when the command could not be run at all (not found, killed by
// the context). A non-zero Exit is not an Err: `ps -p <gone pid>` exits 1.
type runResult struct {
	Stdout string
	Exit   int
	Err    error
}

type procDeps struct {
	// run executes name with args, no shell.
	run func(ctx context.Context, name string, args ...string) runResult
	// readFile reads a PID file.
	readFile func(path string) ([]byte, error)
	// kill sends one signal to one PID.
	kill func(pid int, sig syscall.Signal) error
}

// identity is one server's verification result. Verified is true only when
// all four checks passed; otherwise Reason says which failed. Start is the
// process start time (`ps -o lstart=`) read at verification, so a later
// signal can catch a reused PID.
type identity struct {
	Server   string `json:"server"`
	PID      int    `json:"pid,omitempty"`
	Start    string `json:"start,omitempty"`
	Verified bool   `json:"verified"`
	Reason   string `json:"reason,omitempty"`
}

// pidFilePath is where rig's start_server writes a server's PID.
func pidFilePath(labDir string, s server) string {
	return filepath.Join(labDir, "run", "pid", s.Short+".pid")
}

// verifyServer runs the four identity checks for one server, in order, and
// stops at the first that fails.
//
//  1. the PID file exists under <lab>/run/pid/ and holds one integer;
//  2. the command line is exactly `nats-server -c t-<server>.conf`,
//     optionally followed by `-D`;
//  3. the working directory is <lab>/run;
//  4. it is the one process listening on the server's monitor port.
func verifyServer(ctx context.Context, d procDeps, labDir string, s server) identity {
	id := identity{Server: s.Name}
	refuse := func(format string, a ...any) identity {
		id.Reason = fmt.Sprintf(format, a...)
		return id
	}

	path := pidFilePath(labDir, s)
	raw, err := d.readFile(path)
	if err != nil {
		return refuse("no PID file at %s", path)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return refuse("PID file %s does not hold one positive integer", path)
	}
	id.PID = pid
	p := strconv.Itoa(pid)

	args := d.run(ctx, "ps", "-o", "args=", "-p", p)
	if args.Err != nil {
		return refuse("could not run ps: %v", args.Err)
	}
	argv := strings.TrimSpace(args.Stdout)
	if argv == "" {
		return refuse("no process %d", pid)
	}
	want := "nats-server -c " + s.Name + ".conf"
	if argv != want && argv != want+" -D" {
		return refuse("process %d runs %q, not %q", pid, argv, want)
	}

	cwd := d.run(ctx, "lsof", "-a", "-p", p, "-d", "cwd", "-Fn")
	if cwd.Err != nil {
		return refuse("could not run lsof: %v", cwd.Err)
	}
	wantDir := filepath.Clean(filepath.Join(labDir, "run"))
	gotDir := lsofField(cwd.Stdout, 'n')
	if len(gotDir) != 1 || filepath.Clean(gotDir[0]) != wantDir {
		return refuse("process %d works in %v, not %s", pid, gotDir, wantDir)
	}

	port := strconv.Itoa(s.Monitor)
	listen := d.run(ctx, "lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN", "-Fp")
	if listen.Err != nil {
		return refuse("could not run lsof: %v", listen.Err)
	}
	owners := lsofField(listen.Stdout, 'p')
	if len(owners) != 1 || owners[0] != p {
		return refuse("monitor port %d is held by %v, not by process %d", s.Monitor, owners, pid)
	}

	start := d.run(ctx, "ps", "-o", "lstart=", "-p", p)
	if start.Err != nil || strings.TrimSpace(start.Stdout) == "" {
		return refuse("could not read the start time of process %d", pid)
	}
	id.Start = strings.TrimSpace(start.Stdout)
	id.Verified = true
	return id
}

// lsofField returns every value of one field from `lsof -F` output, where
// each line is a one-letter field code followed by its value. Values repeat
// once per process (p) or per file (n); duplicates are kept out.
func lsofField(out string, code byte) []string {
	var vals []string
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 2 || line[0] != code {
			continue
		}
		v := line[1:]
		if !seen[v] {
			seen[v] = true
			vals = append(vals, v)
		}
	}
	return vals
}

// signalVerified sends sig to the process verified as `was`, after running
// the four checks again and comparing the start time. It refuses — and
// signals nothing — on any mismatch. It never signals a process that was not
// verified in the first place.
func signalVerified(ctx context.Context, d procDeps, labDir string, s server, was identity, sig syscall.Signal) error {
	if !was.Verified {
		return fmt.Errorf("%s was never verified: %s", s.Name, was.Reason)
	}
	now := verifyServer(ctx, d, labDir, s)
	if !now.Verified {
		return fmt.Errorf("%s failed its identity check: %s", s.Name, now.Reason)
	}
	if now.PID != was.PID {
		return fmt.Errorf("%s: PID changed from %d to %d since it was verified", s.Name, was.PID, now.PID)
	}
	if now.Start != was.Start {
		return fmt.Errorf("%s: process %d started at %q, not %q — the PID was reused", s.Name, now.PID, now.Start, was.Start)
	}
	return d.kill(now.PID, sig)
}

// procState is what the OS says about a process. It sits beside the monitor
// state and never stands in for it (rule 2).
type procState string

const (
	procRunning procState = "running"
	procStopped procState = "stopped"
	procGone    procState = "gone"
	procUnknown procState = "unknown"
)

// readProcState reads `ps -o stat= -p <pid>`. A first letter T is stopped
// (SIGSTOP); any other state is running; no line with exit 1 is gone.
// Anything else — ps would not run, or exited oddly — is unknown, never a
// guess.
func readProcState(ctx context.Context, d procDeps, pid int) procState {
	r := d.run(ctx, "ps", "-o", "stat=", "-p", strconv.Itoa(pid))
	if r.Err != nil {
		return procUnknown
	}
	stat := strings.TrimSpace(r.Stdout)
	switch {
	case stat == "" && r.Exit == 1:
		return procGone
	case stat == "" || r.Exit != 0:
		return procUnknown
	case stat[0] == 'T':
		return procStopped
	default:
		return procRunning
	}
}

// labScriptRunning reports the first line of `ps -axo args=` that runs one
// of the lab's scripts, as argv[0] or as the first argument to a shell.
// Attach refuses while one runs, because lab_init kills every t- server.
// It only reads; it never signals what it finds.
func labScriptRunning(psArgs string, scripts []string) (string, bool) {
	isScript := map[string]bool{}
	for _, s := range scripts {
		isScript[s] = true
	}
	shells := map[string]bool{"bash": true, "sh": true, "zsh": true}
	for _, line := range strings.Split(psArgs, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if isScript[filepath.Base(f[0])] {
			return strings.TrimSpace(line), true
		}
		if len(f) > 1 && shells[filepath.Base(f[0])] && isScript[filepath.Base(f[1])] {
			return strings.TrimSpace(line), true
		}
	}
	return "", false
}
