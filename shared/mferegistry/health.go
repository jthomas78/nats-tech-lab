package mferegistry

import "time"

/*
	Frontend health is PUSHED by the plugin and never asked for (BR-AS61,
	Phase 15 decision 14). This file is the whole contract between the two
	sides: the subject a plugin publishes on, the subject the registry
	subscribes on, the cadence both must agree about, and the shape on the
	wire. It lives here for the same reason the browser surface does — two
	owners that would otherwise restate the strings and drift.

	The direction is the design. Nobody outside a plugin knows how to dial
	it, so nothing has to be told: the subject is derived from the plugin id
	that is already inside the signed catalogue entry, never from a
	deployment-supplied map of origins and never from anything a manifest
	says.
*/

// FrontendHealth names the subject one plugin reports itself on. The id is
// the plugin id from the signed entry, which is already constrained to one
// subject token — a dot would split the token and address another plugin.
func FrontendHealth(pluginID string) string {
	return "notify._platform.health.frontend." + pluginID + ".v1"
}

// FrontendHealthAll is the registry's single subscription. One token wide,
// in the plugin-id position: the registry listens for every plugin at once
// and does no work at rest.
const FrontendHealthAll = "notify._platform.health.frontend.*.v1"

// FrontendHealthCensus is reserved and nothing publishes on it yet.
//
// A census — the registry asking every plugin at once on start-up, on
// reconnect, or after a catalogue reset — was considered and DEFERRED, not
// rejected: the heartbeat already covers every trigger a census would fire
// on, so it buys latency rather than correctness (it turns a freshness
// window of unknown into a sub-second one, and unknown is a true statement).
// The subject is named and granted now anyway, because the grant is the one
// part that is expensive to add later: a bootstrap-operator.sh edit and a
// `docker compose down -v` reseed. Naming it costs one line.
const FrontendHealthCensus = "rpc._platform.health.frontend.census.v1"

// The cadence, shared by both sides. These two are ONE pair and are raised
// together or not at all: a heartbeat at or above the freshness window makes
// every healthy plugin flicker stale.
//
// Fifteen seconds is exactly three missed beats, the same margin the polling
// model it replaces had. A longer heartbeat is where the real saving is, but
// freshness must move with it, and that trades detection speed for volume
// this lab has no reason to buy yet.
const (
	// HealthHeartbeat is how often a plugin reports regardless of change, so
	// that a silent plugin is a fact and not an inference.
	HealthHeartbeat = 5 * time.Second
	// HealthSelfCheckTimeout bounds the plugin's own loopback GET. It must
	// expire strictly before the heartbeat, so two checks are never in
	// flight for one plugin.
	HealthSelfCheckTimeout = 2 * time.Second
	// HealthFrontendFreshness is the registry's detection mechanism, not a
	// backstop: no report inside this window is the only way a dead plugin
	// is noticed.
	HealthFrontendFreshness = 15 * time.Second
	// HealthFailureThreshold is decided by the plugin about itself. Two, not
	// one: a single failed check is as likely to be a dropped packet as a
	// dead server, and this decoration sits next to a plugin the user is
	// looking at. The cost of the plugin owning it is named and accepted —
	// one number that lived in one service now lives in every plugin image,
	// and changing it is a fleet redeploy.
	HealthFailureThreshold = 2
)

// The two states a plugin may report ABOUT ITSELF. There is no third: a
// plugin that says nothing is not reporting a state, it is absent, and only
// the registry can observe that.
const (
	HealthReportHealthy   = "healthy"
	HealthReportUnhealthy = "unhealthy"
)

// The closed cause vocabulary a plugin may attach to an unhealthy report.
// Closed because a cause reaches a browser (BR-AS60): text the local server
// chose, or the address that was dialled, does not belong there.
const (
	HealthCauseTimeout         = "timeout"
	HealthCauseUnreachable     = "unreachable"
	HealthCauseHTTPStatus      = "http-status"
	HealthCauseInvalidResponse = "invalid-response"
	// HealthCauseAbsent is never sent by a plugin. The registry attaches it
	// when no report arrived inside the freshness window. It is a separate
	// cause from unhealthy on purpose, and the two are shown differently:
	// "nothing was heard" and "a plugin said so about itself" are different
	// facts and the common case must not be mistaken for the rare one.
	HealthCauseAbsent = "absent"
)

// HealthReport is one plugin's statement about itself, on the wire.
//
// PluginID is carried in the body as well as the subject so the registry can
// refuse a report whose body disagrees with the token it arrived on — a
// plugin granted one subject cannot then speak for another.
type HealthReport struct {
	PluginID string `json:"pluginId"`
	State    string `json:"state"`
	Cause    string `json:"cause,omitempty"`
	// At is milliseconds since the epoch, so a receiver can drop a
	// redelivered report without refreshing its freshness lease.
	At int64 `json:"at"`
}

// ValidHealthReportState reports whether a state is one a plugin may claim.
func ValidHealthReportState(state string) bool {
	return state == HealthReportHealthy || state == HealthReportUnhealthy
}

// ValidHealthCause reports whether a cause is in the closed vocabulary a
// plugin may send. Absent is excluded: only the registry may conclude it.
func ValidHealthCause(cause string) bool {
	switch cause {
	case HealthCauseTimeout, HealthCauseUnreachable, HealthCauseHTTPStatus, HealthCauseInvalidResponse:
		return true
	}
	return false
}
