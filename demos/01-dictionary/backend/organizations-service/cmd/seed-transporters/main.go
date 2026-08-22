// Command seed-transporters populates a tenant with Transporters at every
// stage of the vetting lifecycle (Phase 38g), for testing and demos.
//
// It drives the same api.* NATS endpoints the browser uses, over a tenant
// credential — never Postgres, and never REST: BR-TP16 reduced this service's
// HTTP surface to infra health, so NATS is the only business transport. That
// also means every rung below goes through the real validation an operator
// would hit, rather than writing rows that could never have been created.
//
// Completeness is a deterministic ladder rather than randomness: rung N is
// always the same state, so a re-run reproduces the same data and any row's
// stage is readable from its name. -n above 10 cycles the ladder with a
// counter suffix; below 10 truncates it.
//
// Usage:
//
//	go run ./cmd/seed-transporters [-n 10] [-context acme] [-creds ../../nats/creds/acme.creds]
//
// Prerequisites, all of which the seeder checks and reports rather than
// failing halfway with an opaque refdata error:
//
//   - refdata's region corpus  — go run ./cmd/seed-regions        (refdata-service)
//   - refdata's vehicle types  — go run ./cmd/seed-vehicle-types -context <same context>
//   - refdata's goods types    — go run ./cmd/seed-goods-types   -context <same context>
//
// The vehicle-type and goods-type corpora must live in the *same context* as
// the seeded organizations: BR-TP14/BR-TP64 resolve codes in the
// organization's own context, and Phase 106's context inheritance is not on
// the live read path.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	// vettingTimeout bounds the wait for a Temporal vetting attempt to reach
	// Vetted. The saga itself is fast (a mock GIT verifier); this is really a
	// guard against the worker not running at all, which would otherwise hang
	// the seeder with no explanation.
	vettingTimeout = 45 * time.Second
	requestTimeout = 10 * time.Second
)

func main() {
	count := flag.Int("n", 10, "number of transporters to seed")
	contextKey := flag.String("context", "acme", "business-unit context to seed into")
	credsPath := flag.String("creds", "../../nats/creds/acme.creds", "tenant NATS credentials")
	natsURL := flag.String("nats-url", nats.DefaultURL, "NATS URL")
	flag.Parse()

	if *count < 1 {
		log.Fatal("-n must be at least 1")
	}

	// nats.Name is required of every connection in this repo — an anonymous
	// one is indistinguishable in `nats server list connections`.
	nc, err := nats.Connect(*natsURL, nats.UserCredentials(*credsPath), nats.Name("seed-transporters"))
	if err != nil {
		log.Fatalf("connect to NATS: %v", err)
	}
	defer nc.Close()

	s := &seeder{nc: nc, contextKey: *contextKey, run: int(time.Now().Unix() % 90000)}
	if err := s.checkPrerequisites(); err != nil {
		log.Fatalf("%v", err)
	}

	log.Printf("seeding %d transporters into context %q", *count, *contextKey)
	for i := range *count {
		rung := ladder[i%len(ladder)]
		name := fmt.Sprintf("Seed Transporter %02d — %s", i+1, rung.label)
		if i >= len(ladder) {
			name = fmt.Sprintf("Seed Transporter %02d — %s (%d)", i+1, rung.label, i/len(ladder)+1)
		}
		if err := s.seed(name, rung); err != nil {
			log.Fatalf("rung %d (%s): %v", i+1, rung.label, err)
		}
		log.Printf("  %02d/%02d  %s", i+1, *count, name)
	}
	log.Printf("done: %d transporters across %d rungs", *count, min(*count, len(ladder)))
}

// rung is one step of completeness. The booleans are cumulative — each rung
// does everything the ones above it do, plus its own step.
type rung struct {
	label string

	companyInfo   bool
	operatingArea bool
	fleetAsset    bool
	document      bool
	trackingCreds bool

	// review is what happens to the compliance document once vetting has been
	// submitted: "" leaves it pending (no submission at all), "reject" ends
	// the attempt Rejected, "approve" carries it through to Vetted.
	review string

	activate bool
	suspend  bool
}

