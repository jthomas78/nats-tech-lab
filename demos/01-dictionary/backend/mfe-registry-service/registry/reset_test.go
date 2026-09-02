package registry_test

// Phase 15d — the catalogue-reset notice (BR-AS73, decisions 6, 7, 8, 13).
//
// Two rules are checked here and they fail in opposite directions, which is
// why they are specced apart rather than through one start-up path.
//
// The PREDICATE decides whether a notice fires at all. Getting it wrong in
// one direction costs a re-announce storm on every rolling restart; getting
// it wrong in the other reopens the hole this phase exists to close, and does
// so silently — nothing errors, the catalogue is simply empty forever.
//
// The CLAMP decides what a plugin does with the window the notice carries.
// It is a rule about not trusting input: the field exists so the registry can
// widen the spread across a fleet without redeploying anything, and the clamp
// is what stops that same field from narrowing the spread to zero.

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

var _ = Describe("BR-AS73 — the reset predicate", func() {
	Context("the scenarios the rule enumerates", func() {
		It("stays silent when the registry restarts with its catalogue intact", func() {
			Expect(domain.CatalogueLost(7, 7)).To(BeFalse())
		})

		It("stays silent on a first boot, where there is nothing witnessed at all", func() {
			Expect(domain.CatalogueLost(domain.NoRevision, domain.NoRevision)).To(BeFalse())
		})

		It("stays silent when everything restarts together, because the plugins announce at startup", func() {
			// `docker compose down -v` takes the catalogue, the cache and the
			// plugins in one go. Nothing is witnessed, so nothing fires — and
			// a notice here would be worth nothing anyway.
			Expect(domain.CatalogueLost(domain.NoRevision, domain.NoRevision)).To(BeFalse())
		})

		It("fires when the catalogue is emptied while the plugins are still alive", func() {
			Expect(domain.CatalogueLost(7, domain.NoRevision)).To(BeTrue())
		})

		It("fires when the catalogue is restored from a stale backup", func() {
			// The distinguishing case: not empty, just older. A predicate
			// written as "is the catalogue empty" would miss this entirely
			// and would look correct in every test that only emptied it.
			Expect(domain.CatalogueLost(7, 4)).To(BeTrue())
		})
	})

	Context("what the predicate refuses to read as a loss", func() {
		It("does not fire when the catalogue moved forward, which is ordinary operation", func() {
			Expect(domain.CatalogueLost(4, 7)).To(BeFalse())
		})

		It("does not fire when the witness itself is missing, so recreating the cache is not a reset", func() {
			// Losing the KV bucket while Postgres survives leaves no witness.
			// Inventing one from an empty cache would fire a notice every
			// time that bucket was recreated — a storm caused by the backstop
			// rather than prevented by it.
			Expect(domain.CatalogueLost(domain.NoRevision, 7)).To(BeFalse())
		})
	})
})

var _ = Describe("BR-AS73 — the carried jitter window is clamped before it is used", func() {
	notice := func(ms int64) mferegistry.ResetNotice {
		return mferegistry.ResetNotice{JitterMillis: ms}
	}

	It("honours a window inside the locally-owned range, which is the registry's power to widen", func() {
		window := 90 * time.Second
		Expect(notice(window.Milliseconds()).JitterWindow()).To(Equal(window))
	})

	It("refuses to narrow the window to zero, which is what would turn the notice into a stampede", func() {
		for _, ms := range []int64{0, -1, 1, 250} {
			Expect(notice(ms).JitterWindow()).To(BeNumerically(">=", mferegistry.ResetJitterFloor), ms)
		}
	})

	It("treats an absent window as the default rather than as zero, so a forgetful sender cannot request one", func() {
		Expect(mferegistry.ResetNotice{}.JitterWindow()).To(Equal(mferegistry.ResetJitterDefault))
	})

	It("refuses a window past the ceiling, so a plugin never looks lost rather than patient", func() {
		Expect(notice((24 * time.Hour).Milliseconds()).JitterWindow()).To(Equal(mferegistry.ResetJitterCeiling))
	})

	It("keeps the floor strictly below the ceiling and the default between them, so no clamp is unreachable", func() {
		Expect(mferegistry.ResetJitterFloor).To(BeNumerically("<", mferegistry.ResetJitterCeiling))
		Expect(mferegistry.ResetJitterDefault).To(BeNumerically(">=", mferegistry.ResetJitterFloor))
		Expect(mferegistry.ResetJitterDefault).To(BeNumerically("<=", mferegistry.ResetJitterCeiling))
	})

	It("carries nothing to install from, so the notice cannot become a second way into the catalogue", func() {
		// A reset notice is a statement of fact. The recipient re-announces
		// what it already holds and signed; if the notice could carry entries
		// it would be an unsigned path to the same place.
		Expect(mferegistry.EntriesReset).To(HavePrefix("notify."))
		Expect(mferegistry.EntriesReset).NotTo(HavePrefix("cmd."))
	})
})
