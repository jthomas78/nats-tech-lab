package accounts

import (
	"net/http"
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
	Kind   string `json:"kind"`
	URL    string `json:"url,omitempty"`
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

	RouteMatch string `json:"routeMatch,omitempty"`
}

type frontendPlugin struct {
	ID              string                         `json:"id"`
	Name            string                         `json:"name"`
	Description     string                         `json:"description,omitempty"`
	SchemaVersion   int                            `json:"schemaVersion"`
	ShellAPIVersion int                            `json:"shellApiVersion"`
	RoutePrefix     string                         `json:"routePrefix,omitempty"`
	Enabled         bool                           `json:"enabled"`
	Remote          frontendPluginRemote           `json:"remote"`
	ExtensionPoints []frontendPluginExtensionPoint `json:"extensionPoints,omitempty"`
	Contributions   []frontendPluginContribution   `json:"contributions"`
}

type frontendPluginRegistry struct {
	SchemaVersion int              `json:"schemaVersion"`
	Plugins       []frontendPlugin `json:"plugins"`
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

// @Summary      List curated frontend plugins
// @Description  The application shell's plugin registry: every micro-frontend the platform curates, with its remote entry point and the contributions it declares. The shell loads code only from remotes listed here. Read-only, and carries no {context} token — the curated set is platform-wide, and per-user visibility is decided in the shell from the caller's own claims, not by filtering this document.
// @Tags         accounts
// @Produce      json
// @Success      200  {object}  frontendPluginRegistry
// @Router       /api/accounts/frontend-plugins [get]
func (h *Handlers) listFrontendPlugins(w http.ResponseWriter, r *http.Request) {
	plugins := make([]frontendPlugin, len(curatedFrontendPlugins))
	copy(plugins, curatedFrontendPlugins)
	// Sorted by id so the document is byte-stable across restarts: the shell
	// breaks ordering ties between plugins by id anyway (BR-AS06), and a
	// registry whose order wandered would make a nav bar that reordered
	// itself look like a shell bug.
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].ID < plugins[j].ID })

	writeJSON(w, http.StatusOK, frontendPluginRegistry{
		SchemaVersion: RegistrySchemaVersion,
		Plugins:       plugins,
	})
}
