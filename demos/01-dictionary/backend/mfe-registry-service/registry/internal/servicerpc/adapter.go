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

// Recorder holds the durable writes that consume no catalogue revision.
// Kept apart from Service for that reason: nothing reachable through this
// interface can move the document a shell reads.
type Recorder interface {
	RecordIgnored(context.Context, domain.Write) error
	// AdvanceRelease moves one entry's release watermark and nothing else,
	// reporting whether the row moved (BR-AS73, decision 10).
	AdvanceRelease(ctx context.Context, entryID string, release int64, signingKey string) (bool, error)
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
type Request = mferegistry.Request
type Response = mferegistry.Response

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
	if outcome == domain.AnnounceConverged {
		/*
			A resync that says exactly what is stored (BR-AS73, decision 10).
			No revision, no audit row, no cache refresh and no change
			notification — a notification here would tell every shell in the
			estate to re-read a document that did not move, which during a
			reset storm is the fan-out convergence exists to avoid.

			The one thing that does happen is the watermark: the release
			number this announcement spent must stop being acceptable, or
			every resync would widen the replay window by one.

			The revision reported back is the one that was read. It is
			honest — this call installed no new one.
		*/
		if _, err := e.recorder.AdvanceRelease(ctx, next.ID, next.Release, admission.SigningKey); err != nil {
			return Response{}, err
		}
		return Response{OK: true, Outcome: outcome, Revision: doc.Revision}, nil
	}
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

// UnregisterResponse is the answer to a withdrawal. Its own type rather than
// a reused announcement response: the outcomes are a different closed set,
// and one field carrying two vocabularies is how a caller ends up switching
// on a string it cannot enumerate.
type UnregisterResponse = mferegistry.UnregisterResponse

// Unregister withdraws a publisher's availability (BR-AS54, BR-AS55).
//
// The order below is the same as the announcement's and for the same reason:
// the gate runs before the registry is consulted about what exists, so a
// caller who owns nothing cannot learn which ids are registered by watching
// which refusal comes back.
func (e *Endpoints) Unregister(ctx context.Context, req Request) (UnregisterResponse, error) {
	cmd, err := domain.ParseUnregister(req.Payload)
	if err != nil {
		return UnregisterResponse{}, err
	}
	doc, err := e.svc.Curated(ctx)
	if err != nil {
		return UnregisterResponse{}, err
	}
	var existing *domain.Entry
	for i := range doc.Entries {
		if doc.Entries[i].ID == cmd.PluginID {
			existing = &doc.Entries[i]
			break
		}
	}
	trust, err := e.svc.Publishers(ctx)
	if err != nil {
		return UnregisterResponse{}, err
	}
	u := domain.Unregister{
		Command:    cmd,
		Payload:    req.Payload,
		Signature:  req.Signature,
		SigningKey: req.SigningKey,
	}
	if existing != nil {
		u.Accepted = existing.Release
		u.Withdrawn = existing.Withdrawn
	}
	admission, err := domain.AdmitUnregister(trust, e.verifier, u)
	if err != nil {
		return UnregisterResponse{}, err
	}
	if admission.NoOp {
		return UnregisterResponse{OK: true, NoOp: true, Outcome: domain.UnregisterWithdrawn, Revision: doc.Revision}, nil
	}
	outcome, next, err := domain.DecideUnregister(existing, cmd)
	if err != nil {
		return UnregisterResponse{}, err
	}
	if outcome == domain.UnregisterIgnored {
		// An observation, not a registry write: curation outranks the
		// publisher, and the publisher is told so rather than ignored
		// silently (decision 77, BR-AS23).
		if err := e.recorder.RecordIgnored(ctx, domain.UnregisterWrite(next, admission.SigningKey, doc.Revision)); err != nil {
			return UnregisterResponse{}, err
		}
		return UnregisterResponse{OK: true, Outcome: outcome, Revision: doc.Revision}, nil
	}
	write := domain.UnregisterWrite(next, admission.SigningKey, doc.Revision)
	// BR-AS48 — the key must still be enabled when the write commits, not
	// merely when the signature was checked.
	write.RequireKeyEnabled = admission.SigningKey
	written, err := e.svc.Apply(ctx, write)
	if err != nil {
		return UnregisterResponse{}, err
	}
	return UnregisterResponse{OK: true, Outcome: outcome, Revision: written.Revision}, nil
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
		message, code, status := classify(err, "the announcement could not be recorded")
		out = Response{Error: message, Code: code}
		shared.RespondErrorBody(req, svc, log, err, status, out)
	})
	unregisterHandler := micro.HandlerFunc(func(req micro.Request) {
		var in Request
		var out UnregisterResponse
		err := json.Unmarshal(req.Data(), &in)
		if err != nil {
			err = domain.ErrUnregisterMalformed
		} else {
			out, err = endpoints.Unregister(shared.SpanContext(req), in)
		}
		if err == nil {
			shared.Respond(req, svc, log, req.Subject(), req.Reply(), out)
			return
		}
		message, code, status := classify(err, "the withdrawal could not be recorded")
		shared.RespondErrorBody(req, svc, log, err, status, UnregisterResponse{Error: message, Code: code})
	})
	if err := svc.AddEndpoint("registry-announce", natstrace.New(nc).Middleware(handler), micro.WithEndpointSubject(mferegistry.Announce)); err != nil {
		_ = svc.Stop()
		return nil, err
	}
	if err := svc.AddEndpoint("registry-unregister", natstrace.New(nc).Middleware(unregisterHandler), micro.WithEndpointSubject(mferegistry.Unregister)); err != nil {
		_ = svc.Stop()
		return nil, err
	}
	return &Adapter{svc: svc}, nil
}

