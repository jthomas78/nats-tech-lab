// Package anthropic implements domain.TranslationDrafter (BR-D07) against
// the Anthropic Messages API. It only ever returns a candidate translation —
// it never persists anything; saving a draft is a separate, explicit
// SetLocalization call made by the caller once a human accepts it.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
)

const (
	apiURL       = "https://api.anthropic.com/v1/messages"
	apiVersion   = "2023-06-01"
	defaultModel = "claude-haiku-4-5-20251001"
)

// Client drafts translations via the Anthropic Messages API. The API key is
// held server-side only (read from ANTHROPIC_API_KEY in cmd/main.go) and
// never returned to callers.
type Client struct {
	apiKey string
	model  string
	httpc  *http.Client
}

// New builds a Client. apiKey must be non-empty — callers should only wire
// this adapter in when ANTHROPIC_API_KEY is configured.
func New(apiKey string) *Client {
	return &Client{apiKey: apiKey, model: defaultModel, httpc: &http.Client{Timeout: 20 * time.Second}}
}

type messageRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	Messages  []messageContent `json:"messages"`
}

type messageContent struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messageResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type draftPayload struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Draft implements domain.TranslationDrafter.
func (c *Client) Draft(ctx context.Context, in domain.TranslationDraftInput) (domain.TranslationDraft, error) {
	if c.apiKey == "" {
		return domain.TranslationDraft{}, fmt.Errorf("anthropic: ANTHROPIC_API_KEY not configured")
	}

	reqBody, err := json.Marshal(messageRequest{
		Model:     c.model,
		MaxTokens: 1024,
		Messages:  []messageContent{{Role: "user", Content: buildPrompt(in)}},
	})
	if err != nil {
		return domain.TranslationDraft{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return domain.TranslationDraft{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return domain.TranslationDraft{}, fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.TranslationDraft{}, fmt.Errorf("anthropic: reading response: %w", err)
	}

	var parsed messageResponse
	if jsonErr := json.Unmarshal(body, &parsed); jsonErr != nil {
		return domain.TranslationDraft{}, fmt.Errorf("anthropic: invalid response body: %w", jsonErr)
	}
	if resp.StatusCode != http.StatusOK {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return domain.TranslationDraft{}, fmt.Errorf("anthropic: %s", parsed.Error.Message)
		}
		return domain.TranslationDraft{}, fmt.Errorf("anthropic: unexpected status %d", resp.StatusCode)
	}
	if len(parsed.Content) == 0 {
		return domain.TranslationDraft{}, fmt.Errorf("anthropic: empty response")
	}

	var draft draftPayload
	if jsonErr := json.Unmarshal([]byte(extractJSON(parsed.Content[0].Text)), &draft); jsonErr != nil {
		return domain.TranslationDraft{}, fmt.Errorf("anthropic: could not parse model output as JSON: %w", jsonErr)
	}
	return domain.TranslationDraft{Locale: in.TargetLocale, Label: draft.Label, Description: draft.Description}, nil
}

func buildPrompt(in domain.TranslationDraftInput) string {
	return fmt.Sprintf(`Translate this reference-data entry from locale %q to locale %q.

Label: %s
Description: %s

Respond with ONLY a JSON object of the form {"label": "...", "description": "..."} — no other text, no markdown code fences.`,
		in.SourceLocale, in.TargetLocale, in.SourceLabel, in.SourceDescription)
}

// extractJSON strips a markdown code fence around the model's JSON output,
// if present — models sometimes wrap JSON in ```json ... ``` despite being
// asked not to.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
