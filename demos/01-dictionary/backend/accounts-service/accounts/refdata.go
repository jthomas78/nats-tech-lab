package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// PlatformContext and DefaultBUTemplateContext mirror refdata-service's own
// seed constants (refdata/seed.go).
//
// DefaultBUTemplateContext is no longer any tenant's context (that was Phase
// 22, where one shared value meant two tenants writing the same
// `(context, type_key, code)` rows). It is now the platform-owned template that
// every account's own `{tenant}-default` is parented to, which is what makes
// its reserved `_` prefix accurate rather than an exception granted by fiat —
// see BR-AC29 and refdata-service's BR-D38.
const (
	PlatformContext          = "_platform"
	DefaultBUTemplateContext = "_default_bu"
)

// defaultBULocales are registered explicitly on every tenant default.
//
// This is not belt-and-braces. refdata-service inherits an ancestor's items and
// localizations by flattening them into the child's corpus, but
// `dictionary_locales` is not part of that flattening — it is read on the flat
// `WHERE context = $1` path. A context with no locales of its own has no
// effective default locale, so label resolution silently returns nothing while
// every other signal says the context is healthy. seed.go registers exactly
// these three on `_platform` and `_default_bu` for the same reason.
var defaultBULocales = []struct {
	Locale    string
	IsDefault bool
}{
	{"en", true},
	{"es", false},
	{"af-za", false},
}

// RefdataClient is accounts-service's writer-side view of refdata-service.
//
// accounts-service owns which business units exist (Phase 22); refdata-service
// owns what a context *contains*. Everything accounts-service needs to say to
// it goes through here rather than through hand-rolled requests at each call
// site, which is what let the two pre-existing copies of the register call
// drift apart.
type RefdataClient struct {
	BaseURL string
	Log     *slog.Logger
	// HTTP is optional; http.DefaultClient is used when nil.
	HTTP *http.Client
}

// ContextRegistration is the body of refdata-service's context register
// endpoint. Context and Name are distinct values (BR-AC26) — before Phase 22b
// accounts-service sent the slug for both, which is why every context in the
// admin UI displayed as its own subject token.
type ContextRegistration struct {
	Context     string `json:"context"`
	Parent      string `json:"parent"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Tenant      string `json:"tenant"`
}

func (c *RefdataClient) configured(action, contextKey string) bool {
	if c == nil || c.BaseURL == "" {
		if c != nil && c.Log != nil {
			c.Log.Warn("REFDATA_URL not set — skipping "+action, "context", contextKey)
		}
		return false
	}
	return true
}

// do issues one request and folds refdata-service's response into an error.
// okStatuses lists what counts as success; 409 is always treated as success
// because every write this client makes is meant to be idempotent — a re-run of
// startup seeding must not be an error.
func (c *RefdataClient) do(ctx context.Context, method, path string, body any, okStatuses ...int) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return nil
	}
	for _, ok := range okStatuses {
		if resp.StatusCode == ok {
			return nil
		}
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("refdata %s %s returned %d: %s", method, path, resp.StatusCode, b)
}

// RegisterContext creates the refdata-service context backing a business unit.
func (c *RefdataClient) RegisterContext(ctx context.Context, in ContextRegistration) error {
	if !c.configured("context registration", in.Context) {
		return nil
	}
	return c.do(ctx, http.MethodPost, "/api/refdata/admin/contexts", in,
		http.StatusCreated, http.StatusOK)
}

// SetContextVisible toggles a context's visibility (BR-AC17).
func (c *RefdataClient) SetContextVisible(ctx context.Context, contextKey string, visible bool) error {
	if !c.configured("context visibility update", contextKey) {
		return nil
	}
	return c.do(ctx, http.MethodPatch,
		"/api/refdata/admin/contexts/"+contextKey+"/visible",
		map[string]bool{"visible": visible},
		http.StatusNoContent, http.StatusOK)
}

// AddLocale registers one locale against a context.
func (c *RefdataClient) AddLocale(ctx context.Context, contextKey, locale string, isDefault bool) error {
	if !c.configured("locale registration", contextKey) {
		return nil
	}
	return c.do(ctx, http.MethodPost, "/api/refdata/admin/locales", map[string]any{
		"context":   contextKey,
		"locale":    locale,
		"isDefault": isDefault,
	}, http.StatusCreated, http.StatusOK, http.StatusNoContent)
}

// CreateDraft opens a corpus draft, which is the step that actually walks the
// ancestor chain and flattens inherited items into this context.
func (c *RefdataClient) CreateDraft(ctx context.Context, contextKey string) error {
	if !c.configured("corpus draft", contextKey) {
		return nil
	}
	return c.do(ctx, http.MethodPost,
		"/api/refdata/admin/corpus/"+contextKey+"/draft", map[string]string{
			"notes": "initial inherited corpus (accounts-service, BR-AC29)",
		}, http.StatusCreated, http.StatusOK)
}

// PublishCorpus publishes the open draft.
func (c *RefdataClient) PublishCorpus(ctx context.Context, contextKey string) error {
	if !c.configured("corpus publish", contextKey) {
		return nil
	}
	return c.do(ctx, http.MethodPost,
		"/api/refdata/admin/corpus/"+contextKey+"/publish", nil,
		http.StatusOK, http.StatusCreated)
}

// HasPublishedCorpus reports whether contextKey has at least one published
// corpus version.
func (c *RefdataClient) HasPublishedCorpus(ctx context.Context, contextKey string) (bool, error) {
	if !c.configured("corpus version lookup", contextKey) {
		return false, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/api/refdata/admin/corpus/"+contextKey+"/versions", nil)
	if err != nil {
		return false, err
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("refdata corpus versions returned %d", resp.StatusCode)
	}
	var parsed struct {
		Versions []struct {
			Status string `json:"status"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return false, err
	}
	for _, v := range parsed.Versions {
		if v.Status == "published" {
			return true, nil
		}
	}
	return false, nil
}

