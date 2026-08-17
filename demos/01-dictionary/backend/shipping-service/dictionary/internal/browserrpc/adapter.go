// Package browserrpc is shipping-service's api.* frontend-to-service adapter
// (Phase 15a, renamed from natsrpc/rpc.* in Phase 16b) — a second transport
// onto the same commands/queries application-layer methods the rest/ adapter
// already calls, built on the NATS Micro/Services framework
// (github.com/nats-io/nats.go/micro —
// https://docs.nats.io/using-nats/developer/services; what it provides is
// explained in ARCHITECTURE-COMMUNICATIONS.md §4 "What nats.go/micro
// provides"). It mirrors
// refdata-service's own natsrpc/adapter.go (Phase 12.10/12.11), which
// established the dual-transport pattern first; see Main-POC-Plan.md Phase
// 15 for why shipping-service needed one: the browser (Sea Freight Flow)
// becomes a direct NATS client instead of going through REST + SSE, and
// api.* is the request/reply half of that (notify.* — eventhandler package,
// Phase 15b — is the other half).
//
// Renamed from rpc.* to api.* in Phase 16b: every caller of these subjects
// is a browser, never another backend service, so they belong in the
// frontend-to-service family, not the service-to-service one refdata-service
// genuinely uses. Keeping them separate families (rather than one shared
// rpc.* family) is what lets a browser credential be permission-scoped to
// api.>/notify.> only — never rpc.> — so a future backend-only rpc.* endpoint
// added inside a tenant account can never become browser-reachable by
// accident (ARCHITECTURE-COMMUNICATIONS.md § 2.4). No `internal/natsrpc`
// package exists in shipping-service today because nothing here is
// service-to-service yet; one would be created fresh, following
// refdata-service's adapter as a template, the day that changes — not kept
// as an empty placeholder in the meantime.
//
// Unlike refdata-service's adapter, which always runs on the single
// permanent PLATFORM-account connection, an Adapter here is registered once
// per TENANT connection (see tenant.go's registerRPCAdapter) — a browser
// authenticated into ACME's account must reach ACME's handlers regardless
// of which tenant shipping-service's REST layer currently has active.
package browserrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nats.go/micro"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/natstrace"
)

// Subject constants — {context} is a wildcard token resolved per-request
// from the concrete subject a request arrived on, same convention as
// refdata-service's ItemGetSubject etc. This is the company/business-unit
// scope (domain.ShipSubject's {context}), an axis completely separate from
// which tenant NATS *account* the Adapter is registered on (see tenant.go)
// — every context value exists identically inside every tenant's account.
// Tenant isolation comes entirely from the account boundary itself, not
// from anything in this subject pattern (see accounts-service/auth's
// MintBrowserToken doc comment for the full reasoning).
const (
	ShipArriveSubject          = "api.*.shipping.ship.arrive.v1"
	ShipDepartSubject          = "api.*.shipping.ship.depart.v1"
	ShipRegisterSubject        = "api.*.shipping.ship.register.v1"
	ShipCorrectIDSubject       = "api.*.shipping.ship.correct-id.v1"
	ShipListSubject            = "api.*.shipping.ship.list.v1"
	ContainerRegisterSubject   = "api.*.shipping.container.register.v1"
	ContainerLoadSubject       = "api.*.shipping.container.load.v1"
	ContainerUnloadSubject     = "api.*.shipping.container.unload.v1"
	ContainerListSubject       = "api.*.shipping.container.list.v1"
	ContainerManifestSubject   = "api.*.shipping.container.manifest.v1"
	PortListSubject            = "api.*.shipping.port.list.v1"
	PortRegisterSubject        = "api.*.shipping.port.register.v1"
	MetaKnownContainersSubject = "api.*.shipping.meta.known-containers.v1"
)

