package main

// The rig lifecycle (D03-R27, the ownership table in CLAUDE.md).
//
//	start   runs lab/rig-t4.sh up with a fixed argv, then verifies the nine
//	        and creates exercise 10's three ODOMETER_<SITE> streams.
//	attach  verifies the nine and reads meta size 9 from a monitor. It
//	        changes nothing on the rig.
//	stop    CONT then TERM to the nine verified PIDs, and waits for them to
//	        go. Never rig-t4.sh down: that is a pattern kill.
//
// Never rig-t4.sh freeze or thaw: they sleep and wait with no limit. Both
// start and attach refuse while a lab script runs, because lab_init kills
// every t- server.

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// streamSetup is natsClient.ensureStreams. Specs replace it.
type streamSetup interface {
	ensureStreams(ctx context.Context) error
}

type labRig struct {
	lab   string
	proc  procDeps
	fetch fetchFunc
	nats  streamSetup
	// scripts are the lab's script names, for labScriptRunning.
	scripts []string
	// up runs rig-t4.sh up. Specs replace it.
	up func(ctx context.Context) (string, error)
	// waitEvery is how often stop reads ps while it waits.
	waitEvery time.Duration
}

func newLabRig(lab string, proc procDeps, fetch fetchFunc, nc streamSetup) *labRig {
	r := &labRig{lab: lab, proc: proc, fetch: fetch, nats: nc, waitEvery: 200 * time.Millisecond}
	r.up = r.runUp
	r.scripts = labScripts(lab)
	return r
}

// runUp runs `<lab>/rig-t4.sh up`: no shell of ours, no text from the
// browser. The script gets its own process group, so a Ctrl-C at the
// service's terminal does not reach the nine servers it leaves running; the
// service stops them itself. On the limit the script gets SIGTERM, so its
// own trap tears down a half-started rig.
func (r *labRig) runUp(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, filepath.Join(r.lab, "rig-t4.sh"), "up")
	cmd.Dir = r.lab
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 10 * time.Second
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// labScripts are the lab's script names.
func labScripts(lab string) []string {
	paths, _ := filepath.Glob(filepath.Join(lab, "*.sh"))
	names := make([]string, 0, len(paths))
	for _, p := range paths {
		names = append(names, filepath.Base(p))
	}
	return names
}

func (r *labRig) refuseIfLabRuns(ctx context.Context) error {
	ps := r.proc.run(ctx, "ps", "-axo", "args=")
	if ps.Err != nil {
		return fmt.Errorf("could not read the process list: %v", ps.Err)
	}
	if line, ok := labScriptRunning(ps.Stdout, r.scripts); ok {
		return fmt.Errorf("a lab script is running (%s); its lab_init kills every t- server, so wait for it to end", line)
	}
	return nil
}

func (r *labRig) verifyAll(ctx context.Context) []identity {
	ids := make([]identity, 0, len(servers))
	for _, s := range servers {
		ids = append(ids, verifyServer(ctx, r.proc, r.lab, s))
	}
	return ids
}

func allVerified(ids []identity) bool {
	for _, id := range ids {
		if !id.Verified {
			return false
		}
	}
	return len(ids) == len(servers)
}

func (r *labRig) start(ctx context.Context) ([]identity, error) {
	if err := r.refuseIfLabRuns(ctx); err != nil {
		return nil, err
	}
	out, err := r.up(ctx)
	if err != nil {
		return nil, fmt.Errorf("rig-t4.sh up failed (%v): %s", err, lastLines(out, 3))
	}
	ids := r.verifyAll(ctx)
	if !allVerified(ids) {
		return ids, nil
	}
	if err := r.nats.ensureStreams(ctx); err != nil {
		// This service started the rig, so it does not leave a half-set-up
		// one behind that the page cannot stop.
		stopCtx, cancel := context.WithTimeout(context.Background(), stopWaitFor)
		defer cancel()
		if serr := r.stop(stopCtx, ids); serr != nil {
			return nil, fmt.Errorf("the streams were not created (%v), and the rig did not stop: %v", err, serr)
		}
		return nil, fmt.Errorf("the streams were not created (%v); the rig was stopped again", err)
	}
	return ids, nil
}

