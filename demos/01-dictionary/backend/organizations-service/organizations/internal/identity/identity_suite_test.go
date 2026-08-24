// Package identity_test covers BR-TP73 — organizations-service mints ULIDs,
// not UUIDs (ADR-051).
//
// These specs are deliberately not gated on anything: they assert properties
// of the ID format itself, and every one of them is a property some other part
// of the service depends on without re-checking. Nothing downstream validates
// that an ID is subject-safe before publishing it into an immutable stream, so
// the guarantee has to hold here.
package identity_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestIdentity(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Organizations Identity Suite")
}
