package accounts_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

// These are pure domain specs — no Postgres, so they always run (unlike the
// handler/store integration specs that skip when docker is unavailable).
var _ = Describe("TokenTTLConfig", func() {
	Describe("DefaultTokenTTLConfig (BR-AC20)", func() {
		It("defaults the value to 15 minutes within a 15–30 minute range", func() {
			cfg := accounts.DefaultTokenTTLConfig()
			Expect(cfg.ValueMinutes).To(Equal(15))
			Expect(cfg.MinMinutes).To(Equal(15))
			Expect(cfg.MaxMinutes).To(Equal(30))
			Expect(cfg.Validate()).To(Succeed())
		})

		It("produces a 15-minute TTL duration", func() {
			Expect(accounts.DefaultTokenTTLConfig().TTL()).To(Equal(15 * time.Minute))
		})
	})

	Describe("the hard BR-UA03 envelope", func() {
		It("is 15–30 minutes", func() {
			Expect(accounts.MinTTLMinutes).To(Equal(15))
			Expect(accounts.MaxTTLMinutes).To(Equal(30))
		})
	})

	DescribeTable("Validate (BR-AC21)",
		func(value, min, max int, valid bool) {
			err := accounts.TokenTTLConfig{ValueMinutes: value, MinMinutes: min, MaxMinutes: max}.Validate()
			if valid {
				Expect(err).NotTo(HaveOccurred())
			} else {
				Expect(err).To(HaveOccurred())
			}
		},
		Entry("default: value 15 in range [15,30]", 15, 15, 30, true),
		Entry("value at the range maximum", 30, 15, 30, true),
		Entry("narrowed range with value inside", 20, 18, 22, true),
		Entry("value below the configured range", 15, 18, 22, false),
		Entry("value above the configured range", 25, 18, 22, false),
		Entry("range minimum below the 15-minute envelope", 15, 10, 30, false),
		Entry("range maximum above the 30-minute envelope", 30, 15, 45, false),
		Entry("value dragged below envelope by a widened range", 5, 5, 30, false),
		Entry("inverted range (min > max)", 20, 25, 20, false),
	)
})
