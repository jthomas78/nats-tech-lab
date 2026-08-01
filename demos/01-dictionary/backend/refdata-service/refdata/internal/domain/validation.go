package domain

import (
	"errors"
	"regexp"
	"strings"
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

// ErrReservedContextPrefix — BR-D33: a context value beginning with "_" is
// reserved for platform/system use (the inheritance root, Phase 16d) and may
// never be claimed by a company or business-unit context.
var ErrReservedContextPrefix = errors.New("context name may not start with '_' — that prefix is reserved for platform/system use")

var (
	subjectTokenPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	kvKeyComponentPattern = regexp.MustCompile(`^[-/_=.a-zA-Z0-9]+$`)
)

// ValidateTypeKey enforces BR-D22 at type registration — TypeKey is folded
// into a subject ({context}.refdata.{typeKey}.changed) and a KV key.
func ValidateTypeKey(typeKey string) error {
	return ValidateSubjectToken(typeKey)
}

// ValidateSubjectToken is the shared low-level charset check (BR-D22) every
// subject/KV-bucket token value must satisfy. Exported specifically so
// commands.ContextHandler.RegisterPlatformRoot can apply this check alone,
// bypassing ValidateContextName's reserved-prefix rejection (BR-D33) for the
// one legitimate `_`-prefixed value — see that method's doc comment.
func ValidateSubjectToken(name string) error {
	if !subjectTokenPattern.MatchString(name) {
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

// ValidateContextName enforces BR-D22 and BR-D33 at context registration —
// Context is folded into a subject ({context}.refdata.{typeKey}.changed) and
// a KV/stream bucket-name component (refdata-{context}), both of which
// forbid '.'; and a leading '_' is reserved for the platform root (BR-D33),
// never a company/business-unit value registered through this path.
//
// This is a blanket rejection with NO carve-out for the platform root
// itself (Phase 16d, "_platform") — deliberately, so the public
// POST /api/refdata/admin/contexts endpoint (which always calls this) can
// never create or overwrite the reserved root. Seeding "_platform" instead
// goes through commands.ContextHandler.RegisterPlatformRoot, which applies
// ValidateSubjectToken (the charset check alone) rather than this function.
func ValidateContextName(name string) error {
	if err := ValidateSubjectToken(name); err != nil {
		return err
	}
	if strings.HasPrefix(name, "_") {
		return ErrReservedContextPrefix
	}
	return nil
}
