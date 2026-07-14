package refdata_test

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
)

func TestRefdata(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Refdata Suite")
}

var _ = ReportAfterSuite("spec tree", func(report Report) {
	fmt.Println("\nRefdata Suite — spec tree")
	for _, spec := range report.SpecReports {
		if spec.State == types.SpecStatePassed {
			fmt.Printf("  [PASS] %s\n", spec.FullText())
		} else {
			fmt.Printf("  [FAIL] %s\n", spec.FullText())
		}
	}
})
