package domain

import "errors"

// Context is a tenant or reusable template in the reference-data hierarchy.
// Parent is empty for a root template.
type Context struct {
	Context     string `json:"context"`
	Parent      string `json:"parent,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
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
