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
//
// Every failure here wraps ErrUndecodable -- BR-OD09. That is not decoration.
// The event is already in the log, so no retry will ever make these bytes
// readable, and a consumer that nak'd one would redeliver it for ever. The
// wrap is how a consumer tells this apart from NATS having a bad second,
// without matching on the text of an error message.
func decode(subject string, data []byte) (Event, error) {
	i := strings.LastIndex(subject, ".")
	if i < 0 {
		return nil, fmt.Errorf("%w: subject %q has no event type", ErrUndecodable, subject)
	}
	switch subject[i+1:] {
	case "registered":
		var e Registered
		return e, unmarshal(subject, data, &e)
	case "travelled":
		var e Travelled
		return e, unmarshal(subject, data, &e)
	case "retired":
		var e Retired
		return e, unmarshal(subject, data, &e)
	default:
		return nil, fmt.Errorf("%w: unknown event type in subject %q", ErrUndecodable, subject)
	}
}

// unmarshal reads one body, and calls a body that does not fit its type
// undecodable rather than merely broken.
//
// A payload is as permanent as a subject. `{"km":"far"}` on a `...travelled`
// subject is stored, and it will still say "far" on the hundredth delivery.
func unmarshal(subject string, data []byte, into any) error {
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("%w: body on %q: %v", ErrUndecodable, subject, err)
	}
	return nil
}
