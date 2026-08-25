// Package notify builds this service's notify.* subjects and pairs each with
// the observability tokens that describe it.
//
// The subjects used to be concatenated inline at five call sites across four
// packages, and the tokens recovered afterwards by parsing the result back
// apart. Here each shape is one named constructor that returns both halves
// together, so the tokens are a fact about the subject rather than a guess
// at it — which matters most for the two shapes whose tokens do not sit
// where a positional reader would look for them (see Raw and KVChanged).
//
// Subject construction lives in the service, not in shared/natsnotify:
// arities across the notify.* family run from four tokens to unbounded, so
// there is no grammar a shared module could build against.
package notify

import "github.com/jthomas78/nats-tech-lab/shared/natsnotify"

// service is the {service} token every shape below is attributed to, except
// the KV and refdata-bridge shapes, which name their own.
const service = "shipping"

// Changed builds notify.{context}.shipping.{entity}.changed — the projected
// current state of a ship, container or meta entity after a KV write.
func Changed(kvContext, entity string) natsnotify.Subject {
	return natsnotify.Subject{
		Name:   "notify." + kvContext + "." + service + "." + entity + ".changed",
		Tokens: natsnotify.Tokens{Context: kvContext, Service: service, Entity: entity, Action: "changed"},
	}
}

// Raw builds notify.{context}.shipping.raw.{entity}.{event} — the domain
// event as it arrived off the SHIPPING stream, carrying the actual verb
// rather than a projected snapshot (Phase 23).
//
// The literal "raw" sits where a positional reader takes the entity, and the
// action is the domain verb rather than "changed", so both tokens are named
// here rather than derived.
func Raw(kvContext, entity, event string) natsnotify.Subject {
	return natsnotify.Subject{
		Name:   "notify." + kvContext + "." + service + ".raw." + entity + "." + event,
		Tokens: natsnotify.Tokens{Context: kvContext, Service: service, Entity: entity, Action: event},
	}
}

// PortChanged builds notify.{context}.shipping.port.changed, published by the
// api.* adapter after a port write.
func PortChanged(kvContext string) natsnotify.Subject {
	return Changed(kvContext, "port")
}

// KVChanged builds notify.{context}.kv.{bucket}.{key}.changed, published by
// kvstore after a successful Put or Delete so the Admin UI's KV inspector can
// watch a bucket directly.
//
// This shape has no fixed arity: the key is itself dotted
// ({context}.{entityType}.{id}), so the subject grows a token per key
// segment. Naming the tokens keeps the observation subject stable however
// long a key gets, and says which of "kv" and the bucket is the service and
// which the entity rather than leaving it to position.
func KVChanged(kvContext, bucket, key string) natsnotify.Subject {
	return natsnotify.Subject{
		Name:   "notify." + kvContext + ".kv." + bucket + "." + key + ".changed",
		Tokens: natsnotify.Tokens{Context: kvContext, Service: "kv", Entity: bucket, Action: "changed"},
	}
}

// RefdataChanged builds notify._platform.refdata.{context}.{typeKey}.changed,
// republished into PLATFORM by the bridge that follows refdata-service's
// evt.*.refdata.> traffic.
//
// The subject's own {context} position holds the literal "_platform" — this
// bridge's plumbing — while the change itself belongs to kvContext. The
// observation is filed under the business context an operator would search
// for, which is the discrepancy that makes deriving tokens from this subject
// actively wrong rather than merely fragile.
func RefdataChanged(kvContext, typeKey string) natsnotify.Subject {
	return natsnotify.Subject{
		Name:   "notify._platform.refdata." + kvContext + "." + typeKey + ".changed",
		Tokens: natsnotify.Tokens{Context: kvContext, Service: "refdata", Entity: typeKey, Action: "changed"},
	}
}
