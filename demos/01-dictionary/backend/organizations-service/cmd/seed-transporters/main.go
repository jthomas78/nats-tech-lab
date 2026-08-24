// Command seed-transporters populates a tenant with Transporters at every
// stage of the vetting lifecycle (Phase 38g), for testing and demos.
//
// It drives the same api.* NATS endpoints the browser uses, over a tenant
// credential — never Postgres. The one exception is the document bytes, and
// it is the same exception the browser has: a file cannot ride the api.* JSON
// request/reply path at all (BR-TP41), so the seeder spends the upload ticket
// its registration returned against the same /files/documents ingress the UI
// posts to. Everything else stays on NATS, which means every rung below goes
// through the real validation an operator would hit, rather than writing rows
// that could never have been created.
//
// Completeness is a deterministic ladder rather than randomness: rung N is
// always the same state, so a re-run reproduces the same data and any row's
// stage is readable from its name. -n above 10 cycles the ladder with a
// counter suffix; below 10 truncates it.
//
// Usage:
//
//	go run ./cmd/seed-transporters [-n 10] [-context acme] [-creds ../../nats/creds/acme.creds] [-files-url http://localhost:7204/files/documents]
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
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
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
	// Phase 40: every compliance document is a file, so the seeder needs the
	// HTTP ingress as well as NATS. Default is the host port from
	// demos/01-dictionary/README.md's table, so a run from a checkout needs
	// no flags.
	filesURL := flag.String("files-url", "http://localhost:7204/files/documents", "organizations-service document-file ingress")
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

	s := &seeder{nc: nc, contextKey: *contextKey, filesURL: *filesURL, run: int(time.Now().Unix() % 90000)}
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
// refdata corpus coordinates. The type keys match refdata-service's own
// seeders; platformContext is where `standards` corpora live (BR-D46) and is a
// fixed literal rather than the -context flag on purpose — see
// checkPrerequisites.
const (
	regionTypeKey   = "region"
	vehicleTypeKey  = "vehicle-type"
	goodsTypeKey    = "goods-type"
	platformContext = "_platform"
)

var (
	regionCodes  = []string{"ZA-GP", "ZA-WC", "ZA-KZN", "ZA-EC", "ZA-FS"}
	vehicleTypes = []string{"TAUTLINER", "BACK_END_TIPPER", "FLATBED", "DROPSIDE", "CAR_CARRIER"}
	goodsTypes   = []string{"GENERAL_FREIGHT", "PALLETISED_GOODS", "REFRIGERATED_FOODSTUFFS", "FRESH_PRODUCE", "DRY_BULK", "LIQUID_BULK", "HAZARDOUS_MATERIALS", "LIVESTOCK", "HIGH_VALUE_GOODS", "ABNORMAL_LOAD"}
	providers    = []string{"CARTRACK", "MIX_TELEMATICS", "WEBFLEET", "CTRACK", "NETSTAR"}
)

