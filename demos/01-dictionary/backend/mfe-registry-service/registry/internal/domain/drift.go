package domain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	DriftChecked    = "checked"
	DriftDifferent  = "drift"
	DriftNotChecked = "not checked"
)

// Drift is an observation for the operator, never a fact about whether an
// entry may be served (decisions 77/85). It deliberately lives beside Entry:
// putting it on the stored row would give a failed fetch a way to write to
// the catalogue. Fields are contract names, never values from the remote.
type Drift struct {
	State       string    `json:"state"`
	Fields      []string  `json:"fields,omitempty"`
	Stage       string    `json:"stage,omitempty"`
	Cause       string    `json:"cause,omitempty"`
	AttemptedAt time.Time `json:"attemptedAt,omitzero"`
}

var (
	ErrDriftHTTPStatus   = errors.New("manifest fetch returned a non-200 status")
	ErrDriftBodyTooLarge = errors.New("manifest fetch exceeded the body limit")
)

func UncheckedDrift(cause string) Drift {
	return Drift{State: DriftNotChecked, Stage: "manifest-drift", Cause: cause}
}

// FailedDrift translates failures into a closed vocabulary (BR-AS04).
// Transport errors contain the address that was dialled; parser errors can
// contain text chosen by the publisher. Neither belongs in a browser reply.
func FailedDrift(err error) Drift {
	cause := "fetch-failed"
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		cause = "timeout"
	case errors.Is(err, context.Canceled):
		cause = "cancelled"
	case errors.Is(err, ErrDriftHTTPStatus):
		cause = "http-status"
	case errors.Is(err, ErrDriftBodyTooLarge):
		cause = "body-too-large"
	}
	return UncheckedDrift(cause)
}

// CompareManifest compares the publisher's content, not the platform's
// enabled flag, withdrawal class or attestation envelope. Key order and
// whitespace are irrelevant here: drift asks what changed, not whether a
// signature still verifies. Only a parsed manifest can agree (BR-AS45).
func CompareManifest(curated Entry, body []byte) Drift {
	var served Entry
	var object map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(body))
	// Silently dropping an unfamiliar field would claim agreement about
	// content we did not compare. Refuse the observation instead, without
	// echoing a publisher-chosen field name in the diagnostic.
	decoder.DisallowUnknownFields()
	if json.Unmarshal(body, &object) != nil || object == nil || decoder.Decode(&served) != nil {
		return UncheckedDrift("invalid-manifest")
	}
	// JSON null and an empty object both decode into a zero Entry. They
	// are not manifests, and accepting them would turn an error page into
	// an apparently successful check. This is a shape check, not a second
	// copy of the shell's compatibility or contribution validation.
	if served.ID == "" || served.Name == "" || served.SchemaVersion <= 0 || served.ShellAPIVersion <= 0 ||
		served.Remote.Kind == "" || served.Remote.URL == "" || served.Remote.Module == "" || served.Contributions == nil {
		return UncheckedDrift("invalid-manifest")
	}
	left, right := driftContent(curated), driftContent(served)
	fields := []string{}
	for name, value := range left {
		if !bytes.Equal(value, right[name]) {
			fields = append(fields, name)
		}
	}
	for name := range right {
		if _, exists := left[name]; !exists {
			fields = append(fields, name)
		}
	}
	sort.Strings(fields)
	if len(fields) == 0 {
		return Drift{State: DriftChecked}
	}
	return Drift{State: DriftDifferent, Fields: fields}
}

func driftContent(entry Entry) map[string]json.RawMessage {
	entry = entry.signedContent()
	entry.Withheld = false
	if entry.Contributions == nil {
		entry.Contributions = []Contribution{}
	}
	body, _ := json.Marshal(entry)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(body, &fields)
	return fields
}

// FetchOrigins translates an already granted browser origin to an address
// the service can reach. Both maps are private and built once, so a stored
// entry can choose neither a new grant nor a new network destination.
type FetchOrigins struct {
	allowed   Allowlist
	addresses map[string]string
}

func (m FetchOrigins) Empty() bool { return len(m.addresses) == 0 }

func NewFetchOrigins(allowed Allowlist, mappings map[string]string) (FetchOrigins, []string) {
	out := FetchOrigins{allowed: allowed, addresses: map[string]string{}}
	warnings := []string{}
	for browser, address := range mappings {
		browser, address = strings.TrimSpace(browser), strings.TrimSpace(address)
		if !plainOrigin(browser) || !allowed.Permits(browser) {
			warnings = append(warnings, "ignored fetch mapping: browser origin is invalid or not allowlisted")
			continue
		}
		if !plainOrigin(address) {
			warnings = append(warnings, "ignored fetch mapping: service address must be an HTTP(S) origin without credentials, path, query or fragment")
			continue
		}
		origin, _ := originOf(browser)
		out.addresses[origin] = strings.TrimRight(address, "/") + "/manifest.json"
	}
	sort.Strings(warnings)
	return out, warnings
}

func plainOrigin(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	_, ok := originOf(raw)
	return ok && u.Hostname() != "" && u.User == nil && (u.Path == "" || u.Path == "/") && u.RawQuery == "" && !u.ForceQuery && u.Fragment == ""
}

// Target never falls back to the browser URL. In Docker, localhost names
// this service, not the plugin, and guessing would turn missing config into
// an outbound capability the operator never granted (BR-AS20/AS45).
func (m FetchOrigins) Target(entry Entry, source string) (string, Drift) {
	if source != SourcePreload {
		return "", UncheckedDrift("not-preloaded")
	}
	if m.allowed.Check(entry) != nil {
		return "", UncheckedDrift("origin-not-allowed")
	}
	origin, _ := originOf(entry.Remote.URL)
	if target := m.addresses[origin]; target != "" {
		return target, Drift{}
	}
	return "", UncheckedDrift("origin-unmapped")
}