// ladder is the ten rungs, in order. Each is named for the state it lands in,
// so a seeded row's stage is legible from the organization list alone.
var ladder = []rung{
	{label: "bare"},
	{label: "company info", companyInfo: true},
	{label: "areas", companyInfo: true, operatingArea: true},
	{label: "fleet", companyInfo: true, operatingArea: true, fleetAsset: true},
	{label: "docs pending", companyInfo: true, operatingArea: true, fleetAsset: true, document: true},
	{label: "docs rejected", companyInfo: true, operatingArea: true, fleetAsset: true, document: true, review: "reject"},
	{label: "vetted", companyInfo: true, operatingArea: true, fleetAsset: true, document: true, review: "approve"},
	{label: "tracked", companyInfo: true, operatingArea: true, fleetAsset: true, document: true, review: "approve", trackingCreds: true},
	{label: "active", companyInfo: true, operatingArea: true, fleetAsset: true, document: true, review: "approve", trackingCreds: true, activate: true},
	{label: "suspended", companyInfo: true, operatingArea: true, fleetAsset: true, document: true, review: "approve", trackingCreds: true, activate: true, suspend: true},
}

// Corpus values. Deliberately real codes drawn from the seeded corpora rather
// than plausible-looking inventions — refdata rejects anything else, and a
// wrong guess fails mid-ladder.
var (
	regionCodes  = []string{"ZA-GP", "ZA-WC", "ZA-KZN", "ZA-EC", "ZA-FS"}
	vehicleTypes = []string{"TAUTLINER", "BACK_END_TIPPER", "FLATBED", "DROPSIDE", "CAR_CARRIER"}
	goodsTypes   = []string{"GENERAL_FREIGHT", "PALLETISED_GOODS", "REFRIGERATED_FOODSTUFFS", "FRESH_PRODUCE", "DRY_BULK", "LIQUID_BULK", "HAZARDOUS_MATERIALS", "LIVESTOCK", "HIGH_VALUE_GOODS", "ABNORMAL_LOAD"}
	providers    = []string{"CARTRACK", "MIX_TELEMATICS", "WEBFLEET", "CTRACK", "NETSTAR"}
)

type seeder struct {
	nc         *nats.Conn
	contextKey string
	seq        int

	// run makes this invocation's fleet-asset registration numbers and VINs
	// unique. They are globally unique in the domain (BR-TP13), so a fixed
	// series collides with the previous run on rung 4.
	//
	// Note what this does *not* make the seeder: idempotent. `register` mints
	// a new organization every time, so a second run adds another cohort of
	// ten rather than converging on the first. That is the honest behaviour
	// for a command whose first step has no natural key — re-running is safe,
	// not repeatable.
	run int
}

