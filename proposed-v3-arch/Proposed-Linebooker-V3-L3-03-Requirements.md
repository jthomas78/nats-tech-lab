# Proposed Linebooker V3 Architecture - L3-03 Requirements

This register maintains the requirements represented by
`LB-V3-L3-03 - Application / MFE`.

`LB-V3-L3-03` answers one question: **how is the browser application composed
from one permanent shell and many independently deployed feature plugins - what
does the shell own, how is a plugin discovered and trusted, when is its code
loaded, what happens when one fails, and where is it served from?** It zooms
into `LB-V3-L2-01` requirement `L2-D02`, which assigns shell composition, plugin
discovery, routes, contributions and frontend deployment to this document. It
refines `L2-002` (how consumers enter the platform), `L2-016` (the repeatable
regional-cell model) and `L2-027` (where shell bundles and the plugin registry
are served).

It also receives the **theme and label consumption** slice from
`LB-V3-L3-07 Data Architecture`. `LB-V3-L3-07` holds the *storing and serving*
of localisation, translation, theme and configuration values; this document
holds the *consuming and rendering* of them inside the shell.

`LB-V3-L3-03` is a **mixed-maturity document**. Most of what it draws is built
and running in the Dictionary POC today: the shell kernel, the curated plugin
registry served over messaging, the three publisher trust gates, the seven
observable plugin states, the lazy remote loader and per-contribution failure
isolation. Every element that is *not* built is a **directional V3 position as
at 2026-09-04**, carries a per-tile status, and is not a settled decision.
Content security policy, artefact provenance, per-cell asset delivery and the
publisher key ownership in particular have no agreed values and no V3 decision
of record.

The document is drawn as two A3 landscape sheets:

| Sheet | Question |
|---|---|
| 1 - Ownership, the Registry and Trust | Who owns which part of the application, what is in the approved plugin list, and what must an entry pass before it counts? |
| 2 - Loading, Containment and What a Plugin is Given | When is a plugin's code fetched, what states can an operator see, what happens when one fails, and what is a running plugin handed? |

## Status vocabulary

- **Included** - visibly represented in the current L3-03 document.
- **Derived** - intentionally delegated to another concern view or to an L4
  detailed design.
- **Open** - requires business, legal, regulatory or technical confirmation.

## Maturity vocabulary

Used by the drawing's tile treatment. This describes the element, not the
document.

- **Built** - running in the Dictionary POC today. Solid tile.
- **Proposed** - a directional V3 position with no implementation and no V3 ADR,
  or an ADR that has not been applied. Dashed grey tile.
- **Open** - a position that cannot be taken without a named owner's
  confirmation. Dashed amber tile, with the owner named on the tile.

## L3-03 requirements

| ID | Status | Requirement | L3-03 representation |
|---|---|---|---|
| L3-03-001 | Included | Show exactly one permanent application frame, owned by the shell and never replaced by a plugin. | Sheet 1, `Application frame` tile. |
| L3-03-002 | Included | Show the shell installing the global providers - routing, shared state, theme and notifications - once for everyone. | Sheet 1, `Global providers` tile. |
| L3-03-003 | Included | State that a plugin is discovered only from a platform-curated list, never by self-registration. | Sheet 1, `Plugin discovery` tile. |
| L3-03-004 | Included | State that the host screen owns where a contribution is placed, and a plugin cannot attach itself to arbitrary page elements. | Sheet 1, `Contribution slots` tile; Sheet 2, `A slot, not the page` tile. |
| L3-03-005 | Included | Show feature screens and feature state as owned by the plugin. | Sheet 1, `Feature code` tile. |
| L3-03-006 | Included | Show the registry record as one entry per plugin, carrying identity, version, location and contributions. | Sheet 1, `Registry record` tile. |
| L3-03-007 | Included | Show contribution kinds - routes, navigation and named slots - as separately checked, not one untyped bag. | Sheet 1, `Contribution kinds` tile. |
| L3-03-008 | Included | Show a declared compatibility range, and refusal of anything outside it before any code runs. | Sheet 1, `Compatibility range` tile; Sheet 2, `Refused` tile. |
| L3-03-009 | Included | Show the registry as served at run time by the platform, not shipped as a file inside the shell bundle. | Sheet 1, `Served at run time` tile. |
| L3-03-010 | Included | Show three independent gates - ownership, signature and release number - applied in that order to an entry no operator typed. | Sheet 1, gates group, tiles 1 to 3. |
| L3-03-011 | Included | Show the ownership gate as first and on its own cause, so a valid signature over another plugin's name is a distinguishable failure. | Sheet 1, `1 - Ownership` tile. |
| L3-03-012 | Included | Show the signature gate as verifying against a key that is trusted and enabled at that moment, so a revoked key fails. | Sheet 1, `2 - Signature` tile. |
| L3-03-013 | Included | Show the release gate as refusing a lower number and treating an equal number as a safe repeat. | Sheet 1, `3 - Release number` tile. |
| L3-03-014 | Included | Show an approved-location list as the boundary on where executable code may be fetched from. | Sheet 1, `Where code may come from` tile. |
| L3-03-015 | Included | State plainly that a signature proves who published a description and nothing about what the code does. | Sheet 1, red panel. |
| L3-03-016 | Included | Show permission as read from the person's sign-in token, as one source for every plugin. | Sheet 1, `Permission` tile. |
| L3-03-017 | Included | State that hiding a screen is never a substitute for the service refusing the request. | Sheet 1, `Hidden is not blocked` tile. |
| L3-03-018 | Included | Show each plugin keeping its own separate platform connection, and refuse the merging of rights for convenience. | Sheet 1, `Separate connections` tile; Sheet 2, `Its own connection` tile. |
| L3-03-019 | Included | State that the registry, the loader and the diagnostics never record tokens, credentials or message contents. | Sheet 1, `Nothing secret is logged` tile. |
| L3-03-020 | Included | Show the governing rule that the shell knows a plugin's description before it runs any of that plugin's code. | Sheet 2, first group title and its five steps. |
| L3-03-021 | Included | Show boot, registry read, indexing, first use and activation as five ordered steps. | Sheet 2, steps 1 to 5. |
| L3-03-022 | Included | Show menus and routes as live before any feature code has been fetched. | Sheet 2, `3 Index` tile and `Available` tile. |
| L3-03-023 | Included | Show code as fetched only on first use - a route match or an active slot assignment. | Sheet 2, `4 First use` tile. |
| L3-03-024 | Included | Show activation happening once per plugin identity and version. | Sheet 2, `5 Activate` tile. |
| L3-03-025 | Included | Show every state an operator can observe, and keep refused and broken as different states. | Sheet 2, states group, five tiles carrying all seven names. |
| L3-03-026 | Included | Show the shell as still usable when the plugin list cannot be read. | Sheet 2, `The registry is unreadable` tile. |
| L3-03-027 | Included | Show one invalid entry as dropped without stopping its neighbours. | Sheet 2, `One entry is bad` tile. |
| L3-03-028 | Included | Show a failed or slow code fetch as a local retry that leaves the rest of the application working. | Sheet 2, `A remote will not load` tile. |
| L3-03-029 | Included | Show a failed start as rolled back, leaving no half-installed menus, routes or slots behind. | Sheet 2, `A plugin throws` tile. |
| L3-03-030 | Included | Show language as belonging to the person and chosen once for the whole application. | Sheet 2, `Language from the person` tile. |
| L3-03-031 | Included | Show labels and theme values as supplied by reference data and point back to `LB-V3-L3-07` for their storage and distribution. | Sheet 2, `Labels and theme` tile and its footer reference. |
| L3-03-032 | Included | State that this is not a browser sandbox, and that plugins are first-party code the platform already trusts. | Sheet 2, `This is not a browser sandbox` tile. |
| L3-03-033 | Included | State that a plugin list change during a session adds a new plugin at most, and anything else asks the person to reload. | Sheet 2, `A registry change is not applied mid-session` tile. |
| L3-03-034 | Included | State that no shared connection broker is proposed, because one could hand a plugin wider rights than its own. | Sheet 2, `No shared connection broker` tile. |
| L3-03-035 | Included | Exclude all credential material, tokens, keys, connection strings, host names, endpoint literals and schema listings from the drawing. | Both sheets; no tile names a subject, host, key or credential. |
| L3-03-036 | Included | Mark every element with its maturity, so a reader can tell a built element from a directional V3 position. | Both sheets, per-tile status line and legend. |

