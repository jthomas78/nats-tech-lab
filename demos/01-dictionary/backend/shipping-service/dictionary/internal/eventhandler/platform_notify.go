package eventhandler

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

const (
	refdataChangeStreamName = "REFDATA"

	// platformBridgeRetryDelay bounds how long a platform notify bridge waits
	// before retrying consumer setup after a transient failure (e.g.
	// refdata-service hasn't published its first change yet, so REFDATA
	// doesn't exist) or after its Messages() feed ends unexpectedly.
	platformBridgeRetryDelay = 5 * time.Second
)

// runPlatformNotifyBridge is the shared retry/consume loop behind
// RegisterRefdataNotify (Phase 23) — and, until Phase 28g's retirement,
// RegisterRPCTraceNotify too: it bridges a PLATFORM-account JetStream stream
// shipping-admin's restricted permissions can reach only via the
// ordered-consumer API (bootstrap-operator.sh denies raw core-NATS
// subscribe on evt.> for that user) onto a notify.* subject the Admin UI's
// browser-held PLATFORM connection can subscribe to directly. Runs for the
// life of the process — ctx cancellation
// (process shutdown) is the only way out. A stream that doesn't exist yet,
// or a Messages() feed that ends (server restart, consumer deleted
// underneath it), is retried after platformBridgeRetryDelay rather than
// treated as fatal, since this has no per-request caller to retry it for it
// the way the SSE handler it replaces did.
func runPlatformNotifyBridge(ctx context.Context, platformJS jetstream.JetStream, streamName string, cfg jetstream.OrderedConsumerConfig, log *slog.Logger, publish func(msg jetstream.Msg)) {
	go func() {
		for ctx.Err() == nil {
			consumer, err := platformJS.OrderedConsumer(ctx, streamName, cfg)
			if err != nil {
				if !errors.Is(err, jetstream.ErrStreamNotFound) {
					log.Error("platform notify bridge: create consumer", "stream", streamName, "err", err)
				}
				sleepOrDone(ctx, platformBridgeRetryDelay)
				continue
			}
			msgs, err := consumer.Messages()
			if err != nil {
				log.Error("platform notify bridge: consume messages", "stream", streamName, "err", err)
				sleepOrDone(ctx, platformBridgeRetryDelay)
				continue
			}
			for {
				msg, err := msgs.Next()
				if err != nil {
					break // stream/consumer went away — fall through, re-create it
				}
				publish(msg)
			}
			msgs.Stop()
			sleepOrDone(ctx, platformBridgeRetryDelay)
		}
	}()
}

func sleepOrDone(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// parseRefdataChangedSubject splits an evt.{context}.refdata.{typeKey}.changed
// subject (refdata-service's own published contract — see
// dictionary/internal/rest/sse.go's refdataChangeStreamName doc comment) into
// its context and typeKey tokens.
func parseRefdataChangedSubject(subject string) (kvContext, typeKey string, ok bool) {
	parts := strings.Split(subject, ".")
	if len(parts) != 5 || parts[0] != "evt" || parts[2] != "refdata" {
		return "", "", false
	}
	return parts[1], parts[3], true
}

// RegisterRefdataNotify starts a permanent background bridge (Phase 23):
// refdata-service's REFDATA JetStream stream (PLATFORM account,
// evt.{context}.refdata.{typeKey}.changed) republished onto
// notify._platform.refdata.{context}.{typeKey}.changed — plain core NATS
// pub/sub the Admin UI's PLATFORM-account browser connection subscribes to
// directly, replacing the per-SSE-connection OrderedConsumer
// dictionary/internal/rest/sse.go's watchRefdata used to create. Unfiltered
// across every context, unlike that SSE handler's single-active-tenant
// filter — refdata-service has no per-tenant partition to filter by (Phase
// 13b: it's shared across every tenant), so this bridges every context's
// changes and lets subscribers filter client-side if they want to.
//
// platformNC must be shipping-admin's restricted PLATFORM connection with
// notify._platform.> added to its allow-pub list (bootstrap-operator.sh) —
// without that grant, Publish below fails silently (best-effort, logged).
//
// Nil-safe on both platformJS and platformNC (same convention as
// publishNotify's nc check) — a nil either way means this bridge has nothing
// to consume or nowhere to publish, so it's simply not started rather than
// panicking on a nil-interface method call.
func RegisterRefdataNotify(ctx context.Context, platformJS jetstream.JetStream, platformNC *nats.Conn, log *slog.Logger) {
	if platformJS == nil || platformNC == nil {
		return
	}
	runPlatformNotifyBridge(ctx, platformJS, refdataChangeStreamName, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{"evt.*.refdata.>"},
		DeliverPolicy:  jetstream.DeliverNewPolicy,
	}, log, func(msg jetstream.Msg) {
		kvContext, typeKey, ok := parseRefdataChangedSubject(msg.Subject())
		if !ok {
			return
		}
		subject := "notify._platform.refdata." + kvContext + "." + typeKey + ".changed"
		if err := platformNC.Publish(subject, msg.Data()); err != nil {
			log.Warn("refdata notify publish failed", "subject", subject, "err", err)
			return
		}
		// Phase 43a (BR-045): tokens given explicitly. The subject's own
		// {context} token is the literal "_platform" (this bridge republishes
		// into PLATFORM), but the change itself belongs to kvContext — so the
		// observation is filed under the business context an operator would
		// search for, not under the bridge's plumbing.
		natstrace.ObserveAs(platformNC, nil, subject, msg.Data(), kvContext, "refdata", typeKey, "changed")
	})
}

// RegisterRPCTraceNotify (Phase 23) was retired in Phase 28g: it bridged
// refdata-service's RPCTRACE stream (obs.rpc.*, BR-D29) onto
// notify._platform.rpctrace.entry for the Admin UI's old [messages] tab.
// Nothing had published to obs.rpc.* since Phase 28a-28e replaced every
// adapter's publishObs call with a natstrace span — this bridge was a live
// pipe carrying nothing. The [messages] tab now derives from obs.trace.*/
// the trace-request-reply KV bucket instead (BR-026's Phase 28g amendment,
// BUSINESS_RULES-SHIPPING.md). See platform_notify_test.go's removed
// specs and BUSINESS_RULES-REFDATA.md's BR-D29 for the corresponding
// retirement on the publish/stream-provisioning side.
