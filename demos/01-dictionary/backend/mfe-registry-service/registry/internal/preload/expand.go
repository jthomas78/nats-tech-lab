package preload

import (
	"errors"
	"fmt"
	"strings"
)

// ErrUnsetVar reports a ${VAR} with no value and no :- default. It fails boot
// rather than seeding a literal "${VAR}" origin that the allowlist would then
// withhold with a confusing cause.
var ErrUnsetVar = errors.New("registry preload: unset variable and no default")

// ExpandEnv rewrites every ${VAR} and ${VAR:-default} in the preload file
// against lookup, which is os.LookupEnv in production.
//
// BR-AS74. The preload file is operator-mounted *configuration*, and a cell's
// host ports are variables everywhere else (ADR-055) -- but this file is data,
// not compose, so a literal port in it survived the cell split and broke the
// second cell: registry.json named http://localhost:7112/remoteEntry.js while
// au-1's REGISTRY_ALLOWED_ORIGINS was 7161-7165, so the only preloaded plugin
// was withheld and the registry stayed empty. A port can hide in a data file.
//
// Only the ${...} form is expanded. A bare $name is left alone, so a plugin
// description may contain a dollar sign without being rewritten. An unmatched
// "${" is also left alone -- this is config expansion, not a JSON parser, and
// ParsePreload is still the thing that decides whether the result is valid.
func ExpandEnv(raw []byte, lookup func(string) (string, bool)) ([]byte, error) {
	s := string(raw)
	var b strings.Builder
	b.Grow(len(s))

	for {
		open := strings.Index(s, "${")
		if open < 0 {
			b.WriteString(s)
			break
		}
		close := strings.Index(s[open:], "}")
		if close < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:open])
		expr := s[open+2 : open+close]
		s = s[open+close+1:]

		name, def, hasDefault := strings.Cut(expr, ":-")
		if !validName(name) {
			// Not a variable after all -- the "}" we found belongs to
			// something else, e.g. the JSON object around an unclosed "${".
			// Emit the "${" literally and carry on from just after it.
			b.WriteString("${")
			s = expr + "}" + s
			continue
		}
		value, found := lookup(name)
		switch {
		case found:
			b.WriteString(value)
		case hasDefault:
			b.WriteString(def)
		default:
			return nil, fmt.Errorf("%w: %q", ErrUnsetVar, name)
		}
	}
	return []byte(b.String()), nil
}

// validName holds the expansion to shell-shaped names. Anything else -- a
// quote, a newline, a brace -- means the "}" we matched was not this "${"'s,
// so nothing is substituted.
func validName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_':
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
