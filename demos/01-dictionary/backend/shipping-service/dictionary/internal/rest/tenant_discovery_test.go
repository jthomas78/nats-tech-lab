package rest

// BR-AC06 (BUSINESS_RULES-ACCOUNTS.md) is enforced primarily by
// accounts-service refusing to mint a reserved name at all (see that
// package's reservedAccountNames), but this is the defense-in-depth side:
// discoverTenants' own exclusion of PLATFORM/shipping-admin/SYS/observability
// creds files must not depend on their casing, since nothing in this
// package controls what actually lands in the shared creds directory.
// "observability" is pinned explicitly (Phase 30i live verification):
// nonTenantCredsFiles wasn't updated when observability.creds first landed
// in this same shared directory (Phase 30c), so a real docker-compose run
// tried to provision SHIPPING-stream tenant resources for it and failed at
// every shipping-service startup — a gap this in-memory fixture never
// exercises without the real file present, which is exactly why it's
// pinned here now rather than left to be caught live again.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverTenantsExcludesReservedNamesCaseInsensitively(t *testing.T) {
	dir := t.TempDir()
	for _, stem := range []string{"platform", "PLATFORM", "Platform", "shipping-admin", "sys", "SYS", "observability", "OBSERVABILITY", "acme", "GLOBEX"} {
		if err := os.WriteFile(filepath.Join(dir, stem+".creds"), []byte("placeholder"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	known, err := discoverTenants(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, reserved := range []string{"platform", "PLATFORM", "Platform", "shipping-admin", "sys", "SYS", "observability", "OBSERVABILITY"} {
		if _, ok := known[reserved]; ok {
			t.Errorf("discoverTenants must exclude reserved name %q regardless of casing, but it was offered as a switchable tenant", reserved)
		}
	}

	for _, tenant := range []string{"acme", "GLOBEX"} {
		if _, ok := known[tenant]; !ok {
			t.Errorf("discoverTenants must still surface non-reserved tenant %q", tenant)
		}
	}
}