// checkPrerequisites fails fast and says exactly which seeder to run. Without
// this the ladder dies partway through with "code does not exist in the
// refdata corpus", which names the symptom and not the fix.
//
// It probes *every* code the ladder will use, not just the first of each
// list: the lists are hand-maintained against a 114-entry corpus, and a
// single wrong entry otherwise surfaces only when the ladder happens to reach
// the rung that uses it. That is precisely how FLAT_DECK — a plausible code
// that does not exist — got as far as rung 7 during development.
func (s *seeder) checkPrerequisites() error {
	probe, err := s.register("Seed Prerequisite Probe")
	if err != nil {
		return fmt.Errorf("registering probe transporter: %w", err)
	}

	var bad []string
	var firstErr error
	note := func(err error) bool {
		if err == nil {
			return false
		}
		if firstErr == nil {
			firstErr = err
		}
		return true
	}

	for _, code := range regionCodes {
		if note(s.addOperatingArea(probe, code)) {
			bad = append(bad, fmt.Sprintf("region %q", code))
		}
	}
	if len(bad) == len(regionCodes) {
		bad = []string{"the whole region corpus — run: (refdata-service) go run ./cmd/seed-regions"}
	}

	var badVehicles []string
	for i, code := range vehicleTypes {
		if note(s.addFleetAsset(probe, code, -(i + 1))) {
			badVehicles = append(badVehicles, fmt.Sprintf("vehicle type %q", code))
		}
	}
	if len(badVehicles) == len(vehicleTypes) {
		badVehicles = []string{fmt.Sprintf(
			"the whole vehicle-type corpus in context %q — run: (refdata-service) go run ./cmd/seed-vehicle-types -context %s",
			s.contextKey, s.contextKey)}
	}

	// One certificate carrying every code, not one per code: BR-TP64 allows
	// as many goods types on a certificate as you like, so a single call
	// probes the whole list and leaves one probe row behind instead of ten.
	// It costs nothing in diagnostic power — the per-code loop below still
	// runs when that call fails, and only then, to name which code is at
	// fault.
	var badGoods []string
	if _, err := s.addDocumentWithGoodsTypes(probe, goodsTypes); note(err) {
		for _, code := range goodsTypes {
			if _, err := s.addDocumentWithGoodsTypes(probe, []string{code}); note(err) {
				badGoods = append(badGoods, fmt.Sprintf("goods type %q", code))
			}
		}
	}
	if len(badGoods) == len(goodsTypes) {
		badGoods = []string{fmt.Sprintf(
			"the whole goods-type corpus in context %q — run: (refdata-service) go run ./cmd/seed-goods-types -context %s",
			s.contextKey, s.contextKey)}
	}

	// The underlying error is carried through rather than replaced by the
	// guidance: the guidance is a guess at the cause, and when it guesses
	// wrong the original message is the only thing that helps.
	bad = append(bad, badVehicles...)
	if bad = append(bad, badGoods...); len(bad) > 0 {
		return fmt.Errorf("refdata prerequisites are not satisfied:\n  - %s\nfirst underlying error: %w",
			strings.Join(bad, "\n  - "), firstErr)
	}
	return nil
}

