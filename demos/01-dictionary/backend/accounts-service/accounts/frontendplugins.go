package accounts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
)

// The frontend plugin registry the application shell discovers at boot
// (BR-AS01). The shell fetches this document before it renders anything and
// will load code only from the remotes listed here — so this list, not the
// shell's bundle, is what decides which micro-frontends exist. Adding a plugin
// is a change to this file plus a redeploy of this service; it is not a
// rebuild of the shell, which is the whole point of the registry.
//
// It is Go data rather than Postgres rows for the same reason config.go's
// TTL envelope is: the curated set *is* the platform's own composition, not
// something an operator edits per environment. The `Enabled` flag is the part
// that will move to Postgres first, when disabling a misbehaving plugin has to
// be possible without a deploy (BR-AS03).

// RegistrySchemaVersion is the shape of the document below. The shell refuses
// a document whose version it does not know rather than guessing at the
// fields — a mismatch here means the shell and this service were deployed
// from different generations, and rendering half of it would be worse than
// rendering none of it.
const RegistrySchemaVersion = 1

// ShellAPIVersion is the host contract each plugin is built against. It is a
// separate number from RegistrySchemaVersion on purpose: the document's shape
// and the API a plugin calls change for different reasons and at different
// times.
const ShellAPIVersion = 1

type frontendPluginRemote struct {
	Kind string `json:"kind"`
	URL  string `json:"url,omitempty"`
	// Name is the Module Federation container name the remote was BUILT
	// under, which the shell must ask for by exactly that spelling. It is
	// separate from the plugin id because the two answer to different
	// constraints — an id lands in URLs and store keys (kebab-case), a
	// container name becomes a global identifier (snake_case). Omitted means
	// "same as the id" (Phase 1b).
	Name   string `json:"name,omitempty"`
	Module string `json:"module"`
}

type frontendPluginExtensionPoint struct {
	ID          string `json:"id"`
	Capacity    int    `json:"capacity,omitempty"`
	Description string `json:"description,omitempty"`
}

// frontendPluginContribution is deliberately one flat struct covering all five
// contribution kinds rather than a union: the shell validates each kind's own
// required fields, and duplicating that per-kind knowledge here would give the
// two sides two definitions of the same contract to drift apart.
type frontendPluginContribution struct {
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

	// Routes scopes a shell-control to the route prefixes it appears under.
	// Empty means unscoped. (Phase 1a shipped this as `routeMatch`, which the
	// shell never read — corrected in 1b, before any curated entry used it.)
	Routes []string `json:"routes,omitempty"`
}

type frontendPlugin struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Version is the plugin's own release version. Free-form and never
	// interpreted here or in the shell — compatibility is decided by
	// SchemaVersion and ShellAPIVersion alone — it exists so an operator
	// reading the shell's Plugins screen can see which build is on screen.
	Version         string `json:"version,omitempty"`
	SchemaVersion   int    `json:"schemaVersion"`
	ShellAPIVersion int    `json:"shellApiVersion"`
	RoutePrefix     string `json:"routePrefix,omitempty"`
	// Enabled is a pointer so "absent" and "false" stay distinguishable when
	// a curated document is read from a file: the shell treats an absent flag
	// as enabled, and a Go bool would silently turn every entry that omits it
	// into a disabled one.
	Enabled         *bool                          `json:"enabled"`
	Remote          frontendPluginRemote           `json:"remote"`
	ExtensionPoints []frontendPluginExtensionPoint `json:"extensionPoints,omitempty"`
	Contributions   []frontendPluginContribution   `json:"contributions"`
}

type frontendPluginRegistry struct {
	SchemaVersion int `json:"schemaVersion"`
	// Revision names *which* curated set this is. Opaque to the shell, which
	// displays it and does nothing else with it, so an omitted revision still
	// serves — it is an operator's handle for "the shell read the registry I
	// think it read", not a version the shell branches on.
	Revision string           `json:"revision,omitempty"`
	Plugins  []frontendPlugin `json:"plugins"`
}

