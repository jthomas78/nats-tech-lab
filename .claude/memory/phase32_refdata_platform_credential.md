---
name: phase32-refdata-platform-credential
description: frontend/refdata is a cross-tenant platform-operator tool, not a tenant app — needed its own PLATFORM-account NATS credential (MintRefdataAdminToken) distinct from MintBrowserToken/MintAdminToken
metadata:
  type: project
---

Phase 32's original plan assumed `frontend/refdata` would migrate onto `api.*` the same way `seafreight-app`/`admin` did — a tenant-scoped browser credential. It doesn't fit that shape: `frontend/refdata` has no tenant/account concept in its UI at all (its context selector lists every context, including other tenants' — `_default_bu`, `acme-atlantic-fleet`, etc.), because it edits `_platform`'s shared standards and every tenant's contexts alike. It's a platform-operator tool like the Admin UI, not a Sea Freight Flow-style tenant app.

**Neither existing credential fit:**
- `MintBrowserToken` (tenant-scoped) is denied from `api.*.refdata.admin.>` by BR-D41 — by design, a tenant browser must never reach corpus/context admin.
- Lifting that deny for a tenant token would conflate refdata-admin rights with tenant membership (any tenant's credential could then edit shared `_platform` standards), and a platform tool has no natural tenant to authenticate as in the first place.
- `MintAdminToken` originally was subscribe-only. It now permits three exact
  read-only refdata subjects for Admin UI copy/context bootstrap, but still
  cannot drive refdata-service's admin/write surface.

**Resolution:** a third mint function, `MintRefdataAdminToken` (`accounts-service/auth/token.go`), under the same PLATFORM account as `MintAdminToken` but scoped to the full `api.*.refdata.>` surface (never bare `api.>`) plus `Sub` on `notify._platform.refdata.>`. It remains distinct because Admin's three read operations do not include corpus/context writes. New bootstrap endpoint `GET /api/auth/refdataAdminConnectInfo`. `refdata/composition.go` gained `MountPlatformAPI`, registering `internal/browserrpc`'s adapter a *second* time on refdata-service's existing PLATFORM connection (alongside `internal/natsrpc`'s `rpc.*` adapter) — two independent `micro.Service` registrations sharing one connection works fine (each gets its own instance ID; same mechanism that lets replicas of one named service coexist).

Live-verified 2026-08-17: refdata-service shows 4 `$SRV` instances in the Admin UI's Services panel (2 tenant `api.*` + 1 PLATFORM `rpc.*` + this new PLATFORM `api.*`); a full corpus create-draft → publish → rollback cycle succeeded end-to-end through `frontend/refdata` against the running docker stack.

See [BUSINESS_RULES-REFDATA.md](../../demos/01-dictionary/BUSINESS_RULES-REFDATA.md)'s BR-D41 amendment for the full writeup, and [[tenants_manager_triplication]] for the connection-manager context this sits alongside.

**Also fixed in the same pass:** `internal/browserrpc/adapter.go` originally carried `{context}` as a request-body field on every endpoint (copying `internal/natsrpc`'s rpc.*-specific convention) instead of the established `api.*` convention (`{context}` read off the subject via `contextFromSubject`, matching `pricing-service`/`organizations-service`). Caught while writing `frontend/refdata`'s NATS client, before any real consumer depended on the wrong shape.
