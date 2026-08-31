package domain

import (
	"encoding/json"
	"errors"
	"fmt"
)

// PreloadActor is the audit actor for an entry a mounted preload file
// seeded. Distinct from SharedAdminActor on purpose: "where did this entry
// come from" must stay answerable from the audit trail alone, and
// attributing an operator's file to the admin surface would make the one
// artifact this service sells as honest quietly untrue (BR-AS42,
// decision 76).
const PreloadActor = "preload"

// Lifecycle classes. Stored on the entry, never inferred by a reader: a
// removal is the one moment the shell must not guess (Phase 5 decision 59).
// The registration path supplies the default — preload writes static,
// announce writes dynamic — and an operator may override it afterwards
// (decision 86).
const (
	LifecycleStatic  = "static"
	LifecycleDynamic = "dynamic"
)

// PreloadSchemaVersion is the only preload file shape this service reads.
const PreloadSchemaVersion = 1

var (
	// ErrPreloadRevision refuses a preload file that claims a revision.
	// Revision is the store's monotonic count of accepted writes
	// (BR-AS17/AS18); a file cannot hold one without claiming to be the
	// source of truth decision 75 says it is not (decision 82).
	ErrPreloadRevision = errors.New("registry: a preload file may not carry a revision")

	// ErrPreloadSchemaVersion refuses a shape this service does not write.
	ErrPreloadSchemaVersion = errors.New("registry: unsupported preload schemaVersion")

	// ErrSelfAssertedField refuses a field that states how far the platform
	// trusts a plugin, or by which path it arrived. Refused rather than
	// ignored: a silently dropped claim is one a publisher believes was
	// honoured (BR-AS43).
	ErrSelfAssertedField = errors.New("registry: this field is the platform's to set, not the plugin's")
)

// PreloadFile is the operator's curated set as mounted into the service.
// It is a manifest per plugin plus `enabled` — the one field decision 79
// says the operator's file may add, and the whole of the difference between
// a manifest and a registry entry once decision 82 drops revision.
type PreloadFile struct {
	SchemaVersion int     `json:"schemaVersion"`
	Plugins       []Entry `json:"plugins"`
}

// ParsePreload reads a mounted preload file, refusing anything in it that
// the platform rather than the operator owns.
func ParsePreload(raw []byte) (PreloadFile, error) {
	// Decoded twice on purpose: once loosely to catch fields Entry would
	// silently drop, once into Entry for the value. A refusal that depends
	// on a field the target struct does not have cannot be made after
	// unmarshalling into that struct.
	var loose struct {
		SchemaVersion int               `json:"schemaVersion"`
		Revision      json.RawMessage   `json:"revision"`
		Plugins       []json.RawMessage `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &loose); err != nil {
		return PreloadFile{}, fmt.Errorf("registry: preload file is not a registry document: %w", err)
	}
	if len(loose.Revision) > 0 {
		return PreloadFile{}, ErrPreloadRevision
	}
	if loose.SchemaVersion != PreloadSchemaVersion {
		return PreloadFile{}, fmt.Errorf("%w: %d", ErrPreloadSchemaVersion, loose.SchemaVersion)
	}

	out := PreloadFile{SchemaVersion: loose.SchemaVersion, Plugins: make([]Entry, 0, len(loose.Plugins))}
	for i, rawEntry := range loose.Plugins {
		// `enabled` is permitted here and refused in a manifest: the preload
		// file is the operator's act of curation (gate answer 3), a manifest
		// is the plugin speaking about itself.
		if err := refuseSelfAsserted(rawEntry, "source", "lifecycle", "revision"); err != nil {
			return PreloadFile{}, fmt.Errorf("preload entry %d: %w", i+1, err)
		}
		var e Entry
		if err := json.Unmarshal(rawEntry, &e); err != nil {
			return PreloadFile{}, fmt.Errorf("preload entry %d: %w", i+1, err)
		}
		out.Plugins = append(out.Plugins, e)
	}
	return out, nil
}

// ParseManifest reads a plugin's self-description — served beside its
// remoteEntry.js, or carried as an announcement payload. Stricter than
// ParsePreload by exactly one field: a plugin may not enable itself
// (decisions 72, 79; BR-AS43).
func ParseManifest(raw []byte) (Entry, error) {
	if err := refuseSelfAsserted(raw, "source", "lifecycle", "enabled", "revision"); err != nil {
		return Entry{}, err
	}
	var e Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		return Entry{}, fmt.Errorf("registry: manifest is not readable: %w", err)
	}
	return e, nil
}

func refuseSelfAsserted(raw json.RawMessage, fields ...string) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("registry: entry is not an object: %w", err)
	}
	for _, f := range fields {
		if _, present := probe[f]; present {
			return fmt.Errorf("%w: %q", ErrSelfAssertedField, f)
		}
	}
	return nil
}

// PreloadRefusal is one entry the preload tier would not seed, and why.
// Surfaced with the `withheld` vocabulary BR-AS20 already established, so a
// skipped entry is visible rather than silent (decision 81).
type PreloadRefusal struct {
	ID    string
	Cause error
}

// PreloadResult is what a preload run would do, decided before anything is
// written. Separating the decision from the write is what makes the whole of
// BR-AS41 and decision 81 testable without a database.
type PreloadResult struct {
	Seed     []Entry
	Skipped  []string
	Withheld []PreloadRefusal
}

// PlanPreload decides which of a preload file's entries this store has never
// seen and may legally accept.
//
// A preloaded entry is inserted only for an id with no existing row
// (BR-AS41, decision 75): this is a fallback tier, not a competing source of
// truth, so an id the operator has since edited, disabled or removed is
// never touched. An entry whose origin the allowlist refuses is withheld on
// its own rather than failing the run — one line of operator convenience
// must not take down the tier every shell reads (decision 81).
func PlanPreload(existing []Entry, file []Entry, allowlist Allowlist) PreloadResult {
	known := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		known[e.ID] = struct{}{}
	}

	out := PreloadResult{}
	for _, e := range file {
		if _, seen := known[e.ID]; seen {
			out.Skipped = append(out.Skipped, e.ID)
			continue
		}
		if err := allowlist.Check(e); err != nil {
			out.Withheld = append(out.Withheld, PreloadRefusal{ID: e.ID, Cause: err})
			continue
		}
		// The path supplies the class; the file is not consulted for it and
		// ParsePreload refuses a file that tried (decision 86, BR-AS43).
		e.Lifecycle = LifecycleStatic
		out.Seed = append(out.Seed, e)
	}
	return out
}

// PreloadWrite builds the write that seeds one planned entry. An ordinary
// upsert through the ordinary Apply path, so it carries a revision and an
// audit row like any other write (decision 75) — the only thing that marks
// it out is its actor.
func PreloadWrite(e Entry, ifRevision int64) Write {
	e.Lifecycle = LifecycleStatic
	return Write{Op: OpUpsert, EntryID: e.ID, Actor: PreloadActor, Entry: &e, IfRevision: ifRevision}
}
