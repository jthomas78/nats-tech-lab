package domain

import (
	"errors"
	"regexp"
)

// ErrInvalidToken — BR-D22: a value used as a NATS subject token (TypeKey,
// Context) must be non-empty and match the safe subject-token charset — no
// '.', '*', '>', or whitespace, any of which would silently split the value
// across subject tokens or collide with a wildcard metacharacter.
var ErrInvalidToken = errors.New("value must be a non-empty token of letters, digits, '-' or '_'")

// ErrInvalidKVKeyComponent — BR-D22: a value that becomes part of a KV key
// (Code) must be non-empty and match the KV-key-legal charset ('.' is
// allowed here since it never appears in a subject; ':' is not, per NATS KV
// key rules).
var ErrInvalidKVKeyComponent = errors.New("value must be a non-empty KV-key-legal string ([-/_=.a-zA-Z0-9])")

var (
	subjectTokenPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	kvKeyComponentPattern = regexp.MustCompile(`^[-/_=.a-zA-Z0-9]+$`)
)

// ValidateTypeKey enforces BR-D22 at type registration — TypeKey is folded
// into a subject ({context}.refdata.{typeKey}.changed) and a KV key.
func ValidateTypeKey(typeKey string) error {
	if !subjectTokenPattern.MatchString(typeKey) {
		return ErrInvalidToken
	}
	return nil
}

// ValidateCode enforces BR-D22 at item registration — Code only ever appears
// in a KV key (typeKey+"."+code), never a subject, so the more permissive
// KV-key charset applies.
func ValidateCode(code string) error {
	if !kvKeyComponentPattern.MatchString(code) {
		return ErrInvalidKVKeyComponent
	}
	return nil
}

// ValidateContextName enforces BR-D22 at context registration — Context is
// folded into a subject ({context}.refdata.{typeKey}.changed) and a KV/stream
// bucket-name component (refdata-{context}), both of which forbid '.'.
func ValidateContextName(name string) error {
	if !subjectTokenPattern.MatchString(name) {
		return ErrInvalidToken
	}
	return nil
}
