package domain

import "context"

// VehicleTypeValidator is BR-TP14's port — whether vehicleTypeCode exists in
// refdata-service's vehicle-type corpus. This cannot be a pure domain
// function: it requires a live rpc.* call to refdata-service (BR-D28: no
// REST fallback for backend-to-backend calls), which in turn requires a
// tenant-scoped NATS connection (Phase 21's account-import model) — the
// caller must say which tenant's connection to use, since a single REST
// process serves every tenant and (unlike pricing-service's per-tenant
// api.* adapter) has no connection-level tenant identity to infer it from.
// The real implementation is internal/tenants.Manager itself; this
// interface is what internal/application/commands depends on, so it can be
// exercised with a fake in tests without a live refdata-service.
type VehicleTypeValidator interface {
	// Exists reports whether code is a registered vehicle-type item in
	// contextKey's corpus, resolved over tenant's own NATS connection.
	Exists(ctx context.Context, tenant, contextKey, code string) (bool, error)
}
