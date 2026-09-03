// Package domain holds the frontend plugin registry's rules: what a curated
// document is, which writes are legal, how a revision moves, and which
// remote origins the platform will let the shell fetch code from
// (BR-AS16–BR-AS24 in BUSINESS_RULES-APP-SHELL.md).
//
// Nothing here knows about Postgres, NATS KV or HTTP. That is deliberate and
// load-bearing: Phase 2's claim is that the store choice is reversible, and a
// claim like that is a property of the interface, not of the store.
package domain

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// SchemaVersion is the shape of the document the shell reads. The shell
// refuses a document whose version it does not know rather than guessing at
// the fields, so this number moving is a breaking change for every shell.
const SchemaVersion = 1

// ShellAPIVersion is the host contract each plugin is built against —
// a separate number from SchemaVersion because the document's shape and the
// API a plugin calls change for different reasons.
const ShellAPIVersion = 1

// SharedAdminActor identifies curation through the shared operator surface,
// never an individual human (BR-AS23). Preload and announcements use their
// own actors (BR-AS42).
const SharedAdminActor = "admin"

// Revision values. NoRevision is "this registry has never been written";
// DegradedRevision is reserved for the outage document and can never collide
// with a real revision, which starts at 1 (BR-AS17, BR-AS22).
const (
	NoRevision       int64 = 0
	DegradedRevision int64 = 0
)

// Remote kinds. Only federated remotes exist today; the field is here so a
// second kind is a new case rather than a new schema.
const RemoteFederated = "federated"

// Errors the adapters translate into refusals. Each maps to a rule.
var (
	ErrStaleRevision    = errors.New("registry: write is not keyed on the current revision")
	ErrOriginNotAllowed = errors.New("registry: remote origin is not on the configured allowlist")
	ErrUnknownOp        = errors.New("registry: unknown write op")
	ErrEntryIDMismatch  = errors.New("registry: entry body does not match the id it is filed under")
	ErrNoActor          = errors.New("registry: write carries no actor")
	ErrNoEntryID        = errors.New("registry: write names no entry")
	ErrNoEntry          = errors.New("registry: upsert carries no entry body")
	ErrRevisionRequired = errors.New("registry: the write must carry the revision it was made against")
)

// RequireRevision distinguishes an absent precondition from revision zero,
// which is the legitimate version of a registry that has not yet been curated.
func RequireRevision(present bool) error {
	if !present {
		return ErrRevisionRequired
	}
	return nil
}

// StaleRevisionError carries what the refusal has to say for the admin
// surface to be usable: which revision the writer must reapply on top of.
// Nothing is merged — two curation decisions are not something a server
// should guess at (decision 27).
type StaleRevisionError struct {
	Current  int64
	Supplied int64
}

func (e StaleRevisionError) Error() string {
	return fmt.Sprintf("registry: write supplied revision %d, registry is on %d", e.Supplied, e.Current)
}

func (e StaleRevisionError) Unwrap() error { return ErrStaleRevision }

// NextRevision returns the revision an accepted write installs. Monotonic by
// construction and never 0, so DegradedRevision stays unambiguous.
func NextRevision(current int64) int64 { return current + 1 }

// CheckRevision enforces BR-AS18. A write must be keyed on exactly the
// revision its author read: older means someone else wrote first, newer means
// the author read a registry this one never served. Neither is merged.
func CheckRevision(current, supplied int64) error {
	if current == supplied {
		return nil
	}
	return StaleRevisionError{Current: current, Supplied: supplied}
}

// Remote is where the shell fetches a plugin's code from.
type Remote struct {
	Kind   string `json:"kind"`
	URL    string `json:"url,omitempty"`
	Name   string `json:"name,omitempty"`
	Module string `json:"module"`
}

// ExtensionPoint is a slot an entry declares for others to contribute into.
type ExtensionPoint struct {
	ID          string `json:"id"`
	Capacity    int    `json:"capacity,omitempty"`
	Description string `json:"description,omitempty"`
}

// Contribution is deliberately one flat struct covering all five kinds
// rather than a union: the shell validates each kind's own required fields,
// and duplicating that knowledge here would give the two sides two
// definitions of one contract to drift apart.
type Contribution struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Order      int    `json:"order,omitempty"`
	Permission string `json:"permission,omitempty"`
	Component  string `json:"component,omitempty"`

	Path  string `json:"path,omitempty"`
	Title string `json:"title,omitempty"`

	Label string `json:"label,omitempty"`
	Route string `json:"route,omitempty"`
	Group string `json:"group,omitempty"`
	Icon  string `json:"icon,omitempty"`

	Target string `json:"target,omitempty"`
	Region string `json:"region,omitempty"`

	Routes []string `json:"routes,omitempty"`
}

