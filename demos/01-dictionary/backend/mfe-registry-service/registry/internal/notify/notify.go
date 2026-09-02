// Package notify is this service's subject-builder layer: the one file in
// mfe-registry-service allowed to name a notify.* subject.
//
// One shape, four tokens after `notify.`, always in the platform context —
// the registry administers the platform's frontend catalog rather than
// operating inside a business context, so `_platform` is a literal here and
// not a parameter. The tokens are handed to the Notifier rather than parsed
// back out of the name, because whoever builds a subject knows its tokens and
// a positional reader only guesses at them.
//
// Split out of application/service.go when the registry became its own
// service: shipping-service's notify-coverage gate asks every service in the
// tree for a file that names its subjects and does not publish them, and the
// registry was the last one naming its subject in the same function that
// published it.
package notify

import (
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
)

// Changed is the change notification the shell and the admin surface watch.
// It is a hint and never a payload to install from (decision 55): a shell
// that receives it re-reads, and the read is what is authoritative.
func Changed() natsnotify.Subject {
	return natsnotify.Subject{
		Name: "notify._platform.mfe-registry.frontend-plugins.changed",
		Tokens: natsnotify.Tokens{
			Context: "_platform",
			Service: "registry",
			Entity:  "frontend-plugins",
			Action:  "changed",
		},
	}
}

// HealthChanged is the health plane's pushed snapshot (BR-AS64/AS65). The
// central checker publishes one timestamped observation after each completed
// probe pass. It remains on its own action token and carries no catalogue
// revision, entries, signed bytes or approval state.
func HealthChanged() natsnotify.Subject {
	return natsnotify.Subject{
		Name: "notify._platform.mfe-registry.frontend-plugins.health",
		Tokens: natsnotify.Tokens{
			Context: "_platform",
			Service: "registry",
			Entity:  "frontend-plugins",
			Action:  "health",
		},
	}
}

// EntriesReset is the catalogue-reset notice (BR-AS73). Publisher-facing, so
// the entity is `entries` and not the browser's `frontend-plugins` view: the
// audience is the processes that announce, not the ones that read.
//
// It is a statement of fact and never a command — deliberately not `cmd.*`,
// which would claim the registry has authority over a plugin's process.
func EntriesReset() natsnotify.Subject {
	return natsnotify.Subject{
		Name: mferegistry.EntriesReset,
		Tokens: natsnotify.Tokens{
			Context: "_platform",
			Service: "registry",
			Entity:  "entries",
			Action:  "reset",
		},
	}
}
