package domain

import "sort"

/*
	Which backend services a plugin's health decoration reflects (BR-AS62).

	This used to be a deployment map — one JSON object in the compose file,
	naming every plugin and every service it depended on. It was configuration
	for one reason: a publisher that can name its own probe target can point
	the registry at a service it does not own and read the answer back out of
	the pill next to its plugin. The map was the gate, and it was the only
	gate, because the registry's NATS grant is already one token wide across
	every service's readiness subject.

	The map was also in the wrong place. It lived with the deployment, not with
	the plugin, so shipping a plugin meant editing someone else's compose file
	— and the failure mode of forgetting was silent: an unlisted plugin read
	`not configured` forever and nobody could tell that apart from a plugin
	nobody had got round to.

	So the statement and the authority are separated instead of collapsed into
	one place:

	  - the PLUGIN declares, in its signed manifest, what it depends on. That
	    travels with the plugin, cannot be forgotten by a deployment, and is
	    tamper-evident.
	  - the OPERATOR approves, at curation, and only the approved list is ever
	    dialled. A declaration on its own buys a publisher nothing.

	A signature does not move the gate, and it was never meant to: it proves
	who said a thing, not that they may say it. The publisher signs its own
	declaration, honest or not, and the operator is still the one who decides
	whether the registry asks.
*/

// MaxBackendServices caps one plugin's declaration. A probe is work the
// registry does on a timer for as long as the plugin is enabled, so the
// number of them a publisher can ask for is bounded in the same place the
// rest of the entry's structure is checked, rather than left to the operator
// to notice. Sixteen is far past any real plugin and far short of a lever.
const MaxBackendServices = 16

// ValidateBackendServices checks the declaration and the approval against
// each other and against the subject they will be spelled into.
//
// The subset rule is what makes the audit trail mean something: an approval
// is always an answer to a declaration that exists, so a stored approval can
// be read back as "an operator saw this plugin ask for this service and said
// yes", and never as a list that arrived from somewhere else.
func (e Entry) ValidateBackendServices() error {
	if len(e.BackendServices) > MaxBackendServices {
		return notAdmissible("entry %q declares %d backend services, more than the %d allowed",
			e.ID, len(e.BackendServices), MaxBackendServices)
	}
	declared := make(map[string]struct{}, len(e.BackendServices))
	for _, id := range e.BackendServices {
		if !subjectSafe(id) {
			return notAdmissible("entry %q declares a backend service id that is not subject-safe", e.ID)
		}
		if _, dup := declared[id]; dup {
			return notAdmissible("entry %q declares backend service %q twice", e.ID, id)
		}
		declared[id] = struct{}{}
	}
	for _, id := range e.ApprovedBackendServices {
		if _, ok := declared[id]; !ok {
			// Named without quoting the id: an operator approving something
			// undeclared is a mistake about this entry, and the refusal is
			// read next to the entry.
			return notAdmissible("entry %q approves a backend service it does not declare", e.ID)
		}
	}
	return nil
}

// EffectiveBackendServices is the list the health plane probes, and the only
// place the three answers the shell can show are decided:
//
//	nil        -> not configured. Nobody has answered for this plugin: either
//	              it never declared, or it declared and no operator has
//	              approved yet. Both are true readings of "we do not know",
//	              and neither may be shown as healthy.
//	empty      -> not applicable. Somebody answered, and the answer is that
//	              there is nothing to ask. A plugin declaring [] says it is
//	              frontend-only; an operator approving [] says the same thing
//	              about a plugin that asked for more.
//	non-empty  -> the services to dial.
//
// The intersection is deliberate rather than defensive. An approval is given
// for a declaration, so a publisher that drops a service from its manifest
// drops the approval with it; without this, a plugin could be approved for a
// service, quietly stop declaring it, and keep the probe.
func (e Entry) EffectiveBackendServices() []string {
	if e.BackendServices == nil {
		return nil
	}
	if len(e.BackendServices) == 0 {
		return []string{}
	}
	if e.ApprovedBackendServices == nil {
		return nil
	}
	declared := make(map[string]struct{}, len(e.BackendServices))
	for _, id := range e.BackendServices {
		declared[id] = struct{}{}
	}
	out := make([]string, 0, len(e.ApprovedBackendServices))
	seen := make(map[string]struct{}, len(e.ApprovedBackendServices))
	for _, id := range e.ApprovedBackendServices {
		if _, ok := declared[id]; !ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// carryApproval moves an operator's answer from the stored entry onto an
// incoming announcement, narrowed to what the announcement still declares.
//
// Called on every branch of DecideAnnounce that keeps an existing entry's
// decisions, for the same reason Withdrawn is: an ordinary re-announce is a
// publisher restating its manifest, and a manifest cannot carry an approval,
// so without this every heartbeat of a plugin would silently revoke it.
func carryApproval(existing Entry, incoming Entry) []string {
	if existing.ApprovedBackendServices == nil {
		return nil
	}
	declared := make(map[string]struct{}, len(incoming.BackendServices))
	for _, id := range incoming.BackendServices {
		declared[id] = struct{}{}
	}
	out := make([]string, 0, len(existing.ApprovedBackendServices))
	for _, id := range existing.ApprovedBackendServices {
		if _, ok := declared[id]; ok {
			out = append(out, id)
		}
	}
	return out
}
