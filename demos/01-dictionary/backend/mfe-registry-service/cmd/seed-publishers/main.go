// Command seed-publishers installs the lab's publisher trust rows — one
// publisher per micro-frontend fixture, each holding its own signing key and
// owning its own plugin id — into a running mfe-registry-service over NATS
// request/reply.
//
// Like its sibling seed-registry, this is an operator client: it mints the
// same restricted PLATFORM credential the Admin UI uses and calls the same
// api._platform.mfe-registry.publishers.write.v1 endpoint the Registry Publishers
// panel calls. There is no boot-time trust bypass and no second write path —
// a seeded row obeys the revision check and leaves the same audit trail as an
// operator's own write (decision 7 of app-shell Phase 13).
//
// Unlike seed-registry, this seeder runs on every boot, so it cannot be a
// blind write. Each of the four publisher ops is revision-checked and spends
// a revision plus an audit row even when it changes nothing, and — far worse
// — a blind re-seed would hand a revoked key back its trust on the next
// restart, quietly undoing the one operator decision the revocation demo
// exists to show. So it reads first, applies only what is missing, and never
// reverses a decision an operator has already made (BR-AS68, decision 6).
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

// A row of the bootstrap's publishers.json — the public half of what
// nats/bootstrap-operator.sh minted per fixture (Phase 13c). The seed input
// carries no seeds and no key state: state is the operator's to set, and this
// file only ever proposes a key that should exist.
type publisherSeed struct {
	Publisher  string `json:"publisher"`
	Plugin     string `json:"plugin"`
	SigningKey string `json:"signingKey"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed-publishers:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		file     = flag.String("file", envOr("PUBLISHERS_FILE", ""), "publishers.json to seed (publisher/plugin/signingKey rows)")
		baseURL  = flag.String("url", envOr("ACCOUNTS_URL", "http://localhost:7202"), "accounts-service base URL")
		natsURL  = flag.String("nats-url", envOr("NATS_URL", nats.DefaultURL), "NATS server URL")
		waitFor  = flag.Duration("wait", 60*time.Second, "how long to wait for the registry to answer before giving up")
		waitStep = flag.Duration("wait-interval", 2*time.Second, "delay between readiness attempts")
	)
	flag.Parse()
	if *file == "" {
		return errors.New("-file is required")
	}

	raw, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	var want []publisherSeed
	if err := json.Unmarshal(raw, &want); err != nil {
		return fmt.Errorf("%s is not a publishers document: %w", *file, err)
	}
	if err := validate(want); err != nil {
		return fmt.Errorf("%s: %w", *file, err)
	}

	nc, err := connect(*baseURL, *natsURL, *waitFor, *waitStep)
	if err != nil {
		return err
	}
	defer nc.Close()
	c := &client{request: func(subject string, payload []byte) ([]byte, error) {
		msg, err := nc.Request(subject, payload, 10*time.Second)
		if err != nil {
			return nil, err
		}
		return msg.Data, nil
	}}

	// The registry may still be finishing its migrations when Compose starts
	// this one-shot. Waiting a bounded, logged while is right; racing it and
	// dying is not, because the announcers this gate protects would then start
	// against an unseeded trust table (decision 6).
	doc, err := c.awaitPublishers(*waitFor, *waitStep)
	if err != nil {
		return err
	}

	ops, notes := planOps(doc.Publishers, want)
	for _, note := range notes {
		fmt.Println("seed-publishers: leaving alone —", note)
	}
	if len(ops) == 0 {
		fmt.Printf("publisher trust already converged at revision %d: 0 writes\n", doc.Revision)
		return nil
	}

	revision := doc.Revision
	for _, op := range ops {
		next, err := c.apply(op, revision)
		if err != nil {
			return fmt.Errorf("%s %s: %w", op.Op, op.subject(), err)
		}
		revision = next
	}
	fmt.Printf("seeded publisher trust: %s; now at revision %d\n", summarise(ops), revision)
	return nil
}

// validate rejects a seed file that cannot produce a coherent trust table
// before any write is attempted, so a typo fails the one-shot loudly rather
// than half-applying and leaving the audit trail to explain it.
func validate(want []publisherSeed) error {
	byKey := map[string]string{}
	byPlugin := map[string]string{}
	for i, w := range want {
		switch {
		case w.Publisher == "":
			return fmt.Errorf("row %d names no publisher", i+1)
		case w.Plugin == "":
			return fmt.Errorf("row %d (%s) names no plugin", i+1, w.Publisher)
		case w.SigningKey == "":
			return fmt.Errorf("row %d (%s) carries no signing key", i+1, w.Publisher)
		}
		if other, dup := byKey[w.SigningKey]; dup {
			return fmt.Errorf("%s and %s claim the same signing key", other, w.Publisher)
		}
		byKey[w.SigningKey] = w.Publisher
		if other, dup := byPlugin[w.Plugin]; dup {
			return fmt.Errorf("%s and %s both claim plugin %q", other, w.Publisher, w.Plugin)
		}
		byPlugin[w.Plugin] = w.Publisher
	}
	return nil
}

// write is one publisher op on the wire. It mirrors browserrpc's
// PublisherWriteRequest; Actor is deliberately absent, because the endpoint
// stamps the shared operator identity itself and a client-supplied actor
// would be a claim rather than a fact.
type write struct {
	IfRevision  int64      `json:"ifRevision"`
	Op          string     `json:"op"`
	PublisherID string     `json:"publisherId"`
	Publisher   *publisher `json:"publisher,omitempty"`
	PublicKey   string     `json:"publicKey,omitempty"`
	PluginID    string     `json:"pluginId,omitempty"`
}

// subject is the thing this write is about, for an error message.
func (w write) subject() string {
	switch w.Op {
	case mferegistry.OpPublisherAddKey:
		return w.PublisherID + " key " + w.PublicKey
	case mferegistry.OpPublisherTransfer:
		return w.PublisherID + " plugin " + w.PluginID
	default:
		return w.PublisherID
	}
}

// planOps is the whole convergence decision, kept pure so it can be tested
// without a registry: given what the registry holds and what the bootstrap
// minted, return only the writes that are actually missing, plus a note for
// every difference deliberately left alone.
//
// The three "leave alone" cases are not defensive coding, they are the rule
// (BR-AS68). A key already present in any state stays in that state — forcing
// it back to enabled would undo a revocation on the next restart. A plugin
// already owned by another publisher stays owned by them — that is an
// operator transfer, and re-claiming it here would silently reverse it. A key
// held by another publisher is refused by the server anyway; naming it is
// more useful than sending a write that cannot succeed.
func planOps(current []publisher, want []publisherSeed) ([]write, []string) {
	rows := map[string]bool{}
	keyOwner := map[string]string{}
	keyState := map[string]string{}
	pluginOwner := map[string]string{}
	for _, p := range current {
		rows[p.ID] = true
		for _, k := range p.Keys {
			keyOwner[k.PublicKey] = p.ID
			keyState[k.PublicKey] = k.State
		}
		for _, id := range p.Plugins {
			pluginOwner[id] = p.ID
		}
	}

	// Sorted so a run's op sequence — and its audit trail — is the same every
	// time, whatever order the bootstrap wrote the file in.
	seeds := append([]publisherSeed(nil), want...)
	sort.Slice(seeds, func(i, j int) bool { return seeds[i].Publisher < seeds[j].Publisher })

	var ops []write
	var notes []string
	for _, s := range seeds {
		if !rows[s.Publisher] {
			ops = append(ops, write{
				Op:          mferegistry.OpPublisherUpsert,
				PublisherID: s.Publisher,
				Publisher:   &publisher{ID: s.Publisher, Name: s.Publisher},
			})
			rows[s.Publisher] = true
		}

		switch owner, held := keyOwner[s.SigningKey]; {
		case !held:
			ops = append(ops, write{
				Op:          mferegistry.OpPublisherAddKey,
				PublisherID: s.Publisher,
				PublicKey:   s.SigningKey,
			})
			keyOwner[s.SigningKey] = s.Publisher
			keyState[s.SigningKey] = mferegistry.KeyEnabled
		case owner != s.Publisher:
			notes = append(notes, fmt.Sprintf("%s's signing key is held by publisher %q", s.Publisher, owner))
		case keyState[s.SigningKey] != mferegistry.KeyEnabled:
			notes = append(notes, fmt.Sprintf("%s's signing key is %s — an operator decision, not re-enabled", s.Publisher, keyState[s.SigningKey]))
		}

		switch owner, owned := pluginOwner[s.Plugin]; {
		case !owned:
			ops = append(ops, write{
				Op:          mferegistry.OpPublisherTransfer,
				PublisherID: s.Publisher,
				PluginID:    s.Plugin,
			})
			pluginOwner[s.Plugin] = s.Publisher
		case owner != s.Publisher:
			notes = append(notes, fmt.Sprintf("plugin %q is owned by publisher %q — a transfer, not reversed", s.Plugin, owner))
		}
	}
	return ops, notes
}

func summarise(ops []write) string {
	var rows, keys, plugins int
	for _, op := range ops {
		switch op.Op {
		case mferegistry.OpPublisherUpsert:
			rows++
		case mferegistry.OpPublisherAddKey:
			keys++
		case mferegistry.OpPublisherTransfer:
			plugins++
		}
	}
	return fmt.Sprintf("%d rows, %d keys, %d plugin claims", rows, keys, plugins)
}

type client struct {
	request func(subject string, payload []byte) ([]byte, error)
}

func connect(base, natsURL string, waitFor, waitStep time.Duration) (*nats.Conn, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	// accounts-service starts its HTTP listener after its own migrations and
	// NATS connect, and this one-shot starts beside that container. Minting
	// the credential is a readiness race, never a decision to retry, so it
	// runs on the same bounded schedule as the registry read below; a refused
	// *write* is the only thing that still fails fast.
	deadline := time.Now().Add(waitFor)
	var jwt, seed string
	for {
		minted, err := mintCredential(httpClient, base)
		if err == nil {
			jwt, seed = minted.jwt, minted.seed
			break
		}
		if time.Now().Add(waitStep).After(deadline) {
			return nil, err
		}
		time.Sleep(waitStep)
	}
	// This short-lived operator CLI fails fast, unlike a long-lived service.
	return nats.Connect(natsURL, nats.Name("seed-publishers"), nats.UserJWTAndSeed(jwt, seed), nats.NoReconnect())
}

// mintCredential performs one fetch-and-decode of the operator connect info.
// A non-200 answer and a malformed body are both errors; nothing here decides
// whether the attempt should be retried.
func mintCredential(httpClient *http.Client, base string) (struct {
	jwt  string
	seed string
}, error) {
	resp, err := httpClient.Get(base + "/api/auth/adminConnectInfo")
	if err != nil {
		return struct {
			jwt  string
			seed string
		}{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return struct {
			jwt  string
			seed string
		}{}, fmt.Errorf("minting an operator credential returned %s", resp.Status)
	}
	var info struct {
		JWT  string `json:"jwt"`
		Seed string `json:"nkeySeed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return struct {
			jwt  string
			seed string
		}{}, err
	}
	return struct {
		jwt  string
		seed string
	}{jwt: info.JWT, seed: info.Seed}, nil
}

