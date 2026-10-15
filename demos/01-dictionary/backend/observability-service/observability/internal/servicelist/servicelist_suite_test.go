package servicelist_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestServicelist(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "servicelist Suite (Phase 54 — live service list)")
}