// Entry is one curated plugin.
//
// Enabled is a plain bool here, unlike the pointer the file-backed registry
// needed: a stored row always states it. The pointer existed only so an
// operator hand-editing JSON could omit the field, and rows do not have that
// problem.
type Entry struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	Version         string `json:"version,omitempty"`
	SchemaVersion   int    `json:"schemaVersion"`
	ShellAPIVersion int    `json:"shellApiVersion"`
	RoutePrefix     string `json:"routePrefix,omitempty"`
	Enabled         bool   `json:"enabled"`
	// Lifecycle is the withdrawal class: static or dynamic. Stored, never
	// inferred by a reader (Phase 5 decision 59). The registration path
	// supplies the default and an operator may override it (decision 86).
	// Empty on rows written before the field existed; readers treat empty
	// as "not yet classified", which is not the same as dynamic.
	Lifecycle       string `json:"lifecycle,omitempty"`
	AnnouncedAt     string `json:"announcedAt,omitempty"`
	LastAnnouncedAt string `json:"lastAnnouncedAt,omitempty"`
	// Release is the monotonic counter inside the signed bytes (BR-AS47).
	// Self-asserted on purpose — it is signed, so a publisher cannot move it
	// without the key. Stored only as part of the projection: the highest
	// release accepted for an id is that id's entry, there being one.
	Release int64 `json:"release,omitempty"`
	// Withheld marks an entry taken out of service because the key that
	// signed it was revoked (BR-AS38). Distinct from a merely disabled entry:
	// disabled is "not reviewed yet", withheld is "we withdrew this". The
	// shell needs the difference in 7e — a withheld entry is unloaded from a
	// running shell, which a pending one never was. Store-owned, like Enabled
	// and Lifecycle: no publisher asserts it.
	Withheld bool `json:"withheld,omitempty"`
	// Withdrawn marks an entry whose publisher said it is gone: an accepted,
	// signed unregister (BR-AS54). Separate from Enabled on purpose —
	// availability is the publisher's to state, approval is the operator's,
	// and BR-AS55 turns on the two never being one flag. Store-owned: a
	// payload asserting it says nothing.
	Withdrawn bool `json:"withdrawn,omitempty"`
	// Manifest is the publisher's signed bytes, when there are any. Held
	// beside the projection above rather than derived from it: see
	// manifest.go for why reassembly is what the rule forbids.
	Manifest        *Manifest        `json:"manifest,omitempty"`
	Remote          Remote           `json:"remote"`
	ExtensionPoints []ExtensionPoint `json:"extensionPoints,omitempty"`
	Contributions   []Contribution   `json:"contributions"`
	// BackendServices is what this plugin says it cannot work without: the
	// backend service ids whose readiness its health decoration reflects
	// (BR-AS62). Publisher-asserted and inside the signed bytes, because it
	// is the plugin describing itself and it travels with the plugin rather
	// than with a deployment that has to be told about it separately.
	//
	// It is a REQUEST and never a grant. Nothing is probed on the strength of
	// this field alone — ApprovedBackendServices below is what the health
	// plane reads — because a publisher that could name its own probe target
	// could point the registry at a service it does not own and read the
	// answer back through the decoration.
	//
	// nil and empty mean different things and both are meaningful, which is
	// why there is no omitempty here: absent is "this plugin never said" and
	// an explicit [] is "this plugin says it is frontend-only". A tag that
	// dropped the empty array on the way to the projection column would turn
	// the second answer back into the first on the next read.
	BackendServices []string `json:"backendServices"`
	// ApprovedBackendServices is the operator's answer to that request, and
	// the only list the health plane probes (BR-AS62). Platform-owned, like
	// Enabled: a payload asserting it is refused, and it is kept out of the
	// signed content so approving a plugin cannot un-attest it.
	//
	// It is always a subset of BackendServices — Admissible refuses a
	// superset, and an announcement that narrows the declaration narrows the
	// approval with it, so an approval can never outlive the declaration it
	// was given for. nil is "not answered yet", which reads as not
	// configured; an explicit [] is "answered: probe nothing".
	ApprovedBackendServices []string `json:"approvedBackendServices"`
}

// Document is what a reader gets: the whole curated set at one revision.
type Document struct {
	SchemaVersion int   `json:"schemaVersion"`
	Revision      int64 `json:"revision"`
	Degraded      bool  `json:"degraded,omitempty"`
	// AsOf is when the copy being served was stored, set only on a degraded
	// read that the cache answered (BR-AS51). Never serialised: it is a
	// property of the copy, not of the document, and writing it into the
	// cached bytes would make every later read report the age of the first.
	AsOf    time.Time `json:"-"`
	Entries []Entry   `json:"plugins"`
}

