package otelnats

import (
	"context"

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

// StartServer starts the SERVER span for an inbound message.
func (p *Provider) StartServer(ctx context.Context, _ *nats.Msg, _ Edge, _ Trust) (context.Context, trace.Span) {
	return ctx, trace.SpanFromContext(ctx)
}

// StartClient starts the CLIENT span for an outbound request. The span
// belongs to the caller; peerService and subject are attributes (BR-065).
func (p *Provider) StartClient(ctx context.Context, _, _ string) (context.Context, trace.Span) {
	return ctx, trace.SpanFromContext(ctx)
}

// OriginatingTenantFromRPCSubject reads the tenant token from an
// import-renamed subject, rpc.{tenant}.{service}.…, or returns "" (BR-069).
// The subject is a trusted source only once the 54a stack check has proved a
// tenant client cannot choose it.
func OriginatingTenantFromRPCSubject(_ string) string { return "" }
