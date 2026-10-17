package eventhandler

import (
	"context"
	"log/slog"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

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

// startMessage starts the one span for one projector message and returns
// its ctx and the function that ends it: nil for success, the projection
// error otherwise.
//
// The per-message ctx starts from context.Background(), so the CONSUMER
// span's parent is the message's traceparent (BR-037) and never a span
// left on the registration ctx. Without a Provider the message is untraced
// (Phase 54d).
//
// The span records entity_id and consumer.durable: the container and meta
// projectors consume the same evt.* message, and the durable name is what
// tells their two spans apart.
func (c projectorConfig) startMessage(
	msgCtx context.Context,
	msg jetstream.Msg,
	id, durable string,
) (context.Context, func(error)) {
	if c.otel != nil {
		ctx, span := c.otel.StartConsumer(context.Background(),
			&nats.Msg{Subject: msg.Subject(), Header: msg.Headers()},
			otelnats.Trust{ReceivingAccount: c.account})
		span.SetAttributes(
			attribute.String("entity_id", id),
			attribute.String(consumerDurableAttr, durable),
		)
		return ctx, func(err error) {
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			span.End()
		}
	}

	return msgCtx, func(error) {}
}
