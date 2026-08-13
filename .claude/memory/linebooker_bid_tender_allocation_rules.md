---
name: linebooker-bid-tender-allocation-rules
description: V2's Bid and Tender are two separate, unconnected fulfillment tracks (BidEntity has no FK to TenderEntity) — winner selection is a matrix of competitive-lowest-bid / direct-allocation / contracted-rate / RFQ-tender, crossed with auto-accept vs manual-accept; confirms Admin UI's "Submission Tenders"/"Bidding Tenders" tabs map to exactly these two tracks
metadata:
  type: project
---

Investigated 2026-08-13 in `/Users/jeremy/dev/github/linebooker/linebooker/backend/src/main/java/com/linebooker/console/`, answering "what are the rules for allocating a winning transporter" against the [[linebooker_platform_marketplace_tenant_diagram]] which had drawn `Tenders`/`Bids` as siblings under a `Marketplace` generalization.

**Key correction to the diagram's assumption:** `BidEntity` has **no FK to `TenderEntity`** — its `@ManyToOne`s are `TransporterProfileEntity`, `LoadRequestEntity`, `UserEntity`. `TenderEntity`/`TenderSubmissionEntity` is a wholly separate RFQ-style subsystem (document/online submissions, ranking tiers, own `submissionDeadline`, own scheduler endpoint `/api/scheduler/tenders/recalculate-status`) that never touches `BidEntity`. These are two distinct fulfillment tracks in V2, not one shared aggregate with two subtypes — the diagram's generalization arrow doesn't match the code.

**Track 1 — spot/competitive bidding, attached to `LoadRequestEntity` (not Tender):**
- Live reverse auction: `biddingIsValid` (`LoadRequestEntityServiceImpl.java:~3010-3025`) rejects any new bid that doesn't beat the current lowest bid.
- Bid-sniping guard: a bid placed inside `bidExpiryWindow` of `LoadRequestEntity.expiryDate` pushes the deadline forward (`bidOnLoad`, `LoadRequestEntityServiceImpl.java:2531-2544`).
- At expiry, an **external scheduler webhook** `POST /api/scheduler/loads/expire` (`SchedulerServiceResource.java:110-121`) → `processExpiredLoad`/`processLoad` (`:2361-2456`) **automatically** picks the lowest bid as candidate winner (`allBidsByLoadRequestId.get(0)`), sets status `PENDING_ACCEPTANCE`. No in-process `@Scheduled` cron exists near bidding — closing is driven by an outside caller hitting this endpoint.
- Final booking then needs a **manual Customer action** (`accept-bid` → `acceptBidForUser`, `:1413-1435`) — *except* `BOOK_NOW`/`TAKE_ALLOCATED` bids and non-cash customers get **auto-accepted immediately** (`autoAcceptBid`, `:2889`). Cash customers get a configurable `customerProfileEntity.getLoadAcceptWindow()` to manually accept/decline instead.
- `AllocationType.CONTRACTED` (a field on `BidEntity`, not a separate entity) skips the price-competition check and pulls rate from a pre-agreed `RateSheetVersionEntryEntity` instead of racing other bids.
- `LoadType.ALLOCATED` loads bypass bidding entirely — a specific pre-selected Transporter is assigned directly (`takeAllocatedLoad`, `:2847`, via `LoadAllocationEntity`), then auto-accepted.

**Track 2 — Tender/RFQ:** closed on its own `submissionDeadline` via `updateTenderStatus`; ranking-tier based; no code path connects it to `BidEntity` or to the lowest-bid mechanism above.

**Confirmed against the actual Admin UI nav labels "Submission Tenders" / "Bidding Tenders":** these two tabs are exactly these two tracks, but asymmetrically named. `webui/frontend/src/controls/menu/MenuAppbar.js:69-81` defines `{path:'/tenders', display:'Submission Tenders'}` → `services/Tenders.js` → `/api/tender-entities` + `/api/tender-submission-entities` (Track 2, genuinely `TenderEntity`). `{path:'/bidding-tenders', display:'Bidding Tenders'}` → routes to `pages/contracts/Contracts.js`, a thin wrapper rendering `<Loads displayContractLoads />` filtered by `category: LoadCategoryType.CONTRACT` (`pages/loads/Loads.js:1529`) → `services/Loads.js` → `/api/load-request-entities` + `/api/bid-on-load` (Track 1). `LoadCategoryType.CONTRACT.getReadable()` literally returns the string `"Bidding Tender"` — it's a display label for a load category, not a second Tender subsystem. So "Bidding Tender" borrows the word "tender" for what's really a subset of Loads; only "Submission Tenders" names its actual backend entity.

**Enum reference:** `BidStatusType`: `LOWEST, OUTBID, ACCEPTED, DECLINED, BOOK_NOW, TAKE_ALLOCATED` (only the last four are ever actually set — `LOWEST`/`OUTBID` are dead code). `TenderStatus`: `ACTIVE, UPCOMING, CLOSED, ARCHIVED, DELETED, DRAFT`. `AllocationType`: `CONTRACTED, NON_CONTRACTED`. `PricingType`: `BIDDING, ALLOCATED, FIXED_RATE`. `LoadAllocationStatus`: `AVAILABLE, NOT_TAKEN, TAKEN, EXPIRED, DECLINED, FAILED_PLANNED`. `BidType`: `NORMAL, SUBSIDIZED, BOOK_NOW, BID_ALLOCATED_TAKEN, REJECT_ALLOCATION`.

**How to apply:** when modeling the V3 marketplace (per [[linebooker_platform_marketplace_tenant_diagram]]), decide explicitly whether Bid and Tender stay two separate tracks (matches V2, less unification work) or get unified under one aggregate (matches the diagram's drawn generalization, but is new design, not a port). Per [[design_discussion_vs_implementation_signal]] this is discussion-stage — nothing to build yet.
