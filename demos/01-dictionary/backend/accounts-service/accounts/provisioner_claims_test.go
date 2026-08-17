package accounts

import (
	"strings"
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

// BR-AC30 (Phase 28f amendment) — a tenant account's own claims must export
// obs.trace.> back to PLATFORM, or PLATFORM's cross-account trace store has
// no way to ever see that tenant's spans (see addPlatformTraceImport, the
// PLATFORM-side counterpart exercised by TestAddPlatformTraceImportIsIdempotent
// in provisioner_test.go).
func TestNewAccountClaimsAddsTenantTraceExport(t *testing.T) {
	const tenant = "ATENANTPUBLICKEY"
	const tenantName = "acme"
	const platform = "APLATFORMPUBLICKEY"

	fresh := newAccountClaims(tenant, "", JSLimits{}, nil, CrossAccountOpts{PlatformPublicKey: platform, TenantName: tenantName})
	var traceExport *jwt.Export
	for _, exp := range fresh.Exports {
		if string(exp.Subject) == traceExportSubject {
			traceExport = exp
		}
	}
	if traceExport == nil {
		t.Fatalf("expected an obs.trace.> export on a freshly-minted tenant's own claims, got %#v", fresh.Exports)
	}
	if traceExport.Type != jwt.Stream {
		t.Fatalf("obs.trace.> export must be a Stream export (AllowTrace is only legal on a Service export), got type %v", traceExport.Type)
	}
	if traceExport.AllowTrace {
		t.Fatal("AllowTrace must not be set on this Stream export — jwt.Export.Validate rejects it on anything but a Service export")
	}

	// The trace export must survive a plain re-sign (happy path, prior has
	// imports already) exactly like every other export/import — it must not
	// require crossAccount.PlatformPublicKey to be supplied again.
	preserved := newAccountClaims(tenant, "", JSLimits{}, fresh, CrossAccountOpts{})
	found := false
	for _, exp := range preserved.Exports {
		if string(exp.Subject) == traceExportSubject {
			found = true
		}
	}
	if !found {
		t.Fatalf("obs.trace.> export was dropped on re-sign: %#v", preserved.Exports)
	}
}

// BR-AC31 (Phase 30a) — a tenant account's own claims must export $SRV.>
// back to PLATFORM, or cross-account service discovery
// (observability-service's Services panel, Phase 30f) has no way to ever
// see that tenant's registered services (see addPlatformMonitorImport, the
// PLATFORM-side counterpart exercised by the "re-signs PLATFORM's own
// claims to import each new tenant's $SRV.>" spec in provisioner_test.go).
func TestNewAccountClaimsAddsTenantMonitorExport(t *testing.T) {
	const tenant = "ATENANTPUBLICKEY"
	const tenantName = "acme"
	const platform = "APLATFORMPUBLICKEY"

	fresh := newAccountClaims(tenant, "", JSLimits{}, nil, CrossAccountOpts{PlatformPublicKey: platform, TenantName: tenantName})
	var srvExport *jwt.Export
	for _, exp := range fresh.Exports {
		if string(exp.Subject) == srvExportSubject {
			srvExport = exp
		}
	}
	if srvExport == nil {
		t.Fatalf("expected a $SRV.> export on a freshly-minted tenant's own claims, got %#v", fresh.Exports)
	}
	if srvExport.Type != jwt.Service {
		t.Fatalf("$SRV.> export must be a Service export (reply routing is only meaningful for services), got type %v", srvExport.Type)
	}
	if srvExport.ResponseType != jwt.ResponseTypeStream {
		t.Fatalf("$SRV.> export must set ResponseType Stream — the default Singleton is documented for a service that sends exactly one reply, but $SRV.STATS is answered by every registered instance, got %q", srvExport.ResponseType)
	}

	// Must survive a plain re-sign exactly like the trace export does.
	preserved := newAccountClaims(tenant, "", JSLimits{}, fresh, CrossAccountOpts{})
	found := false
	for _, exp := range preserved.Exports {
		if string(exp.Subject) == srvExportSubject {
			found = true
		}
	}
	if !found {
		t.Fatalf("$SRV.> export was dropped on re-sign: %#v", preserved.Exports)
	}
}

// BR-AC32 (Phase 30b/30i) — a tenant account's own claims must export the
// seven narrow $JS.API introspection subjects back to PLATFORM, or
// observability-service's JetStream/KV panels (Phase 30e) have no way to
// ever reach that tenant's streams/consumers (see addPlatformJSAPIImport,
// the PLATFORM-side counterpart exercised by the "$JS.API introspection
// subjects" spec in provisioner_test.go).
func TestNewAccountClaimsAddsTenantJSAPIExports(t *testing.T) {
	const tenant = "ATENANTPUBLICKEY"
	const tenantName = "acme"
	const platform = "APLATFORMPUBLICKEY"

	fresh := newAccountClaims(tenant, "", JSLimits{}, nil, CrossAccountOpts{PlatformPublicKey: platform, TenantName: tenantName})

	bySubject := map[string]*jwt.Export{}
	for _, exp := range fresh.Exports {
		bySubject[string(exp.Subject)] = exp
	}
	for _, want := range jsAPIExportSubjects {
		exp, ok := bySubject[want.subject]
		if !ok {
			t.Fatalf("expected a %s export on a freshly-minted tenant's own claims, got %#v", want.subject, fresh.Exports)
		}
		if exp.Type != jwt.Service {
			t.Fatalf("%s export must be a Service export, got type %v", want.subject, exp.Type)
		}
		if exp.ResponseType != want.responseType {
			t.Fatalf("%s export ResponseType = %q, want %q — CONSUMER.MSG.NEXT needs Stream (a batch pull can yield multiple replies per request), every other one of the seven is a plain single-reply Singleton", want.subject, exp.ResponseType, want.responseType)
		}
	}

	// Negative check: no mutating $JS.API subject (STREAM.CREATE/DELETE/
	// PURGE/UPDATE/RESTORE, STREAM.MSG.GET, any CONSUMER.CREATE/DELETE form
	// beyond the two/one listed) ever leaks onto a tenant's own claims.
	for subj := range bySubject {
		if !strings.HasPrefix(subj, jsAPIExportPrefix) {
			continue
		}
		allowed := false
		for _, want := range jsAPIExportSubjects {
			if subj == want.subject {
				allowed = true
				break
			}
		}
		if !allowed {
			t.Fatalf("unexpected $JS.API export present on tenant claims: %q", subj)
		}
	}

	// Must survive a plain re-sign exactly like the other two exports do.
	preserved := newAccountClaims(tenant, "", JSLimits{}, fresh, CrossAccountOpts{})
	preservedCount := 0
	for _, exp := range preserved.Exports {
		for _, want := range jsAPIExportSubjects {
			if string(exp.Subject) == want.subject {
				preservedCount++
			}
		}
	}
	if preservedCount != len(jsAPIExportSubjects) {
		t.Fatalf("expected all %d $JS.API exports to survive re-sign, found %d: %#v", len(jsAPIExportSubjects), preservedCount, preserved.Exports)
	}
}
