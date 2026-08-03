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
// permanent DEFAULT-account connection, an Adapter here is registered once
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
	"regexp"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nats.go/micro"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
)

// Subject constants — {context} is a wildcard token resolved per-request
// from the concrete subject a request arrived on, same convention as
// refdata-service's ItemGetSubject etc. This is the company/business-unit
// scope (domain.ShipSubject's {context}), an axis completely separate from
// which tenant NATS *account* the Adapter is registered on (see tenant.go)
// — every context value exists identically inside every tenant's account.
// Tenant isolation comes entirely from the account boundary itself, not
// from anything in this subject pattern (see auth-service's
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
	PortListSubject            = "api.*.shipping.port.list.v1"
	PortRegisterSubject        = "api.*.shipping.port.register.v1"
	MetaKnownContainersSubject = "api.*.shipping.meta.known-containers.v1"
)

// ObsSubjectWildcard is the subject filter for the obs.api.* observability
// side-channel (same BR-D26 mechanism refdata-service's adapter uses, under
// its own rpc.*-family obs.rpc.* wildcard).
// Unlike refdata-service's adapter, events published under this wildcard
// from a TENANT connection stay inside that tenant's isolated NATS account.
// The Admin UI's Request/Reply panel (watchRPCObs, rest/sse.go) sees the
// ACTIVE tenant's events by additionally subscribing on Deps.TenantNC —
// live-only, and only for that one tenant; see the "KNOWN GAP" doc comment
// on Deps below for what remains invisible.
const ObsSubjectWildcard = "obs.api.>"

