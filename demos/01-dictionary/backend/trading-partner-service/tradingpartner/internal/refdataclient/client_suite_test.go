package refdataclient_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRefdataclient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "refdataclient Suite (BR-TP14/BR-037)")
}
