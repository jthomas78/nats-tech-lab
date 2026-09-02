package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

// ------------------------------------------------------------- read helpers

// entryView is the operator's view of one row, narrowed to the fields the
// sequence asserts on. Deliberately a local shape rather than the registry's
// own: browserrpc lives behind an internal package, and a cmd/ client
// restating only what it reads is how seed-publishers already does this.
type entryView struct {
	ID        string `json:"id"`
	Enabled   bool   `json:"enabled"`
	Lifecycle string `json:"lifecycle"`
	Release   int64  `json:"release"`
	Withheld  bool   `json:"withheld"`
	Withdrawn bool   `json:"withdrawn"`
	Remote    struct {
		URL string `json:"url"`
	} `json:"remote"`
	Source       string `json:"source"`
	RegisteredBy string `json:"registeredBy"`
}

func (e entryView) String() string {
	return fmt.Sprintf("enabled=%t withdrawn=%t withheld=%t release=%d url=%s",
		e.Enabled, e.Withdrawn, e.Withheld, e.Release, e.Remote.URL)
}

type curatedDoc struct {
	Revision int64       `json:"revision"`
	Plugins  []entryView `json:"plugins"`
	Error    string      `json:"error"`
}

type auditRow struct {
	Op      string `json:"op"`
	EntryID string `json:"entryId"`
	Actor   string `json:"actor"`
	At      string `json:"at"`
}

func (h *harness) curated() (curatedDoc, error) {
	data, err := h.request(mferegistry.Curated, map[string]any{})
	if err != nil {
		return curatedDoc{}, err
	}
	var doc curatedDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return doc, err
	}
	if doc.Error != "" {
		return doc, fmt.Errorf("the registry refused the read: %s", doc.Error)
	}
	return doc, nil
}

func (h *harness) entry(id string) (entryView, error) {
	doc, err := h.curated()
	if err != nil {
		return entryView{}, err
	}
	for _, e := range doc.Plugins {
		if e.ID == id {
			return e, nil
		}
	}
	return entryView{}, fmt.Errorf("the registry holds no entry %q", id)
}

