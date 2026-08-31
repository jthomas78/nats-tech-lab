// Package servicerpc serves publisher announcements on rpc.*, separately from
// the registry's browser mount. No browser credential has this capability.
package servicerpc

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	shared "github.com/jthomas78/nats-tech-lab/shared/browserrpc"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
)

type Service interface {
	Curated(context.Context) (domain.Document, error)
	Apply(context.Context, domain.Write) (domain.Document, error)
	Allowlist() domain.Allowlist
}

type Recorder interface {
	RecordIgnored(context.Context, domain.Write) error
}

type Endpoints struct {
	svc      Service
	recorder Recorder
	verifier domain.Verifier
}

func New(svc Service, recorder Recorder, verifier domain.Verifier) *Endpoints {
	return &Endpoints{svc: svc, recorder: recorder, verifier: verifier}
}

// Payload is the exact JSON manifest byte sequence verified, not a remarshal of
// Entry. The signature is opaque here; Phase 7 owns its format and trust anchor.
type Request struct {
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
}

type Response struct {
	OK       bool                   `json:"ok"`
	Outcome  domain.AnnounceOutcome `json:"outcome,omitempty"`
	Revision int64                  `json:"revision"`
	Error    string                 `json:"error,omitempty"`
	Code     string                 `json:"code,omitempty"`
}

func (e *Endpoints) Announce(ctx context.Context, req Request) (Response, error) {
	publisher, err := domain.VerifyAnnouncement(e.verifier, req.Payload, req.Signature)
	if err != nil {
		return Response{}, err
	}
	incoming, err := domain.ParseManifest(req.Payload)
	if err != nil {
		return Response{}, errors.Join(errManifest, err)
	}
	if err := e.svc.Allowlist().Check(incoming); err != nil {
		return Response{}, err
	}
	doc, err := e.svc.Curated(ctx)
	if err != nil {
		return Response{}, err
	}
	var existing *domain.Entry
	for i := range doc.Entries {
		if doc.Entries[i].ID == incoming.ID {
			existing = &doc.Entries[i]
			break
		}
	}
	outcome, next := domain.DecideAnnounce(existing, incoming)
	if outcome == domain.AnnounceIgnored {
		// An observation, not a registry write: no revision, cache refresh or hint.
		if err := e.recorder.RecordIgnored(ctx, domain.AnnounceWrite(next, publisher, doc.Revision)); err != nil {
			return Response{}, err
		}
		return Response{OK: true, Outcome: outcome, Revision: doc.Revision}, nil
	}
	next = domain.StampAnnouncement(existing, next, time.Now())
	written, err := e.svc.Apply(ctx, domain.AnnounceWrite(next, publisher, doc.Revision))
	if err != nil {
		return Response{}, err
	}
	return Response{OK: true, Outcome: outcome, Revision: written.Revision}, nil
}

var errManifest = errors.New("registry: manifest refused")

type Adapter struct{ svc micro.Service }

func Mount(nc *nats.Conn, endpoints *Endpoints, log *slog.Logger) (*Adapter, error) {
	svc, err := micro.AddService(nc, micro.Config{Name: "mfe-registry-announcements", Version: "1.0.0", Description: "publisher registry rpc.*"})
	if err != nil {
		return nil, err
	}
	handler := micro.HandlerFunc(func(req micro.Request) {
		var in Request
		var out Response
		err := json.Unmarshal(req.Data(), &in)
		if err != nil {
			err = errManifest
		} else {
			out, err = endpoints.Announce(shared.SpanContext(req), in)
		}
		if err == nil {
			shared.Respond(req, svc, log, req.Subject(), req.Reply(), out)
			return
		}
		out = Response{Error: "the announcement could not be recorded", Code: "registry-unavailable"}
		status := "500"
		switch {
		case errors.Is(err, domain.ErrUnsigned):
			out.Error, out.Code, status = domain.ErrUnsigned.Error(), "unsigned", "401"
		case errors.Is(err, domain.ErrUnverified):
			out.Error, out.Code, status = domain.ErrUnverified.Error(), "unverified", "403"
		case errors.Is(err, errManifest):
			out.Error, out.Code, status = err.Error(), "manifest-refused", "400"
		case errors.Is(err, domain.ErrOriginNotAllowed):
			out.Error, out.Code, status = domain.ErrOriginNotAllowed.Error(), "origin-not-allowed", "422"
		case errors.Is(err, domain.ErrStaleRevision):
			out.Error, out.Code, status = domain.ErrStaleRevision.Error(), "stale-revision", "409"
		case errors.Is(err, domain.ErrNoEntryID):
			out.Error, out.Code, status = domain.ErrNoEntryID.Error(), "manifest-refused", "400"
		}
		shared.RespondErrorBody(req, svc, log, err, status, out)
	})
	if err := svc.AddEndpoint("registry-announce", natstrace.New(nc).Middleware(handler), micro.WithEndpointSubject(mferegistry.Announce)); err != nil {
		_ = svc.Stop()
		return nil, err
	}
	return &Adapter{svc: svc}, nil
}

func (a *Adapter) Stop() error {
	if a == nil || a.svc == nil {
		return nil
	}
	return a.svc.Stop()
}