// publisher and publisherKey are the read shape of the publishers document,
// not the registry's domain model — only the three facts convergence turns
// on (who holds which key in which state, and who owns which plugin). The
// registry's own richer struct lives behind its internal package and is
// unreachable from a cmd/ client by Go's own rule.
type publisher struct {
	ID      string         `json:"id"`
	Name    string         `json:"name,omitempty"`
	Keys    []publisherKey `json:"keys"`
	Plugins []string       `json:"plugins"`
}

type publisherKey struct {
	PublicKey string `json:"publicKey"`
	State     string `json:"state"`
}

type publishersDocument struct {
	Revision   int64       `json:"revision"`
	Publishers []publisher `json:"publishers"`
	Error      string      `json:"error"`
}

func (c *client) publishers() (publishersDocument, error) {
	data, err := c.request(mferegistry.Publishers, []byte(`{}`))
	if err != nil {
		return publishersDocument{}, err
	}
	var out publishersDocument
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	if out.Error != "" {
		return out, errors.New(out.Error)
	}
	return out, nil
}

// awaitPublishers retries the read on a bounded schedule. Only the read is
// retried: a refused *write* is a decision, never a timing accident, and
// retrying one would be how a stale revision turns into a lost update.
func (c *client) awaitPublishers(within, step time.Duration) (publishersDocument, error) {
	deadline := time.Now().Add(within)
	for attempt := 1; ; attempt++ {
		doc, err := c.publishers()
		if err == nil {
			return doc, nil
		}
		if time.Now().After(deadline) {
			return publishersDocument{}, fmt.Errorf("registry did not answer within %s: %w", within, err)
		}
		fmt.Printf("seed-publishers: registry not ready (attempt %d: %v), retrying in %s\n", attempt, err, step)
		time.Sleep(step)
	}
}

func (c *client) apply(w write, revision int64) (int64, error) {
	w.IfRevision = revision
	payload, err := json.Marshal(w)
	if err != nil {
		return 0, err
	}
	data, err := c.request(mferegistry.PublisherWrite, payload)
	if err != nil {
		return 0, err
	}
	var out publishersDocument
	if err := json.Unmarshal(data, &out); err != nil {
		return 0, err
	}
	if out.Error != "" {
		return 0, errors.New(out.Error)
	}
	return out.Revision, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