// Deps are Adapter's collaborators — the exact same application-layer
// structs rest/handlers.go's Deps already holds (see composition via
// tenant.go), so a call over api.* and a call over REST run identical
// domain logic. JS is currently unused by this adapter (Phase 28b retired
// the obs.api.* fire-and-forget/RPCTRACE publishing that used to consult
// it — see natstrace's package doc comment) but is left on Deps rather than
// threaded out of composition.go/tenant.go, since a future call site inside
// this adapter may still want it.
//
// Phase 28b superseded this adapter's old obs.api.* observability
// side-channel (BR-026/BR-027) with natstrace's obs.trace.* one (BR-036/
// BR-037): every api.* call now publishes exactly one reply-side span to
// obs.trace.{context}.shipping.{entity}.{action} — PLATFORM account only —
// instead of the old two-event (request + reply) obs.api.* pair. The Admin
// UI's Request/Reply panel still reads the old obs.api.>/obs.rpc.> subjects
// for its [messages] flat log until Phase 28g switches that view to derive
// from trace spans instead, so that view goes dark for shipping-service
// traffic in the interim — an accepted, phase-scoped gap (Main-POC-Plan.md
// Phase 28's "full rollout in one phase" tradeoff), not a bug in this
// adapter.
type Deps struct {
	Ships      *commands.ShipHandler
	Containers *commands.ContainerHandler
	Ports      *commands.PortHandler
	Terminal   *queries.Terminal
	Meta       *queries.Meta
	ShipReads  *queries.Ships
	JS         jetstream.JetStream
	Log        *slog.Logger
	// Tenant is the friendly tenant name this connection belongs to (e.g.
	// "acme") — attached to the micro registration as metadata (Phase 17c)
	// purely for the admin Services panel to label same-named instances by
	// tenant. Deliberately NOT folded into Config.Name/Version: those must
	// stay identical to nats.Name("shipping-service") across every tenant
	// connection (Phase 18's Nats-Responder invariant — see the Config
	// literal below), so per-tenant identity travels as metadata instead.
	Tenant string
}

// Adapter is shipping-service's api.* frontend-to-service adapter for one
// tenant connection.
type Adapter struct {
	nc         *nats.Conn
	ships      *commands.ShipHandler
	containers *commands.ContainerHandler
	ports      *commands.PortHandler
	terminal   *queries.Terminal
	meta       *queries.Meta
	shipReads  *queries.Ships
	js         jetstream.JetStream
	log        *slog.Logger
	svc        micro.Service
	tracer     *natstrace.Tracer
}

// errorResponse is the wire shape for every failed api.* call — same shape
// as refdata-service's adapter, so a browser client handles both services'
// errors identically.
type errorResponse struct {
	Error    string `json:"error"`
	NotFound bool   `json:"notFound,omitempty"`
}

// isNotFoundErr mirrors rest/handlers.go's writeCommandError/writeQueryError
// "not found" branch, minus the HTTP status code.
func isNotFoundErr(err error) bool {
	return errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrContainerNotFound)
}

// shipResponse/containerResponse mirror REST's own {"ship": ...}/
// {"container": ...} envelopes (handlers.go) so a caller comparing both
// transports' replies sees the same shape.
type shipResponse struct {
	Ship domain.ShipState `json:"ship"`
}

type containerResponse struct {
	Container domain.ContainerState `json:"container"`
}

type shipListResponse struct {
	Ships []domain.ShipState `json:"ships"`
}

type containerListResponse struct {
	Containers []domain.ContainerState `json:"containers"`
}

type valuesResponse struct {
	Values []string `json:"values"`
}

// portRegisterRequest is the api.{context}.shipping.port.register.v1 request
// payload — Name only; Context travels in the subject, same as every other
// endpoint here (a request body's own "context" field, if present, is never
// trusted — see contextFromSubject's doc comment).
type portRegisterRequest struct {
	Name string `json:"name"`
}

type portRegisterResponse struct {
	Port string `json:"port"`
}

