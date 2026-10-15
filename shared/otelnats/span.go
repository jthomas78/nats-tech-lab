package otelnats

import (
	"context"
	"strings"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Tenant attribute names (BR-069). Kept apart on purpose: the receiving
// account is always known; the originating tenant is often not.
const (
	AttrReceivingAccount  = attribute.Key("lab.tenant.receiving_account")
	AttrOriginatingTenant = attribute.Key("lab.tenant.originating")
)

// Edge says how far an inbound message's trace context is trusted.
type Edge int

const (
	// EdgeRPC is a call between trusted services (rpc.*, evt.*, notify.*):
	// the incoming traceparent is the parent.
	EdgeRPC Edge = iota
	// EdgeAPI is a browser call (api.*): the span starts a new root, sampled
	// always-on, and a valid incoming traceparent is only a link (BR-071).
	EdgeAPI
)

// Trust is the tenant context the receiving adapter vouches for. It is the
// only input to the tenant attributes; StartServer never reads a header or
// the body for them (BR-069).
type Trust struct {
	// ReceivingAccount is the account of the connection the message arrived
	// on. Always known.
	ReceivingAccount string
	// OriginatingTenant comes only from server-written routing context. Empty
	// means unattributed.
	OriginatingTenant string
}

// Attribute keys from the OTel messaging and peer conventions (BR-065).
const (
	attrMessagingSystem      = attribute.Key("messaging.system")
	attrMessagingDestination = attribute.Key("messaging.destination.name")
	attrPeerService          = attribute.Key("peer.service")
)

// StartServer starts the SERVER span for an inbound message. The span is
// named for the subject.
//
// EdgeRPC continues the trace in ctx; when ctx holds none, it extracts one
// from msg. EdgeAPI always starts a new, sampled root, and keeps a valid
// traceparent from msg as one link, never as the parent (BR-071).
//
// The tenant attributes come from trust only (BR-069). StartServer reads
// msg's subject and its traceparent, and nothing else in it.
func (p *Provider) StartServer(ctx context.Context, msg *nats.Msg, edge Edge, trust Trust) (context.Context, trace.Span) {
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(tenantAttrs(msg.Subject, trust)...),
	}

	switch edge {
	case EdgeAPI:
		opts = append(opts, trace.WithNewRoot())
		sent := trace.SpanContextFromContext(Extract(context.Background(), msg.Header))
		if sent.IsValid() {
			opts = append(opts, trace.WithLinks(trace.Link{SpanContext: sent}))
		}
	default:
		if !trace.SpanContextFromContext(ctx).IsValid() {
			ctx = Extract(ctx, msg.Header)
		}
	}
	return p.tracer.Start(ctx, msg.Subject, opts...)
}

// tenantAttrs is the attribute set of an inbound span: the messaging
// attributes, and the tenant attributes from trust only (BR-069).
func tenantAttrs(subject string, trust Trust) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attrMessagingSystem.String("nats"),
		attrMessagingDestination.String(subject),
		AttrReceivingAccount.String(trust.ReceivingAccount),
	}
	if trust.OriginatingTenant != "" {
		attrs = append(attrs, AttrOriginatingTenant.String(trust.OriginatingTenant))
	}
	return attrs
}

// StartClient starts the CLIENT span for an outbound request, as the child
// of the span in ctx. The span belongs to the caller; peerService and subject
// are attributes (BR-065).
func (p *Provider) StartClient(ctx context.Context, subject, peerService string) (context.Context, trace.Span) {
	return p.tracer.Start(ctx, subject,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attrMessagingSystem.String("nats"),
			attrMessagingDestination.String(subject),
			attrPeerService.String(peerService),
		),
	)
}

// OriginatingTenantFromRPCSubject reads the tenant token from an
// import-renamed subject, rpc.{tenant}.{service}.…, or returns "" (BR-069).
// The subject is a trusted source only once the 54a stack check has proved a
// tenant client cannot choose it.
func OriginatingTenantFromRPCSubject(subject string) string {
	tokens := strings.Split(subject, ".")
	if len(tokens) < 3 || tokens[0] != "rpc" {
		return ""
	}
	switch tenant := tokens[1]; tenant {
	case "", "*", ">":
		return ""
	default:
		return tenant
	}
}
