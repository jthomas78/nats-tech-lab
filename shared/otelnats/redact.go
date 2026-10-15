package otelnats

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// redactDenylist is the set of JSON object keys (case-insensitive, matched at
// any nesting depth) removed from a body before it leaves the process
// (BR-036, BR-046). Ported unchanged from shared/natstrace; the two lists are
// one list until natstrace goes in 54d.
var redactDenylist = map[string]struct{}{
	"password":      {},
	"secret":        {},
	"token":         {},
	"apikey":        {},
	"api_key":       {},
	"ssn":           {},
	"creditcard":    {},
	"credit_card":   {},
	"authorization": {},
	"privatekey":    {},
	"private_key":   {},

	// Actor PII (BR-046): a cross-tenant operator plane needs to see that a
	// change happened, not which person made it or from where.
	"actorname":       {},
	"actor_name":      {},
	"actorsourceip":   {},
	"actor_source_ip": {},
}

func denied(key string) bool {
	_, ok := redactDenylist[strings.ToLower(key)]
	return ok
}

// redact removes every denylisted key from a JSON payload and returns the
// re-marshalled payload with the sorted paths it removed. A payload that is
// not JSON is returned unchanged: this is a debugging side channel, not a
// validator.
func redact(payload []byte) ([]byte, []string) {
	if len(payload) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(payload, &v); err != nil {
		return payload, nil
	}
	var removed []string
	scrubbed := redactValue(v, "", &removed)
	// Sorted, because map iteration order is random.
	sort.Strings(removed)
	out, err := json.Marshal(scrubbed)
	if err != nil {
		return payload, nil
	}
	return out, removed
}

func redactValue(v any, path string, removed *[]string) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			if denied(k) {
				*removed = append(*removed, childPath)
				continue
			}
			out[k] = redactValue(child, childPath, removed)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, child := range val {
			out[i] = redactValue(child, fmt.Sprintf("%s[%d]", path, i), removed)
		}
		return out
	default:
		return v
	}
}

// redactHeaders removes every denylisted header and names each one as
// headers.<Name>, the same path form redact uses, so one list carries both.
func redactHeaders(h map[string][]string) (map[string][]string, []string) {
	var removed []string
	kept := make(map[string][]string, len(h))
	for k, v := range h {
		if denied(k) {
			removed = append(removed, "headers."+k)
			continue
		}
		kept[k] = v
	}
	sort.Strings(removed)
	return kept, removed
}

// preparePayload is the obs.pubsub.* envelope's redact-then-truncate step,
// as natstrace had it. byteLen is the size after redaction and before
// truncation. A truncated payload becomes a quoted JSON string of the cut
// bytes, because a JSON object cut mid-way is not valid JSON.
func preparePayload(payload []byte) (out json.RawMessage, byteLen int, redacted []string, truncated bool) {
	body, redacted := redact(payload)
	byteLen = len(body)
	if len(body) > MaxBodyBytes {
		quoted, err := json.Marshal(string(body[:MaxBodyBytes]))
		if err != nil {
			quoted = []byte(`""`)
		}
		body = quoted
		truncated = true
	}
	return body, byteLen, redacted, truncated
}
