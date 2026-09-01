package domain

import "errors"

// Lifecycle is the withdrawal class, and the shell has behavior for exactly
// two of them (decision 59). It is stored, never inferred from the
// registration path: a preloaded plugin and an announced one can both be
// either, and guessing from the source is how "how it got here" would start
// deciding "what happens when it goes away".
//
// The empty string is a third state, and it is not a class. It means a row
// written before the column existed, and it resolves to static — the
// conservative direction, because static's answer to a withdrawal is to
// leave the plugin running and offer a reload (BR-AS52).

var ErrUnknownLifecycle = errors.New("registry: unknown lifecycle class")

// ValidateLifecycle refuses a class the shell has no behavior for. Empty is
// accepted here and classified by LifecycleOf, so that an unclassified legacy
// row and an unclassified new write cannot come to mean different things.
func ValidateLifecycle(lifecycle string) error {
	switch lifecycle {
	case LifecycleStatic, LifecycleDynamic, "":
		return nil
	default:
		return ErrUnknownLifecycle
	}
}

// LifecycleOf is the class the shell should act on.
func LifecycleOf(e Entry) string {
	if e.Lifecycle == "" {
		return LifecycleStatic
	}
	return e.Lifecycle
}