// classify turns a refusal into what a publisher can act on: a stage-named
// cause from a closed vocabulary, never a URL and never publisher-chosen text
// (BR-AS04). One table for both endpoints, so the two cannot drift into
// giving different names to the same refusal.
//
// The order mirrors the gate: ownership, then key state, then the signature,
// then ordering — so the code a publisher reads is the first thing actually
// wrong (BR-AS46).
func classify(err error, fallback string) (message, code, status string) {
	message, code, status = fallback, "registry-unavailable", "500"
	switch {
	case errors.Is(err, domain.ErrUnsigned):
		return domain.ErrUnsigned.Error(), "unsigned", "401"
	case errors.Is(err, domain.ErrUnverified):
		return domain.ErrUnverified.Error(), "unverified", "403"
	case errors.Is(err, errManifest):
		return err.Error(), "manifest-refused", "400"
	case errors.Is(err, domain.ErrOriginNotAllowed):
		return domain.ErrOriginNotAllowed.Error(), "origin-not-allowed", "422"
	case errors.Is(err, domain.ErrStaleRevision):
		return domain.ErrStaleRevision.Error(), "stale-revision", "409"
	case errors.Is(err, domain.ErrNoEntryID):
		return domain.ErrNoEntryID.Error(), "manifest-refused", "400"
	case errors.Is(err, domain.ErrNotOwned):
		return domain.ErrNotOwned.Error(), "not-owned", "403"
	case errors.Is(err, domain.ErrKeyNotTrusted):
		return domain.ErrKeyNotTrusted.Error(), "key-not-trusted", "403"
	case errors.Is(err, domain.ErrKeyRetired):
		return domain.ErrKeyRetired.Error(), "key-retired", "403"
	case errors.Is(err, domain.ErrKeyRevoked):
		return domain.ErrKeyRevoked.Error(), "key-revoked", "403"
	case errors.Is(err, domain.ErrNoRelease):
		return domain.ErrNoRelease.Error(), "manifest-refused", "400"
	case errors.Is(err, domain.ErrReleaseBackwards):
		return domain.ErrReleaseBackwards.Error(), "release-backwards", "409"

	// The withdrawal's own causes. Each is its own word because each has a
	// different fix: sign the right envelope, use the key you named, ask an
	// operator, or send a newer release.
	case errors.Is(err, domain.ErrNotUnregister):
		return domain.ErrNotUnregister.Error(), "not-unregister", "400"
	case errors.Is(err, domain.ErrUnregisterVersion):
		return domain.ErrUnregisterVersion.Error(), "unsupported-version", "400"
	case errors.Is(err, domain.ErrUnregisterMalformed):
		return domain.ErrUnregisterMalformed.Error(), "malformed", "400"
	case errors.Is(err, domain.ErrUnregisterKeyMismatch):
		return domain.ErrUnregisterKeyMismatch.Error(), "key-mismatch", "403"
	case errors.Is(err, domain.ErrUnregisterPublisherMismatch):
		return domain.ErrUnregisterPublisherMismatch.Error(), "publisher-mismatch", "403"
	case errors.Is(err, domain.ErrReleaseReused):
		return domain.ErrReleaseReused.Error(), "release-reused", "409"
	case errors.Is(err, domain.ErrUnknownEntry):
		return domain.ErrUnknownEntry.Error(), "unknown-entry", "404"
	}
	return message, code, status
}

func (a *Adapter) Stop() error {
	if a == nil || a.svc == nil {
		return nil
	}
	return a.svc.Stop()
}
