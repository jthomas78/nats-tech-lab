package registry_test

// BR-AS69 — the registry refuses a structurally unusable entry rather than
// storing one every shell will reject.
//
// Derived from the rule, not the implementation: what is asserted below is the
// SPLIT as much as the checks. The registry owns naming and shape, which never
// vary by reader; the shell owns compatibility, which does. So there is a spec
// here for each structural refusal, and a spec proving the registry does NOT
// refuse on version.

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

func admissibleEntry() domain.Entry {
	return domain.Entry{
		ID:              "fleet-ops",
		Name:            "Fleet Ops",
		SchemaVersion:   domain.SchemaVersion,
		ShellAPIVersion: domain.ShellAPIVersion,
		Enabled:         true,
		Remote:          domain.Remote{Kind: domain.RemoteFederated, URL: "http://localhost:7110/remoteEntry.js", Module: "./plugin"},
		ExtensionPoints: []domain.ExtensionPoint{{ID: "fleet-ops/details-sidebar/v1"}},
		Contributions: []domain.Contribution{
			{Kind: "route", ID: "vessels", Path: "/fleet-ops/vessels", Title: "Vessels"},
			{Kind: "navigation", ID: "vessels-nav", Label: "Vessels", Route: "vessels"},
			{Kind: "extension", ID: "home-card", Target: "shell/home-panels/v1"},
			{Kind: "shell-control", ID: "picker", Region: "shell/topbar-controls/v1"},
			{Kind: "shell-footer", ID: "status"},
		},
	}
}

func upsertOf(e domain.Entry) domain.Write {
	return domain.Write{Op: domain.OpUpsert, EntryID: e.ID, Actor: domain.SharedAdminActor, Entry: &e}
}

var _ = Describe("BR-AS69: structural admissibility", func() {
	It("admits an entry that names everything the five kinds need", func() {
		Expect(admissibleEntry().Admissible()).To(Succeed())
		Expect(upsertOf(admissibleEntry()).Validate()).To(Succeed())
	})

	Context("the registry owns naming, because only it sees the whole set", func() {
		It("refuses an id that is not kebab-case", func() {
			e := admissibleEntry()
			e.ID = "Fleet_Ops"
			Expect(errors.Is(e.Admissible(), domain.ErrEntryNotAdmissible)).To(BeTrue())
		})

		It("refuses a route that lives outside its plugin's own prefix", func() {
			e := admissibleEntry()
			e.Contributions[0].Path = "/vessels"
			Expect(errors.Is(e.Admissible(), domain.ErrEntryNotAdmissible)).To(BeTrue())
		})

		It("matches the prefix on the whole segment, so /fleet-opsx is outside /fleet-ops", func() {
			e := admissibleEntry()
			e.Contributions[0].Path = "/fleet-opsx/vessels"
			Expect(errors.Is(e.Admissible(), domain.ErrEntryNotAdmissible)).To(BeTrue())
		})

		It("honours a declared routePrefix that is not the id", func() {
			e := admissibleEntry()
			e.RoutePrefix = "fleet"
			e.Contributions[0].Path = "/fleet/vessels"
			Expect(e.Admissible()).To(Succeed())
		})

		It("refuses an extension point owned by another plugin", func() {
			e := admissibleEntry()
			e.ExtensionPoints[0].ID = "shell/details-sidebar/v1"
			Expect(errors.Is(e.Admissible(), domain.ErrEntryNotAdmissible)).To(BeTrue())
		})

		It("refuses an extension point id that is not {owner}/{region}/v{major}", func() {
			e := admissibleEntry()
			e.ExtensionPoints[0].ID = "fleet-ops/details-sidebar"
			Expect(errors.Is(e.Admissible(), domain.ErrEntryNotAdmissible)).To(BeTrue())
		})

		It("refuses the same contribution id declared twice", func() {
			e := admissibleEntry()
			e.Contributions[1].ID = "vessels"
			Expect(errors.Is(e.Admissible(), domain.ErrEntryNotAdmissible)).To(BeTrue())
		})
	})

	Context("the closed set of kinds", func() {
		It("names exactly the five the shell can place", func() {
			Expect(domain.ContributionKinds()).To(Equal([]string{
				"route", "navigation", "extension", "shell-control", "shell-footer",
			}))
		})

		It("refuses a kind outside it, rather than passing it through", func() {
			e := admissibleEntry()
			e.Contributions[0].Kind = "nav"
			Expect(errors.Is(e.Admissible(), domain.ErrEntryNotAdmissible)).To(BeTrue())
		})
	})

	Context("each kind's own required fields", func() {
		It("refuses a route with no title", func() {
			e := admissibleEntry()
			e.Contributions[0].Title = "  "
			Expect(errors.Is(e.Admissible(), domain.ErrEntryNotAdmissible)).To(BeTrue())
		})

		It("refuses a navigation entry naming a path instead of a local route id", func() {
			e := admissibleEntry()
			e.Contributions[1].Route = "/fleet-ops/vessels"
			Expect(errors.Is(e.Admissible(), domain.ErrEntryNotAdmissible)).To(BeTrue())
		})

		It("refuses an extension whose target is not an extension-point id", func() {
			e := admissibleEntry()
			e.Contributions[2].Target = "home-panels"
			Expect(errors.Is(e.Admissible(), domain.ErrEntryNotAdmissible)).To(BeTrue())
		})

		It("refuses a shell-control whose region is not an extension-point id", func() {
			e := admissibleEntry()
			e.Contributions[3].Region = ""
			Expect(errors.Is(e.Admissible(), domain.ErrEntryNotAdmissible)).To(BeTrue())
		})
	})

	It("refuses an entry that contributes nothing, because it can never reach a screen", func() {
		e := admissibleEntry()
		e.Contributions = []domain.Contribution{}
		Expect(errors.Is(e.Admissible(), domain.ErrEntryNotAdmissible)).To(BeTrue())
	})

	/* The other half of the split. One registry serves shells of several
	   vintages across an upgrade, so refusing on a version the CURRENT shell
	   does not know would refuse an entry that is good for the shell it was
	   published for. Compatibility is reported by the shell as a status; it is
	   never a refusal here. */
	It("does not refuse on schemaVersion or shellApiVersion — those are the shell's to judge", func() {
		e := admissibleEntry()
		e.SchemaVersion = 99
		e.ShellAPIVersion = 99
		Expect(e.Admissible()).To(Succeed())
	})

	/* An upsert is the only write that carries a body, so it is the only one
	   the check can run on. set-enabled names an entry that is already stored
	   and already passed. */
	It("checks structure on upsert and leaves set-enabled alone", func() {
		bad := admissibleEntry()
		bad.Contributions[0].Kind = "nav"
		Expect(errors.Is(upsertOf(bad).Validate(), domain.ErrEntryNotAdmissible)).To(BeTrue())

		toggle := domain.Write{Op: domain.OpSetEnabled, EntryID: bad.ID, Actor: domain.SharedAdminActor, Enabled: false}
		Expect(toggle.Validate()).To(Succeed())
	})
})
