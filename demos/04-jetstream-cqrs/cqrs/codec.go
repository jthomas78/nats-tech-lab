package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The wire format. The domain has no JSON tags for transport reasons and no
// knowledge that NATS exists, so the translation lives here.
//
// An event's TYPE is carried by the subject, not by a field in the body.
// That is deliberate: a consumer can filter on `...travelled` without reading
// a single byte of payload.

// encode returns the JSON body for one event.
func encode(e Event) ([]byte, error) { return json.Marshal(e) }

// decode rebuilds an event from the subject it arrived on and its body.
//
// An unknown event type is an error, not a skip. Silently ignoring a type we
// do not understand would rehydrate an aggregate that is quietly wrong, and a
// wrong aggregate approves a command it should refuse.
func decode(subject string, data []byte) (Event, error) {
	i := strings.LastIndex(subject, ".")
	if i < 0 {
		return nil, fmt.Errorf("subject %q has no event type", subject)
	}
	switch subject[i+1:] {
	case "registered":
		var e Registered
		return e, json.Unmarshal(data, &e)
	case "travelled":
		var e Travelled
		return e, json.Unmarshal(data, &e)
	case "retired":
		var e Retired
		return e, json.Unmarshal(data, &e)
	default:
		return nil, fmt.Errorf("unknown event type in subject %q", subject)
	}
}
