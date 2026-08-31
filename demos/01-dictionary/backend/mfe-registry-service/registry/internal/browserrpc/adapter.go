package browserrpc

import (
	"context"
	"errors"
	"log/slog"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	shared "github.com/jthomas78/nats-tech-lab/shared/browserrpc"
	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

var errMalformed = errors.New("the registry request could not be decoded")

func handle[In, Out any](a *Adapter, call func(*Endpoints, context.Context, In) (Out, error)) micro.HandlerFunc {
	return func(req micro.Request) {
		in, err := shared.Decode[In](req)
		if err != nil {
			a.reply(req, nil, errMalformed)
			return
		}
		out, err := call(a.endpoints, shared.SpanContext(req), in)
		a.reply(req, out, err)
	}
}

type Adapter struct {
	svc       micro.Service
	endpoints *Endpoints
	log       *slog.Logger
}

type endpoint struct {
	name, subject string
	handler       micro.HandlerFunc
}

// Subjects comes from the registration table, not a parallel allowlist.
func Subjects() []string {
	var a Adapter
	var out []string
	for _, ep := range a.routes() {
		out = append(out, ep.subject)
	}
	return out
}

func (a *Adapter) routes() []endpoint {
	return []endpoint{
		{"registry-read", ShellReadSubject, handle(a, (*Endpoints).Read)},
		{"registry-curated", CuratedSubject, func(req micro.Request) {
			out, err := a.endpoints.Curated(shared.SpanContext(req))
			a.reply(req, out, err)
		}},
		{"registry-upsert", UpsertSubject, handle(a, (*Endpoints).Upsert)},
		{"registry-set-enabled", SetEnabledSubject, handle(a, (*Endpoints).SetEnabled)},
		{"registry-audit", AuditSubject, handle(a, (*Endpoints).Audit)},
	}
}

func Mount(nc *nats.Conn, endpoints *Endpoints, log *slog.Logger) (*Adapter, error) {
	a := &Adapter{endpoints: endpoints, log: log}
	svc, err := micro.AddService(nc, micro.Config{Name: "mfe-registry-service", Version: "1.0.0", Description: "curated frontend registry api.*"})
	if err != nil {
		return nil, err
	}
	a.svc = svc
	tracer := natstrace.New(nc)
	for _, ep := range a.routes() {
		if err := svc.AddEndpoint(ep.name, tracer.Middleware(ep.handler), micro.WithEndpointSubject(ep.subject)); err != nil {
			_ = svc.Stop()
			return nil, err
		}
	}
	return a, nil
}

func (a *Adapter) Stop() error {
	if a == nil || a.svc == nil {
		return nil
	}
	return a.svc.Stop()
}

type refusalResponse struct {
	shared.ErrorResponse
	Code     string `json:"code"`
	Current  *int64 `json:"currentRevision,omitempty"`
	Supplied *int64 `json:"yourRevision,omitempty"`
	Merged   bool   `json:"merged"`
}

func (a *Adapter) reply(req micro.Request, out any, err error) {
	if err == nil {
		shared.Respond(req, a.svc, a.log, req.Subject(), req.Reply(), out)
		return
	}
	body := refusalResponse{ErrorResponse: shared.ErrorResponse{Error: err.Error()}, Code: "registry-unavailable"}
	status := "500"
	var stale StaleRefusal
	switch {
	case AsStaleRefusal(err, &stale):
		body.Conflict, body.Current, body.Supplied = true, &stale.Current, &stale.Supplied
		body.Code, status = "stale-revision", "409"
	case errors.Is(err, ErrRevisionRequired):
		body.Code, status = "revision-required", "428"
	case errors.Is(err, domain.ErrOriginNotAllowed):
		body.Code, status = "origin-not-allowed", "422"
	case errors.Is(err, errMalformed), errors.Is(err, domain.ErrNoEntry), errors.Is(err, domain.ErrNoEntryID), errors.Is(err, domain.ErrEntryIDMismatch):
		body.Code, status = "registry-malformed", "400"
	}
	shared.RespondErrorBody(req, a.svc, a.log, err, status, body)
}