// Degraded is the answer when neither Postgres nor the KV cache can be read
// (BR-AS22). An empty document that says so — not a substitute catalog,
// because there is none: the shell's built-ins ship inside the shell's own
// bundle and are deliberately never curated.
func Degraded() Document {
	return Document{SchemaVersion: SchemaVersion, Revision: DegradedRevision, Degraded: true, Entries: []Entry{}}
}

// Readable is the document as the shell may see it: disabled entries and
// entries the allowlist no longer covers are withheld.
//
// The read-side allowlist check is not redundant with the write-side one.
// Narrowing REGISTRY_ALLOWED_ORIGINS leaves already-stored rows
// non-conforming, and that is exactly the case a write-time check cannot
// cover (BR-AS20). Withholding is not a write: the row, its history and the
// revision are all untouched.
func (d Document) Readable(allowlist Allowlist) Document {
	out := Document{SchemaVersion: SchemaVersion, Revision: d.Revision, Degraded: d.Degraded, AsOf: d.AsOf, Entries: []Entry{}}
	for _, e := range d.Entries {
		/* Checked before Enabled and before the allowlist, and both orderings
		   matter. A withheld entry is disabled, so an Enabled check first would
		   drop it; and a tombstone has no remote, so an allowlist check would
		   drop it too. The shell is told about this one precisely because it
		   may be running the code that was just withdrawn (BR-AS49). */
		if e.Withheld {
			out.Entries = append(out.Entries, Tombstone(e.ID))
			continue
		}
		/* A withdrawn plugin is served as a MARKER, not by disappearing
		   (BR-AS54). Absence is not authoritative — a filter, a malformed
		   row or a degraded read all look like absence — so a running shell
		   that withdrew on absence alone could be talked into unloading a
		   plugin by an outage. Checked BEFORE Enabled, because an operator
		   disabling a dynamic plugin is itself a withdrawal: the row is left
		   enabled = false and withdrawn = true together, and an Enabled check
		   first would turn the marker back into plain absence. Nothing leaks
		   from this: withdrawn is only ever set on a row an operator had
		   already approved, so an entry nobody approved still says nothing. */
		if e.Withdrawn {
			out.Entries = append(out.Entries, Withdrawal(e.ID))
			continue
		}
		if !e.Enabled {
			continue
		}
		if allowlist.Check(e) != nil {
			continue
		}
		out.Entries = append(out.Entries, e)
	}
	// Sorted by id so the document is byte-stable across reads: the shell
	// breaks ordering ties by id anyway (BR-AS06), and a nav bar that
	// reordered itself between boots would look like a shell bug.
	sort.Slice(out.Entries, func(i, j int) bool { return out.Entries[i].ID < out.Entries[j].ID })
	return out
}

// Tombstone is a withheld entry as a reader sees it: the id, the mark, and
// nothing else (BR-AS49).
//
// Everything else is deliberately absent rather than merely unused. The
// remote is what a shell would load, and this entry is exactly the one it
// must not; the manifest is the artifact the revoked key signed, and
// re-serving it would hand out the attestation that was just withdrawn. What
// is left is enough to say "you may be running this — stop", which is the
// whole message.
func Tombstone(id string) Entry {
	return Entry{ID: id, Withheld: true, Contributions: []Contribution{}}
}

// Withdrawal is an unregistered entry as a reader sees it: the id, the mark,
// and nothing else.
//
// The same shape as a tombstone and for one of the same reasons — the remote
// is what a shell would load, and this is the entry it must not. It is a
// separate mark because the two mean different things to a shell: a
// tombstone says trust was withdrawn and forces a reload for either class
// (BR-AS49), while this says the publisher is gone, which withdraws a
// dynamic plugin live and offers a static one a reload (BR-AS53, BR-AS54).
//
// The marker carries the mark and the id, nothing more. It used to carry
// Enabled true as well, for no reason a reader could see: Readable ran twice
// on the way to a browser, and the second pass would have dropped a marker
// whose Enabled was false. The document is filtered once now, so the field
// no longer has to lie to survive the trip.
func Withdrawal(id string) Entry {
	return Entry{ID: id, Withdrawn: true, Contributions: []Contribution{}}
}

// Write ops. Exhaustive: there is no delete, and BR-AS24 is checked against
// this list rather than against the absence of one.
const (
	OpUpsert     = "upsert"
	OpSetEnabled = "set-enabled"
)

// OpWithholdKey names the audit row a revocation leaves against the plugin
// document (decision 104). It is not a Write op and never appears in
// WriteOps: no caller may ask for it. It happens only as part of revoking a
// key, inside that same transaction, so that trust cannot be withdrawn
// without the entries following. It files itself under the key, not under any
// one entry — the operator's act was about the key, and which entries follow
// is the registry's job to work out.
const OpWithholdKey = "withhold-key"

// WriteOps returns every legal op. A new op added without a rule fails the
// spec that pins this list.
func WriteOps() []string { return []string{OpUpsert, OpSetEnabled} }

