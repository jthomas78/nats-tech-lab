package accounts

import "context"

// AddPlatformTraceImportForTest exposes the unexported addPlatformTraceImport
// to accounts_test's black-box Ginkgo specs (provisioner_test.go) — the
// standard Go "export_test.go" bridge across the internal/external test
// package boundary. Never compiled into the production binary.
func (p *Provisioner) AddPlatformTraceImportForTest(ctx context.Context, platformPublicKey, tenantAccountPub, tenantName string) error {
	return p.addPlatformTraceImport(ctx, platformPublicKey, tenantAccountPub, tenantName)
}

// AddPlatformMonitorImportForTest exposes the unexported
// addPlatformMonitorImport (BR-AC31) the same way, for the "$SRV.>"
// idempotency spec in provisioner_test.go.
func (p *Provisioner) AddPlatformMonitorImportForTest(ctx context.Context, platformPublicKey, tenantAccountPub, tenantName string) error {
	return p.addPlatformMonitorImport(ctx, platformPublicKey, tenantAccountPub, tenantName)
}

// AddPlatformJSAPIImportForTest exposes the unexported
// addPlatformJSAPIImport (BR-AC32) the same way, for the "$JS.API
// introspection subjects" idempotency spec in provisioner_test.go.
func (p *Provisioner) AddPlatformJSAPIImportForTest(ctx context.Context, platformPublicKey, tenantAccountPub, tenantName string) error {
	return p.addPlatformJSAPIImport(ctx, platformPublicKey, tenantAccountPub, tenantName)
}

// AddPlatformPubsubImportForTest exposes the unexported
// addPlatformPubsubImport (BR-AC34, Phase 43a) the same way, for the
// "obs.pubsub.>" remap/idempotency spec in provisioner_test.go.
func (p *Provisioner) AddPlatformPubsubImportForTest(ctx context.Context, platformPublicKey, tenantAccountPub, tenantName string) error {
	return p.addPlatformPubsubImport(ctx, platformPublicKey, tenantAccountPub, tenantName)
}
