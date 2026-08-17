package tenants

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDiscoverExcludesReservedNamesCaseInsensitively pins the shape
// nonTenantCredsFiles must exclude — mirroring shipping-service's own
// discoverTenants test (dictionary/internal/rest/tenant_discovery_test.go).
// "observability" was missing from this list until this fix; without it,
// Discover offered observability.creds as a switchable tenant, and this
// service then tried to subscribe to notify.accounts.account.* and its own
// api.*.trading-partner.* subjects over that phantom connection — both
// denied, since the observability PLATFORM user was never granted
// tenant-shaped permissions.
func TestDiscoverExcludesReservedNamesCaseInsensitively(t *testing.T) {
	dir := t.TempDir()
	for _, stem := range []string{"platform", "PLATFORM", "Platform", "shipping-admin", "sys", "SYS", "observability", "OBSERVABILITY", "acme", "GLOBEX"} {
		if err := os.WriteFile(filepath.Join(dir, stem+".creds"), []byte("placeholder"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	known, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, reserved := range []string{"platform", "PLATFORM", "Platform", "shipping-admin", "sys", "SYS", "observability", "OBSERVABILITY"} {
		if _, ok := known[reserved]; ok {
			t.Errorf("Discover must exclude reserved name %q regardless of casing, but it was offered as a switchable tenant", reserved)
		}
	}

	for _, tenant := range []string{"acme", "GLOBEX"} {
		if _, ok := known[tenant]; !ok {
			t.Errorf("Discover must still surface non-reserved tenant %q", tenant)
		}
	}
}
