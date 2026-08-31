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
	Publishers(context.Context) (domain.PublisherDocument, error)
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
// Entry.
//
// SigningKey is a lookup hint and nothing more: it says which key to check the
// signature under, and it is worth nothing until the trust table says that key
// belongs to the publisher who owns this plugin id (decision 103). Naming
// someone else's key only changes which check refuses you.
type Request struct {
	Payload    json.RawMessage `json:"payload"`
	Signature  string          `json:"signature"`
	SigningKey string          `json:"signingKey"`
}

type Response struct {
	OK       bool                   `json:"ok"`
	Outcome  domain.AnnounceOutcome `json:"outcome,omitempty"`
	Revision int64                  `json:"revision"`
	Error    string                 `json:"error,omitempty"`
	Code     string                 `json:"code,omitempty"`
	// NoOp marks a re-send of the release already accepted (BR-AS47). It is
	// success, not refusal: a publisher whose request timed out and retried
	// gets the same answer it would have got the first time.
	NoOp bool `json:"noOp,omitempty"`
}

func (e *Endpoints) Announce(ctx context.Context, req Request) (Response, error) {
	// Parsed before anything is decided, because the plugin id the trust
	// table is asked about lives inside the payload. Parsing is not a trust
	// decision — a payload that will not parse names nothing to own.
	incoming, err := domain.ParseManifest(req.Payload)
	if err != nil {
		return Response{}, errors.Join(errManifest, err)
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
	trust, err := e.svc.Publishers(ctx)
	if err != nil {
		return Response{}, err
	}
	var accepted int64
	if existing != nil {
		accepted = existing.Release
	}
	admission, err := domain.AdmitAnnouncement(trust, e.verifier, domain.Announcement{
		PluginID:   incoming.ID,
		SigningKey: req.SigningKey,
		Payload:    req.Payload,
		Signature:  req.Signature,
		Release:    incoming.Release,
		Accepted:   accepted,
	})
	if err != nil {
		return Response{}, err
	}
	if admission.NoOp {
		// The release already stored. Nothing to write, nothing to audit —
		// the accepted announcement is already on the record.
		return Response{OK: true, NoOp: true, Revision: doc.Revision}, nil
	}
	if err := e.svc.Allowlist().Check(incoming); err != nil {
		return Response{}, err
	}
	// Reassembled from the bytes that were just verified, so what is stored
	// is what was signed (BR-AS37) rather than a remarshal of the projection.
	signed, err := domain.EntryFromManifest(req.Payload, req.Signature, admission.SigningKey)
	if err != nil {
		return Response{}, errors.Join(errManifest, err)
	}
	outcome, next := domain.DecideAnnounce(existing, signed)
	if outcome == domain.AnnounceIgnored {
		// An observation, not a registry write: no revision, cache refresh or hint.
		if err := e.recorder.RecordIgnored(ctx, domain.AnnounceWrite(next, admission.SigningKey, doc.Revision)); err != nil {
			return Response{}, err
		}
		return Response{OK: true, Outcome: outcome, Revision: doc.Revision}, nil
	}
	next = domain.StampAnnouncement(existing, next, time.Now())
	write := domain.AnnounceWrite(next, admission.SigningKey, doc.Revision)
	write.RequireKeyEnabled = admission.SigningKey
	written, err := e.svc.Apply(ctx, write)
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
		// Ownership before key state before signature — the same order
		// AdmitAnnouncement decides in, so the code a publisher reads is the
		// first thing actually wrong (BR-AS46).
		case errors.Is(err, domain.ErrNotOwned):
			out.Error, out.Code, status = domain.ErrNotOwned.Error(), "not-owned", "403"
		case errors.Is(err, domain.ErrKeyNotTrusted):
			out.Error, out.Code, status = domain.ErrKeyNotTrusted.Error(), "key-not-trusted", "403"
		case errors.Is(err, domain.ErrKeyRetired):
			out.Error, out.Code, status = domain.ErrKeyRetired.Error(), "key-retired", "403"
		case errors.Is(err, domain.ErrKeyRevoked):
			out.Error, out.Code, status = domain.ErrKeyRevoked.Error(), "key-revoked", "403"
		case errors.Is(err, domain.ErrNoRelease):
			out.Error, out.Code, status = domain.ErrNoRelease.Error(), "manifest-refused", "400"
		case errors.Is(err, domain.ErrReleaseBackwards):
			out.Error, out.Code, status = domain.ErrReleaseBackwards.Error(), "release-backwards", "409"
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
