package mferegistry

import "time"

/*
	The catalogue-reset notice (BR-AS73, Phase 15 decisions 6, 7, 8 and 13).

	Start-up announcement is the primary path and stays the primary path. This
	notice is the backstop for one case it cannot cover: the registry losing
	its catalogue while the plugins keep running. A truncated table, a
	recreated volume, a restore from a stale backup. The common way a
	catalogue is lost in this lab — `docker compose down -v` — restarts the
	plugins too, so the notice earns nothing there.

	It is a STATEMENT OF FACT, not a command. The registry says its catalogue
	was reset; each publisher decides for itself whether to re-announce.
	Deliberately not `cmd.*`: that family is reserved and unused, and opening
	it here would claim the registry has authority over a plugin's process,
	which BR-AS54 says it does not. Silence in reply is inert — a plugin that
	ignores a notice is simply not re-announced, and no path from a notice, or
	from silence in response to one, may reach unregister.

	It needs no durability. Core NATS, no JetStream, no retention: a publisher
	that was down when it fired announces at start-up anyway, so the offline
	case is already covered. That is a simplification, not a gap.
*/

// EntriesReset is the subject the notice travels on. `entries` rather than
// the browser's `frontend-plugins` view, because this is publisher-facing:
// the audience is the processes that announce, not the ones that read.
const EntriesReset = "notify._platform.mfe-registry.entries.reset"

// The jitter window's locally-owned bounds. The herd is not the notice — one
// message fanning out is free. The herd is the REPLIES: five hundred signed
// announces, each a signature verify plus a Postgres write.
//
// The window travels in the message so the registry can widen the spread
// across a fleet without redeploying a single plugin. These two constants are
// what stop that field from being a lever: a plugin clamps the carried value
// between them before using it, so the registry keeps the power to widen and
// nothing on the wire gains the power to narrow the window to zero — which
// would turn the notice into exactly the synchronised stampede it exists to
// prevent.
const (
	// ResetJitterFloor is the narrowest window a plugin will honour, however
	// small the number on the wire.
	ResetJitterFloor = 5 * time.Second
	// ResetJitterCeiling is the widest. A window past it is more likely a
	// mistake or a mischief than an intention, and a plugin that waited an
	// hour to re-announce would look lost rather than patient.
	ResetJitterCeiling = 5 * time.Minute
	// ResetJitterDefault is what a notice that carries no window at all
	// means. Absent is not zero: a sender that forgot the field must not
	// accidentally request a stampede.
	ResetJitterDefault = 60 * time.Second
)

// ResetNotice is the whole payload. It carries no entries, no revision and
// nothing to install from: a recipient re-announces what it already holds,
// and a notice that could carry catalogue content would be a second, unsigned
// way into the catalogue.
type ResetNotice struct {
	// JitterMillis is the window each publisher draws its delay from,
	// uniformly. Milliseconds rather than a duration string so the field is
	// one number in JSON on both sides.
	JitterMillis int64 `json:"jitterMillis"`
	// Reason is for an operator reading logs, never for a decision. Nothing
	// branches on it — a publisher that treated one reason differently from
	// another would be taking instruction from the wire.
	Reason string `json:"reason,omitempty"`
	// At is milliseconds since the epoch, stamped by the registry.
	At int64 `json:"at"`
}

// JitterWindow is the carried window after clamping — the ONLY way a
// publisher should read that field. It is a pure function so the rule about
// not trusting input is a value test, with no clock and no bus in it.
func (n ResetNotice) JitterWindow() time.Duration {
	window := time.Duration(n.JitterMillis) * time.Millisecond
	if n.JitterMillis <= 0 {
		window = ResetJitterDefault
	}
	if window < ResetJitterFloor {
		return ResetJitterFloor
	}
	if window > ResetJitterCeiling {
		return ResetJitterCeiling
	}
	return window
}