// await polls until the entry satisfies want. Polling rather than watching
// notify.*: what is being waited for is the *effect* of a container's
// lifecycle, and a change hint would say a write happened without saying it
// was this one.
func (h *harness) await(desc string, want func(entryView) bool) (entryView, error) {
	deadline := time.Now().Add(h.timeout)
	var last entryView
	for {
		e, err := h.entry(subjectPlugin)
		if err == nil {
			last = e
			if want(e) {
				fmt.Printf("    ok   %s\n", desc)
				return e, nil
			}
		}
		if time.Now().After(deadline) {
			fmt.Printf("    FAIL %s\n", desc)
			return last, fmt.Errorf("%s — after %s the entry was still: %s", desc, h.timeout, last)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// latestActor is the actor on the newest audited write for the plugin. On an
// announcement that actor is the key that signed it, which is the only place
// a rotation is visible: registeredBy names the *creating* actor and by
// design does not move when the signing key does.
func (h *harness) latestActor() (string, error) {
	data, err := h.request(mferegistry.Audit, map[string]any{"limit": 200})
	if err != nil {
		return "", err
	}
	var rows []auditRow
	if err := json.Unmarshal(data, &rows); err != nil {
		return "", err
	}
	for _, r := range rows { // newest first
		if r.EntryID == subjectPlugin {
			return r.Actor, nil
		}
	}
	return "", fmt.Errorf("the audit trail holds no write for %q", subjectPlugin)
}

// ------------------------------------------------------------ health helpers

// healthSignal is one plane's reading, narrowed the same way entryView is.
type healthSignal struct {
	State string `json:"state"`
	Cause string `json:"cause"`
}

func (h healthSignal) String() string {
	if h.Cause == "" {
		return h.State
	}
	return h.State + " (" + h.Cause + ")"
}

type pluginHealth struct {
	Frontend healthSignal `json:"frontend"`
	Backend  healthSignal `json:"backend"`
}

type healthDoc struct {
	OK      bool                    `json:"ok"`
	Plugins map[string]pluginHealth `json:"plugins"`
}

// health reads the plane over the SHELL connection, because that subject is
// the shell's and no operator credential carries it. Read separately from the
// catalogue for the reason the two subjects are separate at all (BR-AS65):
// one is what an operator and a publisher agreed to, the other is what the
// platform noticed a moment ago.
func (h *harness) health() (healthDoc, error) {
	raw, err := json.Marshal(map[string]any{})
	if err != nil {
		return healthDoc{}, err
	}
	msg, err := h.shell.Request(mferegistry.HealthRead, raw, 10*time.Second)
	if err != nil {
		return healthDoc{}, fmt.Errorf("%s: %w", mferegistry.HealthRead, err)
	}
	var doc healthDoc
	if err := json.Unmarshal(msg.Data, &doc); err != nil {
		return doc, err
	}
	if !doc.OK {
		return doc, fmt.Errorf("the health plane answered not-ok")
	}
	return doc, nil
}

// awaitHealth polls the plane until one plugin's frontend signal satisfies
// want. The timeout is generous on purpose: a reading has to travel a
// heartbeat and then age against the freshness window, and both are measured
// in seconds by design (BR-AS64).
func (h *harness) awaitHealth(pluginID, desc string, want func(healthSignal) bool) (healthSignal, error) {
	deadline := time.Now().Add(h.timeout)
	var last healthSignal
	seen := false
	for {
		doc, err := h.health()
		if err == nil {
			entry, present := doc.Plugins[pluginID]
			if present {
				last, seen = entry.Frontend, true
				if want(entry.Frontend) {
					fmt.Printf("    ok   %s\n", desc)
					return entry.Frontend, nil
				}
			}
		}
		if time.Now().After(deadline) {
			fmt.Printf("    FAIL %s\n", desc)
			if !seen {
				return last, fmt.Errorf("%s — after %s the plane held no reading for %q at all", desc, h.timeout, pluginID)
			}
			return last, fmt.Errorf("%s — after %s the signal was still: %s", desc, h.timeout, last)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// ------------------------------------------------------------ write helpers

// Every write is revision-checked, so each reads first. That is not
// ceremony: the registry refuses a blind write, and the refusal is the same
// concurrency rule the Admin UI obeys.
func (h *harness) setEnabled(id string, enabled bool) error {
	doc, err := h.curated()
	if err != nil {
		return err
	}
	data, err := h.request(mferegistry.SetEnabled, map[string]any{
		"ifRevision": doc.Revision, "entryId": id, "enabled": enabled,
	})
	if err != nil {
		return err
	}
	var out curatedDoc
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	if out.Error != "" {
		return fmt.Errorf("set-enabled %s=%t: %s", id, enabled, out.Error)
	}
	return nil
}

func (h *harness) publisherWrite(op, publisher string, fields map[string]any) error {
	data, err := h.request(mferegistry.Publishers, map[string]any{})
	if err != nil {
		return err
	}
	var doc struct {
		Revision int64  `json:"revision"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	if doc.Error != "" {
		return fmt.Errorf("the registry refused the publishers read: %s", doc.Error)
	}
	payload := map[string]any{"ifRevision": doc.Revision, "op": op, "publisherId": publisher}
	for k, v := range fields {
		payload[k] = v
	}
	reply, err := h.request(mferegistry.PublisherWrite, payload)
	if err != nil {
		return err
	}
	var out struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(reply, &out); err != nil {
		return err
	}
	if out.Error != "" {
		return fmt.Errorf("%s on %s: %s", op, publisher, out.Error)
	}
	return nil
}

func (h *harness) addKey(publisher, key string) error {
	return h.publisherWrite(mferegistry.OpPublisherAddKey, publisher, map[string]any{"publicKey": key})
}

func (h *harness) setKeyState(publisher, key, state string) error {
	return h.publisherWrite(mferegistry.OpPublisherSetKeyState, publisher,
		map[string]any{"publicKey": key, "keyState": state})
}

// ------------------------------------------------------------ the sequence

func (h *harness) scenarios() error {
	// ---------------------------------------------------------------- 1
	h.heading("first boot left an announced entry awaiting an operator")
	start, err := h.await("the sidecar's announcement is on the record", func(e entryView) bool { return e.Release > 0 })
	if err != nil {
		return err
	}
	if err := h.check("it registered through the announced tier", start.Source == "announced", "source="+start.Source); err != nil {
		return err
	}
	if err := h.check("its lifecycle is dynamic — a publisher may withdraw it", start.Lifecycle == "dynamic", "lifecycle="+start.Lifecycle); err != nil {
		return err
	}
	if err := h.check("announcing did not enable it (BR-AS42)", !start.Enabled, start.String()); err != nil {
		return err
	}
	release := start.Release
	h.note("release %d, signed by the publisher's bootstrap key", release)

	// ---------------------------------------------------------------- 2
	//
	// Health, before the lifecycle starts moving. Two plugins are read here
	// and they are the two the old HTTP probe could not both serve: one that
	// never announces, and one that is genuinely broken.
	h.heading("every plugin reports its own frontend health, curated included")
	h.note("Nothing dials a plugin any more. Each one self-GETs its loopback /healthz and")
	h.note("publishes the verdict on notify._platform.health.frontend.{id}.v1 (Phase 15 decision 14).")

	// demo-catalog is curated: it is preloaded, it has no announcer and it
	// signs nothing. Before Phase 15 it was the one plugin the registry had
	// no origin for, and it read `not configured` — a state that said "we
	// did not look" while sitting next to plugins that had been looked at.
	// It now answers for itself like everything else, which is what makes
	// `not configured` a state the frontend plane no longer has.
	catalog, err := h.awaitHealth("demo-catalog", "the curated plugin reports healthy about itself",
		func(sig healthSignal) bool { return sig.State == "healthy" })
	if err != nil {
		return err
	}
	if err := h.check("and it is not 'not configured' — that state is gone from the frontend plane",
		catalog.State != "not configured", catalog.String()); err != nil {
		return err
	}

	// The control group's broken fixture. It has no web server at all: its
	// HEALTH_SELF_URL points at the discard port, so the self-GET fails, and
	// after the second consecutive failure it says so itself (BR-AS63). The
	// difference from before is the whole point of the phase — the platform
	// is not deducing this, the plugin is reporting it.
	//
	// It has to be enabled to be watched, because health follows the
	// catalogue an operator curated (BR-AS65). Doing it HERE, before the
	// control group is sampled, is what keeps step 10 honest: the baseline
	// below already holds this decision, so nothing the sequence does later
	// can hide inside it.
	if err := h.setEnabled("example-plugin-unreachable", true); err != nil {
		return err
	}
	broken, err := h.awaitHealth("example-plugin-unreachable", "the broken plugin reports itself unavailable",
		func(sig healthSignal) bool { return sig.State == "unavailable" })
	if err != nil {
		return err
	}
	// The cause matters as much as the state. `unreachable` is a plugin
	// saying its own listener refused it; `absent` is the registry saying it
	// has heard nothing. Step 10 exercises the second, and the two must not
	// be spelled the same way.
	if err := h.check("with cause 'unreachable' — its own listener refused it",
		broken.Cause == "unreachable", broken.String()); err != nil {
		return err
	}

	others, err := h.otherEntries()
	if err != nil {
		return err
	}

	// ---------------------------------------------------------------- 3
	h.heading("an operator approves it")
	if err := h.setEnabled(subjectPlugin, true); err != nil {
		return err
	}
	approved, err := h.await("the entry is enabled", func(e entryView) bool { return e.Enabled })
	if err != nil {
		return err
	}
	// The release counter belongs to the publisher, so an operator's decision
	// must not move it. If approval spent a release, a publisher's next
	// announcement would look like a replay of one already stored.
	if err := h.check("approval spent no release (BR-AS47)", approved.Release == release, approved.String()); err != nil {
		return err
	}

	// ---------------------------------------------------------------- 4
	h.heading("the publisher withdraws on controlled shutdown")
	h.note("SIGTERM to %s — an explicit signed unregister.", subjectService)
	h.note("A crash, a failed health check or a vanished container withdraws NOTHING (BR-AS54):")
	h.note("withdrawal is an action the publisher takes, never something the platform infers from silence.")
	if _, err := h.compose("stop", subjectService); err != nil {
		return err
	}
	// Every wait below tests the release counter as well as the flag it is
	// waiting for. A flag alone can already hold the value being waited for —
	// a lab left withdrawn by an earlier run, say — and a wait that returns on
	// a state it did not cause is a green assertion about nothing.
	gone, err := h.await("the entry is marked withdrawn", func(e entryView) bool {
		return e.Withdrawn && e.Release > approved.Release
	})
	if err != nil {
		return err
	}
	// The two flags are the whole of BR-AS55: the publisher said "not
	// available", the operator never said "not approved", and one flag
	// carrying both would lose the difference.
	if err := h.check("the operator's approval was left alone (BR-AS55)", gone.Enabled, gone.String()); err != nil {
		return err
	}
	if err := h.check("the row is still there — withdrawn, not deleted", gone.ID == subjectPlugin, "missing"); err != nil {
		return err
	}
	if err := h.check(fmt.Sprintf("the withdrawal spent release %d", release+1), gone.Release == release+1, gone.String()); err != nil {
		return err
	}

	// ---------------------------------------------------------------- 5
	h.heading("the same publisher returns unchanged")
	if _, err := h.compose("start", subjectService); err != nil {
		return err
	}
	back, err := h.await("the withdrawal is cleared", func(e entryView) bool { return !e.Withdrawn && e.Release > gone.Release })
	if err != nil {
		return err
	}
	if err := h.check(fmt.Sprintf("the return spent release %d", release+2), back.Release == release+2, back.String()); err != nil {
		return err
	}
	// Enabled + dynamic + same origin is DecideAnnounce's `updated` branch,
	// and staying enabled is what distinguishes it from `pending` (which
	// leaves a disabled entry disabled) and `requeued` (which disables).
	if err := h.check("it came back enabled — an update, not a fresh approval", back.Enabled, back.String()); err != nil {
		return err
	}
	h.note("releases %d / %d / %d — one monotonic sequence shared by announce and unregister (BR-AS67)", release, release+1, release+2)

	// ---------------------------------------------------------------- 6
	h.heading("the publisher rotates its signing key")
	firstKey, err := h.latestActor()
	if err != nil {
		return err
	}
	// Withdraw first, while the old key is still enabled. A retired key signs
	// nothing new, an unregister included — rotating before shutting down
	// would strand the sidecar unable to say goodbye.
	if _, err := h.compose("stop", subjectService); err != nil {
		return err
	}
	rotating, err := h.await("the old key's withdrawal is accepted", func(e entryView) bool {
		return e.Withdrawn && e.Release > back.Release
	})
	if err != nil {
		return err
	}
	secondKey, secondSeed, err := h.newSigningKey("rotation.nk")
	if err != nil {
		return err
	}
	if err := h.addKey(subjectPlugin, secondKey); err != nil {
		return err
	}
	if err := h.setKeyState(subjectPlugin, firstKey, mferegistry.KeyRetired); err != nil {
		return err
	}
	h.original, h.rotatedK, h.rotated = firstKey, secondKey, true
	h.note("added %s… and retired %s…", secondKey[:12], firstKey[:12])

	if err := h.spawn("registry-acceptance-rotated", nil, secondSeed+":/etc/plugin/signing.nk:ro"); err != nil {
		return err
	}
	rotated, err := h.await("the new key's announcement is accepted", func(e entryView) bool {
		return !e.Withdrawn && e.Release > rotating.Release
	})
	if err != nil {
		return err
	}
	actor, err := h.latestActor()
	if err != nil {
		return err
	}
	if err := h.check("the newest write is signed by the new key", actor == secondKey, "actor="+actor); err != nil {
		return err
	}
	if err := h.check("rotation did not disturb the operator's approval", rotated.Enabled, rotated.String()); err != nil {
		return err
	}

	// ---------------------------------------------------------------- 7
	h.heading("the publisher moves the plugin to another origin")
	// The move is a deployment change, not a rebuild. Since BR-AS71 the
	// plugin's own manifest carries no origin at all, so relocating it means
	// giving the publisher a different PLUGIN_PUBLIC_ORIGIN and letting it
	// stamp that in before it signs.
	altDeployment := []string{"PLUGIN_PUBLIC_ORIGIN=" + altOrigin}
	h.note("%s → %s, both allowlisted: the requeue turns on the move, not on a refused origin.", homeOrigin, altOrigin)
	if err := h.kill("registry-acceptance-rotated"); err != nil {
		return err
	}
	moving, err := h.await("the withdrawal before the move is accepted", func(e entryView) bool {
		return e.Withdrawn && e.Release > rotated.Release
	})
	if err != nil {
		return err
	}
	if err := h.spawn("registry-acceptance-moved", altDeployment,
		secondSeed+":/etc/plugin/signing.nk:ro"); err != nil {
		return err
	}
	moved, err := h.await("the moved plugin is queued for review again", func(e entryView) bool {
		return e.Release > moving.Release
	})
	if err != nil {
		return err
	}
	if err := h.check("the registry holds the new origin", hasOrigin(moved.Remote.URL, altOrigin), "url="+moved.Remote.URL); err != nil {
		return err
	}
	// The point of the branch: code served from a different origin is not the
	// code the operator approved, so the approval does not travel with it.
	if err := h.check("the earlier approval did not travel to the new origin", !moved.Enabled, moved.String()); err != nil {
		return err
	}
	// Nor did the move bring the plugin back into availability. One operator
	// decision answers both questions, and until it is taken the entry stays
	// exactly where the publisher's withdrawal left it (BR-AS55).
	if err := h.check("and the move did not clear the withdrawal on its own", moved.Withdrawn, moved.String()); err != nil {
		return err
	}

	// ---------------------------------------------------------------- 8
	h.heading("an operator approves the plugin at its new origin")
	if err := h.setEnabled(subjectPlugin, true); err != nil {
		return err
	}
	approvedAgain, err := h.await("it is serving again", func(e entryView) bool { return e.Enabled })
	if err != nil {
		return err
	}
	// The single decision clears the publisher's withdrawal too: approval
	// outranks availability, and an operator enabling this entry is looking
	// at this entry.
	if err := h.check("the operator's approval also cleared the withdrawal", !approvedAgain.Withdrawn, approvedAgain.String()); err != nil {
		return err
	}

	// ---------------------------------------------------------------- 9
	h.heading("the signing key is revoked, then recovered")
	if err := h.setKeyState(subjectPlugin, secondKey, mferegistry.KeyRevoked); err != nil {
		return err
	}
	withheld, err := h.await("the entry it signed is withheld (BR-AS38)", func(e entryView) bool { return e.Withheld })
	if err != nil {
		return err
	}
	// Revocation is stronger than a withdrawal, and this is the difference:
	// a withdrawal leaves the operator's approval alone (step 3), a
	// revocation clears it in the same transaction. Withheld and disabled
	// still mean different things — "we took this out of service" and "not
	// reviewed" — and a revocation is both at once.
	if err := h.check("revocation also cleared the operator's approval", !withheld.Enabled, withheld.String()); err != nil {
		return err
	}

	if err := h.setKeyState(subjectPlugin, secondKey, mferegistry.KeyEnabled); err != nil {
		return err
	}
	// Trusting the key again restores nothing. The revocation was a decision
	// about an entry, and re-enabling a key is not a look at that entry.
	time.Sleep(2 * time.Second)
	stillHeld, err := h.entry(subjectPlugin)
	if err != nil {
		return err
	}
	if err := h.check("re-enabling the key alone does not restore the entry", stillHeld.Withheld && !stillHeld.Enabled, stillHeld.String()); err != nil {
		return err
	}

	if err := h.kill("registry-acceptance-moved"); err != nil {
		return err
	}
	recovering, err := h.await("the withdrawal is accepted while withheld", func(e entryView) bool {
		return e.Withdrawn && e.Release > stillHeld.Release
	})
	if err != nil {
		return err
	}
	if err := h.spawn("registry-acceptance-recovered", altDeployment,
		secondSeed+":/etc/plugin/signing.nk:ro"); err != nil {
		return err
	}
	reannounced, err := h.await("a fresh, validly signed announcement is accepted", func(e entryView) bool {
		return e.Release > recovering.Release
	})
	if err != nil {
		return err
	}
	// The half of BR-AS38 that is easy to get wrong: a publisher cannot lift
	// a withholding by announcing again, however good its signature. Only an
	// operator enabling that entry clears it, because only that is somebody
	// looking at the entry itself.
	if err := h.check("announcing again does not lift the withholding", reannounced.Withheld, reannounced.String()); err != nil {
		return err
	}
	// It does not restore availability either. An unapproved entry's return
	// lands pending, and pending keeps the publisher's own withdrawal mark:
	// a return is not an approval (BR-AS55).
	if err := h.check("nor does it put the plugin back on offer", reannounced.Withdrawn && !reannounced.Enabled, reannounced.String()); err != nil {
		return err
	}

	if err := h.setEnabled(subjectPlugin, true); err != nil {
		return err
	}
	restored, err := h.await("an operator's approval lifts it and puts it back into service", func(e entryView) bool {
		return e.Enabled && !e.Withheld && !e.Withdrawn
	})
	if err != nil {
		return err
	}
	if err := h.check("the recovered entry is serving from the origin it announced", hasOrigin(restored.Remote.URL, altOrigin), "url="+restored.Remote.URL); err != nil {
		return err
	}

	// ---------------------------------------------------------------- 10
	h.heading("nothing else in the registry moved")
	after, err := h.otherEntries()
	if err != nil {
		return err
	}
	for id, before := range others {
		now, present := after[id]
		if err := h.check(id+" is still registered", present, "gone"); err != nil {
			return err
		}
		if err := h.check(id+" is untouched",
			now.Release == before.Release && now.Enabled == before.Enabled && now.Withdrawn == before.Withdrawn && now.Withheld == before.Withheld,
			fmt.Sprintf("was [%s] now [%s]", before, now)); err != nil {
			return err
		}
	}

	// ---------------------------------------------------------------- 11
	//
	// Last, because it ends with a plugin killed. BR-AS54 is the rule the
	// whole health plane is built not to break, and this is the only step
	// that puts it under real load: a plugin that simply stops talking.
	h.heading("a plugin that goes silent is reported absent — and stays registered")
	before, err := h.entry(subjectPlugin)
	if err != nil {
		return err
	}
	// SIGKILL, not SIGTERM. A stop would let the publisher send its signed
	// unregister, which is step 4's case and a completely different thing.
	// What is wanted here is the case with no message at all: the process is
	// gone and nothing was said about it.
	h.note("SIGKILL — no unregister is sent, so the registry is left with nothing but silence.")
	if err := h.hardKill("registry-acceptance-recovered"); err != nil {
		return err
	}
	// Absent is the registry's own word, attached when a reading ages out of
	// the freshness window. No plugin can send it, which is what keeps it
	// distinguishable from step 2's `unreachable` — a plugin's report about
	// its own listener.
	silent, err := h.awaitHealth(subjectPlugin, "the frontend signal ages out to stale",
		func(sig healthSignal) bool { return sig.State == "stale" })
	if err != nil {
		return err
	}
	if err := h.check("with cause 'absent' — the registry heard nothing, rather than being told anything",
		silent.Cause == "absent", silent.String()); err != nil {
		return err
	}

	// The point of the step. Everything above is decoration on a catalogue
	// row, and the row must not have moved.
	after2, err := h.entry(subjectPlugin)
	if err != nil {
		return err
	}
	if err := h.check("the entry is still registered (BR-AS54)", after2.ID == subjectPlugin, "gone"); err != nil {
		return err
	}
	if err := h.check("silence spent no release", after2.Release == before.Release, after2.String()); err != nil {
		return err
	}
	if err := h.check("silence did not withdraw it", !after2.Withdrawn, after2.String()); err != nil {
		return err
	}
	if err := h.check("nor did it revoke the operator's approval", after2.Enabled, after2.String()); err != nil {
		return err
	}
	return nil
}

// otherEntries is every announced plugin except the one under test — the
// control group. Four sidecars sat still through all of the above, and a
// sequence that could not say so would not have shown that any of it was
// scoped to one publisher.
func (h *harness) otherEntries() (map[string]entryView, error) {
	doc, err := h.curated()
	if err != nil {
		return nil, err
	}
	out := map[string]entryView{}
	for _, e := range doc.Plugins {
		if e.ID != subjectPlugin {
			out[e.ID] = e
		}
	}
	return out, nil
}

// hasOrigin reports whether url is served from origin. A prefix test would
// call http://localhost:71130 a match for http://localhost:7113, which is
// exactly the kind of near-miss this sequence exists to catch.
func hasOrigin(url, origin string) bool {
	return len(url) > len(origin) && url[:len(origin)] == origin && url[len(origin)] == '/'
}
