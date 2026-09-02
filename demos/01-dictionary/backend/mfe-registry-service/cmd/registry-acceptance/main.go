// Command registry-acceptance drives the *running* lab through the publisher
// lifecycle and asserts what the registry does at each step (app-shell 13f).
//
// Why this exists at all: Phase 8 built the announcement path and Phase 7 the
// trust table, but until 13c no credential in the stack could publish an
// announcement, so the whole tier lived inside Ginkgo specs. Those specs still
// own the rules. What they cannot show is that the deployed pieces — a
// bootstrap-minted credential, a seeded trust row, a sidecar holding a seed on
// a read-only mount, a Compose SIGTERM — actually meet in a running stack.
// First boot alone only proves `inserted` and `pending`. This binary drives
// the five outcomes first boot never reaches: `updated`, `requeued`, key
// rotation, revocation and its recovery, and a live withdraw-then-return
// spending N / N+1 / N+2.
//
// A command, not a skipping test suite, and deliberately so. A Postgres-backed
// spec that silently skips when its env var is unset still prints `ok`, and
// this repo has been bitten by exactly that. Here there is nothing to skip:
// run it and it either walks the whole sequence or exits non-zero saying which
// assertion failed.
//
//	cd demos/01-dictionary
//	go run ./backend/mfe-registry-service/cmd/registry-acceptance --reset
//
// --reset clears the registry schema first, which is what makes the run
// repeatable; without it the run starts from whatever the lab happens to hold
// and the release numbers below are the only thing that shifts.
//
// One trace a run leaves either way: the key it mints for the rotation step is
// retired at the end, not deleted, because there is no delete op — a trust
// anchor that can be silently emptied is not one. Repeated runs therefore stack
// up retired keys in the Publishers panel. --reset clears them, since it drops
// the trust table with the rest of the schema.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

const (
	// The plugin the sequence drives. One is enough and one is the point: the
	// other four stay untouched throughout, so "nothing else moved" is
	// checkable rather than asserted.
	subjectPlugin = "example-plugin"
	// Its Compose service, and the origin its deployment configuration stamps
	// into the manifest at announce time (BR-AS71). Before Phase 14 this named
	// the plugin's announcer sidecar; the sidecar is now the plugin's own
	// process, so the service to stop is the plugin itself. The sequence below
	// is unchanged — same nine steps, same four-plugin control group.
	// altOrigin is another *allowlisted* origin — the requeue branch
	// turns on crossing an origin, not on being refused by the allowlist, and
	// an unallowlisted URL would be rejected before the branch was reached.
	subjectService = "example-plugin-frontend"
	homeOrigin     = "http://localhost:7111"
	altOrigin      = "http://localhost:7113"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "\nFAILED: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\nPASSED — the publisher lifecycle behaved as the rules say it should.")
}

type options struct {
	natsURL     string
	accountsURL string
	composeDir  string
	reset       bool
	timeout     time.Duration
}