// curatedFrontendPlugins is the registry itself.
//
// Empty in Phase 1a: the shell's only plugin (the demo catalog) is built in,
// so it ships in the shell's bundle and is never listed here — a built-in is
// trusted because it is part of the shell, and listing it would invite a
// remote URL to be attached to it later. Phase 1b adds the first genuinely
// federated entry (the example plugin on port 7110), which is when this list
// starts earning its keep.
//
// The endpoint exists now, returning an empty-but-well-formed document,
// because "the registry answered and curates nothing" and "the registry is
// unreachable" are different states and the shell reports them differently
// (BR-AS04).
var curatedFrontendPlugins = []frontendPlugin{}

// curatedFrontendRevision accompanies the set above and is replaced with it.
var curatedFrontendRevision string

// LoadCuratedFrontendPlugins replaces the curated set from a JSON document on
// disk, named by FRONTEND_PLUGIN_REGISTRY_FILE.
//
// Curation stays an operator decision either way; the file is what lets that
// decision be made without rebuilding *this* service, in the same way the
// registry itself is what lets it be made without rebuilding the shell
// (BR-AS03). It is also what keeps a localhost:7110 development remote out of
// the platform's own source.
//
// A malformed or unreadable file is an error the caller reports and continues
// past: an unreadable curation file must leave the endpoint serving the
// compiled-in set, not take the service down — the shell already treats a
// missing registry as a degraded shell rather than a broken one (BR-AS04).
func LoadCuratedFrontendPlugins(path string) ([]frontendPlugin, error) {
	plugins, _, err := loadCuratedFrontendRegistry(path)
	return plugins, err
}

// LoadCuratedFrontendRegistry is LoadCuratedFrontendPlugins plus the
// document's revision, for a caller that installs both.
func LoadCuratedFrontendRegistry(path string) ([]frontendPlugin, string, error) {
	return loadCuratedFrontendRegistry(path)
}

func loadCuratedFrontendRegistry(path string) ([]frontendPlugin, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read frontend plugin registry %s: %w", path, err)
	}
	var doc frontendPluginRegistry
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, "", fmt.Errorf("parse frontend plugin registry %s: %w", path, err)
	}
	if doc.SchemaVersion != RegistrySchemaVersion {
		return nil, "", fmt.Errorf(
			"frontend plugin registry %s declares schemaVersion %d; this service serves %d",
			path, doc.SchemaVersion, RegistrySchemaVersion)
	}
	return doc.Plugins, doc.Revision, nil
}

// SetCuratedFrontendPlugins installs a curated set. Called once at startup;
// exported so the loading policy (which file, what to do when it is missing)
// lives in main.go with the rest of the service's configuration rather than
// in this file.
func SetCuratedFrontendPlugins(plugins []frontendPlugin) {
	curatedFrontendPlugins = plugins
}

// SetCuratedFrontendRevision installs the revision served alongside the set.
func SetCuratedFrontendRevision(revision string) {
	curatedFrontendRevision = revision
}

// @Summary      List curated frontend plugins
// @Description  The application shell's plugin registry: every micro-frontend the platform curates, with its remote entry point and the contributions it declares. The shell loads code only from remotes listed here. Read-only, and carries no {context} token — the curated set is platform-wide, and per-user visibility is decided in the shell from the caller's own claims, not by filtering this document.
// @Tags         accounts
// @Produce      json
// @Success      200  {object}  frontendPluginRegistry
// @Router       /api/accounts/frontend-plugins [get]
func (h *Handlers) listFrontendPlugins(w http.ResponseWriter, r *http.Request) {
	plugins := make([]frontendPlugin, len(curatedFrontendPlugins))
	copy(plugins, curatedFrontendPlugins)
	// `enabled` is a pointer so a curated *file* can distinguish "absent" from
	// "false", but the document the shell reads always states it: the flag
	// decides whether code is fetched, and a boundary that carries it
	// sometimes is a worse contract than one that carries it always.
	for i := range plugins {
		if plugins[i].Enabled == nil {
			enabled := true
			plugins[i].Enabled = &enabled
		}
	}
	// Sorted by id so the document is byte-stable across restarts: the shell
	// breaks ordering ties between plugins by id anyway (BR-AS06), and a
	// registry whose order wandered would make a nav bar that reordered
	// itself look like a shell bug.
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].ID < plugins[j].ID })

	writeJSON(w, http.StatusOK, frontendPluginRegistry{
		SchemaVersion: RegistrySchemaVersion,
		Revision:      curatedFrontendRevision,
		Plugins:       plugins,
	})
}
