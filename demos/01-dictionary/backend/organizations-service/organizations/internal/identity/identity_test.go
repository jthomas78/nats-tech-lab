package identity_test

import (
	"regexp"
	"sort"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/identity"
)

// crockford is the ULID alphabet: base32 without I, L, O or U. Written out
// literally rather than imported so a change to the encoder cannot quietly
// change what this spec considers acceptable.
var crockford = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

var _ = Describe("BR-TP73 — entity identifiers are ULIDs", func() {
	Describe("New", func() {
		It("mints 26 characters", func() {
			Expect(identity.New()).To(HaveLen(26))
			Expect(identity.Size).To(Equal(26))
		})

		It("uses only Crockford base32 characters", func() {
			for range 200 {
				Expect(identity.New()).To(MatchRegexp(crockford.String()))
			}
		})

		It("mints something IsValid accepts", func() {
			for range 200 {
				Expect(identity.IsValid(identity.New())).To(BeTrue())
			}
		})

		It("does not repeat itself", func() {
			seen := map[string]bool{}
			for range 10_000 {
				id := identity.New()
				Expect(seen).NotTo(HaveKey(id))
				seen[id] = true
			}
		})

		It("is safe to call concurrently", func() {
			// ulid.Make is documented as concurrency-safe via a sync.Pool.
			// Asserting it here means a future swap of the underlying encoder
			// for one that isn't gets caught by the suite rather than by a
			// duplicate primary key under load.
			const workers, each = 16, 500
			var mu sync.Mutex
			var wg sync.WaitGroup
			seen := map[string]bool{}
			for range workers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					ids := make([]string, 0, each)
					for range each {
						ids = append(ids, identity.New())
					}
					mu.Lock()
					defer mu.Unlock()
					for _, id := range ids {
						seen[id] = true
					}
				}()
			}
			wg.Wait()
			Expect(seen).To(HaveLen(workers * each))
		})

		It("sorts lexicographically in the order it was minted", func() {
			// The property Postgres B-tree locality and stable subject
			// ordering both rest on. Monotonic entropy is what makes this hold
			// even for IDs minted inside the same millisecond, which at this
			// loop's speed is most of them.
			minted := make([]string, 0, 1000)
			for range 1000 {
				minted = append(minted, identity.New())
			}
			sorted := append([]string(nil), minted...)
			sort.Strings(sorted)
			Expect(sorted).To(Equal(minted))
		})

		It("never contains a character that would split a NATS subject token", func() {
			// The reason this format was chosen over a company registration
			// number. A '.' here would silently become a token boundary and
			// break the fixed-arity positional parsing every subject family in
			// ARCHITECTURE-COMMUNICATIONS.md § 2 relies on; '*' and '>' would
			// turn one aggregate's subject into a wildcard.
			for range 500 {
				Expect(identity.New()).NotTo(MatchRegexp(`[.*> ]`))
			}
		})

		It("only contains characters a NATS KV key allows", func() {
			// KV keys are restricted to [-/_=.a-zA-Z0-9]; the Crockford
			// alphabet is a strict subset of the alphanumeric part.
			for range 500 {
				Expect(identity.New()).To(MatchRegexp(`^[-/_=.a-zA-Z0-9]+$`))
			}
		})
	})

	Describe("IsValid", func() {
		It("rejects a UUID", func() {
			// The format this service migrated away from. A UUID is both the
			// wrong length and carries hyphens, so it fails twice over — but
			// it must fail, because a legacy UUID reaching a fresh code path
			// means a row survived the reseed.
			Expect(identity.IsValid("6a2d2d17-6ff3-489c-9f32-6b7ab5b37a5a")).To(BeFalse())
		})

		It("rejects the empty string", func() {
			Expect(identity.IsValid("")).To(BeFalse())
		})

		It("rejects the wrong length", func() {
			id := identity.New()
			Expect(identity.IsValid(id[:25])).To(BeFalse())
			Expect(identity.IsValid(id + "0")).To(BeFalse())
		})

		It("rejects characters outside the Crockford alphabet", func() {
			// I, L, O and U are excluded from the alphabet precisely because
			// they are confusable when a human reads an ID aloud.
			for _, excluded := range []string{"I", "L", "O", "U"} {
				Expect(identity.IsValid("0" + excluded + "M13TE7YBBVXVP79C2YYY9D2")).To(BeFalse())
			}
		})

		It("rejects an overflowing first character", func() {
			// 26 Crockford characters address 130 bits; a ULID is 128. So the
			// leading character is capped at '7' and 8..Z is malformed, not
			// merely unusual. Worth a spec because it is the one ULID rule
			// that a naive length-plus-alphabet validator would miss.
			Expect(identity.IsValid("8ZZZZZZZZZZZZZZZZZZZZZZZZZ")).To(BeFalse())
			Expect(identity.IsValid("ZZZZZZZZZZZZZZZZZZZZZZZZZZ")).To(BeFalse())
			Expect(identity.IsValid("7ZZZZZZZZZZZZZZZZZZZZZZZZZ")).To(BeTrue())
		})

		It("accepts a known-good literal", func() {
			// Pinned so a future encoder swap that still round-trips its own
			// output but changes the wire format is caught here.
			Expect(identity.IsValid("01M13TE7YBBVXVP79C2YYY9D29")).To(BeTrue())
		})
	})
})
