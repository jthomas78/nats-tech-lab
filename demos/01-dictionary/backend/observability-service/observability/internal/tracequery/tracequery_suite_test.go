package tracequery_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTracequery(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "tracequery Suite (Phase 54 — trace query gateway)")
}
