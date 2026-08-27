package accounts

// Phase 50b — accounts-service's first api.* frontend-to-service surface
// (BR-AC40), serving the Admin UI's Users panel.
//
// Two notes on the transport, both deliberate departures worth stating:
//
//   - api.*, not rpc.*. Phase 50's design gate (D3) provisionally named these
//     rpc._platform.accounts.user.*, but the consumer is a browser talking to
//     NATS directly — there is no service in between — and a browser
//     credential is never granted rpc.> anywhere in this repo. api.* is the
//     frontend-to-service family; rpc.* is service-to-service. The subject
//     family follows the consumer, so these are api.*.
//   - Not REST. accounts-service's provisioning API is REST (see
//     RegisterMicroService's comment, now out of date in claiming this
//     service has no endpoints at all), but Phases 31–34 made browser
//     business paths NATS-only and Phase 34's mux allowlist (BR-040) exists
//     to keep them that way. The Phase 33 refdata admin REST exemption does
//     not extend here: it exists because accounts-service calls refdata
//     server-to-server with no NATS path available, which is not the case for
//     a browser panel.
//
// The "_platform" token is a fixed literal, not a wildcard: the registry is
// cross-tenant and one operator call covers every account, so there is no
// per-tenant subject to route by — the same reasoning refdata-service's own
// ContextListSubject uses. Mounted on the PLATFORM connection only, which is
// what makes BR-AC40's "PLATFORM-only" a server-enforced account boundary
// rather than a handler-level check.

