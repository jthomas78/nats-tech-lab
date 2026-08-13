package accounts

import (
	"testing"

	"github.com/nats-io/jwt/v2"
)

// BR-AC22 — an import is satisfiable only when its named exporter declares
// an export of the same type whose subject contains the import's subject.
// Cases mirror nats/bootstrap-operator.sh's real PLATFORM export shapes
// (wildcarded service subjects resolved to a literal per tenant; stream
// subjects imported as the identical wildcarded pattern) rather than
// invented ones, since that's the actual contract this matters for.
func TestMatchExport(t *testing.T) {
	cases := []struct {
		name    string
		imp     *jwt.Import
		exports jwt.Exports
		want    int
	}{
		{
			name: "service import resolves against a wildcarded export subject",
			imp:  &jwt.Import{Subject: "rpc.acme.refdata.item.get.v1", Type: jwt.Service},
			exports: jwt.Exports{
				{Subject: "rpc.*.refdata.item.get.v1", Type: jwt.Service},
			},
			want: 0,
		},
		{
			name: "stream import carries the identical wildcarded subject as its export",
			imp:  &jwt.Import{Subject: "notify.accounts.account.*", Type: jwt.Stream},
			exports: jwt.Exports{
				{Subject: "notify.accounts.account.*", Type: jwt.Stream},
			},
			want: 0,
		},
		{
			name: "literal subject, literal export, no wildcards on either side",
			imp:  &jwt.Import{Subject: "rpc._platform.refdata.context.list.v1", Type: jwt.Service},
			exports: jwt.Exports{
				{Subject: "rpc._platform.refdata.context.list.v1", Type: jwt.Service},
			},
			want: 0,
		},
		{
			name: "subject matches but type does not — a stream export can't satisfy a service import",
			imp:  &jwt.Import{Subject: "rpc.acme.refdata.item.get.v1", Type: jwt.Service},
			exports: jwt.Exports{
				{Subject: "rpc.*.refdata.item.get.v1", Type: jwt.Stream},
			},
			want: -1,
		},
		{
			name: "subject not covered by any export",
			imp:  &jwt.Import{Subject: "rpc.acme.refdata.rates.get.v1", Type: jwt.Service},
			exports: jwt.Exports{
				{Subject: "rpc.*.refdata.item.get.v1", Type: jwt.Service},
			},
			want: -1,
		},
		{
			name: "matches the second export, not the first",
			imp:  &jwt.Import{Subject: "evt.acme.refdata.item.changed", Type: jwt.Stream},
			exports: jwt.Exports{
				{Subject: "rpc.*.refdata.item.get.v1", Type: jwt.Service},
				{Subject: "evt.*.refdata.*.changed", Type: jwt.Stream},
			},
			want: 1,
		},
		{
			name:    "no exports at all",
			imp:     &jwt.Import{Subject: "rpc.acme.refdata.item.get.v1", Type: jwt.Service},
			exports: jwt.Exports{},
			want:    -1,
		},
		{
			name: "a nil export entry in the slice is skipped, not a panic",
			imp:  &jwt.Import{Subject: "rpc.acme.refdata.item.get.v1", Type: jwt.Service},
			exports: jwt.Exports{
				nil,
				{Subject: "rpc.*.refdata.item.get.v1", Type: jwt.Service},
			},
			want: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchExport(tc.imp, tc.exports); got != tc.want {
				t.Fatalf("matchExport() = %d, want %d", got, tc.want)
			}
		})
	}
}

// BR-AC22/BR-AC24/BR-AC25 — the composed status a topology edge reports,
// including which cases must NOT be conflated: unknown-account (the
// exporter isn't a recognized account) is distinct from no-export (the
// exporter is known but declares nothing satisfying this import), which is
// distinct from token-required (the export exists and matches, but demands
// an activation token the import doesn't carry).
func TestImportStatus(t *testing.T) {
	matchingExports := jwt.Exports{
		{Subject: "rpc.*.refdata.item.get.v1", Type: jwt.Service},
	}
	tokenReqExports := jwt.Exports{
		{Subject: "rpc.*.refdata.item.get.v1", Type: jwt.Service, TokenReq: true},
	}
	imp := &jwt.Import{Subject: "rpc.acme.refdata.item.get.v1", Type: jwt.Service}
	impWithToken := &jwt.Import{Subject: "rpc.acme.refdata.item.get.v1", Type: jwt.Service, Token: "sometoken"}

	cases := []struct {
		name               string
		imp                *jwt.Import
		exporterClaims     *jwt.AccountClaims
		exporterRecognized bool
		wantStatus         string
		wantMatchedIdx     int
	}{
		{
			name:               "BR-AC25: exporter pubkey isn't a known account",
			imp:                imp,
			exporterClaims:     nil,
			exporterRecognized: false,
			wantStatus:         topologyUnknownAccount,
			wantMatchedIdx:     -1,
		},
		{
			name:               "known account but its claims lookup failed — treated as no-export, not a third state",
			imp:                imp,
			exporterClaims:     nil,
			exporterRecognized: true,
			wantStatus:         topologyNoExport,
			wantMatchedIdx:     -1,
		},
		{
			name:               "BR-AC22: known exporter, no export covers this subject+type",
			imp:                imp,
			exporterClaims:     &jwt.AccountClaims{},
			exporterRecognized: true,
			wantStatus:         topologyNoExport,
			wantMatchedIdx:     -1,
		},
		{
			name:               "matched: export covers the import and demands no token",
			imp:                imp,
			exporterClaims:     &jwt.AccountClaims{ClaimsData: jwt.ClaimsData{}, Account: jwt.Account{Exports: matchingExports}},
			exporterRecognized: true,
			wantStatus:         topologyMatched,
			wantMatchedIdx:     0,
		},
		{
			name:               "BR-AC24: export matches but requires a token the import doesn't carry",
			imp:                imp,
			exporterClaims:     &jwt.AccountClaims{Account: jwt.Account{Exports: tokenReqExports}},
			exporterRecognized: true,
			wantStatus:         topologyTokenRequired,
			wantMatchedIdx:     0,
		},
		{
			name:               "token-required export is satisfied once the import carries a token",
			imp:                impWithToken,
			exporterClaims:     &jwt.AccountClaims{Account: jwt.Account{Exports: tokenReqExports}},
			exporterRecognized: true,
			wantStatus:         topologyMatched,
			wantMatchedIdx:     0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, matchedIdx := importStatus(tc.imp, tc.exporterClaims, tc.exporterRecognized)
			if status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", status, tc.wantStatus)
			}
			if matchedIdx != tc.wantMatchedIdx {
				t.Fatalf("matchedIdx = %d, want %d", matchedIdx, tc.wantMatchedIdx)
			}
		})
	}
}
