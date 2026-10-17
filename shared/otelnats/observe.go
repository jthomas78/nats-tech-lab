package otelnats

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Observer publishes the obs.pubsub.* observation for an evt.* or notify.*
// publish (BR-045). It is the Messages wire tap's publisher, moved here from
// the retired legacy tracer (decision 13). The trace and span IDs come from the OTel span in
// ctx, so a Messages row still links to its trace in Tempo.
//
// The envelope keeps the legacy tracer's field names, so the
// observability-service ingest (BR-047) and the Admin UI read it unchanged.
type Observer struct {
	nc *nats.Conn
}

// NewObserver returns an Observer that publishes on nc. nc is the account
// the observation belongs to: the tenant connection for a tenant publish
// (BR-AC34). A nil Observer is silent.
func NewObserver(nc *nats.Conn) *Observer {
	return &Observer{nc: nc}
}

// envelope is the obs.pubsub.* wire shape: the legacy tracer's span record, cut to the
// fields a publish observation fills. The JSON names must not change (BR-047).
type envelope struct {
	Direction    string          `json:"direction"`
	Subject      string          `json:"subject"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	Timestamp    time.Time       `json:"timestamp"`
	PayloadBytes int             `json:"payloadBytes"`
	TraceID      string          `json:"traceId,omitempty"`
	SpanID       string          `json:"spanId,omitempty"`
	ParentSpanID string          `json:"parentSpanId,omitempty"`
	Service      string          `json:"service,omitempty"`
	Entity       string          `json:"entity,omitempty"`
	Action       string          `json:"action,omitempty"`
	Redacted     []string        `json:"redacted,omitempty"`
	Truncated    bool            `json:"truncated,omitempty"`
}

// ObservePublish observes a publish whose subject has the positional shape
// {family}.{context}.{service}.{entity}….{action}. It skips obs.* subjects
// (no self-observation) and subjects of fewer than five tokens. The action
// is the last token, because evt.* puts the aggregate ID before it.
func (o *Observer) ObservePublish(ctx context.Context, subject string, payload []byte) {
	if strings.HasPrefix(subject, "obs.") {
		return
	}
	parts := strings.Split(subject, ".")
	if len(parts) < 5 {
		return
	}
	o.ObservePublishAs(ctx, subject, payload, parts[1], parts[2], parts[3], parts[len(parts)-1])
}

// ObservePublishAs observes a publish under tokens the caller names, for a
// subject whose shape the positional reading would get wrong.
//
// ctx should hold the publish's PRODUCER span: its trace ID and span ID
// become the envelope's traceId and spanId, and its parent becomes
// parentSpanId. With no span in ctx, the envelope gets a new root trace ID.
// Nats-Msg-Id is the envelope's spanId (BR-047's dedup).
//
// Fire-and-forget (BR-045): no flush, the publish error is dropped, and a
// panic is recovered. Observing must never fail the business publish.
func (o *Observer) ObservePublishAs(ctx context.Context, subject string, payload []byte, contextValue, service, entity, action string) {
	if o == nil || o.nc == nil {
		return
	}
	defer func() { _ = recover() }()

	traceID, spanID, parentSpanID := observationIDs(ctx)

	// Redact before truncating (BR-046).
	body, payloadBytes, redacted, truncated := preparePayload(payload)

	data, err := json.Marshal(envelope{
		Direction:    "publish",
		Subject:      subject,
		Payload:      body,
		Timestamp:    time.Now().UTC(),
		PayloadBytes: payloadBytes,
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		Service:      service,
		Entity:       entity,
		Action:       action,
		Redacted:     redacted,
		Truncated:    truncated,
	})
	if err != nil {
		return
	}
	msg := nats.NewMsg(fmt.Sprintf("obs.pubsub.%s.%s.%s.%s", contextValue, service, entity, action))
	msg.Header.Set(nats.MsgIdHdr, spanID)
	msg.Data = data
	_ = o.nc.PublishMsg(msg)
}

// observationIDs reads the envelope's IDs from the span in ctx. The parent
// is known only for an SDK span; a remote or non-SDK span gives none. With
// no valid span, it mints a new root.
func observationIDs(ctx context.Context) (traceID, spanID, parentSpanID string) {
	if ctx == nil {
		ctx = context.Background()
	}
	span := trace.SpanFromContext(ctx)
	sc := span.SpanContext()
	if !sc.IsValid() {
		return randomHex(16), randomHex(8), ""
	}
	if ro, ok := span.(sdktrace.ReadOnlySpan); ok && ro.Parent().IsValid() {
		parentSpanID = ro.Parent().SpanID().String()
	}
	return sc.TraceID().String(), sc.SpanID().String(), parentSpanID
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