// Write is one curated change. It carries its own actor because an
// authorless write cannot be audited, and BR-AS23 requires every accepted
// *and* refused write to leave a row.
type Write struct {
	Op      string
	EntryID string
	Actor   string
	Entry   *Entry
	Enabled bool
	// IfRevision is the revision the author read. Checked by Apply, not here:
	// the shape of a write is knowable on its own, its freshness is not.
	IfRevision int64
	// RequireKeyEnabled names a publisher key that must still be enabled at
	// the moment the write commits (BR-AS48, decision 99). Verifying and then
	// reading the trust table leaves a window: a key revoked in between would
	// still get its announcement in. The store re-reads this key inside the
	// same transaction that holds the revision lock, so revocation and write
	// cannot interleave. Empty on operator writes — an operator's authority
	// is their credential, not a publisher key.
	RequireKeyEnabled string
}

// Validate checks everything about a write that is true without reading the
// registry.
func (w Write) Validate() error {
	if w.Actor == "" {
		return ErrNoActor
	}
	if w.EntryID == "" {
		return ErrNoEntryID
	}
	switch w.Op {
	case OpSetEnabled:
		return nil
	case OpUpsert:
		if w.Entry == nil {
			return ErrNoEntry
		}
		if w.Entry.ID != w.EntryID {
			return ErrEntryIDMismatch
		}
		// An operator may state the class or leave it unclassified; they may
		// not invent one, because the shell's behavior is a closed set
		// (BR-AS52). Checked here rather than at the store so a refusal is
		// still audited as a refused write.
		if err := ValidateLifecycle(w.Entry.Lifecycle); err != nil {
			return err
		}
		// Structure, checked on the way in rather than left for every shell
		// to reject on the way out (see admissible.go for where the line
		// between the two sits, and why it stays there).
		return w.Entry.Admissible()
	default:
		return fmt.Errorf("%w: %q", ErrUnknownOp, w.Op)
	}
}

// Allowlist is the set of origins the platform will let the shell fetch
// plugin code from. Configuration, never a stored row: a dynamic write path
// widens the blast radius of a compromised registry from "filesystem access
// on the host" to "one API call", and an envelope that *is* the business
// rule belongs in configuration, not behind a runtime toggle the same
// compromised path could widen (decisions 28, 43).
type Allowlist struct {
	origins map[string]struct{}
}

// NewAllowlist builds the allowlist from configured origin strings. An empty
// list permits nothing — not everything. A deployment that forgot to
// configure it curates no remotes, which is the safe direction for a list
// that decides which code a browser fetches.
func NewAllowlist(origins []string) Allowlist {
	set := make(map[string]struct{}, len(origins))
	for _, raw := range origins {
		if o, ok := originOf(strings.TrimSpace(raw)); ok {
			set[o] = struct{}{}
		}
	}
	return Allowlist{origins: set}
}

// Origins returns the configured origins, sorted — for the degraded/config
// display, and so a spec can assert what a deployment actually parsed.
func (a Allowlist) Origins() []string {
	out := make([]string, 0, len(a.origins))
	for o := range a.origins {
		out = append(out, o)
	}
	sort.Strings(out)
	return out
}

// Permits reports whether rawURL is same-origin by construction or its
// absolute origin is configured. Scheme, host and port all count: an https
// entry does not bless the same host over http. Protocol-relative URLs are
// neither form and are refused (BR-AS72).
func (a Allowlist) Permits(rawURL string) bool {
	if sameOriginPath(rawURL) {
		return true
	}
	o, ok := originOf(rawURL)
	if !ok {
		return false
	}
	_, allowed := a.origins[o]
	return allowed
}

func sameOriginPath(raw string) bool {
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "" && u.Host == "" && u.Opaque == ""
}

// Check is Permits for a whole entry, as an error, so a refusal reads the
// same on the write path and the read path.
func (a Allowlist) Check(e Entry) error {
	if e.Remote.Kind != RemoteFederated {
		return fmt.Errorf("%w: unknown remote kind %q", ErrOriginNotAllowed, e.Remote.Kind)
	}
	if !a.Permits(e.Remote.URL) {
		// The error names the entry, never the URL: a refusal surfaced to a
		// user carries stage and cause only (BR-AS04).
		return fmt.Errorf("%w: entry %q", ErrOriginNotAllowed, e.ID)
	}
	return nil
}

// originOf reduces a URL to scheme://host[:port], the unit the allowlist
// compares. Anything without both a scheme and a host — a bare path, a
// javascript: URL, an empty string — has no origin and is refused.
func originOf(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", false
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return "", false
	}
	return u.Scheme + "://" + u.Host, true
}

// Audit outcomes. Every write leaves a row, applied or not (BR-AS23).
const (
	AuditAccepted = "accepted"
	AuditRefused  = "refused"
)
