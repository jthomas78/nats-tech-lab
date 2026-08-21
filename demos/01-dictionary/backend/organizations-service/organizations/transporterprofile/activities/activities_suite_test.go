package activities_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTransporterProfileActivities(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "TransporterProfile Activities Suite")
}
