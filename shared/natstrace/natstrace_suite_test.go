package natstrace_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNatstrace(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "natstrace Suite (Phase 35 — shared/natstrace)")
}
