// Package natsnotify owns the notify.* subject family's publish path.
//
// notify.* is service-side change notification: core NATS, fire-and-forget,
// no PubAck, no durability. It is the sibling of the evt.* seam
// (jstream.Publisher.PublishWithTrace) under the same contract — a single
// place where a domain publish and its BR-045 observation are sequenced —
// but over a different transport, which is why the two stay separate rather
// than merging.
//
// The module deliberately does not build subjects. Arities across the family
// run from four tokens (notify.accounts.account.{action}, which carries no
// {context} at all) to structurally unbounded
// (notify.{context}.kv.{bucket}.{key}.changed, where a KV key contains dots),
// so there is no grammar to build against. Each service owns its own named
// subject constructors and passes the finished subject here, together with
// the four observability tokens.
//
// Those tokens are always explicit and are never derived from the subject.
// Deriving them by position is what natstrace.ObservePublish does for evt.*,
// and it is wrong for two members of this family: refdata publishes
// notify._platform.refdata.{context}.… but must be observed under
// {context}, not the literal _platform in token 1; and accounts' four-token
// subject falls below that deriver's floor and would be skipped outright.
// Taking the tokens from the caller, who knew them when it built the subject,
// removes the guess rather than centralising it.
package natsnotify

import (
	"context"
	"log/slog"

	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
	"github.com/nats-io/nats.go"
)

// Tokens are the four observability tokens BR-045 attributes an observation
// by: the business {context}, the publishing service, the entity that
// changed, and the action taken. A struct rather than four positional
// strings, because all four are strings and a transposed pair would
// otherwise be invisible at the call site and misattribute the envelope.
type Tokens struct {
	Context string
	Service string
	Entity  string
	Action  string
}

// Subject is a built notify.* subject together with the tokens describing
// it. The two travel as one value, returned by a service's own subject
// constructor, so a caller cannot pair a subject with another shape's tokens
// — the mistake that has no symptom until an operator searches the Messages
// panel for traffic filed under the wrong tenant.
type Subject struct {
	Name   string
	Tokens Tokens
}

// Notifier publishes notify.* messages on one connection, and — when
// observation is enabled — emits their BR-045 obs.pubsub.* envelopes on
// another.
//
// It holds its connection rather than taking one per publish because
// connection identity is load-bearing: BR-D45 requires the envelope to go out
// on the tenant's own connection, since that is what places it inside that
// tenant's account so BR-AC34's import remap attributes the right tenant. A
// Notifier is a connection plus a gate, so publishing on the wrong one is not
// expressible. A fan-out publisher serving many tenants (refdata's
// tenantPublisher) therefore holds one Notifier per tenant.
type Notifier struct {
	nc  *nats.Conn
	log *slog.Logger
	obs *natstrace.Tracer // nil unless WithObservation was given
}

// Option configures a Notifier at construction.
type Option func(*Notifier)

// WithObservation turns on BR-045 observation, emitting each published
// message's obs.pubsub.* envelope on obsNC.
//
// Observation is opt-in, matching the evt.* seam's own gate: a publisher that
// was never given one stays silent, and its silence is a fact about how it
// was constructed rather than an entry in a hand-maintained exclusion list.
// observability-service's own notify.* publishers rely on exactly this —
// pubsubstore announces writes to the bucket these envelopes land in, so
// observing it would be an unbounded feedback loop.
//
// Unlike the evt.* seam's two-phase EnableObservation this is a constructor
// option, so no Notifier ever exists in a half-configured state. The
// divergence is deliberate; the evt.* seam converges on this shape when the
// shared/jstream deduplication lands.
func WithObservation(obsNC *nats.Conn) Option {
	return func(n *Notifier) { n.obs = natstrace.New(obsNC) }
}

// New returns a Notifier publishing on nc and logging failures to log. Both
// may be nil: a nil connection makes Publish a no-op, and a nil logger
// silences the warning. This mirrors what every call site already tolerated
// as scattered helpers, where an unconfigured notify connection is a
// supported deployment rather than an error.
func New(nc *nats.Conn, log *slog.Logger, opts ...Option) *Notifier {
	n := &Notifier{nc: nc, log: log}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

// Publish sends payload on subj and, if observation is enabled, emits its
// BR-045 envelope.
//
// It returns nothing. notify.* is a change notification, not a command: a
// dropped one costs a subscriber a refresh it will take on its next poll or
// watch, and there is no caller in this repo that could act on the error.
// Failures are logged and swallowed, exactly as the call sites this replaces
// already did.
//
// The observation is emitted only after the publish reaches the wire — a
// notify that failed must not appear in the Messages panel as though it had
// happened — and continues the span on ctx when there is one, so a
// notification caused by an api.*/rpc.* call joins that call's waterfall
// instead of arriving as an orphan.
func (n *Notifier) Publish(ctx context.Context, subj Subject, payload []byte) {
	if n == nil || n.nc == nil {
		return
	}

	// A nil ctx is tolerated: several publishers this replaces are reached
	// from a KV watch callback that has no request context behind it.
	var sp *natstrace.Span
	if ctx != nil {
		sp = natstrace.SpanFromContext(ctx)
	}
	msg := &nats.Msg{Subject: subj.Name, Data: payload}
	if tp := sp.Traceparent(); tp != "" {
		msg.Header = nats.Header{natstrace.TraceparentHeader: []string{tp}}
	}

	if err := n.nc.PublishMsg(msg); err != nil {
		if n.log != nil {
			n.log.Warn("notify publish failed", "subject", subj.Name, "err", err)
		}
		return
	}

	tok := subj.Tokens
	n.obs.ObservePublishAs(sp, subj.Name, payload, tok.Context, tok.Service, tok.Entity, tok.Action)
}
