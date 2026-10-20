package otelnats_test

// Phase 57, decision 9: the dependency guard. Service code reaches OTel only
// through the otelnats wrappers. This spec scans the demo 01 backend modules
// and shared/ and fails on:
//   - a non-test .go file outside the allowlist that imports any
//     go.opentelemetry.io/ package; or
//   - a non-test file outside shared/otelnats that calls the low-level
//     StartServer / StartClient / StartProducer / StartConsumer, or
//     otelnats.Inject / otelnats.Extract. The import rule alone would miss a
//     file that ends a span without importing OTel (notifybridge.go did).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const otelnatsImport = "github.com/jthomas78/nats-tech-lab/shared/otelnats"

// guardAllowlist holds the directories, relative to the repo root, whose
// non-test files may import OTel and call the low-level API. Each needs a
// reason.
var guardAllowlist = map[string]string{
	"shared/otelnats": "the wrapper package itself: the one place OTel is used",
	"demos/01-dictionary/backend/observability-service/cmd/otel-stackcheck": "a CLI that checks the OTel stack end to end; it starts its own spans on purpose and is not a service",
}

// lowLevelMethods are the Provider methods only shared/otelnats may call.
var lowLevelMethods = map[string]bool{
	"StartServer": true, "StartClient": true, "StartProducer": true, "StartConsumer": true,
}

// lowLevelFuncs are the package functions only shared/otelnats may call.
var lowLevelFuncs = map[string]bool{"Inject": true, "Extract": true}

// guardViolations returns one line per rule the file breaks.
func guardViolations(fset *token.FileSet, file *ast.File) []string {
	var out []string
	otelnatsName := ""
	for _, imp := range file.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if strings.HasPrefix(path, "go.opentelemetry.io/") {
			out = append(out, "imports "+path)
		}
		if path == otelnatsImport {
			otelnatsName = "otelnats"
			if imp.Name != nil {
				otelnatsName = imp.Name.Name
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := sel.Sel.Name
		if lowLevelMethods[name] {
			out = append(out, fset.Position(call.Pos()).String()+": calls "+name)
		}
		if lowLevelFuncs[name] && otelnatsName != "" {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == otelnatsName {
				out = append(out, fset.Position(call.Pos()).String()+": calls otelnats."+name)
			}
		}
		return true
	})
	return out
}

// repoRoot is two levels above this package (shared/otelnats).
func repoRoot() string {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	Expect(err).NotTo(HaveOccurred())
	return root
}

var _ = Describe("Decision 9: the dependency guard", func() {
	It("finds no OTel import or low-level call outside the allowlist", func() {
		root := repoRoot()
		scanRoots := []string{
			filepath.Join(root, "shared"),
			filepath.Join(root, "demos", "01-dictionary", "backend"),
		}
		var violations []string
		scanned := 0
		for _, dir := range scanRoots {
			err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					if d.Name() == "node_modules" || d.Name() == "vendor" || strings.HasPrefix(d.Name(), ".") {
						return filepath.SkipDir
					}
					return nil
				}
				if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				rel, _ := filepath.Rel(root, path)
				rel = filepath.ToSlash(rel)
				for allowed := range guardAllowlist {
					if strings.HasPrefix(rel, allowed+"/") {
						return nil
					}
				}
				src, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				fset := token.NewFileSet()
				file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
				if err != nil {
					return err
				}
				scanned++
				for _, v := range guardViolations(fset, file) {
					violations = append(violations, rel+": "+v)
				}
				return nil
			})
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(scanned).To(BeNumerically(">", 100), "the scan must reach the service code, or it passes for nothing")
		Expect(violations).To(BeEmpty(), "use otelnats.Call / Consume / Produce, or add an allowlist entry with a reason")
	})

	It("has a reason for every allowlist entry, and every entry exists", func() {
		root := repoRoot()
		for dir, reason := range guardAllowlist {
			Expect(reason).NotTo(BeEmpty(), dir)
			_, err := os.Stat(filepath.Join(root, filepath.FromSlash(dir)))
			Expect(err).NotTo(HaveOccurred(), "allowlisted %s is gone; remove its entry", dir)
		}
	})

	It("flags each kind of violation, so it cannot pass by not looking", func() {
		src := `package svc

import (
	"context"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/codes"
	tel "github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

func f(ctx context.Context, p *tel.Provider, msg *nats.Msg) {
	ctx, span := p.StartConsumer(ctx, msg, tel.Trust{})
	defer span.End()
	span.SetStatus(codes.Error, "x")
	tel.Inject(ctx, msg)
	_ = tel.Extract(ctx, msg.Header)
	_, _ = p.StartClient(ctx, "s", "peer")
}
`
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "fixture.go", src, parser.SkipObjectResolution)
		Expect(err).NotTo(HaveOccurred())
		got := strings.Join(guardViolations(fset, file), "\n")
		Expect(got).To(ContainSubstring("imports go.opentelemetry.io/otel/codes"))
		Expect(got).To(ContainSubstring("calls StartConsumer"))
		Expect(got).To(ContainSubstring("calls StartClient"))
		Expect(got).To(ContainSubstring("calls otelnats.Inject"))
		Expect(got).To(ContainSubstring("calls otelnats.Extract"))
	})

	It("does not flag the wrappers", func() {
		src := `package svc

import (
	"context"

	"github.com/nats-io/nats.go"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

func f(ctx context.Context, p *otelnats.Provider, msg *nats.Msg) error {
	return p.Produce(ctx, msg, func(context.Context) error { return nil })
}
`
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "fixture.go", src, parser.SkipObjectResolution)
		Expect(err).NotTo(HaveOccurred())
		Expect(guardViolations(fset, file)).To(BeEmpty())
	})
})
