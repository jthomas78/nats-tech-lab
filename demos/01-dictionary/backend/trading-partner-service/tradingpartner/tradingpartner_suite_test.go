package tradingpartner_test

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
)

func TestTradingPartner(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Trading Partner Suite")
}

var _ = ReportAfterSuite("spec tree", func(report Report) {
	fmt.Println("\nTrading Partner Suite — spec tree")
	for _, spec := range report.SpecReports {
		if spec.State == types.SpecStatePassed {
			fmt.Printf("  [PASS] %s\n", spec.FullText())
		} else {
			fmt.Printf("  [FAIL] %s\n", spec.FullText())
		}
	}
})
