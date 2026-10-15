package otelnats

import (
	"context"

	"github.com/nats-io/nats.go"
)

// Observer publishes the obs.pubsub.* observation for an evt.* or notify.*
// publish (BR-045). It is the Messages wire tap's publisher, moved here from
// natstrace (decision 13). The trace and span IDs come from the OTel span in
// ctx, so a Messages row still links to its trace in Tempo.
//
// The envelope keeps natstrace's field names, so the observability-service
// ingest (BR-047) and the Admin UI read it unchanged.
type Observer struct {
	nc *nats.Conn
}

// NewObserver returns an Observer that publishes on nc. nc is the account
// the observation belongs to: the tenant connection for a tenant publish
// (BR-AC34). A nil Observer is silent.
func NewObserver(nc *nats.Conn) *Observer {
	return &Observer{nc: nc}
}

// ObservePublish observes a publish whose subject has the positional shape
// {family}.{context}.{service}.{entity}….{action}. It skips obs.* subjects
// (no self-observation) and subjects of fewer than five tokens.
//
// STATUS: skeleton (54b-1). Specs are red until 54b-2.
func (o *Observer) ObservePublish(ctx context.Context, subject string, payload []byte) {}

// ObservePublishAs observes a publish under tokens the caller names, for a
// subject whose shape the positional reading would get wrong.
//
// ctx should hold the publish's PRODUCER span: its trace ID and span ID
// become the envelope's traceId and spanId, and its parent becomes
// parentSpanId. With no span in ctx, the envelope gets a new root trace ID.
// Nats-Msg-Id is the envelope's spanId (BR-047's dedup).
//
// STATUS: skeleton (54b-1). Specs are red until 54b-2.
func (o *Observer) ObservePublishAs(ctx context.Context, subject string, payload []byte, contextValue, service, entity, action string) {
}
