package tenants

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDiscoverExcludesReservedNamesCaseInsensitively mirrors
// trading-partner-service's identical test (internal/tenants/tenants_test.go)
// and shipping-service's original (dictionary/internal/rest/
// tenant_discovery_test.go). "observability" was missing from
// nonTenantCredsFiles here too — the same live bug found in
// trading-partner-service, just never triggered because nothing has yet
// exercised this service's tenant-discovery path against a real
// observability.creds file.
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
