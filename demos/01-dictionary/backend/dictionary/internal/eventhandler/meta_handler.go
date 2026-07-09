package eventhandler

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/kvstore"
)

// metaEvent is the union of the fields the meta projector needs from either
// event envelope — both ShipEvent and ContainerEvent decode into it.
type metaEvent struct {
	Context     string `json:"context"`
	Port        string `json:"port"`
	ContainerID string `json:"containerID"`
	OriginPort  string `json:"originPort"`
	DestPort    string `json:"destPort"`
}

// RegisterMeta starts the meta projector: it maintains the cross-cutting
// lookup sets in the meta-{context} KV bucket:
//
//	known-ports      — ship arrival/departure ports plus container origin and
//	                   destination ports (JSON array, sorted)
//	known-containers — every registered container ID (JSON array, sorted)
//
// These survive reload without event replay and without the frontend having
// to accumulate them client-side. A single durable consumer processes events
// sequentially, so the read-merge-write below has no concurrent writers.
func RegisterMeta(ctx context.Context, js jetstream.JetStream, kv *kvstore.Store, log *slog.Logger) (jetstream.ConsumeContext, error) {
	cons, err := js.CreateOrUpdateConsumer(ctx, domain.StreamName, jetstream.ConsumerConfig{
		Durable:       "meta-projector",
		FilterSubject: domain.SubjectWildcard,
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

		var err error
		switch msg.Subject() {
		case domain.SubjectShipArrived, domain.SubjectShipDeparted:
			err = mergeSet(ctx, kv, event.Context, queries.MetaKeyKnownPorts, event.Port)
		case domain.SubjectContainerRegistered:
			err = mergeSet(ctx, kv, event.Context, queries.MetaKeyKnownPorts, event.OriginPort, event.DestPort)
			if err == nil {
				err = mergeSet(ctx, kv, event.Context, queries.MetaKeyKnownContainers, event.ContainerID)
			}
		}
		if err != nil {
			log.Error("meta projection failed, will redeliver", "subject", msg.Subject(), "err", err)
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
}

// mergeSet reads the JSON string array at key, merges the values in, and
// writes it back sorted. No-op when every value is already present.
func mergeSet(ctx context.Context, kv *kvstore.Store, kvContext, key string, values ...string) error {
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
		return nil
	}
	sort.Strings(existing)
	data, err := json.Marshal(existing)
	if err != nil {
		return err
	}
	_, err = kv.Put(ctx, kvContext, key, data)
	return err
}
