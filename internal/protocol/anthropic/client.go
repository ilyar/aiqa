package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ilyar/aiqa/internal/api"
)

const StableAPIVersion = "2023-06-01"

type Client struct {
	baseURL         string
	apiKey          string
	apiVersion      string
	userAgent       string
	requestInterval time.Duration
	concurrency     int
	inner           *api.Client
}

func NewClient(baseURL, apiKey, apiVersion, userAgent string, requestInterval time.Duration) (*Client, error) {
	baseURL = NormalizeBaseURL(baseURL)
	if apiVersion == "" {
		apiVersion = StableAPIVersion
	}
	c := &Client{baseURL: baseURL, apiKey: apiKey, apiVersion: apiVersion, userAgent: userAgent, requestInterval: requestInterval}
	inner, err := c.client(apiKey, apiVersion)
	if err != nil {
		return nil, err
	}
	c.inner = inner
	return c, nil
}

func NormalizeBaseURL(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if strings.HasSuffix(raw, "/v1") {
		raw = strings.TrimSuffix(raw, "/v1")
	}
	return raw
}

func (c *Client) client(key, version string) (*api.Client, error) {
	headers := credentialHeaders(key, version)
	client, err := api.NewClientWithHeaders(c.baseURL+"/v1", c.userAgent, c.requestInterval, headers)
	if err == nil && c.concurrency > 0 {
		client.SetConcurrency(c.concurrency)
	}
	return client, err
}

func credentialHeaders(key, version string) http.Header {
	headers := make(http.Header)
	if key != "" {
		headers.Set("x-api-key", key)
	}
	if version != "" {
		headers.Set("anthropic-version", version)
	}
	return headers
}

func (c *Client) SetConcurrency(limit int) {
	c.concurrency = limit
	c.inner.SetConcurrency(limit)
}

func (c *Client) ListModels(ctx context.Context) ([]api.Model, api.Response, error) {
	return c.ListModelsPage(ctx, "", 1000)
}

func (c *Client) ListModelsPage(ctx context.Context, afterID string, limit int) ([]api.Model, api.Response, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", fmt.Sprint(limit))
	}
	if afterID != "" {
		query.Set("after_id", afterID)
	}
	path := "models"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	resp, err := c.inner.DoJSON(ctx, http.MethodGet, path, nil)
	if err != nil || !resp.Is2xx() {
		return nil, resp, err
	}
	data, _ := resp.JSON["data"].([]any)
	models := make([]api.Model, 0, len(data))
	for _, item := range data {
		if raw, ok := item.(map[string]any); ok {
			if id, ok := raw["id"].(string); ok && id != "" {
				models = append(models, api.Model{ID: id, Raw: raw})
			}
		}
	}
	return models, resp, nil
}

func (c *Client) RetrieveModel(ctx context.Context, id string) (api.Response, error) {
	return c.inner.DoJSON(ctx, http.MethodGet, "models/"+url.PathEscape(id), nil)
}

func (c *Client) Message(ctx context.Context, body map[string]any) (api.Response, error) {
	return c.inner.DoJSON(ctx, http.MethodPost, "messages", body)
}

func (c *Client) MessageRaw(ctx context.Context, raw []byte) (api.Response, error) {
	return c.inner.DoRaw(ctx, http.MethodPost, "messages", raw, "application/json")
}

func (c *Client) StreamMessage(ctx context.Context, body map[string]any) (api.StreamResponse, error) {
	body = cloneMap(body)
	body["stream"] = true
	return c.inner.StreamJSON(ctx, "messages", body)
}

func (c *Client) CountTokens(ctx context.Context, body map[string]any) (api.Response, error) {
	return c.inner.DoJSON(ctx, http.MethodPost, "messages/count_tokens", body)
}

func (c *Client) MessageWithCredentials(ctx context.Context, body map[string]any, key, version string) (api.Response, error) {
	client := c.inner.CloneWithHeaders(credentialHeaders(key, version))
	return client.DoJSON(ctx, http.MethodPost, "messages", body)
}

func (c *Client) PacingStats() api.PacingStats { return c.inner.PacingStats() }
func (c *Client) PacingStatsForModel(model string) api.PacingStats {
	return c.inner.PacingStatsForModel(model)
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+1)
	for key, value := range in {
		out[key] = value
	}
	return out
}
