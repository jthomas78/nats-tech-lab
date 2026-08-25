// Package notifycoverage holds BR-049's checked convention: every notify.*
// publisher in the backend tree is either instrumented with an obs.pubsub.*
// observation (BR-045) or on a documented exclusion list.
//
// ADR-047's headline cost is that an uninstrumented publisher is silently
// invisible — the Messages panel shows nothing and nothing anywhere says why.
// BR-045's placement rule discharges half of that structurally: evt.* is
// instrumented in each service's seam, so a new evt.* publisher is observed by
// construction. notify.* has no seam — its publishes are scattered bare
// nc.Publish calls — so this scan is the other half. A new notify.* publisher
// that is neither instrumented nor excluded fails here rather than shipping as
// a silent gap.
//
// It lives in shipping-service because BR-049 does, and it reads the whole
// backend tree rather than one module: the property is cross-service, and a
// per-module copy would be four places to forget.
package notifycoverage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// backendRoot is demos/01-dictionary/backend — three levels up from this
// package (internal/notifycoverage → internal → shipping-service → backend).
const backendRoot = "../../.."

// observeFuncs are the emit primitives that count as instrumenting a
// publisher: natstrace's package-level helpers for bare call sites, and the
// Tracer methods the evt.* seams use.
var observeFuncs = map[string]bool{
	"Observe": true, "ObserveAs": true,
	"ObservePublish": true, "ObservePublishAs": true,
}

// excluded maps "<path relative to backendRoot>:<func>" to why that notify.*
// publisher carries no observation. Two kinds live here, and the distinction
// matters:
//
//   - Instrumented elsewhere — the literal and the publish are in different
//     functions, so the scan cannot see the emit from here.
//   - Genuinely excluded — observing it would be wrong, not merely unwired.
//
// Anything added here needs a reason in this table, not just an entry.
var excluded = map[string]string{
	// ── instrumented elsewhere ──
	"refdata-service/refdata/internal/notifybridge/notifybridge.go:Run": "" +
		"builds the subject and calls the PublishToAll port; the port's only " +
		"implementation (refdata-service/refdata/composition.go's " +
		"tenantPublisher) observes each per-tenant publish, which is where " +
		"the tenant's own *nats.Conn is — see BR-D45.",

	"accounts-service/accounts/handler.go:publishAccountCreated":         accountsSharedPublisher,
	"accounts-service/accounts/handler.go:publishAccountSuspended":       accountsSharedPublisher,
	"accounts-service/accounts/handler.go:publishAccountReactivated":     accountsSharedPublisher,
	"accounts-service/accounts/handler.go:publishAccountJSLimitsUpdated": accountsSharedPublisher,

	// ── genuinely excluded ──
	"observability-service/observability/internal/tracestore/tracestore.go:publishNotify": "" +
		"this service's internal KV-change plumbing for its own " +
		"trace-request-reply bucket, not a domain event (BR-045's named " +
		"exclusion). Note it is a copy of shipping's kvstore helper, which IS " +
		"instrumented: the original carries domain KV changes, the copy does not.",
	"observability-service/observability/internal/pubsubstore/pubsubstore.go:publishNotify": "" +
		"same internal-plumbing reasoning as tracestore's, and additionally " +
		"unsafe to observe: this notify announces a write to the very bucket " +
		"obs.pubsub.* envelopes are stored in, so observing it would publish " +
		"an obs.pubsub.* message that is ingested, stored, notified and " +
		"observed again — an unbounded feedback loop, not a gap in coverage.",
}

// accountsSharedPublisher covers BR-AC34's four lifecycle notifies: each names
// its own subject and hands it to the shared publishAccountEvent, which is
// where the observation is emitted (and where Phase 43a moved it off
// obs.trace.*). That indirection is a seam, not a gap — a fifth lifecycle
// event added the same way is observed by construction, and
// accounts/pubsub_export_test.go asserts one obs.pubsub.* envelope per subject
// for all four.
const accountsSharedPublisher = "names its subject and delegates to accounts/handler.go's " +
	"publishAccountEvent, which emits the observation for all four (BR-AC34)."

type publisher struct {
	key      string
	observed bool
}

// findNotifyPublishers reports every function that both mentions a notify.*
// subject literal and publishes — the working definition of "a notify.*
// publish site" — along with whether that same function emits an observation.
//
// Function-scoped rather than call-scoped on purpose: the common shape in this
// tree is `subject := "notify." + ctx + ...` several lines above a
// `PublishMsg(msg)` that names neither, so anchoring on the call's arguments
// would see almost none of them.
func findNotifyPublishers(t *testing.T) []publisher {
	t.Helper()
	var found []publisher

	err := filepath.Walk(backendRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "vendor" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		rel, relErr := filepath.Rel(backendRoot, path)
		if relErr != nil {
			return relErr
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			var hasNotifyLiteral, publishes, observes bool
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.BasicLit:
					if node.Kind == token.STRING && strings.HasPrefix(node.Value, `"notify.`) {
						hasNotifyLiteral = true
					}
				case *ast.CallExpr:
					sel, ok := node.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					switch {
					case observeFuncs[sel.Sel.Name]:
						observes = true
					case strings.HasPrefix(strings.ToLower(sel.Sel.Name), "publish"):
						// Case-insensitive on purpose. accounts-service's four
						// lifecycle notifies name their subject at the call
						// site and hand it to an unexported publishAccountEvent
						// — matching only exported Publish* would leave all
						// four invisible to this scan, which is the failure
						// mode it exists to catch.
						publishes = true
					}
				}
				return true
			})
			if hasNotifyLiteral && publishes {
				found = append(found, publisher{key: filepath.ToSlash(rel) + ":" + fn.Name.Name, observed: observes})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the backend tree: %v", err)
	}
	return found
}

func TestEveryNotifyPublisherIsInstrumentedOrExcluded(t *testing.T) {
	publishers := findNotifyPublishers(t)
	if len(publishers) == 0 {
		t.Fatal("found no notify.* publishers at all — the scan is broken, not the tree")
	}

	for _, p := range publishers {
		if p.observed {
			if _, isExcluded := excluded[p.key]; isExcluded {
				t.Errorf("%s is on the exclusion list but emits an observation — remove the entry or the emit, and say which is right", p.key)
			}
			continue
		}
		if _, isExcluded := excluded[p.key]; !isExcluded {
			t.Errorf("%s publishes notify.* without an obs.pubsub.* observation.\n"+
				"Either instrument it (natstrace.Observe / ObserveAs, after the publish succeeds — "+
				"see BUSINESS_RULES-SHIPPING.md's BR-045) or add it to `excluded` above with a reason. "+
				"An uninstrumented publisher is invisible in the Admin UI's Messages panel with nothing "+
				"saying why, which is exactly what BR-049 exists to prevent.", p.key)
		}
	}
}

func TestExclusionListHasNoStaleEntries(t *testing.T) {
	live := map[string]bool{}
	for _, p := range findNotifyPublishers(t) {
		live[p.key] = true
	}
	for key, reason := range excluded {
		if !live[key] {
			t.Errorf("exclusion %q no longer matches any notify.* publisher — delete it (reason on file: %s)", key, reason)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("exclusion %q carries no reason", key)
		}
	}
}
