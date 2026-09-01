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

import "github.com/jthomas78/nats-tech-lab/shared/natsnotify"

// Changed is the change notification the shell and the admin surface watch.
// It is a hint and never a payload to install from (decision 55): a shell
// that receives it re-reads, and the read is what is authoritative.
func Changed() natsnotify.Subject {
	return natsnotify.Subject{
		Name: "notify._platform.registry.frontend-plugins.changed",
		Tokens: natsnotify.Tokens{
			Context: "_platform",
			Service: "registry",
			Entity:  "frontend-plugins",
			Action:  "changed",
		},
	}
}

// HealthChanged is the health plane's hint (BR-AS64/AS65). Same shape and the
// same promise as Changed, on its own action token: a shell that hears it
// re-reads the health subject, and the read is what is authoritative.
//
// The hint carries no state at all. A delivery proves that a message arrived,
// never that a service was alive, so a payload here would be an observation
// that nothing re-checked.
func HealthChanged() natsnotify.Subject {
	return natsnotify.Subject{
		Name: "notify._platform.registry.frontend-plugins.health",
		Tokens: natsnotify.Tokens{
			Context: "_platform",
			Service: "registry",
			Entity:  "frontend-plugins",
			Action:  "health",
		},
	}
}
