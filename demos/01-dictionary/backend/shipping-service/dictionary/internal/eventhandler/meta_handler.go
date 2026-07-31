package eventhandler

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
)

// metaEvent carries the fields the meta projector needs from a
// ContainerEvent envelope.
type metaEvent struct {
	Context     string `json:"context"`
	ContainerID string `json:"containerID"`
}

// RegisterMeta starts the meta projector: it maintains the cross-cutting
// lookup sets in the meta-{context} KV bucket:
//
//	known-containers — every registered container ID (JSON array, sorted)
//
// This survives reload without event replay and without the frontend having
// to accumulate it client-side. A single durable consumer processes events
// sequentially, so the read-merge-write below has no concurrent writers.
// (known-ports was retired — ports are now the Postgres reference table.)
//
// nc is optional (nil-safe, Phase 15b) — see RegisterShapeA's doc comment;
// after every KV write that actually changes the set, this fire-and-forget
// publishes notify.{context}.shipping.meta.changed carrying the full,
// updated known-containers array.
func RegisterMeta(ctx context.Context, js jetstream.JetStream, kv *kvstore.Store, nc *nats.Conn, log *slog.Logger) (jetstream.ConsumeContext, error) {
	// See handler.go's register() for why: the Consume callback below closes
	// over this context for the projector's entire lifetime, so it must not
	// be tied to whatever short-lived context the caller used to register it
	// (e.g. an HTTP request context, Phase 13b's tenant switch).
	msgCtx := context.WithoutCancel(ctx)
	cons, err := js.CreateOrUpdateConsumer(ctx, domain.StreamName, jetstream.ConsumerConfig{
		Durable:       "meta-projector",
		FilterSubject: domain.SubjectContainerWildcard,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return nil, err
	}
	return cons.Consume(func(msg jetstream.Msg) {
		var event metaEvent
		if err := json.Unmarshal(msg.Data(), &event); err != nil || event.Context == "" {
			_ = msg.Ack()
			return
		}

		aggregate, eventType, ok := domain.SubjectTokens(msg.Subject())
		if !ok || aggregate != "container" || eventType != domain.ContainerRegisteredEvent {
			_ = msg.Ack()
			return
		}
		data, err := mergeSet(msgCtx, kv, event.Context, queries.MetaKeyKnownContainers, event.ContainerID)
		if err != nil {
			log.Error("meta projection failed, will redeliver", "subject", msg.Subject(), "err", err)
			_ = msg.Nak()
			return
		}
		if data != nil {
			publishNotify(nc, log, event.Context, "meta", data)
		}
		_ = msg.Ack()
	})
}

// mergeSet reads the JSON string array at key, merges the values in, and
// writes it back sorted. No-op when every value is already present: returns
// a nil byte slice (not an error) so the caller can skip publishNotify
// without an extra KV read for its payload.
func mergeSet(ctx context.Context, kv *kvstore.Store, kvContext, key string, values ...string) ([]byte, error) {
	existing := []string{}
	if raw, _, err := kv.Get(ctx, kvContext, key); err == nil {
		_ = json.Unmarshal(raw, &existing)
	}

	seen := make(map[string]bool, len(existing))
	for _, v := range existing {
		seen[v] = true
	}
	changed := false
	for _, v := range values {
		if v != "" && !seen[v] {
			existing = append(existing, v)
			seen[v] = true
			changed = true
		}
	}
	if !changed {
		return nil, nil
	}
	sort.Strings(existing)
	data, err := json.Marshal(existing)
	if err != nil {
		return nil, err
	}
	if _, err := kv.Put(ctx, kvContext, key, data); err != nil {
		return nil, err
	}
	return data, nil
}
