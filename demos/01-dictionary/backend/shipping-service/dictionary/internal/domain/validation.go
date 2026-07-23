package domain

import (
	"errors"
	"regexp"
)

// ErrInvalidToken — BR-020: a value used as a NATS subject token (and, for
// context, also a KV bucket-name component) must be non-empty and match the
// safe subject-token charset — no '.', '*', '>', or whitespace, any of which
// would silently split the value across subject tokens or collide with a
// wildcard metacharacter.
var ErrInvalidToken = errors.New("value must be a non-empty token of letters, digits, '-' or '_'")

var subjectTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidateShipID enforces BR-020 on a shipID before it is folded into a
// subject (ShipSubject/ShipInstanceSubject).
func ValidateShipID(id string) error {
	if !subjectTokenPattern.MatchString(id) {
		return ErrInvalidToken
	}
	return nil
}

// ValidateContext enforces BR-020 on the context/tenant value threaded into
// every ship/container subject and KV bucket name.
func ValidateContext(context string) error {
	if !subjectTokenPattern.MatchString(context) {
		return ErrInvalidToken
	}
	return nil
}
