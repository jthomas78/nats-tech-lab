package eventhandler

import (
	"context"
	"log/slog"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
)

// Option configures a projector at registration.
type Option func(*projectorConfig)

type projectorConfig struct {
	otel    *otelnats.Provider
	account string
}

// WithTracing puts a projector on the OTel path (Phase 54b-4): each message
// is consumed under a CONSUMER span that continues the evt.* publish's trace
// (decision 5), with account as the receiving account (BR-069), and the
// projector's notify.* publishes are PRODUCER spans under it. A nil
// provider is the same as not passing the option.
func WithTracing(p *otelnats.Provider, account string) Option {
	return func(c *projectorConfig) {
		c.otel = p
		c.account = account
	}
}

func newProjectorConfig(opts []Option) projectorConfig {
	var c projectorConfig
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

// notifier is the projector's notify.* seam: notifier(nc, log), on the OTel
// path when the projector is.
func (c projectorConfig) notifier(nc *nats.Conn, log *slog.Logger) *natsnotify.Notifier {
	if c.otel == nil {
		return notifier(nc, log)
	}
	return notifier(nc, log, natsnotify.WithOTel(c.otel))
}

// runMessage runs project for one projector message under its one span and
// returns project's error. The caller Acks or Naks after it returns, so the
// span has ended by then.
//
// The span is otelnats.Consume's CONSUMER span: its parent is the message's
// traceparent (BR-037), never a span left on the registration ctx (Phase 57,
// decision 6). Without a Provider the message is untraced (Phase 54d).
//
// The base ctx differs by path, and Phase 57 keeps that split exactly
// (BR-082, characterized in projector_ctx_test.go): traced, the message runs
// on context.Background(); untraced, on msgCtx, which is
// context.WithoutCancel(registration ctx). Neither is cancelled with the
// registration ctx; only the untraced one carries its values. Phase 110
// owns any change to this.
//
// The span records entity_id and consumer.durable: the container and meta
// projectors consume the same evt.* message, and the durable name is what
// tells their two spans apart.
func (c projectorConfig) runMessage(
	msgCtx context.Context,
	msg jetstream.Msg,
	id, durable string,
	project func(ctx context.Context) error,
) error {
	base := msgCtx
	if c.otel != nil {
		base = context.Background() // Phase 110: see above.
	}
	return c.otel.Consume(base, msg.Subject(), msg.Headers(),
		otelnats.Trust{ReceivingAccount: c.account},
		project,
		otelnats.Label{Key: "entity_id", Value: id},
		otelnats.Label{Key: consumerDurableAttr, Value: durable},
	)
}
