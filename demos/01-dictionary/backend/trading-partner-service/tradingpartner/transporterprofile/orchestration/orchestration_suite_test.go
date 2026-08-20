package orchestration_test

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
)

func TestTransporterProfileOrchestration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "TransporterProfile Orchestration Suite")
}

var _ = ReportAfterSuite("spec tree", func(report Report) {
	fmt.Println("\nTransporterProfile Orchestration Suite — spec tree")
	for _, spec := range report.SpecReports {
		state := "FAIL"
		if spec.State == types.SpecStatePassed {
			state = "PASS"
		}
		fmt.Printf("  [%s] %s\n", state, spec.FullText())
	}
})
