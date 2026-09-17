package main

// The pattern cards — step 4 of the demo lifecycle.
//
// A live guard, not a unit test. It reads the card deck itself and asks
// whether it still keeps the promises the lifecycle rule makes: one card per
// finding, every finding this demo actually measured, pros AND cons on every
// card, and a page that says where every number came from.
//
// It deliberately does NOT check the wording or the conclusions. A deck can
// be wrong about a recommendation and no test will catch that. What a test
// CAN catch is a deck that quietly loses a card, drops the provenance page,
// or prints a measurement with no date against it — and those are the three
// ways a reference document rots.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const cardsFile = "docs/demo-04-pattern-cards.html"

func cardsHTML() string {
	b, err := os.ReadFile(filepath.Join(demoRoot(), cardsFile))
	Expect(err).NotTo(HaveOccurred(), cardsFile+" must exist — it is step 4 of the demo lifecycle")
	return string(b)
}

// prose strips the <style> block and then every tag, so a check for a phrase
// cannot be satisfied by a CSS class name or an attribute.
func cardsProse() string {
	s := cardsHTML()
	if i := strings.Index(s, "<style"); i >= 0 {
		if j := strings.Index(s, "</style>"); j > i {
			s = s[:i] + s[j+len("</style>"):]
		}
	}
	s = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(s, " ")
	return regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
}

// The findings this demo actually produced. One card each. A finding that
// stops being a card is either a finding the demo lost or a card somebody
// deleted, and both are worth a red suite.
var cardTitles = []string{
	"The log is the only source of truth",
	"Two projections, one log",
	"What a snapshot buys",
	"The ordered consumer that ate the measurement",
	"A fold is defined by order",
	"A worker pool buys throughput with correctness",
	"MaxAckPending is the loss dial",
	"Redelivery after AckWait is not recovery",
}

var _ = Describe("The pattern cards", func() {
	It("exist, because a demo is not finished without them", func() {
		Expect(cardsHTML()).To(ContainSubstring("<!doctype html>"))
	})

	It("print on A4, because the deliverable is a PDF", func() {
		Expect(cardsHTML()).To(ContainSubstring("@page"))
		Expect(cardsHTML()).To(ContainSubstring("A4"))
	})

	It("have been exported, so the PDF beside them is not stale by absence", func() {
		_, err := os.Stat(filepath.Join(demoRoot(), "docs/demo-04-pattern-cards.pdf"))
		Expect(err).NotTo(HaveOccurred(), "export the deck: node ../01-dictionary/diagrams/export-html-pdf.mjs")
	})

	for _, title := range cardTitles {
		title := title
		It("carries the card: "+title, func() {
			Expect(cardsProse()).To(ContainSubstring(title))
		})
	}

	It("gives every card a pro side and a con side", func() {
		html := cardsHTML()
		pros := strings.Count(html, `class="panel pro"`)
		cons := strings.Count(html, `class="panel con"`)
		Expect(pros).To(BeNumerically(">=", len(cardTitles)))
		Expect(cons).To(BeNumerically(">=", len(cardTitles)))
	})

	It("names the business rules it leans on, so a reader can find them", func() {
		prose := cardsProse()
		for _, br := range []string{"BR-OD06", "BR-OD07", "BR-OD08", "BR-OD09"} {
			Expect(prose).To(ContainSubstring(br))
		}
	})

	It("says where every number came from", func() {
		prose := cardsProse()
		Expect(prose).To(ContainSubstring("Where every number came from"))
		// The machine and the date, not just the number. A measurement with
		// neither is the stale constant this repo keeps deleting.
		Expect(prose).To(ContainSubstring("NATS 2.14.3"))
		Expect(prose).To(MatchRegexp(`2026-09-1[456]`))
	})

	It("carries a recommendation, not only a description", func() {
		Expect(cardsHTML()).To(ContainSubstring(`class="pill"`))
		Expect(cardsProse()).To(ContainSubstring("Verdict"))
	})

	It("keeps the corrected rehydration number and not the retracted one", func() {
		prose := cardsProse()
		Expect(prose).To(ContainSubstring("25 ms"))
		Expect(prose).NotTo(MatchRegexp(`\b600x\b`))
	})

	// The demo's own trap: ODOMETER is a prefix of ODOMETER_POOL.
	It("keeps lesson 02 on its own log", func() {
		Expect(cardsProse()).To(ContainSubstring("ODOMETER_POOL"))
	})
})
