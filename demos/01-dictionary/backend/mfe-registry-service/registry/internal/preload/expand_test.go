package preload_test

// BR-AS74 -- the preload file's variables are expanded before it is parsed.
// Derived from the rule, not the implementation: a cell's host ports are
// variables everywhere, and the preload file is configuration too.

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/preload"
)

func TestPreload(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Preload suite")
}

func env(pairs map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := pairs[name]
		return v, ok
	}
}

var _ = Describe("BR-AS74: the preload file's variables are expanded", func() {
	It("substitutes a set variable", func() {
		out, err := preload.ExpandEnv([]byte(`{"url":"http://localhost:${PORT}/r.js"}`), env(map[string]string{"PORT": "7162"}))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(out)).To(Equal(`{"url":"http://localhost:7162/r.js"}`))
	})

	It("falls back to the :- default when the variable is unset", func() {
		out, err := preload.ExpandEnv([]byte(`http://localhost:${PORT:-7112}/r.js`), env(nil))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(out)).To(Equal(`http://localhost:7112/r.js`))
	})

	It("prefers a set variable over its default", func() {
		out, err := preload.ExpandEnv([]byte(`${PORT:-7112}`), env(map[string]string{"PORT": "7162"}))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(out)).To(Equal("7162"))
	})

	It("expands every occurrence, not only the first", func() {
		out, err := preload.ExpandEnv([]byte(`${A}-${B}-${A}`), env(map[string]string{"A": "1", "B": "2"}))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(out)).To(Equal("1-2-1"))
	})

	It("accepts an empty default, so a variable may expand to nothing", func() {
		out, err := preload.ExpandEnv([]byte(`x${GONE:-}y`), env(nil))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(out)).To(Equal("xy"))
	})

	It("fails boot on a variable with no value and no default", func() {
		_, err := preload.ExpandEnv([]byte(`http://localhost:${PORT}/r.js`), env(nil))
		Expect(err).To(MatchError(preload.ErrUnsetVar))
	})

	Context("what it deliberately leaves alone", func() {
		It("leaves a bare $name untouched, so a description may hold a dollar sign", func() {
			in := `{"description":"costs $PORT dollars"}`
			out, err := preload.ExpandEnv([]byte(in), env(map[string]string{"PORT": "7162"}))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(out)).To(Equal(in))
		})

		It("leaves an unclosed ${ untouched rather than guessing", func() {
			in := `{"url":"http://localhost:${PORT"}`
			out, err := preload.ExpandEnv([]byte(in), env(map[string]string{"PORT": "7162"}))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(out)).To(Equal(in))
		})

		It("passes a file with no variables through byte for byte", func() {
			in := `{"schemaVersion":1,"plugins":[]}`
			out, err := preload.ExpandEnv([]byte(in), env(nil))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(out)).To(Equal(in))
		})
	})
})
