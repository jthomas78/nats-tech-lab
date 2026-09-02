package healthnats

import (
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

/*
	The receiving end of the frontend health plane (BR-AS61, Phase 15
	decision 14). One subscription, one token wide in the plugin-id position,
	and no work at rest: the registry listens and never asks.

	This file is transport and nothing else. It decodes, it reads the plugin
	id off the SUBJECT rather than trusting the body, and it hands both to the
	inbox. Every decision about what to believe — the two ids agreeing, the
	closed vocabulary, the freshness lease — is in domain.HealthInbox, so it
	is specced without a broker.
*/

// FrontendReports is the one thing this subscriber needs from the checker.
type FrontendReports interface {
	AcceptFrontendReport(subjectPluginID string, report mferegistry.HealthReport, at time.Time) bool
}

// SubscribeFrontend starts the registry's single health subscription.
func SubscribeFrontend(nc *nats.Conn, reports FrontendReports, log *slog.Logger) (*nats.Subscription, error) {
	return nc.Subscribe(mferegistry.FrontendHealthAll, func(msg *nats.Msg) {
		id := frontendPluginID(msg.Subject)
		if id == "" {
			return
		}
		var report mferegistry.HealthReport
		if json.Unmarshal(msg.Data, &report) != nil {
			// A report nobody can read is a report that did not arrive, and
			// the freshness window already says what that means. There is no
			// reply subject on a notification, so there is nobody to tell.
			log.Warn("registry health: undecodable frontend report", "plugin", id)
			return
		}
		if !reports.AcceptFrontendReport(id, report, time.Now().UTC()) {
			log.Warn("registry health: refused frontend report", "plugin", id)
		}
	})
}

// frontendPluginID reads the id out of the subject by POSITION. The subject
// has fixed arity, so this is a length check and an index — never a split on
// the first or last dot, which a plugin id containing one would defeat (ids
// cannot contain one, and this does not depend on that staying true).
func frontendPluginID(subject string) string {
	tokens := strings.Split(subject, ".")
	if len(tokens) != 6 {
		return ""
	}
	return tokens[4]
}
