package domain

/*
	The reset predicate (BR-AS73, Phase 15 decision 13).

	Firing a notice on every start-up would be simpler and self-healing by
	construction — the registry would never have to know WHY it was empty. It
	is rejected on cost: a rolling restart would set off a full re-announce
	storm, and at five hundred plugins that is five hundred signature verifies
	and five hundred Postgres writes that a jitter window can only spread out,
	not avoid.

	The cost accepted in exchange is stated plainly in the rule: the registry
	must now tell "I restarted" apart from "I lost my catalogue", and IF THAT
	CHECK IS EVER WRONG THE HOLE THIS PHASE EXISTS TO CLOSE REOPENS SILENTLY.
	That is why the predicate is a rule with its own specs and not an
	implementation detail, and why it is a pure function of two numbers here
	rather than a condition spelled out inside a start-up sequence.
*/

// CatalogueLost reports whether the source of truth has gone BACKWARDS from a
// revision this deployment is known to have served.
//
// `witnessed` is the highest revision the deployment has evidence of — in
// practice the revision in the read cache, which is written through from the
// same call that commits Postgres and therefore never runs ahead of it under
// normal operation. `current` is what the source of truth says now.
//
// So `witnessed > current` is not an ordinary state. It means Postgres lost
// content the cache still remembers, which is precisely the case start-up
// announcement cannot cover: the plugins never restarted, so they will never
// announce again on their own.
//
// The two cases that are NOT a loss are the ones this predicate exists to
// stay quiet about:
//
//   - A plain restart with the catalogue intact reads equal revisions.
//     Nothing fires, which is decision 13's whole point.
//   - A first boot, or a `docker compose down -v`, has nothing witnessed at
//     all. Nothing fires, and nothing needs to: the plugins were torn down
//     with it and announce at start-up.
//
// Losing the cache while the catalogue survives is also not a loss — there is
// simply no witness, and inventing one from an empty cache would fire a
// notice every time the KV bucket was recreated.
func CatalogueLost(witnessed, current int64) bool {
	return witnessed > NoRevision && current < witnessed
}
