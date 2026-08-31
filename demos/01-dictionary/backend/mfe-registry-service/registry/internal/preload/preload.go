// Package preload reads operator-mounted configuration and drives the domain's
// preload plan through the ordinary revisioned application write path.
package preload

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

type Service interface {
	Curated(context.Context) (domain.Document, error)
	Apply(context.Context, domain.Write) (domain.Document, error)
	Allowlist() domain.Allowlist
}

// Run permits an unset path. File/read/whole-document errors fail boot;
// per-entry refusals are logged and never prevent the remaining seeds.
func Run(ctx context.Context, path string, svc Service, log *slog.Logger) (domain.PreloadResult, error) {
	var result domain.PreloadResult
	if path == "" {
		return result, nil
	}
	if log == nil {
		log = slog.Default()
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return result, fmt.Errorf("registry preload: %w", err)
	}
	file, err := domain.ParsePreload(raw)
	if err != nil {
		return result, fmt.Errorf("registry preload %q: %w", path, err)
	}
	withhold := func(refused domain.PreloadRefusal) {
		result.Withheld = append(result.Withheld, refused)
		log.Warn("registry preload: withheld", "id", refused.ID, "cause", refused.Cause)
	}
	for _, entry := range file.Plugins {
		// Read AND re-plan each entry, including repeated ids. Reading only a
		// new revision could let an old plan overwrite a concurrent curation.
		doc, err := svc.Curated(ctx)
		if err != nil {
			return result, fmt.Errorf("registry preload: read curated: %w", err)
		}
		plan := domain.PlanPreload(doc.Entries, []domain.Entry{entry}, svc.Allowlist())
		result.Skipped = append(result.Skipped, plan.Skipped...)
		for _, refused := range plan.Withheld {
			withhold(refused)
		}
		for _, seed := range plan.Seed {
			if _, err := svc.Apply(ctx, domain.PreloadWrite(seed, doc.Revision)); err != nil {
				withhold(domain.PreloadRefusal{ID: seed.ID, Cause: err})
				continue
			}
			result.Seed = append(result.Seed, seed)
			log.Info("registry preload: seeded entry", "id", seed.ID)
		}
	}
	log.Info("registry preload complete", "seeded", len(result.Seed), "skipped", len(result.Skipped), "withheld", len(result.Withheld))
	return result, nil
}
