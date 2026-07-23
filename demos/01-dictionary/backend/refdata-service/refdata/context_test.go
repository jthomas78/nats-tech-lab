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
})
