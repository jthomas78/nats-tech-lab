package accounts

// Pending tests for Phase 43a (BUSINESS_RULES-ACCOUNTS.md's BR-AC34): a
// second Stream export, obs.pubsub.>, imported into PLATFORM under a
// per-tenant LocalSubject remap. Design approved (ADR-047) and amended
// 2026-08-25 by a pre-implementation review — the remap assertion below is
// that amendment (A1); the rule as originally drafted asserted no remap was
// needed, which would have left the Messages panel unable to tell which
// tenant a message came from. Implementation on hold. Skipped rather than
// asserting against tenantExports()/addPlatformPubsubImport, which don't
// exist yet, so `go test ./...` stays green until Phase 43a lands.

import "testing"

func TestTenantExportsIncludesObsPubsubStreamExport(t *testing.T) {
	t.Skip("pending Phase 43a implementation — see BR-AC34: tenantExports() " +
		"must include {Subject: \"obs.pubsub.>\", Type: jwt.Stream} — no " +
		"AllowTrace, no ResponseType, same shape as the existing obs.trace.> " +
		"export — alongside the existing obs.trace.>, $SRV.>, and $JS.API.* exports.")
}

func TestAddPlatformPubsubImportRemapsToPerTenantLocalSubject(t *testing.T) {
	t.Skip("pending Phase 43a implementation — see BR-AC34 (ADR-047 A1): " +
		"addPlatformPubsubImport must set LocalSubject to " +
		"monitor.{tenant}.pubsub.>, the way addPlatformMonitorImport and the " +
		"$JS.API.* imports do and unlike addPlatformTraceImport. Without it, " +
		"every tenant's export lands on one identical local subject and a " +
		"PLATFORM subscriber cannot recover which account a message came from " +
		"— the account boundary disambiguates delivery, not provenance.")
}

func TestAddPlatformPubsubImportMirrorsAddPlatformTraceImportMechanism(t *testing.T) {
	t.Skip("pending Phase 43a implementation — see BR-AC34: apart from the " +
		"LocalSubject remap above, addPlatformPubsubImport must mirror " +
		"addPlatformTraceImport — same idempotency-by-(Account, Subject) scan " +
		"over claims.Imports, same jwt.Import{Type: jwt.Stream, AllowTrace: " +
		"true} shape, same re-sign-and-push via $SYS.REQ.CLAIMS.UPDATE — and " +
		"be called from CreateAccount alongside it, gated the same way on " +
		"platformPublicKey != \"\".")
}

func TestAccountLifecycleNotifiesAreInstrumented(t *testing.T) {
	t.Skip("pending Phase 43a implementation — see BR-AC34/BR-045: this " +
		"service's four notify.accounts.account.* publishes (handler.go's " +
		"publishAccountEvent — created/suspended/reactivated/jslimits_updated) " +
		"must each publish one obs.pubsub.* envelope, and appear on BR-049's " +
		"checked instrumented list.")
}
