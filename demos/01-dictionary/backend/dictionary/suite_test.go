package dictionary

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
)

func TestDictionary(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Dictionary Suite")
}

// ReportAfterSuite prints the full spec tree once all specs have completed.
// Specs are sorted by container path so siblings always group together.
var _ = ReportAfterSuite("tree output", func(report types.Report) {
	var specs []types.SpecReport
	for _, r := range report.SpecReports {
		if r.LeafNodeType == types.NodeTypeIt {
			specs = append(specs, r)
		}
	}

	sort.Slice(specs, func(i, j int) bool {
		pi := strings.Join(specs[i].ContainerHierarchyTexts, "\x00")
		pj := strings.Join(specs[j].ContainerHierarchyTexts, "\x00")
		if pi != pj {
			return pi < pj
		}
		return specs[i].LeafNodeText < specs[j].LeafNodeText
	})

	fmt.Println()
	var lastPath []string
	passed, failed := 0, 0

	for _, spec := range specs {
		containers := spec.ContainerHierarchyTexts
		depth := commonPrefixLen(lastPath, containers)
		for i := depth; i < len(containers); i++ {
			fmt.Printf("%s%s\n", strings.Repeat("  ", i), containers[i])
		}
		lastPath = containers

		indent := strings.Repeat("  ", len(containers))
		if spec.Failed() {
			fmt.Printf("%s\033[31m✗\033[0m %s\n", indent, spec.LeafNodeText)
			failed++
		} else {
			fmt.Printf("%s\033[32m✓\033[0m %s\n", indent, spec.LeafNodeText)
			passed++
		}
	}

	fmt.Printf("\n\033[32m%d passed\033[0m, \033[31m%d failed\033[0m\n", passed, failed)
})

func commonPrefixLen(a, b []string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
