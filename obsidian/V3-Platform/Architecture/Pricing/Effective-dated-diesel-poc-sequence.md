# How a diesel price update propagates into RateSheet pricing (pricing-service, as-built)

*Phase 25i — the Go pricing-service implementation.* Companion to [Effective-dated-diesel-sequence.md](Effective-dated-diesel-sequence.md), which covers the equivalent Linebooker source flow and cross-references it against this one. That document's Scope section carries a separate sequence diagram for Linebooker's own load-publishing branch (`Bidding` / `Allocation` / `Fixed Rate`) — this page is only the POC side.

```mermaid
sequenceDiagram
    autonumber
    participant UI as Sea Freight UI (Pricing tab)
    participant REST as pricing-service REST / browserrpc
    participant CMD as ApplyDieselOverlay (application/commands)
    participant DOM as domain.RateSheetVersion (rate_sheet.go)
    participant PG as Postgres (schema pricing)

    Note over UI,PG: PHASE A — index a diesel price (BR-P18)
    UI->>REST: POST /diesel-prices {activeDate, coastalCents, inlandCents}
    REST->>PG: IndexDieselPrice — UPSERT (context, active_date)
    PG-->>REST: ok
    REST-->>UI: 200 OK

    Note over UI,PG: later — an operator applies that indexed price as an overlay on a published RateSheet
    UI->>REST: POST /rate-sheets/{name}/diesel-overlay {activeDate}
    REST->>CMD: ApplyDieselOverlay(context, name, activeDate)
    CMD->>PG: ListDieselPrices(context)
    PG-->>CMD: []DieselPrice
    CMD->>PG: ActiveVersion(context, name)
    PG-->>CMD: RateSheetVersion{Entries, Overlays}
    CMD->>DOM: AppendDieselOverlayFromIndex(version, prices, activeDate)
    DOM->>DOM: DieselPriceOn(prices, activeDate) — greatest ActiveDate ≤ activeDate (BR-P18)

    alt no price covers activeDate
        DOM-->>CMD: ErrNoDieselPrice (BR-P21 — fail-closed, no silent zero/base fallback)
        CMD-->>REST: error
        REST-->>UI: 404 ErrNoDieselPrice
    else price found
        DOM->>DOM: close every currently open-ended overlay: EndDate = activeDate
        loop each entry with InitialDieselCents > 0 (BR-P19)
            DOM->>DOM: AdjustedRate = base + base·(pct/100)·((current−initial)/initial) (BR-P20)
            DOM->>DOM: append DieselOverlay{MinorVersion+1, RouteKey, VehicleType, StartDate=activeDate, EndDate=nil, CentAdjustedRate}
        end
        Note right of DOM: entry has InitialDieselCents == 0 → skipped, no overlay appended (BR-P24 zero-guard)
        DOM-->>CMD: new RateSheetVersion{MinorVersion+1, Overlays}
        CMD->>PG: PersistDieselOverlay — txn: bump minor_version, close prior overlays' end_date, insert new overlay rows
        PG-->>CMD: ok
        CMD-->>REST: version (major.minor)
        REST-->>UI: 200 {major}.{minor}
    end

    Note over UI,PG: independently — resolving what a load pays, for any effective date
    Note over DOM: RateForLoad(routeKey, vehicleType, effectiveDate, addressCount) — domain-only today: no REST/browserrpc endpoint or command calls this yet (no Load aggregate in the demo). Exercised only by the Ginkgo specs in rate_sheet_diesel_test.go.
    DOM->>DOM: find the entry for routeKey × vehicleType (else ErrEntryNotFound)
    DOM->>DOM: find the overlay window with StartDate ≤ effectiveDate < EndDate (or EndDate = nil)
    alt window found
        DOM->>DOM: adjusted rate + drop surcharge (BR-P22)
    else effectiveDate precedes every overlay window
        DOM->>DOM: fall back to the authored CentBaseRate + drop surcharge (BR-P23)
    end
```

*(Previously rendered as a Draw.io diagram — [pricing-diesel-overlay.drawio](pricing-diesel-overlay.drawio) / [images/diesel-overlay-sequence.png](images/diesel-overlay-sequence.png) remain on disk for reference; the Mermaid version above is now the source of truth for this page since it edits directly in Markdown.)*

## Three phases

1. **Index a diesel price** (BR-P18) — a plain upsert into `pricing.diesel_prices`, keyed by `(context, active_date)`.
2. **Apply the overlay** — an explicit, per-rate-sheet operator action (`POST .../diesel-overlay`). Resolves the diesel price in effect on the requested date (BR-P18), fails closed if none exists (BR-P21), closes every open overlay window, computes the adjusted rate for each entry that has an authored diesel baseline (BR-P20), skips entries that don't (BR-P24), and persists the result transactionally.
3. **Resolve a load's price** (`RateForLoad`) — shown for completeness, but flagged in the diagram as **domain-only today**: nothing in pricing-service's REST, browserrpc, or command layers calls it yet, because the demo has no Load aggregate. It's exercised only by the Ginkgo specs in `rate_sheet_diesel_test.go`.