// Deps are Adapter's collaborators — the exact same application-layer
// structs rest/handlers.go's Deps already holds (see composition via
// tenant.go), so a call over api.* and a call over REST run identical
// domain logic. JS is optional (nil-safe, mirroring refdata's adapter):
// when configured, publishObs retains events on RPCTRACE (BR-D29) instead
// of just fire-and-forget core-NATS publishing.
//
// KNOWN GAP (narrowed Phase 16, originally Main-POC-Plan.md Phase 15a):
// unlike refdata-service's adapter, which always runs on the single
// permanent DEFAULT-account connection, THIS adapter's obs.api.* events
// publish onto whichever TENANT account the Adapter is registered on (see
// New's doc comment) — a fully separate NATS account with no
// exports/imports configured. The Admin UI's Request/Reply panel
// (watchRPCObs, rest/sse.go) now also subscribes obs.api.> on the ACTIVE
// tenant's connection, so that one tenant's traffic is visible live; what
// remains invisible is (a) every non-active tenant's obs.api.* traffic and
// (b) any replay — RPCTRACE lives on the DEFAULT account and cannot retain
// tenant-account events. Publishing them anyway is a deliberate choice so a
// later cross-account-imports phase (explicitly out of scope for Phase 15)
// closes the rest with zero changes to this adapter.
type Deps struct {
	Ships      *commands.ShipHandler
	Containers *commands.ContainerHandler
	Ports      *commands.PortHandler
	Terminal   *queries.Terminal
	Meta       *queries.Meta
	ShapeA     *queries.ShapeA
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
	shapeA     *queries.ShapeA
	js         jetstream.JetStream
	log        *slog.Logger
	svc        micro.Service
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
		shapeA:     deps.ShapeA,
		js:         deps.JS,
		log:        deps.Log,
	}

	svc, err := micro.AddService(nc, micro.Config{
		// Matches this connection's own nats.Name("shipping-service") (both the
		// DEFAULT-account monolith.go connection and every per-tenant
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
		{"port-list", a.handlePortList, PortListSubject},
		{"port-register", a.handlePortRegister, PortRegisterSubject},
		{"meta-known-containers", a.handleMetaKnownContainers, MetaKnownContainersSubject},
	}
	for _, ep := range endpoints {
		if err := svc.AddEndpoint(ep.name, ep.handler, micro.WithEndpointSubject(ep.subject)); err != nil {
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
	a.publishObs(subject, correlationID, "request", map[string][]string(req.Headers()), req.Data(), "")

	var in commands.ShipInput
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	in.Context = itemContext

	state, err := cmd(context.Background(), in)
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
	a.publishObs(subject, correlationID, "request", map[string][]string(req.Headers()), req.Data(), "")

	var in commands.ShipCorrectionInput
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	in.Context = itemContext

	state, err := a.ships.CorrectShipID(context.Background(), in)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.respond(req, subject, correlationID, shipResponse{Ship: state})
}

// handleShipList serves api.*.shipping.ship.list.v1 — the browser's
// bootstrap/reconnect query for the Shape A fleet view (Main-POC-Plan.md
// Phase 15d). No REST endpoint ever needed "every ship in the context" as a
// single call, but queries.ShapeA.ListShips already existed (used by the
// demo frontend's Shape A panel) — this just reuses it instead of adding a
// second, duplicate query type.
func (a *Adapter) handleShipList(req micro.Request) {
	subject := req.Subject()
	itemContext := contextFromSubject(subject)
	correlationID := req.Reply()
	a.publishObs(subject, correlationID, "request", map[string][]string(req.Headers()), req.Data(), "")

	ships, err := a.shapeA.ListShips(context.Background(), itemContext)
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
	a.publishObs(subject, correlationID, "request", map[string][]string(req.Headers()), req.Data(), "")

	var in commands.ContainerInput
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	in.Context = itemContext

	state, err := cmd(context.Background(), in)
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
	a.publishObs(subject, correlationID, "request", map[string][]string(req.Headers()), req.Data(), "")

	containers, err := a.terminal.List(context.Background(), itemContext)
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
	a.publishObs(subject, correlationID, "request", map[string][]string(req.Headers()), req.Data(), "")

	ports, err := a.ports.List(context.Background(), itemContext)
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
	a.publishObs(subject, correlationID, "request", map[string][]string(req.Headers()), req.Data(), "")

	var in portRegisterRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	if err := a.ports.Register(context.Background(), itemContext, in.Name); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.publishPortsChanged(itemContext)
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
func (a *Adapter) publishPortsChanged(kvContext string) {
	ports, err := a.ports.List(context.Background(), kvContext)
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
	a.publishObs(subject, correlationID, "request", map[string][]string(req.Headers()), req.Data(), "")

	values, err := a.meta.KnownContainers(context.Background(), itemContext)
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

// respond marshals out, fires the reply-side obs.api.* event, and sends the
// reply — the shared tail end of every handler above. responderHeader is
// attached to both the real wire reply and its observability copy
// (mirroring how respondError attaches the real error headers to both,
// BR-D36).
func (a *Adapter) respond(req micro.Request, subject, correlationID string, out any) {
	data, err := json.Marshal(out)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	headers := map[string][]string{responderHeader: {a.responderIdentity()}}
	a.publishObs(subject, correlationID, "reply", headers, data, "")
	if err := req.Respond(data, micro.WithHeaders(micro.Headers(headers))); err != nil && a.log != nil {
		a.log.Error("browserrpc: respond failed", "subject", subject, "err", err)
	}
}

// respondError also fires the reply-side obs.api.* event on failure (BR-D26
// parity with refdata-service's adapter — a failed call must still be
// visible in the Admin UI's Request/Reply panel). The reply carries real
// Nats-Service-Error/Nats-Service-Error-Code headers (micro's own
// error-header convention, BR-D36) via WithHeaders — additive to the
// existing JSON error body, so no client that reads the body (NotFound
// bool, error string) needs to change.
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
	a.publishObs(subject, correlationID, "reply", headers, data, err.Error())
	if respErr := req.Respond(data, micro.WithHeaders(micro.Headers(headers))); respErr != nil && a.log != nil {
		a.log.Error("browserrpc: respond failed", "subject", subject, "err", respErr)
	}
}

var versionSuffix = regexp.MustCompile(`\.v\d+$`)

// obsSubjectFor derives the observability subject from a real api.* subject
// (e.g. api.acme-atlantic-fleet.shipping.ship.arrive.v1 ->
// obs.api.acme-atlantic-fleet.shipping.ship.arrive),
// identical convention to refdata-service's adapter (which does the same
// for its own rpc.* family).
func obsSubjectFor(apiSubject string) string {
	return "obs." + versionSuffix.ReplaceAllString(apiSubject, "")
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

// obsEnvelope's Headers/Timestamp/PayloadBytes (BR-D36, Phase 17a) are
// additive to the original BR-D26 shape — every field is optional to decode,
// so events already retained (or produced by older code) still parse; a
// consumer must treat their absence as "unknown", not an error.
type obsEnvelope struct {
	Direction     string              `json:"direction"`
	CorrelationID string              `json:"correlationId"`
	Subject       string              `json:"subject"`
	Payload       json.RawMessage     `json:"payload,omitempty"`
	Error         string              `json:"error,omitempty"`
	Headers       map[string][]string `json:"headers,omitempty"`
	Timestamp     time.Time           `json:"timestamp"`
	PayloadBytes  int                 `json:"payloadBytes"`
}

// publishObs fire-and-forget publishes an observability event (BR-D26
// parity) — must never block or fail the real API reply. Identical
// mechanics to refdata-service's adapter: PublishAsync onto RPCTRACE when
// a.js is configured (BR-D29 replay), otherwise a plain nc.Publish.
//
// headers is the real headers carried by this message — the caller's
// req.Headers() on the request side, or nil on a success reply (this
// adapter attaches no custom headers there); respondError builds and passes
// the actual Nats-Service-Error/-Code headers it also sends on the wire
// (BR-D36), never fabricated ones.
func (a *Adapter) publishObs(apiSubject, correlationID, direction string, headers map[string][]string, payload []byte, errMsg string) {
	defer func() {
		if r := recover(); r != nil && a.log != nil {
			a.log.Error("browserrpc: obs publish panicked", "recovered", r)
		}
	}()
	data, err := json.Marshal(obsEnvelope{
		Direction:     direction,
		CorrelationID: correlationID,
		Subject:       apiSubject,
		Payload:       payload,
		Error:         errMsg,
		Headers:       headers,
		Timestamp:     time.Now().UTC(),
		PayloadBytes:  len(payload),
	})
	if err != nil {
		return
	}
	subject := obsSubjectFor(apiSubject)
	if a.js != nil {
		if _, pubErr := a.js.PublishAsync(subject, data); pubErr != nil && a.log != nil {
			a.log.Warn("browserrpc: obs publish failed", "err", pubErr)
		}
		return
	}
	if pubErr := a.nc.Publish(subject, data); pubErr != nil && a.log != nil {
		a.log.Warn("browserrpc: obs publish failed", "err", pubErr)
	}
}