func run() error {
	var opt options
	flag.StringVar(&opt.natsURL, "nats-url", envOr("NATS_URL", "nats://localhost:4222"), "NATS URL, as published on the host")
	flag.StringVar(&opt.accountsURL, "accounts-url", envOr("ACCOUNTS_URL", "http://localhost:7202"), "accounts-service base URL, which mints the operator credential")
	flag.StringVar(&opt.composeDir, "compose-dir", envOr("COMPOSE_DIR", "."), "directory holding docker-compose.yml")
	flag.BoolVar(&opt.reset, "reset", false, "clear the registry schema and re-seed before starting (destructive: the plugin registry only)")
	flag.DurationVar(&opt.timeout, "timeout", 45*time.Second, "how long any single expected state change may take")
	flag.Parse()

	dir, err := filepath.Abs(opt.composeDir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err != nil {
		return fmt.Errorf("no docker-compose.yml in %s — run this from demos/01-dictionary or pass --compose-dir", dir)
	}

	h := &harness{dir: dir, timeout: opt.timeout}
	// Scratch holds the generated rotation seed and the re-origined manifest.
	// Inside the repo on purpose: a Docker bind mount source has to be a path
	// the Docker daemon can see, and macOS's os.MkdirTemp lands under
	// /var/folders, which Docker Desktop does not share by default.
	h.scratch = filepath.Join(dir, ".acceptance")
	if err := os.MkdirAll(h.scratch, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(h.scratch) //nolint:errcheck

	if opt.reset {
		if err := h.reset(); err != nil {
			return err
		}
	}

	nc, err := connect(opt.accountsURL, opt.natsURL, "/api/auth/adminConnectInfo", "registry-acceptance")
	if err != nil {
		return fmt.Errorf("connect as operator: %w", err)
	}
	defer nc.Close()
	h.nc = nc

	shell, err := connect(opt.accountsURL, opt.natsURL, "/api/auth/shellConnectInfo", "registry-acceptance-shell")
	if err != nil {
		return fmt.Errorf("connect as shell: %w", err)
	}
	defer shell.Close()
	h.shell = shell

	defer h.cleanup()
	return h.scenarios()
}

type harness struct {
	nc *nats.Conn
	// shell is a SECOND credential, and it has to be. Health is read on
	// api._platform.mfe-registry.frontend-plugins.health.v1, which is the
	// shell's subject and deliberately not the operator's (BR-AS25/AS27) — an
	// operator credential is refused there by the server. Reading health the
	// way a browser reads it is the honest way round anyway: the alternative
	// would have been to widen a grant so a test could reach it.
	shell   *nats.Conn
	dir     string
	scratch string
	timeout time.Duration
	// containers created by `docker compose run`, torn down by cleanup even
	// when a step fails — a stray detached sidecar would keep announcing into
	// the next run and make its first assertion lie.
	spawned []string
	// rotated is set once key rotation has happened, so cleanup knows to put
	// the original key back rather than leaving the lab unable to announce.
	rotated  bool
	original string
	rotatedK string
	step     int
}

// ---------------------------------------------------------------- reporting

func (h *harness) heading(format string, args ...any) {
	h.step++
	fmt.Printf("\n%2d. %s\n", h.step, fmt.Sprintf(format, args...))
}

func (h *harness) note(format string, args ...any) {
	fmt.Printf("    %s\n", fmt.Sprintf(format, args...))
}

func (h *harness) check(desc string, ok bool, detail string) error {
	if ok {
		fmt.Printf("    ok   %s\n", desc)
		return nil
	}
	fmt.Printf("    FAIL %s\n", desc)
	return fmt.Errorf("%s (%s)", desc, detail)
}

// ------------------------------------------------------------ compose control

func (h *harness) compose(args ...string) (string, error) {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = h.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker compose %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// spawn starts a detached one-off sidecar with overridden mounts. This is how
// the sequence announces as a *different* key or a *different* manifest
// without editing anything the repo owns: the Compose service keeps its own
// mounts, and the override lives only for the step that needs it.
// spawn starts a one-shot publisher over the plugin's own Compose service.
// env overrides that service's deployment configuration for this container
// only — since BR-AS71 the public origin is deployment configuration rather
// than a manifest field, so "the publisher moved origin" is expressed here and
// not by rewriting the manifest the plugin was built with.
func (h *harness) spawn(name string, env []string, mounts ...string) error {
	args := []string{"run", "-d", "--no-deps", "--name", name}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	for _, m := range mounts {
		args = append(args, "-v", m)
	}
	args = append(args, subjectService)
	if _, err := h.compose(args...); err != nil {
		return err
	}
	h.spawned = append(h.spawned, name)
	return nil
}

// kill sends SIGTERM and waits, so the unregister on shutdown is actually
// sent. `docker rm -f` would SIGKILL it, which is precisely the case BR-AS54
// says must NOT withdraw anything.
func (h *harness) kill(name string) error {
	if out, err := exec.Command("docker", "stop", "-t", "30", name).CombinedOutput(); err != nil {
		return fmt.Errorf("stop %s: %w\n%s", name, err, out)
	}
	_ = exec.Command("docker", "rm", "-f", name).Run()
	for i, n := range h.spawned {
		if n == name {
			h.spawned = append(h.spawned[:i], h.spawned[i+1:]...)
			break
		}
	}
	return nil
}

// hardKill is SIGKILL, and it exists so one step can express what `kill`
// deliberately cannot: a process that vanishes without saying anything. kill
// above stops with a grace period precisely so the unregister is sent; this
// one denies it that chance, which is the only way to reach the case BR-AS54
// is about.
func (h *harness) hardKill(name string) error {
	if out, err := exec.Command("docker", "kill", name).CombinedOutput(); err != nil {
		return fmt.Errorf("kill %s: %w\n%s", name, err, out)
	}
	_ = exec.Command("docker", "rm", "-f", name).Run()
	for i, n := range h.spawned {
		if n == name {
			h.spawned = append(h.spawned[:i], h.spawned[i+1:]...)
			break
		}
	}
	return nil
}

func (h *harness) cleanup() {
	for _, name := range append([]string{}, h.spawned...) {
		_ = h.kill(name)
	}
	if h.rotated {
		// Leave the lab able to announce again. The retired original is put
		// back to enabled and the generated key retired: a demo run that ends
		// with the only trusted key retired looks identical to a broken
		// bootstrap the next time someone starts the stack.
		fmt.Println("\n    restoring the original signing key")
		if err := h.setKeyState(subjectPlugin, h.original, mferegistry.KeyEnabled); err != nil {
			fmt.Fprintf(os.Stderr, "    could not re-enable the original key: %v\n", err)
		}
		if err := h.setKeyState(subjectPlugin, h.rotatedK, mferegistry.KeyRetired); err != nil {
			fmt.Fprintf(os.Stderr, "    could not retire the generated key: %v\n", err)
		}
	}
	if _, err := h.compose("start", subjectService); err != nil {
		fmt.Fprintf(os.Stderr, "    could not restart %s: %v\n", subjectService, err)
	}
}

func (h *harness) reset() error {
	fmt.Println("resetting the registry (schema drop, re-seed, sidecar restart) — the plugin registry only")
	if _, err := h.compose("exec", "-T", "mfe-registry-postgres",
		"psql", "-U", "mfe_registry", "-d", "mfe_registry", "-c", "DROP SCHEMA IF EXISTS registry CASCADE"); err != nil {
		return err
	}
	if _, err := h.compose("restart", "mfe-registry-service"); err != nil {
		return err
	}
	// The seeder does its own bounded wait on the registry's read subject, so
	// it is also the readiness gate here — if it exits 0 the registry answers.
	if _, err := h.compose("run", "--rm", "--no-deps", "registry-publisher-seed"); err != nil {
		return err
	}
	if _, err := h.compose("restart",
		"example-plugin-frontend", "example-plugin-slow-frontend",
		"example-plugin-unreachable-announcer", "example-plugin-activate-throws-frontend",
		"example-plugin-incompatible-frontend"); err != nil {
		return err
	}
	return nil
}

// ------------------------------------------------------------------- helpers

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func connect(accountsURL, natsURL, mint, name string) (*nats.Conn, error) {
	// Same mint as cmd/seed-publishers: accounts-service owns the trust chain
	// even though the subjects called are the registry's. Which mint decides
	// which half of the surface the connection can reach, so the caller names
	// it rather than this function assuming the operator's.
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(accountsURL + mint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("minting an operator credential returned %s", resp.Status)
	}
	var info struct {
		JWT  string `json:"jwt"`
		Seed string `json:"nkeySeed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return nats.Connect(natsURL, nats.Name(name), nats.UserJWTAndSeed(info.JWT, info.Seed), nats.NoReconnect())
}

func (h *harness) request(subject string, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	msg, err := h.nc.Request(subject, raw, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", subject, err)
	}
	return msg.Data, nil
}

// newSigningKey mints the rotation key. A user NKey, the same kind
// bootstrap-operator.sh mints for each publisher, written to the scratch
// directory so a sidecar can mount it read-only exactly as it mounts its own.
func (h *harness) newSigningKey(name string) (public, path string, err error) {
	kp, err := nkeys.CreateUser()
	if err != nil {
		return "", "", err
	}
	seed, err := kp.Seed()
	if err != nil {
		return "", "", err
	}
	public, err = kp.PublicKey()
	if err != nil {
		return "", "", err
	}
	path = filepath.Join(h.scratch, name)
	if err := os.WriteFile(path, seed, 0o600); err != nil {
		return "", "", err
	}
	return public, path, nil
}
