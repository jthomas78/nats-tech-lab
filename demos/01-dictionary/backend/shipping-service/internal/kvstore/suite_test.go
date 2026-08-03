package kvstore_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

func TestKVStore(t *testing.T) {
	gomega.RegisterFailHandler(Fail)
	RunSpecs(t, "KVStore Suite")
}
