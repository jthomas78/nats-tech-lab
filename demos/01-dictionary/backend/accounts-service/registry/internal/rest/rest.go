// Package rest records the retired registry HTTP surface. Registry discovery
// and curation now use PLATFORM api.* subjects; there is no HTTP fallback.
package rest

import "net/http"

// Mount's exhaustive empty list keeps accidental reintroduction testable.
func Mount(_ *http.ServeMux) []string { return []string{} }
