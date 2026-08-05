package eventhandler_test

import (
	"log/slog"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(GinkgoWriter, nil))
}

func TestEventhandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Eventhandler Suite")
}
