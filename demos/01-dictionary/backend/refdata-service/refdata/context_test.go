package refdata_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
)

var _ = Describe("Context Domain Rules", func() {
	var (
		ctx context.Context
		ch  *commands.ContextHandler
	)

	BeforeEach(func() {
		ctx = context.Background()
		ch = commands.NewContextHandler(newFakeContextRepo())
	})

	Context("BR-D22: context name must be a valid subject/KV-bucket token", func() {
		It("registers a valid lowercase-hyphenated context", func() {
			Expect(ch.Register(ctx, domain.Context{Context: "emea-globex", Name: "EMEA Globex"})).To(Succeed())
		})

		It("rejects an empty context", func() {
			err := ch.Register(ctx, domain.Context{Context: "", Name: "Mystery"})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})

		It("rejects a context containing a dot", func() {
			err := ch.Register(ctx, domain.Context{Context: "emea.globex", Name: "Mystery"})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})

		It("rejects a context containing '*'", func() {
			err := ch.Register(ctx, domain.Context{Context: "emea*globex", Name: "Mystery"})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})

		It("rejects a context containing '>'", func() {
			err := ch.Register(ctx, domain.Context{Context: "emea>globex", Name: "Mystery"})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})

		It("rejects a context containing whitespace", func() {
			err := ch.Register(ctx, domain.Context{Context: "emea globex", Name: "Mystery"})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})
	})

	Context("BR-D33: a context beginning with '_' is reserved for platform use", func() {
		It("rejects a company context that starts with an underscore", func() {
			err := ch.Register(ctx, domain.Context{Context: "_acme", Name: "Sneaky"})
			Expect(errors.Is(err, domain.ErrReservedContextPrefix)).To(BeTrue())
		})

		It("rejects the exact reserved platform root name too — this endpoint is not how it gets created", func() {
			err := ch.Register(ctx, domain.Context{Context: "_platform", Name: "Platform"})
			Expect(errors.Is(err, domain.ErrReservedContextPrefix)).To(BeTrue())
		})

		It("still allows a hyphenated business-unit context that merely contains an underscore mid-string", func() {
			Expect(ch.Register(ctx, domain.Context{Context: "acme_northdiv", Name: "Acme North Division"})).To(Succeed())
		})
	})

	Context("Phase 16d: RegisterPlatformRoot is the one sanctioned exception to BR-D33", func() {
		It("registers the reserved platform root, unlike Register above", func() {
			Expect(ch.RegisterPlatformRoot(ctx, domain.Context{Context: "_platform", Name: "Platform"})).To(Succeed())
			got, err := ch.Get(ctx, "_platform")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Name).To(Equal("Platform"))
		})

		It("still applies the base charset check — a malformed value is not exempted", func() {
			err := ch.RegisterPlatformRoot(ctx, domain.Context{Context: "_plat form", Name: "Platform"})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})
	})

	Context("Phase 22: RegisterDefaultBu is the second sanctioned exception to BR-D33 (BR-D38)", func() {
		It("registers _default_bu, unlike Register above", func() {
			Expect(ch.RegisterDefaultBu(ctx, domain.Context{Context: "_default_bu", Name: "Default Business Unit"})).To(Succeed())
			got, err := ch.Get(ctx, "_default_bu")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Name).To(Equal("Default Business Unit"))
		})

		It("still applies the base charset check — a malformed value is not exempted", func() {
			err := ch.RegisterDefaultBu(ctx, domain.Context{Context: "_default bu", Name: "Default Business Unit"})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})
	})
})
