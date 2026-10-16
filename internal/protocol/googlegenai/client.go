package googlegenai

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ilyar/aiqa/internal/api"
)

const DefaultAPIVersion = "v1beta"

// Client is the target-owned Gemini Developer API transport. It deliberately
// exposes only the REST surfaces exercised by the compatibility profile.
type Client struct {
	baseURL    string
	apiVersion string
	apiKey     string
	inner      *api.Client
}

func NewClient(baseURL, apiKey, apiVersion, userAgent string, interval time.Duration) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	apiVersion = strings.Trim(strings.TrimSpace(apiVersion), "/")
	if apiVersion == "" {
		apiVersion = DefaultAPIVersion
	}
	if baseURL == "" || apiKey == "" {
		return nil, fmt.Errorf("Google GenAI base URL and API key are required")
	}
	// Keep the client safe for callers that construct TargetConfig directly:
	// the configured base is the prefix before the API version, and the
	// version must be appended exactly once even when a caller supplied it.
	normalized, err := normalizeBaseURL(baseURL, apiVersion)
	if err != nil {
		return nil, err
	}
	baseURL = normalized
	inner, err := api.NewClientWithHeaders(baseURL+"/"+apiVersion, userAgent, interval, credentialHeaders(apiKey))
	if err != nil {
		return nil, err
	}
	return &Client{baseURL: baseURL, apiVersion: apiVersion, apiKey: apiKey, inner: inner}, nil
}

