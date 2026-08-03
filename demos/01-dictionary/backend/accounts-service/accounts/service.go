package accounts

import (
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
)

// RegisterMicroService registers accounts-service with nats.go/micro (Phase
// 17c) so it's discoverable via the admin Admin UI's Services panel
// ($SRV.PING/$SRV.STATS broadcast — see ARCHITECTURE-COMMUNICATIONS.md §4)
// — registration only, no endpoints: accounts-service exposes its
// provisioning API over REST, not rpc.*/api.*, so there's nothing for micro
// to route requests to here. Registers on nc, expected to be this process's
// existing SYS-account connection (the one it already holds for
// $SYS.REQ.CLAIMS.* operations) — not a new connection. $SRV subjects don't
// cross NATS account boundaries, so this is only discoverable by a query
// connection on that same SYS account; a query from shipping-service's
// DEFAULT/tenant connections (nats_ops.go's listNatsServices) won't see it
// — a known, accepted gap, not a bug (Main-POC-Plan.md Phase 17c).
//
// Extracted out of cmd/main.go into this testable function deliberately:
// main() itself has no test coverage anywhere in this codebase (same
// convention shipping-service's cmd/main.go follows — bootstrap wiring
// isn't unit-tested, the packages it wires are), so the registration
// config itself needs to live somewhere a test can reach it directly.
func RegisterMicroService(nc *nats.Conn) (micro.Service, error) {
	return micro.AddService(nc, micro.Config{
		Name:        "accounts-service",
		Version:     "1.0.0",
		Description: "dynamic NATS account provisioning (Phase 14b) — no rpc.*/api.* endpoints, REST-only",
	})
}