// containerManifestRequest is the api.{context}.shipping.container.manifest.v1
// request payload — the one query endpoint in this adapter that needs an
// argument beyond {context} (every other query here — container-list,
// port-list, meta-known-containers — takes none). Since the subject scheme
// carries no second wildcard segment, shipID travels in the body like a
// command's fields do, even though this handler performs no mutation.
type containerManifestRequest struct {
	ShipID string `json:"shipID"`
}

// New starts the browserrpc microservice on nc and registers every endpoint.
// nc is expected to be a single tenant's NATS connection (see tenant.go) —
// every subject registered here only ever resolves within that connection's
// own account. Callers should Stop() the returned Adapter when that tenant
// connection is torn down (unlike the ordinary REST projectors, an
// in-flight browser session's api.* handlers are NOT stopped on a
// SwitchTenant call for a *different* tenant — see tenant.go's doc comment
// on why api.* adapters outlive the single-active-tenant REST/SSE swap).
func New(nc *nats.Conn, deps Deps) (*Adapter, error) {
	a := &Adapter{
		nc:         nc,
		ships:      deps.Ships,
		containers: deps.Containers,
		ports:      deps.Ports,
		terminal:   deps.Terminal,
		meta:       deps.Meta,
		shipReads:  deps.ShipReads,
		js:         deps.JS,
		log:        deps.Log,
		tracer:     natstrace.New(nc),
	}

	svc, err := micro.AddService(nc, micro.Config{
		// Matches this connection's own nats.Name("shipping-service") (both the
		// PLATFORM-account monolith.go connection and every per-tenant
		// rest/tenant.go connection use that same name) rather than a
		// family-derived name like "shipping-api" — Nats-Responder
		// (responderIdentity below) and Nats-Requestor (useNatsConnection.js's
		// REQUESTOR_NAME) must agree on one identity string per service, or the
		// Admin UI's Request/Reply panel reads as if they're different entities.
		Name:        "shipping-service",
		Version:     "1.0.0",
		Description: "shipping-service api.* frontend-to-service endpoints (Phase 15a/16b)",
		// Metadata (Phase 17c), not Name: the admin Services panel needs a
		// way to tell this tenant's instance apart from another tenant's —
		// today only distinguishable by a raw instance NUID — without
		// touching the Name/Version identity pinned above.
		Metadata: map[string]string{"tenant": deps.Tenant},
	})
	if err != nil {
		return nil, err
	}
	a.svc = svc

	endpoints := []struct {
		name    string
		handler micro.HandlerFunc
		subject string
	}{
		{"ship-arrive", a.handleShipArrive, ShipArriveSubject},
		{"ship-depart", a.handleShipDepart, ShipDepartSubject},
		{"ship-register", a.handleShipRegister, ShipRegisterSubject},
		{"ship-correct-id", a.handleShipCorrectID, ShipCorrectIDSubject},
		{"ship-list", a.handleShipList, ShipListSubject},
		{"container-register", a.handleContainerRegister, ContainerRegisterSubject},
		{"container-load", a.handleContainerLoad, ContainerLoadSubject},
		{"container-unload", a.handleContainerUnload, ContainerUnloadSubject},
		{"container-list", a.handleContainerList, ContainerListSubject},
		{"container-manifest", a.handleContainerManifest, ContainerManifestSubject},
		{"port-list", a.handlePortList, PortListSubject},
		{"port-register", a.handlePortRegister, PortRegisterSubject},
		{"meta-known-containers", a.handleMetaKnownContainers, MetaKnownContainersSubject},
	}
	for _, ep := range endpoints {
		if err := svc.AddEndpoint(ep.name, a.tracer.Middleware(ep.handler), micro.WithEndpointSubject(ep.subject)); err != nil {
			_ = svc.Stop()
			return nil, err
		}
	}
	return a, nil
}

// Stop drains the adapter's subscriptions.
func (a *Adapter) Stop() error {
	if a.svc == nil {
		return nil
	}
	return a.svc.Stop()
}