func normalizeBaseURL(raw, version string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid google-genai base URL %q", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("google-genai base URL must not contain query parameters or a fragment")
	}
	if u.User != nil {
		return "", fmt.Errorf("google-genai base URL must not contain user information or credentials")
	}
	trimmed := strings.TrimRight(u.Path, "/")
	versionPath := "/" + strings.Trim(version, "/")
	if strings.HasSuffix(trimmed, versionPath) {
		u.Path = strings.TrimSuffix(trimmed, versionPath)
		if u.Path == "" {
			u.Path = "/"
		}
		u.RawPath = ""
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if u.Path == "" {
		u.Path = "/"
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func credentialHeaders(key string) http.Header {
	h := make(http.Header)
	if key != "" {
		h.Set("x-goog-api-key", key)
	}
	return h
}

func NormalizeModelID(id string) string {
	id = strings.TrimSpace(id)
	return strings.TrimPrefix(id, "models/")
}

func (c *Client) SetConcurrency(limit int)     { c.inner.SetConcurrency(limit) }
func (c *Client) TransportClient() *api.Client { return c.inner }
func (c *Client) PacingStats() api.PacingStats { return c.inner.PacingStats() }
func (c *Client) PacingStatsForModel(model string) api.PacingStats {
	return c.inner.PacingStatsForModel(model)
}

func (c *Client) ListModels(ctx context.Context) ([]api.Model, api.Response, error) {
	return c.ListModelsPage(ctx, "")
}

func (c *Client) ListModelsPage(ctx context.Context, pageToken string) ([]api.Model, api.Response, error) {
	path := "models"
	if pageToken != "" {
		path += "?pageToken=" + url.QueryEscape(pageToken)
	}
	resp, err := c.inner.DoJSON(ctx, http.MethodGet, path, nil)
	if err != nil || !resp.Is2xx() {
		if err == nil && !resp.Is2xx() {
			err = fmt.Errorf("Google models list returned HTTP %d", resp.StatusCode)
		}
		return nil, resp, err
	}
	items, ok := resp.JSON["models"].([]any)
	if !ok {
		return nil, resp, fmt.Errorf("Google models list has no models array")
	}
	models := make([]api.Model, 0, len(items))
	for _, item := range items {
		if raw, ok := item.(map[string]any); ok {
			name, _ := raw["name"].(string)
			if name != "" {
				if !safeRawModelName(name) {
					return nil, resp, fmt.Errorf("%w: invalid Google model name", api.ErrWorkLimit)
				}
				models = append(models, api.Model{ID: NormalizeModelID(name), Raw: raw})
			}
		}
	}
	return models, resp, nil
}

func safeRawModelName(name string) bool {
	if len(name) > 1024 || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if r <= 0x1f || (r >= 0x7f && r <= 0x9f) || r == 0x061c || r == 0x200e || r == 0x200f || (r >= 0x202a && r <= 0x202e) || r == 0x2028 || r == 0x2029 || (r >= 0x2066 && r <= 0x2069) {
			return false
		}
	}
	return true
}

func (c *Client) NextPageToken(resp api.Response) string {
	token, _ := resp.JSON["nextPageToken"].(string)
	return strings.TrimSpace(token)
}

func (c *Client) GetModel(ctx context.Context, model string) (api.Response, error) {
	model = NormalizeModelID(model)
	return c.inner.DoJSONForModel(ctx, http.MethodGet, "models/"+url.PathEscape(model), nil, model)
}

func (c *Client) GenerateContent(ctx context.Context, model string, body map[string]any) (api.Response, error) {
	model = NormalizeModelID(model)
	return c.inner.DoJSONForModel(ctx, http.MethodPost, "models/"+url.PathEscape(model)+":generateContent", body, model)
}

func (c *Client) GenerateContentRaw(ctx context.Context, model string, raw []byte) (api.Response, error) {
	model = NormalizeModelID(model)
	return c.inner.DoRawForModel(ctx, http.MethodPost, "models/"+url.PathEscape(model)+":generateContent", raw, "application/json", model)
}

func (c *Client) StreamGenerateContent(ctx context.Context, model string, body map[string]any) (api.StreamResponse, error) {
	model = NormalizeModelID(model)
	return c.inner.StreamJSONForModel(ctx, "models/"+url.PathEscape(model)+":streamGenerateContent?alt=sse", body, model)
}

func (c *Client) CountTokens(ctx context.Context, model string, body map[string]any) (api.Response, error) {
	model = NormalizeModelID(model)
	return c.inner.DoJSONForModel(ctx, http.MethodPost, "models/"+url.PathEscape(model)+":countTokens", body, model)
}

func (c *Client) EmbedContent(ctx context.Context, model string, body map[string]any) (api.Response, error) {
	model = NormalizeModelID(model)
	return c.inner.DoJSONForModel(ctx, http.MethodPost, "models/"+url.PathEscape(model)+":embedContent", body, model)
}

func (c *Client) Interaction(ctx context.Context, body map[string]any) (api.Response, error) {
	return c.inner.DoJSONForModel(ctx, http.MethodPost, "interactions", body, modelFromBody(body))
}

func (c *Client) InteractionRaw(ctx context.Context, raw []byte) (api.Response, error) {
	return c.InteractionRawForModel(ctx, raw, "")
}

// InteractionRawForModel keeps malformed-request probes model-scoped for
// pacing telemetry even though their raw JSON may be intentionally invalid.
func (c *Client) InteractionRawForModel(ctx context.Context, raw []byte, model string) (api.Response, error) {
	return c.inner.DoRawForModel(ctx, http.MethodPost, "interactions", raw, "application/json", NormalizeModelID(model))
}

func (c *Client) StreamInteraction(ctx context.Context, body map[string]any) (api.StreamResponse, error) {
	return c.inner.StreamJSONForModel(ctx, "interactions?alt=sse", body, modelFromBody(body))
}

// DeleteInteraction removes an interaction created with store=true.  The
// stateful compatibility probe uses this endpoint during cleanup and never
// exposes the provider-generated identifier in report text.
func (c *Client) DeleteInteraction(ctx context.Context, model, id string) (api.Response, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return api.Response{}, fmt.Errorf("interaction id is empty")
	}
	return c.inner.DoJSONForModel(ctx, http.MethodDelete, "interactions/"+url.PathEscape(id), nil, NormalizeModelID(model))
}

func modelFromBody(body map[string]any) string {
	if body == nil {
		return ""
	}
	model, _ := body["model"].(string)
	return NormalizeModelID(model)
}
