package otelnats

import (
	"context"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Middleware wraps a micro handler in a SERVER span (BR-064). It is the
// receiving adapter's seam: the edge and the trust function are the
// adapter's to choose (BR-069, BR-071).
//
// The handler reads its context with ContextFrom. The span ends just before
// the reply is sent, so the caller never sees a reply whose span is still
// open; a handler that never replies has its span ended when it returns.
// A reply carrying micro's Nats-Service-Error header marks the span ERROR,
// as natstrace does today (BR-055).
func (p *Provider) Middleware(edge Edge, trust func(micro.Request) Trust, h micro.HandlerFunc) micro.HandlerFunc {
	return func(req micro.Request) {
		msg := &nats.Msg{Subject: req.Subject(), Header: nats.Header(req.Headers())}
		ctx, span := p.StartServer(context.Background(), msg, edge, trust(req))
		r := &serverRequest{Request: req, ctx: ctx, span: span}
		defer r.end(nil)
		h(r)
	}
}

// ContextFrom returns the context Middleware gave req, or a plain
// background context when Middleware did not wrap req.
func ContextFrom(req micro.Request) context.Context {
	if r, ok := req.(*serverRequest); ok {
		return r.ctx
	}
	return context.Background()
}

// serverRequest is a micro.Request that carries its span's context and ends
// the span when the reply goes out.
type serverRequest struct {
	micro.Request
	ctx  context.Context
	span trace.Span
	once sync.Once
}

func (r *serverRequest) Respond(data []byte, opts ...micro.RespondOpt) error {
	r.end(replyHeader(opts))
	return r.Request.Respond(data, opts...)
}

func (r *serverRequest) RespondJSON(v any, opts ...micro.RespondOpt) error {
	r.end(replyHeader(opts))
	return r.Request.RespondJSON(v, opts...)
}

func (r *serverRequest) Error(code, description string, data []byte, opts ...micro.RespondOpt) error {
	h := replyHeader(opts)
	h.Set(micro.ErrorHeader, description)
	h.Set(micro.ErrorCodeHeader, code)
	r.end(h)
	return r.Request.Error(code, description, data, opts...)
}

// end sets the status from the reply header and ends the span, once.
func (r *serverRequest) end(h nats.Header) {
	r.once.Do(func() {
		if desc := h.Get(micro.ErrorHeader); desc != "" {
			r.span.SetStatus(codes.Error, desc)
		}
		r.span.End()
	})
}

// replyHeader applies micro's reply options to a scratch message, to read
// the headers the reply will carry.
func replyHeader(opts []micro.RespondOpt) nats.Header {
	m := &nats.Msg{Header: nats.Header{}}
	for _, opt := range opts {
		opt(m)
	}
	return m.Header
}
