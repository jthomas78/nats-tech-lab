package otelnats

import (
	"context"
	"encoding/json"
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
// as the retired legacy tracer did (BR-055).
//
// With Config.CaptureBodies, the request body is a span event at the start
// and the reply body is one just before the span ends (decision 7).
func (p *Provider) Middleware(edge Edge, trust func(micro.Request) Trust, h micro.HandlerFunc) micro.HandlerFunc {
	return func(req micro.Request) {
		msg := &nats.Msg{Subject: req.Subject(), Header: nats.Header(req.Headers())}
		ctx, span := p.StartServer(context.Background(), msg, edge, trust(req))
		p.RecordRequestBody(span, req.Data(), msg.Header)
		r := &serverRequest{Request: req, p: p, ctx: ctx, span: span}
		defer r.end(nil, nil)
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
	p    *Provider
	ctx  context.Context
	span trace.Span
	once sync.Once
}

func (r *serverRequest) Respond(data []byte, opts ...micro.RespondOpt) error {
	r.end(data, replyHeader(opts))
	return r.Request.Respond(data, opts...)
}

func (r *serverRequest) RespondJSON(v any, opts ...micro.RespondOpt) error {
	data, _ := json.Marshal(v)
	r.end(data, replyHeader(opts))
	return r.Request.RespondJSON(v, opts...)
}

func (r *serverRequest) Error(code, description string, data []byte, opts ...micro.RespondOpt) error {
	h := replyHeader(opts)
	h.Set(micro.ErrorHeader, description)
	h.Set(micro.ErrorCodeHeader, code)
	r.end(data, h)
	return r.Request.Error(code, description, data, opts...)
}

// end records the reply body, sets the status from the reply header and
// ends the span, once. A nil h means no reply was sent.
func (r *serverRequest) end(data []byte, h nats.Header) {
	r.once.Do(func() {
		if h != nil {
			r.p.RecordReplyBody(r.span, data, h)
		}
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