func (s *seeder) seed(name string, r rung) error {
	id, err := s.register(name)
	if err != nil {
		return err
	}
	s.seq++

	if r.companyInfo {
		if err := s.updateCompanyInfo(id, name); err != nil {
			return err
		}
	}
	if r.operatingArea {
		if err := s.addOperatingArea(id, regionCodes[s.seq%len(regionCodes)]); err != nil {
			return err
		}
	}
	if r.fleetAsset {
		if err := s.addFleetAsset(id, vehicleTypes[s.seq%len(vehicleTypes)], s.seq); err != nil {
			return err
		}
	}
	if !r.document {
		return nil
	}
	documentID, err := s.addDocument(id)
	if err != nil {
		return err
	}
	if r.review == "" {
		return nil
	}

	if err := s.request("organization.submit-vetting", map[string]any{"id": id}, nil); err != nil {
		return err
	}
	switch r.review {
	case "reject":
		if err := s.request("document.reject", map[string]any{"id": id, "documentId": documentID}, nil); err != nil {
			return err
		}
		return s.awaitStatus(id, "Rejected")
	case "approve":
		if err := s.request("document.approve", map[string]any{
			"id": id, "documentId": documentID,
			"insurerName":            "Seed Mutual Insurance",
			"insuranceContactName":   "Seed Insurance Desk",
			"insuranceContactNumber": "+27 10 555 0100",
		}, nil); err != nil {
			return err
		}
		if err := s.awaitStatus(id, "Vetted"); err != nil {
			return err
		}
	}

	if r.trackingCreds {
		if err := s.request("tracking-credential.configure", map[string]any{
			"id": id, "provider": providers[s.seq%len(providers)],
			"credentialType": "API_KEY", "payload": fmt.Sprintf("seed-key-%04d", s.seq),
		}, nil); err != nil {
			return err
		}
	}
	if r.activate {
		if err := s.request("organization.activate", map[string]any{"id": id}, nil); err != nil {
			return err
		}
	}
	if r.suspend {
		if err := s.request("organization.suspend", map[string]any{
			"id": id, "reason": "Seeded in a suspended state for testing",
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *seeder) register(name string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := s.request("organization.register", map[string]any{"name": name, "type": "TRANSPORTER"}, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// updateCompanyInfo needs the current version — BR-TP32/34 make the update
// optimistically concurrent, and a stale version is a 409 rather than a
// silent overwrite.
func (s *seeder) updateCompanyInfo(id, name string) error {
	var current struct {
		Version int `json:"version"`
	}
	if err := s.request("organization.get", map[string]any{"id": id}, &current); err != nil {
		return err
	}
	return s.request("organization.update", map[string]any{
		"id": id, "version": current.Version, "name": name,
		"tradingAs":         strings.SplitN(name, " —", 2)[0],
		"companyName":       name + " (Pty) Ltd",
		"registrationNo":    fmt.Sprintf("20%02d/%06d/07", 10+s.seq%15, 100000+s.seq),
		"vatRegistrationNo": fmt.Sprintf("4%09d", 100000000+s.seq),
	}, nil)
}

func (s *seeder) addOperatingArea(id, code string) error {
	return s.request("operating-area.add", map[string]any{"id": id, "level": "REGION", "code": code}, nil)
}

func (s *seeder) addFleetAsset(id, vehicleType string, n int) error {
	return s.request("fleet-asset.add", map[string]any{
		"id":             id,
		"registrationNo": s.plate(n),
		"vin":            "1HGCM82633A" + s.plate(n)[2:],
		"make":           "Volvo", "model": "FH16", "vehicleTypeCode": vehicleType,
	}, nil)
}

// plate builds a registration number unique to this run. BR-TP13 makes
// registrationNo globally unique (it is the table's primary key), so a series
// that restarts from a fixed base collides with the previous run.
func (s *seeder) plate(n int) string {
	return fmt.Sprintf("CA%05d%03d", s.run, 500+n)
}

func (s *seeder) addDocument(id string) (string, error) {
	return s.addDocumentWithGoodsTypes(id, []string{
		goodsTypes[s.seq%len(goodsTypes)],
		goodsTypes[(s.seq+1)%len(goodsTypes)],
	})
}

func (s *seeder) addDocumentWithGoodsTypes(id string, codes []string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	// GOODS_IN_TRANSIT is the Transporter-specific type (BR-TP07), and the one
	// BR-TP38 derives the GIT badge from.
	if err := s.request("document.add", map[string]any{
		"id": id, "type": "GOODS_IN_TRANSIT", "reference": fmt.Sprintf("GIT-%05d", s.seq),
		"goodsTypes": codes,
	}, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// awaitStatus polls the profile until the vetting saga reaches want. The wait
// is unavoidable: BR-TP56 starts a Temporal workflow and returns immediately,
// so the profile is still DocumentsInReview when submit-vetting replies.
func (s *seeder) awaitStatus(id, want string) error {
	deadline := time.Now().Add(vettingTimeout)
	var last string
	for time.Now().Before(deadline) {
		var out struct {
			Profile struct {
				Status string `json:"status"`
			} `json:"profile"`
		}
		if err := s.request("organization.profile", map[string]any{"id": id}, &out); err != nil {
			return err
		}
		last = out.Profile.Status
		if last == want {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %s waiting for profile status %q (last seen %q) — is the vetting worker running? check `temporal task-queue describe --task-queue organizations-vetting` for pollers",
		vettingTimeout, want, last)
}

// request sends one api.* call and surfaces a micro error as a Go error. The
// service replies with a body on success and sets Nats-Service-Error on
// failure, so a caller that only unmarshals the body would read a refusal as
// success.
func (s *seeder) request(action string, in any, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	subject := fmt.Sprintf("api.%s.organizations.%s.v1", s.contextKey, action)
	reply, err := s.nc.Request(subject, body, requestTimeout)
	if err != nil {
		return fmt.Errorf("%s: %w", subject, err)
	}
	if msg := reply.Header.Get("Nats-Service-Error"); msg != "" {
		return fmt.Errorf("%s: %s (%s)", subject, msg, reply.Header.Get("Nats-Service-Error-Code"))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(reply.Data, out); err != nil {
		return fmt.Errorf("%s: decoding reply: %w", subject, err)
	}
	return nil
}