import (
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	sharedbrowserrpc "github.com/jthomas78/nats-tech-lab/shared/browserrpc"
	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

const (
	UserListSubject = "api._platform.accounts.user.list.v1"
	UserGetSubject  = "api._platform.accounts.user.get.v1"
	// UserRevokeSubject is Phase 51b (BR-AC43). It is a write on an api.*
	// subject, which the two reads above are not — worth stating, because
	// the guard is not in this file. What keeps a tenant from calling it is
	// the same thing that keeps them from calling .list.v1: the adapter is
	// mounted on the PLATFORM connection only, so a tenant credential has no
	// route to the subject at all.
	UserRevokeSubject = "api._platform.accounts.user.revoke.v1"
)

// ErrPublicKeyRequired is the .get.v1 argument check. A get with no key is a
// client bug, not an empty result.
var ErrPublicKeyRequired = errors.New("publicKey is required")

// UsersAdapter serves the two Users-panel subjects on one connection.
type UsersAdapter struct {
	svc     micro.Service
	reader  *UserClaimsReader
	revoker *UserRevoker
	log     *slog.Logger
	tracer  *natstrace.Tracer
}

// UserGetRequest is .get.v1's body. The user NKey is the identity everything
// else joins on — /connz reports it as authorized_user — so it is the only
// argument.
type UserGetRequest struct {
	PublicKey string `json:"publicKey"`
}

// UserRevokeRequest is .revoke.v1's body. Same single argument as .get.v1
// and for the same reason: the user NKey is the identity, and a name is not
// unique enough to revoke by (BR-058).
type UserRevokeRequest struct {
	PublicKey string `json:"publicKey"`
}

// UserListResponse wraps the roster rather than returning a bare array, so
// the shape has room to grow a paging envelope without breaking a client.
type UserListResponse struct {
	Users []UserSummary `json:"users"`
}

// NewUsersAdapter registers the endpoints on nc — expected to be this
// process's PLATFORM connection. Callers Stop() it on shutdown.
// revoker may be nil, in which case .revoke.v1 is still served and answers
// every request with an error. That is deliberate: an endpoint that is
// absent looks to a browser exactly like a NATS timeout, whereas one that
// refuses can say why.
func NewUsersAdapter(nc *nats.Conn, reader *UserClaimsReader, revoker *UserRevoker, log *slog.Logger) (*UsersAdapter, error) {
	a := &UsersAdapter{reader: reader, revoker: revoker, log: log, tracer: natstrace.New(nc)}

	svc, err := micro.AddService(nc, micro.Config{
		// Matches this connection's own nats.Name (natsconn.Options
		// "accounts-service-platform"'s service identity) — the BR-D37 rule
		// that a micro registration names the service, not the connection.
		Name:        "accounts-service",
		Version:     "1.0.0",
		Description: "accounts-service api.* Users panel endpoints (Phase 50b)",
	})
	if err != nil {
		return nil, err
	}
	a.svc = svc

	for _, ep := range a.endpoints() {
		if err := svc.AddEndpoint(ep.name, a.tracer.Middleware(ep.handler), micro.WithEndpointSubject(ep.subject)); err != nil {
			_ = svc.Stop()
			return nil, err
		}
	}
	return a, nil
}

type usersEndpoint struct {
	name    string
	handler micro.HandlerFunc
	subject string
}

// endpoints is the single registration table; UsersAdapterSubjects reads it,
// so the BR-AC40 permission tests can never drift from what is served.
func (a *UsersAdapter) endpoints() []usersEndpoint {
	return []usersEndpoint{
		{"user-list", a.handleList, UserListSubject},
		{"user-get", a.handleGet, UserGetSubject},
		{"user-revoke", a.handleRevoke, UserRevokeSubject},
	}
}

// UsersAdapterSubjects is every subject NewUsersAdapter serves — the
// authority the BR-AC40 specs and MintAdminToken's grant are checked against.
func UsersAdapterSubjects() []string {
	eps := (&UsersAdapter{}).endpoints()
	out := make([]string, 0, len(eps))
	for _, ep := range eps {
		out = append(out, ep.subject)
	}
	return out
}

// Stop drains the adapter's subscriptions.
func (a *UsersAdapter) Stop() error {
	if a == nil || a.svc == nil {
		return nil
	}
	return a.svc.Stop()
}

func (a *UsersAdapter) respondError(req micro.Request, subject string, err error) {
	sharedbrowserrpc.RespondError(req, a.svc, a.log, subject, "", err, errors.Is(err, ErrUserNotFound))
}

func (a *UsersAdapter) handleList(req micro.Request) {
	users, err := a.reader.List(sharedbrowserrpc.SpanContext(req))
	if err != nil {
		a.respondError(req, UserListSubject, err)
		return
	}
	sharedbrowserrpc.Respond(req, a.svc, a.log, UserListSubject, "", UserListResponse{Users: users})
}

func (a *UsersAdapter) handleGet(req micro.Request) {
	var in UserGetRequest
	if len(req.Data()) > 0 {
		if err := json.Unmarshal(req.Data(), &in); err != nil {
			a.respondError(req, UserGetSubject, err)
			return
		}
	}
	if in.PublicKey == "" {
		a.respondError(req, UserGetSubject, ErrPublicKeyRequired)
		return
	}
	view, err := a.reader.Get(sharedbrowserrpc.SpanContext(req), in.PublicKey)
	if err != nil {
		a.respondError(req, UserGetSubject, err)
		return
	}
	sharedbrowserrpc.Respond(req, a.svc, a.log, UserGetSubject, "", view)
}

func (a *UsersAdapter) handleRevoke(req micro.Request) {
	var in UserRevokeRequest
	if len(req.Data()) > 0 {
		if err := json.Unmarshal(req.Data(), &in); err != nil {
			a.respondError(req, UserRevokeSubject, err)
			return
		}
	}
	if in.PublicKey == "" {
		a.respondError(req, UserRevokeSubject, ErrPublicKeyRequired)
		return
	}
	if a.revoker == nil {
		a.respondError(req, UserRevokeSubject, errors.New("revocation is not configured on this deployment"))
		return
	}
	out, err := a.revoker.Revoke(sharedbrowserrpc.SpanContext(req), in.PublicKey)
	if err != nil {
		a.respondError(req, UserRevokeSubject, err)
		return
	}
	a.log.Info("nats user credential revoked", "user", out.PublicKey, "account", out.Account)
	sharedbrowserrpc.Respond(req, a.svc, a.log, UserRevokeSubject, "", out)
}