func (a *Adapter) handleShipArrive(req micro.Request) {
	a.shipCommand(req, a.ships.ArrivePort)
}

func (a *Adapter) handleShipDepart(req micro.Request) {
	a.shipCommand(req, a.ships.DepartPort)
}

func (a *Adapter) handleShipRegister(req micro.Request) {
	a.shipCommand(req, a.ships.RegisterShip)
}

// shipCommand is the shared request/response plumbing for every ship.*
// endpoint whose request body decodes into commands.ShipInput: unmarshal,
// force Context from the subject (never the body — see
// contextFromSubject's doc comment), call cmd, wrap the result in
// shipResponse, respond.
func (a *Adapter) shipCommand(req micro.Request, cmd func(context.Context, commands.ShipInput) (domain.ShipState, error)) {
	subject := req.Subject()
	itemContext := contextFromSubject(subject)
	correlationID := req.Reply()

	var in commands.ShipInput
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	in.Context = itemContext

	state, err := cmd(natstrace.ContextWithSpan(context.Background(), natstrace.SpanFrom(req)), in)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.respond(req, subject, correlationID, shipResponse{Ship: state})
}

func (a *Adapter) handleShipCorrectID(req micro.Request) {
	subject := req.Subject()
	itemContext := contextFromSubject(subject)
	correlationID := req.Reply()

	var in commands.ShipCorrectionInput
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	in.Context = itemContext

	state, err := a.ships.CorrectShipID(natstrace.ContextWithSpan(context.Background(), natstrace.SpanFrom(req)), in)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.respond(req, subject, correlationID, shipResponse{Ship: state})
}

// handleShipList serves api.*.shipping.ship.list.v1 — the browser's
// bootstrap/reconnect query for the fleet view (Main-POC-Plan.md Phase 15d).
// Reads Shape B's Postgres-backed ListShips (BR-038, Phase 31), not KV: the
// KV cache is per-entity and never guaranteed to hold every ship, so a
// list built by enumerating it could silently omit an evicted or
// never-cached entry.
func (a *Adapter) handleShipList(req micro.Request) {
	subject := req.Subject()
	itemContext := contextFromSubject(subject)
	correlationID := req.Reply()

	ships, err := a.shipReads.ListShips(natstrace.ContextWithSpan(context.Background(), natstrace.SpanFrom(req)), itemContext)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.respond(req, subject, correlationID, shipListResponse{Ships: ships})
}

func (a *Adapter) handleContainerRegister(req micro.Request) {
	a.containerCommand(req, a.containers.RegisterContainer)
}

func (a *Adapter) handleContainerLoad(req micro.Request) {
	a.containerCommand(req, a.containers.LoadContainer)
}

func (a *Adapter) handleContainerUnload(req micro.Request) {
	a.containerCommand(req, a.containers.UnloadContainer)
}

// containerCommand mirrors shipCommand for every container.* endpoint.
func (a *Adapter) containerCommand(req micro.Request, cmd func(context.Context, commands.ContainerInput) (domain.ContainerState, error)) {
	subject := req.Subject()
	itemContext := contextFromSubject(subject)
	correlationID := req.Reply()

	var in commands.ContainerInput
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	in.Context = itemContext

	state, err := cmd(natstrace.ContextWithSpan(context.Background(), natstrace.SpanFrom(req)), in)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.respond(req, subject, correlationID, containerResponse{Container: state})
}

// handleContainerList serves api.*.shipping.container.list.v1 — the
// browser's bootstrap/reconnect query for the container KV projection,
// same role as REST's GET /api/containers/{context} (queries.Terminal.List).
func (a *Adapter) handleContainerList(req micro.Request) {
	subject := req.Subject()
	itemContext := contextFromSubject(subject)
	correlationID := req.Reply()

	containers, err := a.terminal.List(natstrace.ContextWithSpan(context.Background(), natstrace.SpanFrom(req)), itemContext)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.respond(req, subject, correlationID, containerListResponse{Containers: containers})
}

