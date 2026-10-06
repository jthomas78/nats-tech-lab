// The T4 playground's control service. See ../PLAYGROUND-PLAN.md.
//
//	playground serve [-addr 127.0.0.1:20302] [-lab <dir>] [-origin a,b]
//
// -lab is demos/03-multi-cluster-and-accounts/lab; by default it is found
// beside the working directory. The service listens on 127.0.0.1 only.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: playground serve [-addr 127.0.0.1:20302] [-lab <dir>] [-origin a,b]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:20302", "listen address; 127.0.0.1 only")
	lab := fs.String("lab", "../lab", "the demo's lab/ folder, holding rig-t4.sh")
	origin := fs.String("origin", strings.Join(allowedOrigins, ","), "Origins that may POST, comma-separated, exact match")
	fs.Parse(os.Args[2:])
	if err := serve(*addr, *lab, strings.Split(*origin, ",")); err != nil {
		log.Fatal(err)
	}
}

// labDir resolves -lab to an absolute, symlink-free path that holds
// rig-t4.sh, so the identity checks compare against one exact path.
func labDir(lab string) (string, error) {
	abs, err := filepath.Abs(lab)
	if err != nil {
		return "", err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("-lab %s: %v", lab, err)
	}
	if _, err := os.Stat(filepath.Join(abs, "rig-t4.sh")); err != nil {
		return "", fmt.Errorf("-lab %s holds no rig-t4.sh", abs)
	}
	return abs, nil
}

// osProc is the real procDeps: a fixed argv, no shell.
func osProc() procDeps {
	return procDeps{
		run: func(ctx context.Context, name string, args ...string) runResult {
			out, err := exec.CommandContext(ctx, name, args...).Output()
			var exit *exec.ExitError
			if errors.As(err, &exit) && ctx.Err() == nil {
				return runResult{Stdout: string(out), Exit: exit.ExitCode()}
			}
			return runResult{Stdout: string(out), Err: err}
		},
		readFile: os.ReadFile,
		kill:     func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) },
	}
}

func serve(addr, lab string, origins []string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host != "127.0.0.1" {
		return fmt.Errorf("-addr %s: the service listens on 127.0.0.1 only", addr)
	}
	lab, err = labDir(lab)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	now := time.Now
	session := now().Format("20060102-150405")
	proc := osProc()
	fetch := httpFetch(&http.Client{})
	st := newStore()
	hist := newHistory(now)
	files := &sessionFiles{
		dir:     filepath.Join(filepath.Dir(lab), "playground", ".run", "sessions"),
		session: session, hist: hist, fetch: fetch,
		commit: gitCommit(lab),
	}
	defer files.close()

	// Commands and pollers hang off bg, not the signal context: on Ctrl-C
	// release cancels each command with its reason (rule 9) first.
	bg, cancelBg := context.WithCancel(context.Background())
	defer cancelBg()
	rig := newLabRig(lab, proc, fetch, &natsClient{})
	c := newController(bg, controllerDeps{
		lab: lab, proc: proc, rig: rig, nats: natsClient{},
		now: now, t: defaultTimings(), session: session,
		onReady: files.open,
	}, st, hist)
	m := &monitor{fetch: fetch, proc: proc, pids: c.verifiedPIDs, st: st, hist: hist, now: now}

	go m.run(bg)
	go c.runTicks(bg)

	srv := &http.Server{Addr: addr, Handler: newAPI(c, origins), ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Printf("playground: session %s, lab %s, listening on http://%s", session, lab, addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Print("playground: shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	srv.Shutdown(sctx)
	cancel()
	log.Print("playground: " + c.release(context.Background()))
	return nil
}
