# Phase 38e — `trading-partner-service` renamed to `organizations-service`

Commit `6c1bfe6` ("feat(lab): complete Phase 38 — organizations rename (38e),
live Temporal worker (38b completion), streams-rail kind tagging").

**The old names appear nowhere in the tree any more.** Grepping for them
returns nothing, which is exactly the failure mode this file exists to
prevent — several memory files and plan entries written before 2026-08-20
used the old names, and an agent trusting them will conclude the code is
missing rather than renamed.

| Written before Phase 38e | Today |
| --- | --- |
| `demos/01-dictionary/backend/trading-partner-service/` | `demos/01-dictionary/backend/organizations-service/` |
| `BUSINESS_RULES-TRADING-PARTNER.md` | `demos/01-dictionary/BUSINESS_RULES-ORGANIZATIONS.md` |
| `TradingPartnersPanel.vue` | `frontend/refdata/src/components/OrganizationsPanel.vue` |

**Two separate moves, easily conflated.** Phase 36 (`823993b`) moved the panel
out of the Admin UI into the `refdata` app as part of the Tech Lab Operator
rebrand, keeping the `TradingPartners` name; Phase 38e (`6c1bfe6`) did the
rename, of both the service and the panel, and left the app placement alone.
A memory or plan entry naming `TradingPartnersPanel.vue` *in the Admin UI* is
therefore pre-Phase-36, not merely pre-38e.

**"Trading partner" survives as domain vocabulary, and that is not stale.**
It is still the collective term for Shippers and Transporters (see
[[linebooker_trading_partners_term_and_fleet_cardinality]]), and the business
rules are still numbered `BR-TP*`. Only the *service*, *file*, and *component*
names changed — don't "fix" the prose term or renumber the rules.

Related: [[linebooker_trading_partner_phase_v1_scope]],
[[phase36_tech_lab_operator_rebrand]], [[phase38b_transporter_vetting]],
[[phase38_document_object_store]].