func (r *labRig) attach(ctx context.Context) ([]identity, error) {
	if err := r.refuseIfLabRuns(ctx); err != nil {
		return nil, err
	}
	ids := r.verifyAll(ctx)
	if !allVerified(ids) {
		return ids, nil
	}
	var why []string
	for _, s := range servers {
		status, body, err := r.fetch(ctx, jszURL(s))
		if err != nil || status != 200 {
			why = append(why, fmt.Sprintf("%s: no /jsz answer", s.Name))
			continue
		}
		size, err := parseMetaSize(body)
		if err != nil {
			why = append(why, fmt.Sprintf("%s: %v", s.Name, err))
			continue
		}
		if size != metaPeers {
			return nil, fmt.Errorf("%s reads meta size %d, not %d: this is not the T4 rig", s.Name, size, metaPeers)
		}
		return ids, nil
	}
	return nil, fmt.Errorf("no monitor gave a meta size: %s", joinErrs(why))
}

// stop sends CONT, so a frozen server can act on the TERM, then TERM, to
// each verified PID, and waits for all of them to go.
func (r *labRig) stop(ctx context.Context, ids []identity) error {
	var errs []string
	var sent []identity
	for _, id := range ids {
		s, ok := serverNamed(id.Server)
		if !ok || !id.Verified {
			continue
		}
		if err := signalVerified(ctx, r.proc, r.lab, s, id, syscall.SIGCONT); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if err := signalVerified(ctx, r.proc, r.lab, s, id, syscall.SIGTERM); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		sent = append(sent, id)
	}
	alive := r.waitGone(ctx, sent)
	for _, id := range alive {
		errs = append(errs, fmt.Sprintf("%s (PID %d) did not end", id.Server, id.PID))
	}
	if len(errs) > 0 {
		return errors.New(joinErrs(errs))
	}
	return nil
}

// waitGone reads ps until every process is gone or ctx ends, and returns
// the ones still there.
func (r *labRig) waitGone(ctx context.Context, ids []identity) []identity {
	t := time.NewTicker(r.waitEvery)
	defer t.Stop()
	for {
		var alive []identity
		for _, id := range ids {
			if readProcState(ctx, r.proc, id.PID) != procGone {
				alive = append(alive, id)
			}
		}
		if len(alive) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return alive
		case <-t.C:
		}
	}
}

// release is the service's own shutdown (the ownership table). An owned rig
// is stopped. An attached rig gets CONT only for the processes this service
// stopped, and is left running.
func (c *controller) release(ctx context.Context) string {
	c.cancelAll("the service is shutting down")
	c.mu.Lock()
	owner, status := c.owner, c.status
	ids := make([]identity, 0, len(servers))
	for _, s := range servers {
		if id, ok := c.ids[s.Name]; ok {
			ids = append(ids, id)
		}
	}
	ours := map[string]bool{}
	for k := range c.stoppedByUs {
		ours[k] = true
	}
	c.mu.Unlock()

	if status != rigReady {
		return "no ready rig: nothing to release"
	}
	if owner == ownerOwned {
		ctx, cancel := context.WithTimeout(ctx, stopWaitFor)
		defer cancel()
		if err := c.d.rig.stop(ctx, ids); err != nil {
			return "owned rig: stop incomplete: " + err.Error()
		}
		return "owned rig: nine processes ended"
	}
	var errs []string
	n := 0
	for _, id := range ids {
		if !ours[id.Server] {
			continue
		}
		s, _ := serverNamed(id.Server)
		if err := signalVerified(ctx, c.d.proc, c.d.lab, s, id, syscall.SIGCONT); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		n++
	}
	text := fmt.Sprintf("attached rig left running; CONT sent to the %d processes this service stopped", n)
	if len(errs) > 0 {
		text += "; not sent: " + joinErrs(errs)
	}
	return text
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " / ")
}
