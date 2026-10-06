package main

// The session file (D03-R28): the history, appended as JSON lines to
// playground/.run/sessions/<stamp>.jsonl. Gitignored, and outside lab/run/,
// so no report renderer ever reads it. A new file opens each time a rig
// becomes ready, because its first line names that rig's owner and server
// version.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type sessionHeader struct {
	Kind          string    `json:"kind"`
	Evidence      bool      `json:"evidence"`
	Session       string    `json:"session"`
	Opened        time.Time `json:"opened"`
	Owner         string    `json:"owner"`
	ServerVersion string    `json:"serverVersion"`
	Commit        string    `json:"commit"`
}

// swapSink runs swap under the history's lock, with the kept events, and
// makes the func it returns the new sink. No event can arrive in between.
func (h *history) swapSink(swap func(kept []event) func(event)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sink = swap(h.events)
}

type sessionFiles struct {
	dir     string
	session string
	hist    *history
	fetch   fetchFunc
	commit  string

	// mu orders opens. f and lastSeq belong to the history's lock.
	mu      sync.Mutex
	f       *os.File
	lastSeq int
}

// gitCommit is the service's commit, read once at start-up. "-dirty" means
// the service ran with uncommitted changes.
func gitCommit(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "describe", "--always", "--dirty", "--abbrev=7").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// serverVersion reads /varz from the first server that answers.
func serverVersion(ctx context.Context, fetch fetchFunc) string {
	for _, s := range servers {
		status, body, err := fetch(ctx, fmt.Sprintf("http://127.0.0.1:%d/varz", s.Monitor))
		if err != nil || status != 200 {
			continue
		}
		var v struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(body, &v) == nil && v.Version != "" {
			return v.Version
		}
	}
	return "unknown"
}

// open starts a new session file for a rig that just became ready. The
// events since the last file closed are replayed into it first.
func (s *sessionFiles) open(owner string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	version := serverVersion(ctx, s.fetch)
	cancel()

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		s.hist.add(eventObservation, 0, "", "session file not written: "+err.Error())
		return
	}
	now := time.Now()
	path := filepath.Join(s.dir, now.Format("20060102-150405")+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		s.hist.add(eventObservation, 0, "", "session file not written: "+err.Error())
		return
	}
	head, _ := json.Marshal(sessionHeader{
		Kind: "playground", Evidence: false, Session: s.session, Opened: now,
		Owner: owner, ServerVersion: version, Commit: s.commit,
	})
	fmt.Fprintf(f, "%s\n", head)
	// The swap, the replay and every write run under the history's lock.
	s.hist.swapSink(func(kept []event) func(event) {
		if s.f != nil {
			s.f.Close()
		}
		s.f = f
		for _, e := range kept {
			if e.Seq > s.lastSeq {
				s.write(e)
			}
		}
		return s.write
	})
}

// write runs only under the history's lock.
func (s *sessionFiles) write(e event) {
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	if s.f != nil {
		fmt.Fprintf(s.f, "%s\n", line)
	}
	s.lastSeq = e.Seq
}

func (s *sessionFiles) close() {
	s.hist.swapSink(func([]event) func(event) {
		if s.f != nil {
			s.f.Close()
			s.f = nil
		}
		return nil
	})
}
