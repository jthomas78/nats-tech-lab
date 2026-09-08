package natstenants

// BR-AC46 — NATS_CREDS_DIR is a PATH-style list, so the bootstrap trust
// material can be mounted read-only while a runtime-minted tenant still
// lands somewhere writable (ADR-055). Derived from the rule: the seed wins,
// a missing directory is not an error, no directory readable is.

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func writeCreds(dir, stem string) string {
	path := filepath.Join(dir, stem+".creds")
	Expect(os.WriteFile(path, []byte("-----BEGIN NATS USER JWT-----\n"), 0o600)).To(Succeed())
	return path
}

func list(dirs ...string) string {
	out := ""
	for i, d := range dirs {
		if i > 0 {
			out += string(os.PathListSeparator)
		}
		out += d
	}
	return out
}

var _ = Describe("BR-AC46: NATS_CREDS_DIR is a PATH-style list", func() {
	It("still takes a single directory", func() {
		dir := GinkgoT().TempDir()
		want := writeCreds(dir, "acme")

		got, err := Discover(dir)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveKeyWithValue("acme", Credentials{CredsPath: want}))
	})

	It("unions the tenants across every listed directory", func() {
		seed, minted := GinkgoT().TempDir(), GinkgoT().TempDir()
		writeCreds(seed, "acme")
		writeCreds(minted, "newco")

		got, err := Discover(list(seed, minted))
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveKey("acme"))
		Expect(got).To(HaveKey("newco"))
	})

	It("lets the first directory win, so a writable volume cannot shadow the seed", func() {
		seed, minted := GinkgoT().TempDir(), GinkgoT().TempDir()
		want := writeCreds(seed, "acme")
		writeCreds(minted, "acme")

		got, err := Discover(list(seed, minted))
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveKeyWithValue("acme", Credentials{CredsPath: want}))
	})

	It("skips a listed directory that does not exist, because the writable volume starts empty", func() {
		seed := GinkgoT().TempDir()
		writeCreds(seed, "acme")

		got, err := Discover(list(seed, filepath.Join(seed, "not-mounted-yet")))
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveKey("acme"))
	})

	It("still excludes a service credential found in any directory", func() {
		seed, minted := GinkgoT().TempDir(), GinkgoT().TempDir()
		writeCreds(seed, "platform")
		writeCreds(minted, "observability")
		writeCreds(minted, "newco")

		got, err := Discover(list(seed, minted))
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveLen(1))
		Expect(got).To(HaveKey("newco"))
	})

	It("fails when no listed directory can be read at all", func() {
		base := GinkgoT().TempDir()
		_, err := Discover(list(filepath.Join(base, "nope"), filepath.Join(base, "also-nope")))
		Expect(err).To(HaveOccurred())
	})

	It("fails on an empty list rather than silently finding no tenants", func() {
		_, err := Discover("")
		Expect(err).To(HaveOccurred())
	})

	Describe("CredsDirs", func() {
		It("splits a list and drops empty segments", func() {
			Expect(CredsDirs(list("/a", "", "/b"))).To(Equal([]string{"/a", "/b"}))
		})

		It("returns a single directory unchanged", func() {
			Expect(CredsDirs("/etc/nats/creds")).To(Equal([]string{"/etc/nats/creds"}))
		})

		It("returns nothing for an empty value", func() {
			Expect(CredsDirs("")).To(BeEmpty())
		})
	})
})
