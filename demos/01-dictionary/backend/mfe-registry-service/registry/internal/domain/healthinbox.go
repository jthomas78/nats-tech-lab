package domain

import (
	"time"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

/*
	Frontend health ARRIVES here (BR-AS61, Phase 15 decision 14). The registry
	does not dial a plugin, does not hold a map of origins, and does not ask:
	a plugin reports itself, and this is the box those reports land in.

	It is the mirror image of HealthWorker, which still schedules the backend
	readiness probes BR-AS62 owns. That one decides WHEN to look; this one
	only decides what to believe about something that already spoke. Both are
	stepped with a `now` a caller supplies and neither owns a clock.

	The freshness window is the whole detection mechanism here, not a backstop
	as it was under polling. Nothing else notices a dead plugin.
*/

// HealthInbox keeps the last report per plugin.
type HealthInbox struct {
	reports map[string]HealthSignal
}

func NewHealthInbox() *HealthInbox {
	return &HealthInbox{reports: map[string]HealthSignal{}}
}

// Accept records one report and says whether it was believed.
//
// `subjectPluginID` is the token the message arrived on and `report` is what
// the body claimed. They must agree: a plugin's grant is one subject, and a
// body that named a different plugin would let the holder of one credential
// speak for another. The refusal is silent to the sender by design — this is
// a notification, and there is nobody to answer.
//
// `at` is the receiver's own clock. A report timestamped in the future is
// clamped to it rather than refused: a plugin with a fast clock is a
// deployment problem, but a plugin that could stamp its own report an hour
// ahead would be permanently fresh and could never go absent.
func (i *HealthInbox) Accept(subjectPluginID string, report mferegistry.HealthReport, at time.Time) bool {
	if subjectPluginID == "" || report.PluginID != subjectPluginID {
		return false
	}
	if !mferegistry.ValidHealthReportState(report.State) {
		return false
	}
	if report.Cause != "" && !mferegistry.ValidHealthCause(report.Cause) {
		return false
	}

	observed := time.UnixMilli(report.At).UTC()
	if report.At == 0 || observed.After(at) {
		observed = at
	}

	next := HealthSignal{State: HealthHealthy, LastCheckAt: observed}
	if report.State == mferegistry.HealthReportUnhealthy {
		next = HealthSignal{State: HealthUnavailable, Cause: report.Cause, LastCheckAt: observed}
	}

	// A redelivered or out-of-order report must not refresh the lease. Merge
	// is the same rule the browser plane uses, and for the same reason: a
	// plugin that died would otherwise stay green for as long as something
	// kept resending its last good reading (BR-AS64).
	if previous, ok := i.reports[subjectPluginID]; ok {
		if !next.LastCheckAt.After(previous.LastCheckAt) {
			return false
		}
	}
	i.reports[subjectPluginID] = next
	return true
}

// Signal is one plugin's frontend health as of now.
//
// A plugin nothing has been heard from is UNKNOWN, not absent: "we have
// never heard" and "it went quiet" are different facts, and collapsing them
// would invent a past that never happened. A plugin that spoke and then
// stopped is stale, with the cause that says so.
func (i *HealthInbox) Signal(pluginID string, now time.Time) HealthSignal {
	report, ok := i.reports[pluginID]
	if !ok {
		return HealthSignal{State: HealthUnknown}
	}
	if now.Sub(report.LastCheckAt) > mferegistry.HealthFrontendFreshness {
		// Absent and unhealthy are separate causes and are shown
		// differently. "Nothing was heard" is the common case — a restart, a
		// dropped bus connection — and must not be mistaken for the rare one,
		// where a plugin looked at itself and said it was broken.
		return HealthSignal{State: HealthStale, Cause: mferegistry.HealthCauseAbsent, LastCheckAt: report.LastCheckAt}
	}
	return report
}

// Forget drops reports for plugins that are no longer in the catalogue, so a
// removed plugin's last reading cannot outlive it.
func (i *HealthInbox) Forget(keep map[string]bool) {
	for id := range i.reports {
		if !keep[id] {
			delete(i.reports, id)
		}
	}
}
