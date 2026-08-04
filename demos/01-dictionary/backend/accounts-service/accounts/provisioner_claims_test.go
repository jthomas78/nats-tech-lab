package accounts

import (
	"testing"

	"github.com/nats-io/jwt/v2"
)

func TestNewAccountClaimsAddsTenantImportsAndPreservesPriorCrossAccountWiring(t *testing.T) {
	const tenant = "ATENANTPUBLICKEY"
	const tenantName = "acme"
	const platform = "APLATFORMPUBLICKEY"
	prior := jwt.NewAccountClaims(tenant)
	prior.Exports.Add(&jwt.Export{Subject: "tenant.export.>", Type: jwt.Stream})
	prior.Imports.Add(&jwt.Import{Account: "AOTHER", Subject: "tenant.import.>", Type: jwt.Stream})

	preserved := newAccountClaims(tenant, "", JSLimits{}, prior, CrossAccountOpts{PlatformPublicKey: platform, TenantName: tenantName})
	if len(preserved.Exports) != 1 || string(preserved.Exports[0].Subject) != "tenant.export.>" {
		t.Fatalf("exports were not preserved: %#v", preserved.Exports)
	}
	if len(preserved.Imports) != 1 || string(preserved.Imports[0].Subject) != "tenant.import.>" {
		t.Fatalf("imports were not preserved: %#v", preserved.Imports)
	}

	fresh := newAccountClaims(tenant, "", JSLimits{}, nil, CrossAccountOpts{PlatformPublicKey: platform, TenantName: tenantName})
	if len(fresh.Imports) != 7 {
		t.Fatalf("expected four refdata services, one context service, and two streams; got %#v", fresh.Imports)
	}
	want := map[string]string{
		"refdata.item.get.v1":                   "rpc." + tenantName + ".refdata.item.get.v1",
		"refdata.type.list.v1":                  "rpc." + tenantName + ".refdata.type.list.v1",
		"refdata.item.get-versioned.v1":         "rpc." + tenantName + ".refdata.item.get-versioned.v1",
		"refdata.locales.list.v1":               "rpc." + tenantName + ".refdata.locales.list.v1",
		"rpc._platform.refdata.context.list.v1": "rpc._platform.refdata.context.list.v1",
	}
	for _, imp := range fresh.Imports {
		if imp.Type != jwt.Service {
			continue
		}
		if imp.Account != platform {
			t.Fatalf("service import %q uses %q, want PLATFORM %q", imp.Subject, imp.Account, platform)
		}
		local := string(imp.LocalSubject)
		if remote, ok := want[local]; !ok || string(imp.Subject) != remote {
			t.Fatalf("unexpected service import local=%q remote=%q", local, imp.Subject)
		}
		delete(want, local)
	}
	if len(want) != 0 {
		t.Fatalf("missing service imports: %#v", want)
	}
}