type seeder struct {
	nc         *nats.Conn
	contextKey string
	filesURL   string
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
// It checks *every* code the ladder will use, not just the first of each
// list: the lists are hand-maintained against a 114-entry corpus, and a
// single wrong entry otherwise surfaces only when the ladder happens to reach
// the rung that uses it. That is precisely how FLAT_DECK — a plausible code
// that does not exist — got as far as rung 7 during development.
//
// It reads the corpora rather than writing to them. The original version
// probed by registering a throwaway "Seed Prerequisite Probe" transporter and
// attaching every code to it, which answered the question but left that
// organization behind on every run — eight had accumulated in the dev tenant
// before anyone looked, three of them carrying ten single-goods-type
// certificates each, because the fallback loop that names a bad goods type
// writes one document per code. Reading costs one call per corpus, names every
// bad code in a single pass with no fallback at all, and creates nothing.
//
// One thing the write-probe covered that this does not: a region that exists
// but is missing BR-D47's `country` relation used to fail here, via BR-TP47's
// ResolveArea, and now fails at rung 3 instead. That relation is not visible
// on the browser-facing api.* item.get — only the service-to-service rpc.* one
// carries References, and this command holds a tenant credential, not a
// backend one. The trade is deliberate: what this guards is the hand-
// maintained lists above, and a corpus missing its own relations is a bug in
// seed-regions rather than a wrong entry here.
func (s *seeder) checkPrerequisites() error {
	var bad []string

	// Regions live in _platform, not the business-unit context: they are a
	// `standards` corpus (BR-D46), and refdata's item.get is an exact context
	// match with no ancestor walk, so asking for ZA-GP in "acme" finds
	// nothing however well-seeded _platform is.
	var missingRegions []string
	for _, code := range regionCodes {
		found, err := s.refdataItemExists(platformContext, regionTypeKey, code)
		if err != nil {
			return fmt.Errorf("checking region %q: %w", code, err)
		}
		if !found {
			missingRegions = append(missingRegions, fmt.Sprintf("region %q", code))
		}
	}
	if len(missingRegions) == len(regionCodes) {
		missingRegions = []string{"the whole region corpus — run: (refdata-service) go run ./cmd/seed-regions"}
	}
	bad = append(bad, missingRegions...)

	missingVehicles, err := s.missingFromCorpus(vehicleTypeKey, vehicleTypes, "vehicle type", "seed-vehicle-types")
	if err != nil {
		return err
	}
	bad = append(bad, missingVehicles...)

	missingGoods, err := s.missingFromCorpus(goodsTypeKey, goodsTypes, "goods type", "seed-goods-types")
	if err != nil {
		return err
	}
	bad = append(bad, missingGoods...)

	if len(bad) > 0 {
		return fmt.Errorf("refdata prerequisites are not satisfied:\n  - %s", strings.Join(bad, "\n  - "))
	}
	return nil
}

// missingFromCorpus lists typeKey's corpus in the seeder's own context once
// and reports which of want is absent from it. Listing beats one item.get per
// code for the context-scoped corpora: it is a single call whatever the list
// length, and an empty corpus is then distinguishable from a list of wrong
// codes — the two need different advice, and only the first is worth naming a
// seeder for.
func (s *seeder) missingFromCorpus(typeKey string, want []string, label, seederCmd string) ([]string, error) {
	have, err := s.refdataCodes(s.contextKey, typeKey)
	if err != nil {
		return nil, fmt.Errorf("listing the %s corpus: %w", label, err)
	}
	if len(have) == 0 {
		return []string{fmt.Sprintf(
			"the whole %s corpus in context %q — run: (refdata-service) go run ./cmd/%s -context %s",
			label, s.contextKey, seederCmd, s.contextKey)}, nil
	}
	var missing []string
	for _, code := range want {
		if !have[code] {
			missing = append(missing, fmt.Sprintf("%s %q", label, code))
		}
	}
	return missing, nil
}

// refdataCodes returns the active codes in one context's corpus, as a set.
// all is left false so deprecated items are excluded (BR-D06): a code the
// ladder uses must be one an operator could still pick today, and a
// deprecated one would be accepted here and refused at the rung.
func (s *seeder) refdataCodes(contextKey, typeKey string) (map[string]bool, error) {
	data, err := s.refdataRequest(contextKey, "type.list", map[string]any{
		"typeKey": typeKey, "locale": "en", "all": false,
	})
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []struct {
			Item struct {
				Code string `json:"code"`
			} `json:"item"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	codes := make(map[string]bool, len(out.Items))
	for _, entry := range out.Items {
		codes[entry.Item.Code] = true
	}
	return codes, nil
}

func (s *seeder) refdataItemExists(contextKey, typeKey, code string) (bool, error) {
	_, err := s.refdataRequest(contextKey, "item.get", map[string]any{
		"typeKey": typeKey, "code": code, "locale": "en",
	})
	if errors.Is(err, errRefdataNotFound) {
		return false, nil
	}
	return err == nil, err
}

// errRefdataNotFound separates "refdata answered, and the code is not there"
// from "the call failed". Only the first is a prerequisite finding; the second
// must abort rather than be reported as a missing code, or a NATS timeout
// would print a confident list of ten perfectly good codes as though they were
// all wrong.
var errRefdataNotFound = errors.New("refdata: item not found")

// refdataRequest sends one api.* call to refdata-service and returns the raw
// reply body. It is a sibling of request rather than an argument to it because
// the two differ in all three things that matter: the service token in the
// subject, the context (standards corpora are read from _platform, not the
// seeder's own), and how a refusal arrives — organizations-service sets
// Nats-Service-Error, while refdata answers not-found with a normal reply
// carrying {"error":…,"notFound":true}. A decoder that only knew the header
// form would read that as success.
func (s *seeder) refdataRequest(contextKey, action string, in any) ([]byte, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	subject := fmt.Sprintf("api.%s.refdata.%s.v1", contextKey, action)
	reply, err := s.nc.Request(subject, body, requestTimeout)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", subject, err)
	}
	// Not-found arrives in *both* shapes and neither is redundant: the micro
	// framework sets the header with a 404 code, and the body carries
	// {"error":…,"notFound":true}. Checking the header first without reading
	// the code turns a missing region into a hard abort instead of a
	// prerequisite finding — which is exactly what it did on first run here.
	if code := reply.Header.Get("Nats-Service-Error-Code"); code == "404" {
		return nil, errRefdataNotFound
	}
	if msg := reply.Header.Get("Nats-Service-Error"); msg != "" {
		return nil, fmt.Errorf("%s: %s (%s)", subject, msg, reply.Header.Get("Nats-Service-Error-Code"))
	}
	var refusal struct {
		Error    string `json:"error"`
		NotFound bool   `json:"notFound"`
	}
	if err := json.Unmarshal(reply.Data, &refusal); err == nil && refusal.Error != "" {
		if refusal.NotFound {
			return nil, errRefdataNotFound
		}
		return nil, fmt.Errorf("%s: %s", subject, refusal.Error)
	}
	return reply.Data, nil
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

// addDocument gives each certificate two goods types rather than one, walking
// the list by rung so the cohort spreads across the corpus. Two is the point:
// BR-TP64 allows any number, and a seeded set where every certificate carried
// exactly one code would let a single-value regression through the UI unnoticed.
func (s *seeder) addDocument(id string) (string, error) {
	codes := []string{
		goodsTypes[s.seq%len(goodsTypes)],
		goodsTypes[(s.seq+1)%len(goodsTypes)],
	}
	// Phase 40: registration is a file drop, so the seeder registers and then
	// spends the ticket the reply carries. The stub PDF is deliberately a
	// real, if minimal, PDF — a seeded row whose bytes no viewer can open
	// would make the download path untestable from seeded data.
	var out struct {
		Document struct {
			ID string `json:"id"`
		} `json:"document"`
		Ticket string `json:"ticket"`
	}
	documentName := fmt.Sprintf("GIT-%05d.pdf", s.seq)
	// GOODS_IN_TRANSIT is the Transporter-specific type (BR-TP07), and the one
	// BR-TP38 derives the GIT badge from.
	if err := s.request("document.git-register", map[string]any{
		"id": id, "documentName": documentName, "goodsTypes": codes,
	}, &out); err != nil {
		return "", err
	}
	if err := s.uploadStub(out.Ticket, documentName); err != nil {
		return "", err
	}
	return out.Document.ID, nil
}

// stubPDF is the smallest thing a PDF reader will open. The bytes matter only
// in that they are non-empty (BR-TP44 refuses a zero-length upload) and that
// their content type is honest.
const stubPDF = "%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF\n"

// uploadStub spends one upload ticket. A ticket is single-use (BR-TP41), so a
// failure here is terminal for that rung rather than retryable — which is why
// it aborts the seed instead of warning.
func (s *seeder) uploadStub(ticket, documentName string) error {
	req, err := http.NewRequest(http.MethodPost, s.filesURL, bytes.NewBufferString(stubPDF))
	if err != nil {
		return err
	}
	req.Header.Set("X-Document-Ticket", ticket)
	req.Header.Set("X-Document-Name", url.QueryEscape(documentName))
	req.Header.Set("Content-Type", "application/pdf")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload %s: %w (is -files-url reachable?)", documentName, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		body := make([]byte, 512)
		n, _ := resp.Body.Read(body)
		return fmt.Errorf("upload %s: %s: %s", documentName, resp.Status, strings.TrimSpace(string(body[:n])))
	}
	return nil
}

// awaitStatus polls the profile until the vetting saga reaches want. The wait
// is unavoidable: BR-TP56 starts a Temporal workflow and returns immediately,
// so the profile is still InReview when submit-vetting replies.
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