// handleContainerManifest serves api.*.shipping.container.manifest.v1 — the
// containers currently on the named ship (the onShipID join;
// queries.Terminal.ListByShip IS the manifest, since the ship aggregate no
// longer carries one itself). Phase 33.2's api.* equivalent of the now-REST-only
// GET /api/manifest/{context}/{shipID} (BR-039: business operations are
// reachable only over api.*/rpc.*, never REST).
func (a *Adapter) handleContainerManifest(req micro.Request) {
	subject := req.Subject()
	itemContext := contextFromSubject(subject)
	correlationID := req.Reply()

	var in containerManifestRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	containers, err := a.terminal.ListByShip(natstrace.ContextWithSpan(context.Background(), natstrace.SpanFrom(req)), itemContext, in.ShipID)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.respond(req, subject, correlationID, containerListResponse{Containers: containers})
}

// handlePortList is the api.* counterpart of REST's GET /api/ports/{context}.
func (a *Adapter) handlePortList(req micro.Request) {
	subject := req.Subject()
	itemContext := contextFromSubject(subject)
	correlationID := req.Reply()

	ports, err := a.ports.List(natstrace.ContextWithSpan(context.Background(), natstrace.SpanFrom(req)), itemContext)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.respond(req, subject, correlationID, valuesResponse{Values: ports})
}

// handlePortRegister is the api.* counterpart of REST's POST /api/ports.
//
// Unlike the ship/container projectors (eventhandler package, Phase 15b),
// ports have no event-sourced projector to hang a notify.* publish off of —
// PortHandler writes straight to Postgres (see commands.PortHandler's doc
// comment: ports are plain reference data, not an event-sourced aggregate)
// and is a single STATIC instance shared by every tenant's Adapter (Ports
// is Postgres-backed, not account-scoped — see tenant.go's
// ensureTenantResources). So this handler publishes
// notify.{context}.shipping.port.changed itself, on a.nc — the one
// connection that's actually scoped to the right tenant account for
// whichever browser made this call — rather than teaching the shared
// PortHandler about per-tenant NATS connections it has no other reason to
// know about.
func (a *Adapter) handlePortRegister(req micro.Request) {
	subject := req.Subject()
	itemContext := contextFromSubject(subject)
	correlationID := req.Reply()

	var in portRegisterRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	spanCtx := natstrace.ContextWithSpan(context.Background(), natstrace.SpanFrom(req))
	if err := a.ports.Register(spanCtx, itemContext, in.Name); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.publishPortsChanged(spanCtx, itemContext)
	a.respond(req, subject, correlationID, portRegisterResponse{Port: in.Name})
}

// publishPortsChanged fire-and-forget publishes the full, current port list
// for kvContext to notify.{kvContext}.shipping.port.changed — a bare JSON
// array, same wire shape as notify.*.shipping.meta.changed
// (eventhandler.RegisterMeta), not the {"values": [...]} envelope
// api.*.shipping.port.list.v1's REQUEST/REPLY responses use. notify.*
// payloads are always "the updated value itself", so a browser subscriber
// doesn't need to know which entity's envelope shape to unwrap. Best-effort
// — same contract as eventhandler.publishNotify (Phase 15b): a failed
// publish or list read is logged, never returned, since it must not fail
// the command that already succeeded.
func (a *Adapter) publishPortsChanged(ctx context.Context, kvContext string) {
	ports, err := a.ports.List(ctx, kvContext)
	if err != nil {
		if a.log != nil {
			a.log.Warn("browserrpc: read port list for notify failed", "context", kvContext, "err", err)
		}
		return
	}
	data, err := json.Marshal(ports)
	if err != nil {
		return
	}
	if err := a.nc.Publish("notify."+kvContext+".shipping.port.changed", data); err != nil && a.log != nil {
		a.log.Warn("browserrpc: notify publish failed", "context", kvContext, "err", err)
	}
}

