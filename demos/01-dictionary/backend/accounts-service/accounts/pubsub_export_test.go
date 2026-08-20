package accounts

// Pending tests for Phase 67a (BUSINESS_RULES-ACCOUNTS.md's BR-AC34): a
// second Stream export, obs.pubsub.>, mirrored into PLATFORM the same way
// obs.trace.> already is (BR-AC30). Design approved (ADR-047);
// implementation on hold. Skipped rather than asserting against
// tenantExports()/addPlatformPubsubImport, which don't exist yet, so
// `go test ./...` stays green until Phase 67a lands.

import "testing"

func TestTenantExportsIncludesObsPubsubStreamExport(t *testing.T) {
	t.Skip("pending Phase 67a implementation — see BR-AC34: tenantExports() " +
		"must include {Subject: \"obs.pubsub.>\", Type: jwt.Stream} — no " +
		"AllowTrace, no ResponseType, same shape as the existing obs.trace.> " +
		"export — alongside the existing obs.trace.>, $SRV.>, and $JS.API.* exports.")
}

func TestAddPlatformPubsubImportMirrorsAddPlatformTraceImport(t *testing.T) {
	t.Skip("pending Phase 67a implementation — see BR-AC34: a new " +
		"addPlatformPubsubImport must mirror addPlatformTraceImport exactly — " +
		"same idempotency-by-(Account, Subject) scan over claims.Imports, same " +
		"jwt.Import{Type: jwt.Stream, AllowTrace: true} shape, same re-sign-and-" +
		"push via $SYS.REQ.CLAIMS.UPDATE — and be called from CreateAccount " +
		"alongside addPlatformTraceImport, gated the same way on platformPublicKey != \"\".")
}
