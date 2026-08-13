package accounts

import "testing"

// BR-AC27 — a context slug must be a legal NATS subject token and KV
// bucket-name component, lowercase, with no leading/trailing hyphen and no
// reserved `_` prefix. Cases mirror what an operator can actually type into the
// Add Business Unit dialog rather than invented strings.
func TestValidateContext(t *testing.T) {
	cases := []struct {
		name    string
		slug    string
		wantErr bool
	}{
		{"the shape every seeded business unit already uses", "acme-pacific-fleet", false},
		{"an account's auto-created default", "globex-default", false},
		{"digits are fine anywhere", "acme-fleet-2", false},
		{"a single token with no hyphen", "acme", false},

		{"empty", "", true},
		{"uppercase — subjects are case-sensitive, so this addresses a different bucket than it reads as", "Acme-Pacific-Fleet", true},
		{"a space, the most likely thing an operator types", "acme pacific fleet", true},
		{"a dot would break every positional subject parser", "acme.pacific.fleet", true},
		{"a subject wildcard", "acme-*", true},
		{"a subject full-wildcard", "acme->", true},
		{"leading underscore is reserved for platform use (BR-D33)", "_acme-default", true},
		{"an interior underscore is legal for refdata but not for a slug we derive", "acme_northdiv", true},
		{"leading hyphen", "-acme", true},
		{"trailing hyphen", "acme-", true},
		{"slash, legal in a KV key but not a subject token", "acme/fleet", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateContext(tc.slug)
			if tc.wantErr && err == nil {
				t.Fatalf("ValidateContext(%q) = nil, want error", tc.slug)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateContext(%q) = %v, want nil", tc.slug, err)
			}
		})
	}
}

func TestValidateContextLength(t *testing.T) {
	atLimit := "a" + string(make([]byte, 0))
	for len(atLimit) < MaxContextLen {
		atLimit += "b"
	}
	if err := ValidateContext(atLimit); err != nil {
		t.Fatalf("a slug exactly at MaxContextLen must be accepted, got %v", err)
	}
	if err := ValidateContext(atLimit + "b"); err == nil {
		t.Fatal("a slug one character over MaxContextLen must be rejected — it ends up inside a KV bucket name")
	}
}

// BR-AC26 — the slug proposed in the registration dialog is derived from the
// display name, prefixed with the tenant so BR-AC27's global uniqueness is not
// a trap two tenants fall into on the same obvious name.
func TestDeriveContext(t *testing.T) {
	cases := []struct {
		name   string
		tenant string
		input  string
		want   string
	}{
		{"the ordinary case", "acme", "Pacific Fleet", "acme-pacific-fleet"},
		{"reproduces the value Phase 22 seeded by hand", "acme", "Atlantic Fleet", "acme-atlantic-fleet"},
		{"punctuation collapses to single hyphens", "acme", "Pacific / Fleet — North", "acme-pacific-fleet-north"},
		{"already lowercase and hyphenated", "globex", "north-div", "globex-north-div"},
		{"an operator who types the tenant in themselves is not prefixed twice", "acme", "Acme Pacific Fleet", "acme-pacific-fleet"},
		{"a name that is exactly the tenant stays a single token", "acme", "Acme", "acme"},
		{"surrounding whitespace is trimmed, not turned into hyphens", "acme", "  Pacific Fleet  ", "acme-pacific-fleet"},
		{"a name of only punctuation falls back to the tenant alone", "acme", "!!!", "acme"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveContext(tc.tenant, tc.input)
			if got != tc.want {
				t.Fatalf("DeriveContext(%q, %q) = %q, want %q", tc.tenant, tc.input, got, tc.want)
			}
			if err := ValidateContext(got); err != nil {
				t.Fatalf("DeriveContext produced %q, which its own validator rejects: %v", got, err)
			}
		})
	}
}

// BR-AC28 — every account's default resolves to an ordinary tenant-owned slug,
// deliberately *not* the `_`-prefixed shared value Phase 22 used.
func TestDefaultContext(t *testing.T) {
	for _, tenant := range []string{"acme", "globex", "test"} {
		got := DefaultContext(tenant)
		if want := tenant + "-default"; got != want {
			t.Fatalf("DefaultContext(%q) = %q, want %q", tenant, got, want)
		}
		if err := ValidateContext(got); err != nil {
			t.Fatalf("default slug %q must need no validation exception, got %v", got, err)
		}
	}

	// Two tenants must never resolve to the same default — that collision is
	// the defect Phase 22b exists to remove.
	if DefaultContext("acme") == DefaultContext("globex") {
		t.Fatal("per-tenant defaults collapsed to one shared value")
	}
}
