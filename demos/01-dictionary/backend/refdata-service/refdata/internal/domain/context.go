package domain

import "errors"

// Context is a company/business-unit scope or reusable template in the
// reference-data hierarchy — see ARCHITECTURE-COMMUNICATIONS.md § 2.3 for
// what this token means and does not mean (never the tenant, never the
// region). Parent is empty for a root template.
type Context struct {
	Context     string `json:"context"`
	Parent      string `json:"parent,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Tenant is governance/ownership metadata only (Phase 16d, BR-D34) — the
	// NATS account name this context belongs to, empty for "_"-reserved
	// platform contexts which no tenant owns. NOT enforced: refdata-service
	// has no caller identity on its single shared NATS account to check this
	// against. See Refdata-Versioning-Tenancy-Design.md § 2.1.
	Tenant string `json:"tenant,omitempty"`
}

var (
	ErrContextNotFound = errors.New("reference-data context not found")
	ErrContextCycle    = errors.New("reference-data context hierarchy contains a cycle")
)

// AncestorChain returns the context followed by each parent up to the root.
// It makes a malformed cyclic hierarchy explicit rather than allowing an
// unbounded traversal in inheritance resolution.
func AncestorChain(context string, contexts map[string]Context) ([]string, error) {
	chain := make([]string, 0)
	seen := make(map[string]bool)
	for current := context; current != ""; {
		if seen[current] {
			return nil, ErrContextCycle
		}
		entry, ok := contexts[current]
		if !ok {
			return nil, ErrContextNotFound
		}
		seen[current] = true
		chain = append(chain, current)
		current = entry.Parent
	}
	return chain, nil
}
