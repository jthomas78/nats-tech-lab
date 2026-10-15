// Package notifybridge republishes refdata-service's own
// evt.{context}.refdata.{typeKey}.changed change-event pointers onto
// notify.{context}.refdata.{typeKey}.changed inside every tenant account
// (BR-D42), replacing shipping-service's /api/refdata-watch SSE relay.
//
// Why a bridge rather than publishing notify.* directly at mutation time:
// the mutation path (kvcache.Projector) runs on refdata-service's single
// PLATFORM connection, but a browser subscribes from inside its own tenant
// account — the two are different NATS accounts, and a PLATFORM publish is
// not visible from a tenant connection. Consuming the durable evt.* feed
// once and fanning out over the per-tenant connections (BR-D40) is what
// crosses that boundary, and it reuses the existing published contract
// rather than adding a second publish call to every mutation site.
//
// notify.* is deliberately lossy: DeliverNewPolicy, no replay, failures
// logged and dropped. A client that needs the guaranteed feed reads evt.*
// from the backend side (BR-D42); a browser only needs "something changed,
// refetch".
package notifybridge

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvcache"
	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

// retryDelay bounds how long the bridge waits before retrying consumer
// setup after a transient failure (e.g. REFDATA doesn't exist yet because
// nothing has been published) or after its Messages() feed ends.
const retryDelay = 5 * time.Second

// Changed builds this service's one notify.* shape,
// notify.{context}.refdata.{typeKey}.changed, paired with the tokens it is
// observed under.
//
// The pair travels as one value so the fan-out below cannot attribute it to
// anything else: the bridge knows the context and type key here, having just
// parsed them out of the evt.* subject, and nothing downstream has to read
// them back out of the subject it built.
func Changed(contextKey, typeKey string) natsnotify.Subject {
	return natsnotify.Subject{
		Name: "notify." + contextKey + "." + kvcache.Domain + "." + typeKey + ".changed",
		Tokens: natsnotify.Tokens{
			Context: contextKey,
			Service: kvcache.Domain,
			Entity:  typeKey,
			Action:  "changed",
		},
	}
}

// Publisher is the fan-out sink — satisfied by tenants.Manager.PublishToAll.
type Publisher interface {
	PublishToAll(ctx context.Context, subj natsnotify.Subject, data []byte)
}

// parseChangedSubject splits an evt.{context}.refdata.{typeKey}.changed
// subject into its context and typeKey tokens.
func parseChangedSubject(subject string) (contextKey, typeKey string, ok bool) {
	parts := strings.Split(subject, ".")
	if len(parts) != 5 || parts[0] != "evt" || parts[2] != kvcache.Domain || parts[4] != "changed" {
		return "", "", false
	}
	return parts[1], parts[3], true
}

// Option configures Run.
type Option func(*config)

type config struct {
	otel *otelnats.Provider
}

// WithOTel traces each bridged message with a CONSUMER span (Phase 54b-3a).
// The span continues the evt.* message's traceparent, and the fan-out runs
// under it, so each notify.* PRODUCER span is its child. A nil Provider
// leaves the bridge untraced, as before.
func WithOTel(p *otelnats.Provider) Option {
	return func(c *config) { c.otel = p }
}

// Run starts the bridge as a permanent background goroutine. Runs for the
// life of the process — ctx cancellation (shutdown) is the only way out.
// Nil-safe on both platformJS and pub: a nil either way means there is
// nothing to consume or nowhere to publish, so the bridge is simply not
// started rather than panicking.
func Run(ctx context.Context, platformJS jetstream.JetStream, pub Publisher, log *slog.Logger, opts ...Option) {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	if platformJS == nil || pub == nil {
		return
	}
	go func() {
		for ctx.Err() == nil {
			consumer, err := platformJS.OrderedConsumer(ctx, kvcache.ChangeStreamName, jetstream.OrderedConsumerConfig{
				FilterSubjects: []string{kvcache.ChangeSubjectWildcard},
				DeliverPolicy:  jetstream.DeliverNewPolicy,
			})
			if err != nil {
				// REFDATA not existing yet is a legitimate startup race, not
				// an error worth logging on every retry.
				if !errors.Is(err, jetstream.ErrStreamNotFound) && log != nil {
					log.Error("refdata notify bridge: create consumer", "err", err)
				}
				sleepOrDone(ctx, retryDelay)
				continue
			}
			msgs, err := consumer.Messages()
			if err != nil {
				if log != nil {
					log.Error("refdata notify bridge: consume messages", "err", err)
				}
				sleepOrDone(ctx, retryDelay)
				continue
			}
			for {
				msg, err := msgs.Next()
				if err != nil {
					break // stream/consumer went away — fall through, re-create it
				}
				contextKey, typeKey, ok := parseChangedSubject(msg.Subject())
				if !ok {
					continue
				}
				bridge(ctx, cfg.otel, pub, msg, Changed(contextKey, typeKey))
			}
			msgs.Stop()
			sleepOrDone(ctx, retryDelay)
		}
	}()
}

// platformAccount is the account the bridge consumes REFDATA in. It is the
// receiving account on the CONSUMER span (BR-069).
const platformAccount = "PLATFORM"

// bridge fans one evt.* message out. With OTel the fan-out runs under a
// CONSUMER span that continues the message's traceparent, so each tenant's
// notify.* PRODUCER span is its child (Phase 54b-3a). Without OTel the
// fan-out runs under the process ctx, as before.
func bridge(ctx context.Context, p *otelnats.Provider, pub Publisher, msg jetstream.Msg, subj natsnotify.Subject) {
	if p == nil {
		pub.PublishToAll(ctx, subj, msg.Data())
		return
	}
	// StartConsumer reads the subject and the traceparent header only.
	spanCtx, span := p.StartConsumer(ctx, &nats.Msg{Subject: msg.Subject(), Header: msg.Headers()},
		otelnats.Trust{ReceivingAccount: platformAccount})
	defer span.End()
	pub.PublishToAll(spanCtx, subj, msg.Data())
}

func sleepOrDone(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
