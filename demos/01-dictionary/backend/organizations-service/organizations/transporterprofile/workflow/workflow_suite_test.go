package workflow_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTransporterProfileWorkflow(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "TransporterProfile Workflow Suite")
}
