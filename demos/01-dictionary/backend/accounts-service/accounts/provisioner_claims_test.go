package accounts

import (
	"testing"

	"github.com/nats-io/jwt/v2"
)

// BR-AC19 — a claim re-sign accumulates signing keys rather than replacing
// them. The failure this guards against is not hypothetical: dropping a prior
// key invalidated every .creds file signed by it, which is what left globex
// unable to connect at all after a `docker compose down -v` (2026-08-06).
func TestNewAccountClaimsPreservesPriorSigningKeys(t *testing.T) {
	const tenant = "ATENANTPUBLICKEY"
	const priorSigningKey = "APRIORSIGNINGKEY"
	const newSigningKey = "ANEWSIGNINGKEY"

	prior := jwt.NewAccountClaims(tenant)
	prior.SigningKeys.Add(priorSigningKey)

	claims := newAccountClaims(tenant, newSigningKey, JSLimits{}, prior, CrossAccountOpts{})
	if !claims.SigningKeys.Contains(priorSigningKey) {
		t.Fatalf("prior signing key was dropped, invalidating every credential signed by it: %#v", claims.SigningKeys.Keys())
	}
	if !claims.SigningKeys.Contains(newSigningKey) {
		t.Fatalf("newly established signing key is missing: %#v", claims.SigningKeys.Keys())
	}

	// Establishing a key on an account that has none must not invent history.
	fresh := newAccountClaims(tenant, newSigningKey, JSLimits{}, nil, CrossAccountOpts{})
	if got := fresh.SigningKeys.Keys(); len(got) != 1 || got[0] != newSigningKey {
		t.Fatalf("expected exactly the new signing key, got %#v", got)
	}

	// A re-sign that establishes no key of its own (limits update on an
	// account whose seed is unknown) must still carry the prior key forward.
	limitsOnly := newAccountClaims(tenant, "", JSLimits{}, prior, CrossAccountOpts{})
	if got := limitsOnly.SigningKeys.Keys(); len(got) != 1 || got[0] != priorSigningKey {
		t.Fatalf("expected the prior signing key to survive a limits-only re-sign, got %#v", got)
	}
}

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