// ProvisionDefaultContext creates and populates an account's default business
// unit context (BR-AC29): registered under the platform template, given its own
// locales, then drafted and published so it inherits the platform corpus.
//
// WaitForPublishedAncestor gates the whole sequence, not just the draft step.
// refdata-service skips an ancestor with no published corpus *silently* — a
// draft created too early inherits nothing, publishes successfully, and leaves
// a context that looks provisioned and resolves to an empty dictionary. And on
// a cold `docker compose up`, accounts-service and refdata-service start as
// independent containers with no ordering guarantee between them — the very
// first call below can just as easily hit "connection refused" as it can hit
// "context not found yet", so the wait has to tolerate both, not only poll a
// count once a connection already succeeds.
func (c *RefdataClient) ProvisionDefaultContext(ctx context.Context, tenant, slug string) error {
	if !c.configured("default context provisioning", slug) {
		return nil
	}
	if err := c.WaitForPublishedAncestor(ctx, DefaultBUTemplateContext); err != nil {
		return err
	}
	if err := c.RegisterContext(ctx, ContextRegistration{
		Context:     slug,
		Parent:      DefaultBUTemplateContext,
		Name:        DefaultBUName,
		Description: "Default business unit for " + tenant,
		Tenant:      tenant,
	}); err != nil {
		return err
	}
	for _, l := range defaultBULocales {
		if err := c.AddLocale(ctx, slug, l.Locale, l.IsDefault); err != nil {
			return err
		}
	}
	// BR-AC29: the draft/publish half runs once, not once per boot.
	//
	// RegisterContext and AddLocale above are genuinely idempotent (409 folds
	// to success in do()), so reasserting them every startup is free and
	// self-heals a dropped locale. CreateDraft + PublishCorpus are not: they
	// mint a *new* corpus version every time, and every published version
	// gets its own KV bucket, which is its own JetStream stream. Seven
	// restarts on 2026-08-20 produced acme-default v2-v8 and globex-default
	// v2-v8 — all byte-identical to v1 — and exhausted the platform account's
	// MaxStreams, after which publish failed with err_code=10027 for every
	// context. refdata-service's own retention window (BR-D49) bounds the
	// bucket count, but the version churn itself is this side's bug.
	//
	// Deliberately a "has any published corpus" check rather than a content
	// comparison against the template: this call is bootstrap, not
	// reconciliation. A context that has been published — by us on a previous
	// boot, or by an operator since — is somebody's current state, and a
	// startup path has no business republishing over it.
	published, err := c.HasPublishedCorpus(ctx, slug)
	if err != nil {
		return err
	}
	if published {
		if c.Log != nil {
			c.Log.Info("default context already has a published corpus — skipping draft/publish",
				"context", slug, "tenant", tenant)
		}
		return nil
	}
	if err := c.CreateDraft(ctx, slug); err != nil {
		return err
	}
	return c.PublishCorpus(ctx, slug)
}

// WaitForPublishedAncestor polls until ancestorContext has a published corpus,
// so a draft created below it inherits a populated ancestor rather than
// silently nothing — see ProvisionDefaultContext's comment on why this also
// has to double as "wait until refdata-service is reachable at all" on a cold
// start. Every error is treated as "not ready yet" rather than distinguished
// from "not published yet": a dial failure and an empty corpus list both mean
// the same thing to a caller about to draft against this ancestor.
func (c *RefdataClient) WaitForPublishedAncestor(ctx context.Context, ancestorContext string) error {
	if !c.configured("ancestor readiness wait", ancestorContext) {
		return nil
	}
	const attempts = 30
	for i := range attempts {
		published, err := c.HasPublishedCorpus(ctx, ancestorContext)
		if err == nil && published {
			return nil
		}
		if i == attempts-1 {
			if err != nil {
				return fmt.Errorf("waiting for %s corpus: %w", ancestorContext, err)
			}
			return fmt.Errorf("%s has no published corpus after %d attempts — a draft below it would inherit nothing", ancestorContext, attempts)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil
}