// handleMetaKnownContainers is the api.* counterpart of REST's
// GET /api/meta/{context}/known-containers.
func (a *Adapter) handleMetaKnownContainers(req micro.Request) {
	subject := req.Subject()
	itemContext := contextFromSubject(subject)
	correlationID := req.Reply()

	values, err := a.meta.KnownContainers(natstrace.ContextWithSpan(context.Background(), natstrace.SpanFrom(req)), itemContext)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.respond(req, subject, correlationID, valuesResponse{Values: values})
}

// responderHeader identifies which service — and which running instance,
// via micro's own auto-generated per-process instance ID — answered a
// request. Mirrors refdataconsumer's Nats-Requestor (the caller-identity
// header) on the reply side: NATS doesn't propagate responder identity onto
// a reply either, and the subject alone doesn't distinguish which replica
// of a horizontally-scaled service actually handled the call.
const responderHeader = "Nats-Responder"

// responderIdentity is "<service name>/<instance ID>" — svc.Info().ID is a
// fresh, unique value per running process (assigned by micro.AddService),
// so this changes across restarts/replicas without any config of our own.
func (a *Adapter) responderIdentity() string {
	info := a.svc.Info()
	return fmt.Sprintf("%s/%s", info.Name, info.ID)
}

// respond marshals out, finishes this request's natstrace span (BR-036/
// BR-037), and sends the reply — the shared tail end of every handler
// above. responderHeader is attached to both the real wire reply and the
// span's headers (mirroring how respondError attaches the real error
// headers to both). natstrace.SpanFrom is nil-safe, so a handler invoked
// directly (not through Tracer.Middleware, e.g. a unit test calling
// a.handleX(req) with a bare micro.Request) still replies correctly, it
// just publishes no span.
func (a *Adapter) respond(req micro.Request, subject, correlationID string, out any) {
	data, err := json.Marshal(out)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	headers := map[string][]string{responderHeader: {a.responderIdentity()}}
	natstrace.SpanFrom(req).End(data, headers)
	if err := req.Respond(data, micro.WithHeaders(micro.Headers(headers))); err != nil && a.log != nil {
		a.log.Error("browserrpc: respond failed", "subject", subject, "err", err)
	}
}

// respondError also finishes this request's natstrace span on failure
// (BR-036/BR-037 parity with refdata-service's adapter — a failed call must
// still be visible in the Admin UI's tracing side-channel). The reply
// carries real Nats-Service-Error/Nats-Service-Error-Code headers (micro's
// own error-header convention) via WithHeaders — additive to the existing
// JSON error body, so no client that reads the body (NotFound bool, error
// string) needs to change.
func (a *Adapter) respondError(req micro.Request, subject, correlationID string, err error) {
	data, _ := json.Marshal(errorResponse{Error: err.Error(), NotFound: isNotFoundErr(err)})
	code := "500"
	if isNotFoundErr(err) {
		code = "404"
	}
	headers := map[string][]string{
		micro.ErrorHeader:     {err.Error()},
		micro.ErrorCodeHeader: {code},
		responderHeader:       {a.responderIdentity()},
	}
	natstrace.SpanFrom(req).Fail(err, data, headers)
	if respErr := req.Respond(data, micro.WithHeaders(micro.Headers(headers))); respErr != nil && a.log != nil {
		a.log.Error("browserrpc: respond failed", "subject", subject, "err", respErr)
	}
}

// contextFromSubject extracts the {context} token from an api.{context}....
// subject. This is the ONLY source of truth for which CONTEXT — the company /
// business-unit scope — a request belongs to: every handler above overwrites
// any "context" field a request body might carry with this value before
// calling into the application layer, so scoping can't be spoofed via the
// body.
//
// Context is NOT the tenant. The tenant boundary is the NATS ACCOUNT the
// connection authenticated into, and the tenant name never appears in this
// subject (nor does the region, which is a separate regional deployment).
// See ARCHITECTURE-COMMUNICATIONS.md § 2.3 and BR-023.
func contextFromSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}
