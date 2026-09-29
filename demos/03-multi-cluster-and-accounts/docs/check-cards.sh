#!/usr/bin/env bash
# The pattern cards -- stage 04 of the demo lifecycle.
#
# A shape guard, not a truth test. Demo 03 has no Go code and no Ginkgo suite,
# so this script is the demo's spec. It reads the deck and asks whether it still
# keeps the promises the lifecycle rule makes: one card per pattern, a pro AND a
# con on every card, a verdict on every card, a provenance page, and an exported
# PDF beside the HTML.
#
# It deliberately does NOT check the wording or the conclusions. A deck can be
# wrong about a recommendation and no script will catch that. What a script CAN
# catch is a deck that quietly loses a card, drops the provenance page, or
# prints a measurement with no date against it -- and those are the three ways a
# reference document rots.
#
#   ./docs/check-cards.sh      # from the demo root, or from anywhere
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HTML="$HERE/03-multi-cluster-and-accounts-pattern-cards-v0.5.html"
PDF="$HERE/03-multi-cluster-and-accounts-pattern-cards-v0.5.pdf"

fail=0
ok()   { printf '  \033[32mPASS\033[0m  %s\n' "$1"; }
bad()  { printf '  \033[31mFAIL\033[0m  %s\n' "$1"; fail=$((fail+1)); }
want() { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1 -- expected $3, got $2"; fi; }

# prose strips the <style> block and then every tag, so a check for a phrase
# cannot be satisfied by a CSS class name or an attribute.
prose() {
  sed '/<style>/,/<\/style>/d' "$HTML" | sed 's/<[^>]*>/ /g' | tr -s ' \n' ' '
}
has() {
  if prose | grep -qF -- "$2"; then ok "$1"; else bad "$1 -- missing: $2"; fi
}

echo "pattern cards -- shape guard"

[ -f "$HTML" ] && ok "the deck exists, because a demo is not finished without it" \
               || { bad "missing $HTML"; exit 1; }
[ -f "$PDF" ]  && ok "the deck has been exported, so the PDF is not stale by absence" \
               || bad "missing $PDF -- export it: node ../../01-dictionary/diagrams/export-html-pdf.mjs"

# One card, one page, and the page is as tall as the card (v0.3). Each page
# prints on its own named @page, sized by fit-pages.mjs. A page with no size
# falls back to A4 and clips its card, so the counts must match.
npages=$(grep -c '<section class="page"' "$HTML")
nsized=$(grep -cE '<section class="page"[^>]* data-p="' "$HTML")
nrules=$(grep -cE '@page p[0-9]+ \{ size: 210mm [0-9]+mm; \}' "$HTML")
[ "$npages" -eq "$nsized" ] && [ "$npages" -eq "$nrules" ] \
  && ok "every page has its own size, so no card is cut ($npages pages)" \
  || bad "$npages pages, $nsized with data-p, $nrules @page sizes -- run: node docs/fit-pages.mjs $HTML"

# One card per pattern. A pattern that stops being a card is either a finding
# the demo lost or a card somebody deleted, and both are worth a red run.
TITLES=(
 "Five shapes, four variants, one letter each"
 "A vote needs more than half, and servers vote, not sites"
 "is three questions"
 "Placement decides where bytes sit, not who survives"
 "A stream name is unique in one meta group and one account"
 "A domain names a JetStream. It is not a wall."
 "A gateway buys one name and sells your survival"
 "An arbiter site buys the vote back"
 "This lab's leaf link gives each region a vote, and a double copy"
 "A mirror is the second copy"
 "Export / import: the explicit way across an account"
 "Both links at once is not both shapes at once"
 "Stop, rewrite, restart is not a way to switch shapes"
)
for t in "${TITLES[@]}"; do has "carries the card: $t" "$t"; done

pros=$(grep -c 'class="panel pro"' "$HTML")
cons=$(grep -c 'class="panel con"' "$HTML")
[ "$pros" -ge "${#TITLES[@]}" ] && ok "every card has a pro side ($pros)" \
  || bad "only $pros pro panels for ${#TITLES[@]} cards"
[ "$cons" -ge "${#TITLES[@]}" ] && ok "every card has a con side ($cons)" \
  || bad "only $cons con panels for ${#TITLES[@]} cards"

verdicts=$(grep -c 'class="verdict"' "$HTML")
[ "$verdicts" -ge "${#TITLES[@]}" ] && ok "every card carries a recommendation, not only a description" \
  || bad "only $verdicts verdicts for ${#TITLES[@]} cards"

# Provenance. A figure with no date and no machine becomes a stale constant.
has "says where every number came from"   "Where every number came from"
has "names the server it ran on"          "nats-server v2.14.6"
has "names the run that produced it"      "196 rig checks passed"
prose | grep -qE '2026-09-2[0-9]' \
  && ok "dates its numbers" || bad "no measurement date in the deck"

# Every drawing must be readable by something that cannot see it.
figs=$(grep -c 'role="img"' "$HTML")
[ "$figs" -gt "${#TITLES[@]}" ] && ok "one drawing per card, plus the key ($figs figures)" \
  || bad "only $figs figures for ${#TITLES[@]} cards -- every card gets its own drawing"
labels=$(grep -c 'aria-label=' "$HTML")
want "every figure has an aria-label" "$labels" "$figs"

# The drawings follow REPORT.html's convention, so a boundary means the same
# thing in both. The colours ARE the argument of cards 05-06: if the account box
# and the domain box ever become the same thing, the deck stops making its point.
for cls in 'nb dom' 'nb acct' 'nb clu' 'nb clu dead'; do
  grep -qF "class=\"$cls\"" "$HTML" \
    && ok "draws the report's boundary: $cls" \
    || bad "no $cls box in any figure -- the report convention has been dropped"
done
grep -qE '\.fig \.nb\.acct *\{[^}]*--warn' "$HTML" \
  && ok "the account box keeps the report's colour" \
  || bad "the account box no longer uses --warn"
grep -qE '\.fig \.nb\.dom *\{[^}]*--dom' "$HTML" \
  && ok "the domain box keeps the report's colour" \
  || bad "the domain box no longer uses --dom"
has "explains how to read a figure before the first card" "How to read a figure"
has "keeps the selection guide"          "Pick the shape from the outage you must survive"

echo
if [ "$fail" -eq 0 ]; then
  printf '  \033[32mdeck shape OK\033[0m\n'
else
  printf '  \033[31m%d problem(s)\033[0m\n' "$fail"
fi
exit $(( fail > 0 ))
