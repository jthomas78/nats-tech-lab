// Package domain holds the DictionaryEntry entity, its events, and the
// repository port. It has no framework dependencies.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

var ErrNotFound = errors.New("dictionary entry not found")

// identRe restricts identifiers to characters that are safe inside both KV
// keys (joined with ':') and bucket names.
var identRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// DictionaryEntry is a single piece of reference data, always scoped to an
// application context (tenant/region/locale).
type DictionaryEntry struct {
	Context    string         `json:"context"`
	EntityType string         `json:"entityType"`
	ID         string         `json:"id"`
	Label      string         `json:"label"`
	Attributes map[string]any `json:"attributes,omitempty"`
	Version    int            `json:"version,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
}

// KVKey returns the entry's key within its context-scoped bucket:
// {entityType}.{id}, e.g. currency.GBP. NATS KV keys only allow
// [-/_=.a-zA-Z0-9], so '.' is the separator (the plan's original ':' is not
// a legal KV key character).
func (e DictionaryEntry) KVKey() string {
	return e.EntityType + "." + e.ID
}

// Validate checks the identifying fields and the label.
func (e DictionaryEntry) Validate() error {
	for name, v := range map[string]string{
		"context":    e.Context,
		"entityType": e.EntityType,
		"id":         e.ID,
	} {
		if !identRe.MatchString(v) {
			return fmt.Errorf("%s must be non-empty and match %s", name, identRe)
		}
	}
	if e.Label == "" {
		return errors.New("label must not be empty")
	}
	return nil
}
