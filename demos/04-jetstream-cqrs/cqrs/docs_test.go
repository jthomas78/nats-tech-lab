package main

// 04.9.10 — the documents catch up.
//
// A live guard, not a unit test. It reads the ROUTES OUT OF serve.go and
// asks whether this demo's CLAUDE.md lists them. The point is the route
// that gets added later: /pool/stop arrived in 04.9.8 and the document did
// not notice, because nothing was watching.
//
// It deliberately does NOT check the wording. A document that says the right
// words about the wrong routes is the failure this guards against; a
// document that says the right routes in its own words is fine.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// demoRoot walks up from the test's working directory (cqrs/) until it finds
// the demo's own CLAUDE.md. The suite's working directory is the package
// directory, but that is a promise of the tool, not of the repository, so
// the walk is cheap insurance.
func demoRoot() string {
	dir, err := os.Getwd()
	Expect(err).NotTo(HaveOccurred())
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "BUSINESS_RULES-ODOMETER.md")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	Fail("could not find the demo root above the test's working directory")
	return ""
}

var routeLine = regexp.MustCompile(`mux\.HandleFunc\("([^"]+)"`)

var _ = Describe("the documents and the shim", func() {
	served, err := os.ReadFile("serve.go")
	Expect(err).NotTo(HaveOccurred())

	guide, err := os.ReadFile(filepath.Join(demoRoot(), "CLAUDE.md"))
	Expect(err).NotTo(HaveOccurred())

	var routes []string
	for _, m := range routeLine.FindAllStringSubmatch(string(served), -1) {
		routes = append(routes, m[1])
	}

	It("found the routes to check, rather than checking none", func() {
		// A regexp that matched nothing would pass every assertion below.
		Expect(len(routes)).To(BeNumerically(">=", 8))
	})

	for _, r := range routes {
		route := r
		It("names "+route+" in CLAUDE.md", func() {
			Expect(string(guide)).To(ContainSubstring(route))
		})
	}

	It("says lesson 02 is driven from the screen", func() {
		Expect(strings.ToLower(string(guide))).To(ContainSubstring("lesson 02 runs itself"))
	})
})
