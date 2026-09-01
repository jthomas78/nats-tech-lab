// Package mferegistry names the micro-frontend registry's browser surface,
// service announcement subject and change notification.
//
// It exists because the surface has two owners that must not drift apart.
// mfe-registry-service SERVES these subjects; accounts-service GRANTS them,
// because minting a browser credential stays with the service that owns the
// trust chain (BR-AS25/AS27: the shell's origin gets the read subject and
// nothing else, and every other subject is the operator's). Before the split
// both sides read one list from one package. Restating the strings on either
// side of the new service boundary is exactly the drift
// TestShellReadIsUngatedAndEverythingElseIsNot exists to catch, so the list
// stays in one place and the boundary moves around it.
//
// The service's subject-builder layer wraps Changed at publication time,
// preserving shipping-service's notify-coverage gate.
//
// Deliberately dependency-free: a contract, not a client. Nothing here dials,
// encodes or knows what a registry entry looks like.
package mferegistry

const (
	// ShellRead and HealthRead are the only subjects a shell's own credential
	// may publish on.
	// Federated plugin code runs in the shell's JS realm, so any capability
	// this subject carries is a capability every loaded plugin holds — which
	// is why it is read-only and why the split below is exhaustive.
	ShellRead = "api._platform.registry.frontend-plugins.read.v1"

	// HealthRead is the shell's read of the health plane (BR-AS65). A second
	// subject rather than more fields on the first, because the two carry
	// different things at different rates: the catalogue is signed manifests
	// that change when an operator curates, health is an observation that
	// changes every few seconds. Folding them together would make a shell
	// re-read the whole signed catalogue on a five-second timer, and would
	// give an observation a way into the reply that carries curation.
	//
	// Read-only in the strong sense: nothing a plugin or a shell says can
	// write here, so a decoration cannot be forged from inside the realm the
	// plugins share.
	HealthRead = "api._platform.registry.frontend-plugins.health.v1"

	// HealthChanged is the hint that a snapshot moved. A hint, not the
	// observation: it carries no state and refreshes nothing, because a
	// delivery is proof that a message arrived and never proof that a service
	// was alive (BR-AS64). The shell reads HealthRead to find out.
	HealthChanged = "notify._platform.registry.frontend-plugins.health"

	// Curated, Upsert, SetEnabled and Audit are the operator's. The Admin
	// UI's credential publishes on them; a shell's never does.
	Curated    = "api._platform.registry.entries.curated.v1"
	Upsert     = "api._platform.registry.entries.upsert.v1"
	SetEnabled = "api._platform.registry.entries.set-enabled.v1"
	Audit      = "api._platform.registry.audit.list.v1"

	// The trusted-publishers table, also the operator's (BR-AS38). One read
	// and one write, rather than a subject per op as the entry surface has:
	// four ops over one curated table, all operator-only and all
	// revision-checked identically, are not four capabilities, and four more
	// grants would say they were.
	Publishers     = "api._platform.registry.publishers.list.v1"
	PublisherWrite = "api._platform.registry.publishers.write.v1"

	// Changed is the registry's change notification. It is not an api.*
	// subject and so is not in Subjects(), but it lives here for the same
	// reason they do: the shell subscribes to it under a grant minted by
	// accounts-service, and the publisher is now another service.
	Changed = "notify._platform.registry.frontend-plugins.changed"

	// Announce is a publisher's service-to-service request, never a browser
	// grant. It deliberately belongs to neither Subjects nor Operator.
	Announce = "rpc._platform.registry.entries.announce.v1"

	// Unregister is the same kind of subject and for a stronger reason: it
	// is the one message that takes running code off an operator's screen
	// (BR-AS54). Service-to-service only — it belongs to neither Subjects
	// nor Operator, so no browser credential can carry it.
	Unregister = "rpc._platform.registry.entries.unregister.v1"
)

// ServiceReady names the readiness probe for one backend service. It is
// built from a deployment-configured service ID and never from anything in a
// manifest: a publisher choosing its own probe target could point the
// registry at a service it does not own and read the answer back through the
// health decoration (BR-AS62).
//
// Outbound and service-to-service, like Announce and Unregister — it is in
// neither browser profile, so no shell and no operator credential carries it.
func ServiceReady(serviceID string) string {
	return "rpc._platform.health." + serviceID + ".ready.v1"
}

// Subjects is the exhaustive browser-facing API surface, in registration
// order. Exhaustive is the point: the grant test iterates it and asserts that
// every subject is either the shell's one read or an operator-only write, so
// a subject added to the service without a decision about who may reach it
// fails there rather than shipping open.
func Subjects() []string {
	return []string{ShellRead, HealthRead, Curated, Upsert, SetEnabled, Audit, Publishers, PublisherWrite}
}

// Operator is everything a curating operator may publish on: Subjects minus
// the shell's read.
func Operator() []string {
	return []string{Curated, Upsert, SetEnabled, Audit, Publishers, PublisherWrite}
}
