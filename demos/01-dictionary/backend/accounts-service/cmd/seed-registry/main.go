// Command seed-registry loads a curated frontend plugin registry document
// into a running accounts-service over NATS request/reply.
//
// An operator client, run by hand, never at process start (decision 24). It
// mints the same restricted PLATFORM credential as the Admin UI and calls
// its api.* subjects; this is not a service-to-service rpc.* caller. A boot-time file
// read alongside a database would let a restart silently revert curation;
// running the seeder through the same endpoint the admin surface uses means
// the seed obeys the same rules — origin allowlist, revision check, audit —
// as any other write, rather than a second path around them.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/nats-io/nats.go"
)

type seedDocument struct {
	SchemaVersion int               `json:"schemaVersion"`
	Plugins       []json.RawMessage `json:"plugins"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed-registry:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		file    = flag.String("file", "", "registry document to seed (schemaVersion + plugins)")
		baseURL = flag.String("url", envOr("ACCOUNTS_URL", "http://localhost:7202"), "accounts-service base URL")
		natsURL = flag.String("nats-url", envOr("NATS_URL", nats.DefaultURL), "NATS server URL")
	)
	flag.Parse()
	if *file == "" {
		return errors.New("-file is required")
	}

	raw, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	var doc seedDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("%s is not a registry document: %w", *file, err)
	}
	if doc.SchemaVersion != 1 {
		return fmt.Errorf("%s is schemaVersion %d, this seeder writes 1", *file, doc.SchemaVersion)
	}

	nc, err := connect(*baseURL, *natsURL)
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

	// One entry per request, each keyed on the revision the last one
	// installed. Sequential rather than batched on purpose: a batch would
	// need its own all-or-nothing semantics, and a half-applied seed is
	// visible and fixable in the audit trail, where a silently rolled-back
	// one is not.
	for i, entry := range doc.Plugins {
		rev, err := c.revision()
		if err != nil {
			return err
		}
		if err := c.upsert(entry, rev); err != nil {
			return fmt.Errorf("entry %d: %w", i+1, err)
		}
	}
	fmt.Printf("seeded %d entries from %s\n", len(doc.Plugins), *file)
	return nil
}

type client struct {
	request func(subject string, payload []byte) ([]byte, error)
}

func connect(base, natsURL string) (*nats.Conn, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Get(base + "/api/auth/adminConnectInfo")
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
	// This short-lived operator CLI fails fast, unlike a long-lived service.
	return nats.Connect(natsURL, nats.Name("seed-registry"), nats.UserJWTAndSeed(info.JWT, info.Seed), nats.NoReconnect())
}

type response struct {
	Revision int64  `json:"revision"`
	Error    string `json:"error"`
}

func (c *client) call(subject string, payload []byte) (response, error) {
	data, err := c.request(subject, payload)
	if err != nil {
		return response{}, err
	}
	var out response
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	if out.Error != "" {
		return out, errors.New(out.Error)
	}
	return out, nil
}

func (c *client) revision() (int64, error) {
	out, err := c.call("api._platform.registry.entries.curated.v1", []byte(`{}`))
	return out.Revision, err
}

func (c *client) upsert(entry json.RawMessage, revision int64) error {
	var identity struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(entry, &identity); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"entry": entry, "entryId": identity.ID, "ifRevision": revision})
	if err != nil {
		return err
	}
	_, err = c.call("api._platform.registry.entries.upsert.v1", payload)
	return err
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
