---
name: project-ports-tenant-scoping
description: Pending task — ports and refdata should be scoped to tenant/account, not BU context
metadata:
  type: project
---

Ports and refdata are currently scoped to the BU context (e.g. `acme-atlantic-fleet`), but they should be scoped to the tenant/account instead. A physical port like Hamburg belongs to a tenant, not to one of its BUs.

**Why:** All 6 seeded ports live under `_default_bu` in Postgres. When real BU contexts became active, port dropdowns went empty. A temporary hack routes `getPorts` to `_default_bu` to unblock demos.

**How to apply:** When working on this, change `getPorts(this.context)` / `registerPort(this.context, ...)` in `seafreight-app/src/stores/port.js` to use the tenant name instead of the BU context. Also review whether the port notify subscription (`notifySubject(this.context, 'port')`) should similarly be tenant-scoped. The shipping-service backend port endpoints (`/api/ports/{context}`) will need a parallel change to accept a tenant key rather than a BU context.
