package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Structural admissibility: the shape rules the registry itself enforces on
// the way in (BR-AS20, BR-AS69).
//
// Why this exists at all. The shell validates every manifest it reads and
// records a rejection as a *status*, which is right for a shell — one bad
// entry must not take a browser down. But a status is only ever a report:
// the entry stays in the registry, every shell that reads it does the same
// work to reach the same refusal, and the operator who curated it learns
// nothing until someone opens the Plugins screen. A registry that can refuse
// tells them at the moment they wrote it.
//
// Why this is not the same contract written twice. What is checked here is a
// deliberate SUBSET of what the shell checks, and the two halves are split on
// a line that will not move:
//
//   - Structure is the registry's. An id's spelling, a kind being one of the
//     five, a route living under its plugin's own prefix, an extension point
//     being owned by the plugin that declares it — these are naming rules
//     across the whole curated set, and the registry is the only place that
//     sees the whole set. They are also the rules that never vary by reader.
//
//   - Compatibility is the shell's, and is checked nowhere here. schemaVersion
//     and shellApiVersion say whether THIS shell can run the plugin, and one
//     registry serves shells of several vintages over an upgrade. A registry
//     that refused on version would refuse an entry that is perfectly good for
//     the shell it was published for.
//
// So the shell may always reject more than the registry refuses, and a rule
// tightening on the shell side never has to be mirrored here. The relationship
// only goes one way, which is what keeps the two from drifting into two
// definitions of one contract.
var (
	ErrEntryNotAdmissible = errors.New("registry: entry is not structurally admissible")
)

// idPattern is the shell's ID_PATTERN: kebab-case, no leading, trailing or
// doubled hyphens. Narrow on purpose — these ids land in route paths, DOM ids
// and store keys, and a spelling legal in one is not legal in all.
var idPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// extensionPointPattern is {owner}/{region}/v{major}.
var extensionPointPattern = regexp.MustCompile(`^([a-z0-9]+(?:-[a-z0-9]+)*)/([a-z0-9]+(?:-[a-z0-9]+)*)/v(\d+)$`)

// ContributionKinds is the closed set of contribution kinds (BR-AS02). Closed
// because the shell renders each kind itself: a kind it does not know is a
// kind it cannot place, so there is no pass-through case to leave open here
// either.
func ContributionKinds() []string {
	return []string{"route", "navigation", "extension", "shell-control", "shell-footer"}
}

func knownKind(kind string) bool {
	for _, k := range ContributionKinds() {
		if k == kind {
			return true
		}
	}
	return false
}

func notAdmissible(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrEntryNotAdmissible, fmt.Sprintf(format, args...))
}

// Admissible reports whether the entry is structurally usable by any shell.
// The error names the entry and the field, never a URL or a remote's text, so
// a refusal can be surfaced as stage and cause (BR-AS04).
func (e Entry) Admissible() error {
	if !idPattern.MatchString(e.ID) {
		return notAdmissible("entry id %q is not kebab-case", e.ID)
	}
	if strings.TrimSpace(e.Name) == "" {
		return notAdmissible("entry %q has no name", e.ID)
	}

	// A plugin's routes live under one segment, which is its id unless it
	// names another. Not required to repeat the id — what BR-AS12 needs is a
	// namespaced, unique segment knowable from the URL alone.
	prefix := e.RoutePrefix
	if prefix == "" {
		prefix = e.ID
	}
	if !idPattern.MatchString(prefix) {
		return notAdmissible("entry %q route prefix %q is not kebab-case", e.ID, prefix)
	}

	for _, p := range e.ExtensionPoints {
		parts := extensionPointPattern.FindStringSubmatch(p.ID)
		if parts == nil {
			return notAdmissible("entry %q extension point %q is not {owner}/{region}/v{major}", e.ID, p.ID)
		}
		// A plugin cannot open a region in another plugin's namespace, or it
		// could shadow a host region and capture contributions meant for the
		// shell.
		if parts[1] != e.ID {
			return notAdmissible("entry %q declares extension point %q, owned by %q", e.ID, p.ID, parts[1])
		}
		if p.Capacity < 0 {
			return notAdmissible("entry %q extension point %q declares a negative capacity", e.ID, p.ID)
		}
	}

	// An entry that contributes nothing is inert: it can be curated, enabled
	// and loaded, and still put nothing on any screen.
	if len(e.Contributions) == 0 {
		return notAdmissible("entry %q contributes nothing", e.ID)
	}

	seen := make(map[string]struct{}, len(e.Contributions))
	for i, c := range e.Contributions {
		if err := admissibleContribution(e.ID, prefix, i, c); err != nil {
			return err
		}
		if _, dup := seen[c.ID]; dup {
			return notAdmissible("entry %q declares contribution %q twice", e.ID, c.ID)
		}
		seen[c.ID] = struct{}{}
	}
	return nil
}

func admissibleContribution(entryID, prefix string, index int, c Contribution) error {
	if !knownKind(c.Kind) {
		return notAdmissible("entry %q contribution %d declares unknown kind %q", entryID, index, c.Kind)
	}
	if !idPattern.MatchString(c.ID) {
		return notAdmissible("entry %q contribution %d id %q is not kebab-case", entryID, index, c.ID)
	}

	switch c.Kind {
	case "route":
		if !strings.HasPrefix(c.Path, "/") {
			return notAdmissible("entry %q route %q has no absolute path", entryID, c.ID)
		}
		// Whole-segment prefix match: "/demos-archive" is not inside "/demos".
		if c.Path != "/"+prefix && !strings.HasPrefix(c.Path, "/"+prefix+"/") {
			return notAdmissible("entry %q route %q declares %q, outside /%s", entryID, c.ID, c.Path, prefix)
		}
		if strings.TrimSpace(c.Title) == "" {
			return notAdmissible("entry %q route %q has no title", entryID, c.ID)
		}
	case "navigation":
		if strings.TrimSpace(c.Label) == "" {
			return notAdmissible("entry %q navigation %q has no label", entryID, c.ID)
		}
		// A nav entry names a route contribution by its LOCAL id, not a path,
		// so a nav entry pointing at nothing is caught at index time rather
		// than on click.
		if !idPattern.MatchString(c.Route) {
			return notAdmissible("entry %q navigation %q names no route", entryID, c.ID)
		}
	case "extension":
		if !extensionPointPattern.MatchString(c.Target) {
			return notAdmissible("entry %q extension %q target %q is not an extension-point id", entryID, c.ID, c.Target)
		}
	case "shell-control":
		if !extensionPointPattern.MatchString(c.Region) {
			return notAdmissible("entry %q shell-control %q region %q is not an extension-point id", entryID, c.ID, c.Region)
		}
	case "shell-footer":
		// Nothing beyond the base: a footer item names a component or takes
		// the module's default export.
	}
	return nil
}
