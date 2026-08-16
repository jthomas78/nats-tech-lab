package accounts

import "context"

// AddPlatformTraceImportForTest exposes the unexported addPlatformTraceImport
// to accounts_test's black-box Ginkgo specs (provisioner_test.go) — the
// standard Go "export_test.go" bridge across the internal/external test
// package boundary. Never compiled into the production binary.
func (p *Provisioner) AddPlatformTraceImportForTest(ctx context.Context, platformPublicKey, tenantAccountPub string) error {
	return p.addPlatformTraceImport(ctx, platformPublicKey, tenantAccountPub)
}
