// Command seed-registry loads a curated frontend plugin registry document
// into a running accounts-service over REST.
//
// Over REST, by hand, never at process start (decision 24). A boot-time file
// read alongside a database would let a restart silently revert curation;
// running the seeder through the same endpoint the admin surface uses means
// the seed obeys the same rules — origin allowlist, revision check, audit —
// as any other write, rather than a second path around them.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
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
		user    = flag.String("user", envOr("ACCOUNTS_USER", "admin"), "basic auth user")
		pass    = flag.String("pass", envOr("ACCOUNTS_PASS", "accounts-spike-pass"), "basic auth password")
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

	c := &client{base: *baseURL, user: *user, pass: *pass, http: &http.Client{Timeout: 10 * time.Second}}

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
	base string
	user string
	pass string
	http *http.Client
}

func (c *client) revision() (int64, error) {
	req, err := http.NewRequest(http.MethodGet, c.base+"/api/registry/entries", nil)
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth(c.user, c.pass)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("reading the registry returned %s", resp.Status)
	}
	var body struct {
		Revision int64 `json:"revision"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, err
	}
	return body.Revision, nil
}

func (c *client) upsert(entry json.RawMessage, revision int64) error {
	payload, err := json.Marshal(map[string]json.RawMessage{"entry": entry})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.base+"/api/registry/entries", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", strconv.FormatInt(revision, 10))

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(detail))
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
