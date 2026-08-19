# Accounts

<EyebrowLabel text="Architecture" />

`accounts-service` administers the tenant axis itself: NATS operator-mode
trust chain, tenant account create/suspend/reactivate lifecycle, and user
auth/token lifecycle. Tenancy is enforced by the NATS account boundary —
hard, server-enforced, resource-limited — not by an application-level
check.

<VerdictBadge status="completed" /> Running in code today.

*Content in progress — trust chain and lifecycle diagrams to follow.*