## Derived requirements

| ID | Status | Requirement | Delegated to |
|---|---|---|---|
| L3-03-037 | Derived | The plugin description's exact fields, contribution shapes and version rules. | An L4 detailed design under `LB-V3-L3-03`, not yet numbered. |
| L3-03-038 | Derived | The messaging subjects, permissions and request shapes the shell uses to fetch the plugin list. | `LB-V3-L3-05 Messaging and NATS`, and an L4 under it. |
| L3-03-039 | Derived | The sign-in token's claim shape and its issue, refresh and revoke lifecycle. | `LB-V3-L3-04 Security, Identity and Access`. |
| L3-03-040 | Derived | Where theme, label, localisation and configuration values are stored, versioned and distributed from. | `LB-V3-L3-07 Data Architecture`, received as its theme and label slice. |
| L3-03-041 | Derived | Per-cell web delivery, caching and asset immutability for the shell bundle and the plugin list. | `LB-V3-L3-08 Multi-Region`. |
| L3-03-042 | Derived | Content security policy, artefact provenance and integrity checking in production delivery. | `LB-V3-L3-06 Deployment and Environments`. |
| L3-03-043 | Derived | The publisher key store, its rotation and its revocation mechanics. | `LB-V3-L3-04 Security, Identity and Access`, and an L4 under it. |
| L3-03-044 | Derived | The observable signals, counters and alerts for plugin load and activation health. | `LB-V3-L3-09 Observability`. |
| L3-03-045 | Derived | The mobile driver application's composition, which is not a shell-and-plugin model. | `LB-V3-L3-03` is browser scope only; the mobile channel stays at `LB-V3-L2-01`. |

## Open requirements

| ID | Status | Requirement | Owner needed |
|---|---|---|---|
| L3-03-O01 | Open | Who owns and holds the publisher signing keys in V3, and who may enable or revoke one. | Platform engineering and security owner. |
| L3-03-O02 | Open | Whether a third party outside Linebooker may ever publish a plugin, and under what commercial and legal terms. | Business owner and legal. |
| L3-03-O03 | Open | The supported shell compatibility window - how long an older plugin keeps working after the shell moves on. | Platform engineering owner. |
| L3-03-O04 | Open | Whether a plugin failure is visible to the person using the application, to an operator only, or to both. | Product owner. |
| L3-03-O05 | Open | Whether the plugin list is per tenant, per region or global, and who may vary it. | Business owner and platform engineering. |
| L3-03-O06 | Open | Whether an operator may disable a plugin in a live session, and what the person then sees. | Product owner and platform engineering. |
| L3-03-O07 | Open | The retention period for plugin load, activation and failure records. | Data owner, with `LB-V3-L3-07`. |
| L3-03-O08 | Open | Whether accessibility conformance is asserted by the shell alone or must be proven per plugin. | Product owner and legal. |
| L3-03-O09 | Open | Whether a plugin may ever be served from a location Linebooker does not operate. | Security owner. |
