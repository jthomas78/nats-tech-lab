// Package notifycoverage holds BR-049's checked convention: nothing in the
// backend tree publishes notify.* outside the seam.
//
// It used to check something else. Phase 43a instrumented notify.* at each
// call site, because there was no seam to instrument, so BR-049 was
// "every publish site emits an observation or is on a documented exclusion
// list" and this scan walked the tree looking for uninstrumented ones.
// Phase 43d gave notify.* the seam evt.* already had, and that premise died
// with it: a shared/natsnotify.Notifier publishes and observes as one step,
// so a site cannot be half-wired.
//
// What survives is the guard, which is the part that was ever load-bearing.
// A future publisher that doesn't know the seam exists can still write
// nc.Publish("notify."+ctx+...) inline, and it would be invisible in the
// Messages panel with nothing saying why — the exact failure ADR-047 named
// as its headline cost. So the scan now asserts the two structural
// properties that keep the seam the only path:
//
//  1. notify.* subject literals appear only in subject-builder files.
//  2. No function both names a notify.* subject and publishes.
//
// It lives in shipping-service because BR-049 does, and reads the whole
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

// subjectBuilders maps a path relative to backendRoot to why that file is
// allowed to name notify.* subjects. Each is a service's own subject-builder
// layer, or a place that names the subjects without publishing them at all.
//
// shared/natsnotify is deliberately absent: it publishes every notify.* in
// the tree and knows none of their names. That split is the design — arities
// in this family run from four tokens to unbounded, so there is no grammar a
// shared module could build against, and the observability tokens come from
// whoever built the subject rather than from parsing it back apart.
var subjectBuilders = map[string]string{
	"shipping-service/internal/notify/notify.go": "" +
		"shipping's subject-builder layer — five shapes across four calling " +
		"packages, including the two whose tokens do not sit where a " +
		"positional reader would look (Raw's literal \"raw\", and the refdata " +
		"bridge's _platform context).",

	"refdata-service/refdata/internal/notifybridge/notifybridge.go": "" +
		"refdata's one shape, built where the bridge has just parsed the " +
		"context and type key out of the evt.* subject. The fan-out that " +
		"publishes it holds one Notifier per tenant, per BR-D45.",

	"accounts-service/accounts/handler.go": "" +
		"accounts' one shape. Four tokens and no {context} at all — this " +
		"service administers the tenant axis rather than operating inside " +
		"one — which is below natstrace.ObservePublish's floor and the " +
		"clearest case for naming tokens rather than deriving them.",

	"observability-service/observability/internal/tracestore/tracestore.go": "" +
		"its own internal KV-change plumbing for the trace-request-reply " +
		"bucket. Its Notifier is built without WithObservation: BR-045 names " +
		"this excluded, and the gate is now what says so.",

	"observability-service/observability/internal/pubsubstore/pubsubstore.go": "" +
		"same internal-plumbing reasoning, and additionally unsafe to " +
		"observe: this notify announces a write to the very bucket " +
		"obs.pubsub.* envelopes are stored in, so observing it would be an " +
		"unbounded feedback loop rather than a gap in coverage. Its Notifier " +
		"is built without WithObservation for that reason.",

	"accounts-service/auth/token.go": "" +
		"names notify.* subjects in JWT permission grants — subscribe " +
		"allow-lists, not publishes.",

	"mfe-registry-service/registry/internal/notify/notify.go": "" +
		"the registry's subject-builder layer, extracted from its application " +
		"service when the context left accounts-service. One shape, four " +
		"tokens, _platform context — the subject is platform-wide because " +
		"the catalog is.",

	"accounts-service/accounts/provisioner.go": "" +
		"names notify.accounts.account.* as a JetStream export subject, not " +
		"a publish.",
}

type finding struct {
	file string
	fn   string // "" when the literal is at file scope
}

// scan walks the backend tree and reports, per file, whether it names a
// notify.* subject, and every function that both names one and publishes.
func scan(t *testing.T) (files map[string]bool, publishers []finding) {
	t.Helper()
	files = map[string]bool{}

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
		rel = filepath.ToSlash(rel)

		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if ok && lit.Kind == token.STRING && strings.HasPrefix(lit.Value, `"notify.`) {
				files[rel] = true
			}
			return true
		})

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			var namesSubject, publishes bool
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.BasicLit:
					if node.Kind == token.STRING && strings.HasPrefix(node.Value, `"notify.`) {
						namesSubject = true
					}
				case *ast.CallExpr:
					sel, ok := node.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					// Case-insensitive: an unexported publishFoo helper that
					// takes the subject is the shape this is looking for.
					if strings.HasPrefix(strings.ToLower(sel.Sel.Name), "publish") {
						publishes = true
					}
				}
				return true
			})
			if namesSubject && publishes {
				publishers = append(publishers, finding{file: rel, fn: fn.Name.Name})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the backend tree: %v", err)
	}
	return files, publishers
}

func TestNotifySubjectsAreNamedOnlyInSubjectBuilders(t *testing.T) {
	files, _ := scan(t)
	if len(files) == 0 {
		t.Fatal("found no notify.* subject literals at all — the scan is broken, not the tree")
	}
	for file := range files {
		if _, ok := subjectBuilders[file]; !ok {
			t.Errorf("%s names a notify.* subject but is not a subject-builder file.\n"+
				"Build the subject in this service's subject-builder layer and publish it through "+
				"shared/natsnotify (see BUSINESS_RULES-SHIPPING.md's BR-049), or add this file to "+
				"`subjectBuilders` above with a reason. A publisher outside the seam is invisible in "+
				"the Admin UI's Messages panel with nothing saying why.", file)
		}
	}
}

func TestNoFunctionBothNamesANotifySubjectAndPublishesIt(t *testing.T) {
	// The bypass this exists to catch, and the one an allow-listed file could
	// still commit: `subject := "notify." + ctx + ...` a few lines above an
	// `nc.Publish(subject, data)`. Building and publishing in one function is
	// precisely the shape Phase 43d removed — the tokens get concatenated
	// away and then guessed at again downstream.
	_, publishers := scan(t)
	for _, p := range publishers {
		t.Errorf("%s:%s both names a notify.* subject and publishes it.\n"+
			"Split them: the subject (and its natsnotify.Tokens) belongs in this service's "+
			"subject-builder layer, and the publish belongs to a shared/natsnotify.Notifier.", p.file, p.fn)
	}
}

func TestSubjectBuilderListHasNoStaleEntries(t *testing.T) {
	files, _ := scan(t)
	for file, reason := range subjectBuilders {
		if !files[file] {
			t.Errorf("subject-builder entry %q no longer names any notify.* subject — delete it (reason on file: %s)", file, reason)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("subject-builder entry %q carries no reason", file)
		}
	}
}
