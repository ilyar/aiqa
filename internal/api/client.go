package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxResponseBody = 64 << 20

type Client struct {
	baseURL   *url.URL
	apiKey    string
	http      *http.Client
	userAgent string
	pacer     *pacer
	headers   http.Header
}

func NewClient(baseURL, apiKey, userAgent string, initialInterval time.Duration) (*Client, error) {
	return newClient(baseURL, apiKey, userAgent, initialInterval, nil)
}

// NewClientWithHeaders creates a client for a non-Bearer protocol. Headers are
// copied and cannot be changed through ambient process environment variables.
func NewClientWithHeaders(baseURL, userAgent string, initialInterval time.Duration, headers http.Header) (*Client, error) {
	return newClient(baseURL, "", userAgent, initialInterval, headers)
}

func newClient(baseURL, apiKey, userAgent string, initialInterval time.Duration, headers http.Header) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.MaxIdleConns = 32
	transport.MaxIdleConnsPerHost = 16
	transport.ResponseHeaderTimeout = 0
	return &Client{
		baseURL:   u,
		apiKey:    apiKey,
		http:      &http.Client{Transport: transport},
		userAgent: userAgent,
		pacer:     newPacer(initialInterval),
		headers:   headers.Clone(),
	}, nil
}

func (c *Client) CloneWithKey(apiKey string) *Client {
	clone := *c
	clone.apiKey = apiKey
	return &clone
}

func (c *Client) CloneWithHeaders(headers http.Header) *Client {
	clone := *c
	clone.headers = headers.Clone()
	return &clone
}

func (c *Client) BaseURL() string          { return c.baseURL.String() }
func (c *Client) PacingStats() PacingStats { return c.pacer.snapshot() }
func (c *Client) PacingConstraints() PacingStats {
	return c.pacer.constraints()
}
func (c *Client) SetConcurrency(limit int) { c.pacer.setConcurrency(limit) }
func (c *Client) PacingStatsForModel(model string) PacingStats {
	return c.pacer.snapshotForModel(model)
}

// PacedHTTPClient exposes the target's transport and adaptive pacing to an
// official SDK without exposing AIQA credentials or request construction. The
// SDK must still disable its own retries; this transport owns the shared retry
// and rate-limit policy.
func (c *Client) PacedHTTPClient(model string) *http.Client {
	return &http.Client{Transport: &pacedRoundTripper{client: c, model: model}}
}

type pacedRoundTripper struct {
	client *Client
	model  string
}

func (r *pacedRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	for attempt := 0; attempt < 3; attempt++ {
		if err := r.client.pacer.wait(request.Context(), r.model); err != nil {
			return nil, err
		}
		current := request
		if attempt > 0 {
			if request.GetBody == nil {
				r.client.pacer.release(r.model)
				return nil, errors.New("cannot retry SDK request with a non-replayable body")
			}
			body, err := request.GetBody()
			if err != nil {
				r.client.pacer.release(r.model)
				return nil, err
			}
			current = request.Clone(request.Context())
			current.Body = body
		}
		response, err := r.client.http.Transport.RoundTrip(current)
		retry := err != nil || response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		if attempt == 2 && response != nil && response.StatusCode == http.StatusTooManyRequests {
			r.client.pacer.limited(r.model, retryAfter(response.Header))
		}
		if !retry || attempt == 2 {
			if err == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
				r.client.pacer.success(r.model)
				if attempt > 0 {
					r.client.pacer.recovered(r.model)
				}
			}
			r.client.pacer.release(r.model)
			return response, err
		}
		r.client.pacer.release(r.model)
		delay := time.Duration(1<<attempt) * 100 * time.Millisecond
		if response != nil {
			if response.StatusCode == http.StatusTooManyRequests {
				delay = r.client.pacer.limited(r.model, retryAfter(response.Header))
			}
			if response.Body != nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
				_ = response.Body.Close()
			}
		}
		if response == nil || response.StatusCode != http.StatusTooManyRequests {
			if err := waitDuration(request.Context(), delay); err != nil {
				return nil, err
			}
		}
	}
	panic("unreachable")
}

func (c *Client) ListModels(ctx context.Context) ([]Model, Response, error) {
	resp, err := c.DoJSON(ctx, http.MethodGet, "models", nil)
	if err != nil {
		return nil, resp, err
	}
	if !resp.Is2xx() {
		return nil, resp, fmt.Errorf("list models: HTTP %d", resp.StatusCode)
	}
	data, ok := resp.JSON["data"].([]any)
	if !ok {
		return nil, resp, errors.New("list models: data is missing or not an array")
	}
	models := make([]Model, 0, len(data))
	for i, item := range data {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, resp, fmt.Errorf("list models: data[%d] is not an object", i)
		}
		id, _ := m["id"].(string)
		if id == "" {
			return nil, resp, fmt.Errorf("list models: data[%d].id is missing", i)
		}
		models = append(models, Model{ID: id, Raw: m})
	}
	return models, resp, nil
}

func (c *Client) RetrieveModel(ctx context.Context, model string) (Response, error) {
	endpoint := strings.TrimRight(c.baseURL.String(), "/") + "/models/" + url.PathEscape(model)
	return c.doRawURL(ctx, http.MethodGet, endpoint, nil, "application/json", model)
}

func (c *Client) Chat(ctx context.Context, body map[string]any) (Response, error) {
	return c.doJSON(ctx, http.MethodPost, "chat/completions", body, modelFromBody(body))
}

func (c *Client) ChatRaw(ctx context.Context, raw []byte, contentType string) (Response, error) {
	return c.DoRaw(ctx, http.MethodPost, "chat/completions", raw, contentType)
}

func (c *Client) ImagesGenerate(ctx context.Context, body map[string]any) (Response, error) {
	return c.doJSON(ctx, http.MethodPost, "images/generations", body, modelFromBody(body))
}

func (c *Client) ImagesRaw(ctx context.Context, raw []byte, contentType string) (Response, error) {
	return c.DoRaw(ctx, http.MethodPost, "images/generations", raw, contentType)
}

func (c *Client) DoJSON(ctx context.Context, method, path string, body any) (Response, error) {
	return c.DoJSONForModel(ctx, method, path, body, modelFromBody(body))
}

// DoJSONForModel is the model-scoped variant of DoJSON.  Protocols whose
// model identifier lives in the URL (rather than the JSON body), such as
// Google GenAI, use this method so adaptive pacing and per-model telemetry are
// attributed to the right model.
func (c *Client) DoJSONForModel(ctx context.Context, method, path string, body any, model string) (Response, error) {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return Response{}, fmt.Errorf("marshal request: %w", err)
		}
	}
	return c.doRaw(ctx, method, path, raw, "application/json", model)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, model string) (Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}
	return c.doRaw(ctx, method, path, raw, "application/json", model)
}

func (c *Client) DoRaw(ctx context.Context, method, path string, body []byte, contentType string) (Response, error) {
	return c.DoRawForModel(ctx, method, path, body, contentType, "")
}

// DoRawForModel is the model-scoped variant of DoRaw.  It preserves the raw
// request bytes while still associating pacing, retries, and telemetry with a
// URL-addressed model.
func (c *Client) DoRawForModel(ctx context.Context, method, path string, body []byte, contentType, model string) (Response, error) {
	return c.doRaw(ctx, method, path, body, contentType, model)
}

func (c *Client) doRaw(ctx context.Context, method, path string, body []byte, contentType, model string) (Response, error) {
	u := *c.baseURL
	rel, err := url.Parse(path)
	if err != nil {
		return Response{}, fmt.Errorf("parse request path: %w", err)
	}
	u.Path = joinURLPath(c.baseURL.Path, rel.Path)
	u.RawPath = joinURLPath(c.baseURL.EscapedPath(), rel.EscapedPath())
	u.RawQuery = rel.RawQuery
	return c.doRawURL(ctx, method, u.String(), body, contentType, model)
}

func (c *Client) doRawURL(ctx context.Context, method, endpoint string, body []byte, contentType, model string) (Response, error) {
	for attempt := 0; attempt < 3; attempt++ {
		if err := c.pacer.wait(ctx, model); err != nil {
			return Response{}, err
		}
		resp, err := c.doRawURLOnce(ctx, method, endpoint, body, contentType)
		if !shouldRetry(resp, err) {
			if resp.Is2xx() {
				c.pacer.success(model)
				if attempt > 0 {
					c.pacer.recovered(model)
				}
			}
			c.pacer.release(model)
			return resp, err
		}
		c.pacer.release(model)
		delay := retryDelay(resp, attempt)
		if resp.StatusCode == http.StatusTooManyRequests {
			c.pacer.limited(model, delay)
		}
		if attempt == 2 {
			return resp, err
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			select {
			case <-ctx.Done():
				return resp, ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	panic("unreachable")
}

func (c *Client) doRawURLOnce(ctx context.Context, method, endpoint string, body []byte, contentType string) (Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return Response{}, err
	}
	c.applyHeaders(req, contentType)
	started := time.Now()
	httpResp, err := c.http.Do(req)
	if err != nil {
		return Response{Duration: time.Since(started)}, err
	}
	defer httpResp.Body.Close()
	limited := io.LimitReader(httpResp.Body, maxResponseBody+1)
	responseBody, readErr := io.ReadAll(limited)
	duration := time.Since(started)
	resp := Response{
		StatusCode:  httpResp.StatusCode,
		Header:      httpResp.Header.Clone(),
		ContentType: httpResp.Header.Get("Content-Type"),
		RequestID:   requestID(httpResp.Header),
		Body:        responseBody,
		Duration:    duration,
	}
	if len(responseBody) > maxResponseBody {
		return resp, fmt.Errorf("response body exceeds %d bytes", maxResponseBody)
	}
	if readErr != nil {
		return resp, fmt.Errorf("read response: %w", readErr)
	}
	if len(bytes.TrimSpace(responseBody)) > 0 && strings.Contains(strings.ToLower(resp.ContentType), "json") {
		dec := json.NewDecoder(bytes.NewReader(responseBody))
		dec.UseNumber()
		if err := dec.Decode(&resp.JSON); err != nil {
			return resp, fmt.Errorf("decode JSON response: %w", err)
		}
	} else if len(bytes.TrimSpace(responseBody)) > 0 {
		var generic map[string]any
		dec := json.NewDecoder(bytes.NewReader(responseBody))
		dec.UseNumber()
		if err := dec.Decode(&generic); err == nil {
			resp.JSON = generic
		}
	}
	return resp, nil
}

func retryAfter(header http.Header) time.Duration {
	v := strings.TrimSpace(header.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(v); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(v); err == nil {
		return time.Until(when)
	}
	return 0
}

func (c *Client) ChatStream(ctx context.Context, body map[string]any) (StreamResponse, error) {
	body = cloneMap(body)
	body["stream"] = true
	return c.StreamJSON(ctx, "chat/completions", body)
}

// StreamJSON sends a JSON request and returns parsed SSE events without
// requiring OpenAI-specific event framing. OpenAI fields are still accumulated
// when present for backwards compatibility with ChatStream.
func (c *Client) StreamJSON(ctx context.Context, path string, body map[string]any) (StreamResponse, error) {
	return c.StreamJSONForModel(ctx, path, body, modelFromBody(body))
}

// StreamJSONForModel is the model-scoped variant of StreamJSON for protocols
// that put the model in the request path rather than in the body.
func (c *Client) StreamJSONForModel(ctx context.Context, path string, body map[string]any, model string) (StreamResponse, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return StreamResponse{}, fmt.Errorf("marshal stream request: %w", err)
	}
	u := *c.baseURL
	pathPart, queryPart, _ := strings.Cut(path, "?")
	u.Path = joinURLPath(c.baseURL.Path, pathPart)
	u.RawQuery = queryPart
	var httpResp *http.Response
	var started time.Time
	attempts := 0
	for attempt := 0; attempt < 3; attempt++ {
		attempts = attempt
		if err := c.pacer.wait(ctx, model); err != nil {
			return StreamResponse{}, err
		}
		started = time.Now()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(raw))
		if err != nil {
			return StreamResponse{}, err
		}
		c.applyHeaders(req, "application/json")
		req.Header.Set("Accept", "text/event-stream")
		httpResp, err = c.http.Do(req)
		if err != nil {
			c.pacer.release(model)
			if attempt == 2 {
				return StreamResponse{Duration: time.Since(started)}, err
			}
			delay := time.Duration(1<<attempt) * 100 * time.Millisecond
			select {
			case <-ctx.Done():
				return StreamResponse{Duration: time.Since(started)}, ctx.Err()
			case <-time.After(delay):
			}
			continue
		}
		if !shouldRetryStatus(httpResp.StatusCode, httpResp.Header) {
			break
		}
		delay := retryAfter(httpResp.Header)
		if delay <= 0 {
			delay = time.Duration(1<<attempt) * 100 * time.Millisecond
		}
		if attempt == 2 {
			if httpResp.StatusCode == http.StatusTooManyRequests {
				c.pacer.limited(model, delay)
			}
			break
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(httpResp.Body, 32<<10))
		httpResp.Body.Close()
		c.pacer.release(model)
		if httpResp.StatusCode == http.StatusTooManyRequests {
			c.pacer.limited(model, delay)
		} else {
			select {
			case <-ctx.Done():
				return StreamResponse{Duration: time.Since(started)}, ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	defer c.pacer.release(model)
	defer httpResp.Body.Close()
	result := StreamResponse{
		StatusCode:  httpResp.StatusCode,
		Header:      httpResp.Header.Clone(),
		ContentType: httpResp.Header.Get("Content-Type"),
		RequestID:   requestID(httpResp.Header),
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(httpResp.Body, 32<<10))
		result.Duration = time.Since(started)
		result.RawSnippet = strings.TrimSpace(string(b))
		return result, fmt.Errorf("stream request returned HTTP %d", httpResp.StatusCode)
	}
	c.pacer.success(model)
	if attempts > 0 {
		c.pacer.recovered(model)
	}

	limitedBody := &io.LimitedReader{R: httpResp.Body, N: maxResponseBody + 1}
	reader := bufio.NewReaderSize(limitedBody, 64*1024)
	var event SSEEvent
	var dataLines []string
	var snippet strings.Builder
	flush := func() {
		if len(dataLines) == 0 && event.Event == "" && event.ID == "" {
			return
		}
		event.Data = strings.Join(dataLines, "\n")
		if event.Data == "[DONE]" {
			result.Done = true
		} else if event.Data != "" {
			dec := json.NewDecoder(strings.NewReader(event.Data))
			dec.UseNumber()
			if err := dec.Decode(&event.JSON); err != nil {
				result.InvalidEvents = append(result.InvalidEvents, truncate(event.Data, 512)+": "+err.Error())
			} else {
				consumeStreamJSON(&result, event.JSON)
			}
		}
		if len(result.Events) < 2000 {
			result.Events = append(result.Events, event)
		}
		if snippet.Len() < 8192 {
			snippet.WriteString("data: ")
			snippet.WriteString(truncate(event.Data, 512))
			snippet.WriteByte('\n')
		}
		event = SSEEvent{}
		dataLines = nil
	}

	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			switch {
			case line == "":
				flush()
			case strings.HasPrefix(line, ":"):
				// SSE comment / heartbeat.
			case strings.HasPrefix(line, "data:"):
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			case strings.HasPrefix(line, "event:"):
				event.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "id:"):
				event.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			default:
				result.InvalidEvents = append(result.InvalidEvents, "invalid SSE line: "+truncate(line, 256))
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				flush()
				if limitedBody.N <= 0 {
					result.Duration = time.Since(started)
					result.RawSnippet = snippet.String()
					return result, fmt.Errorf("stream response exceeds %d bytes", maxResponseBody)
				}
				break
			}
			result.Duration = time.Since(started)
			result.RawSnippet = snippet.String()
			return result, fmt.Errorf("read SSE stream: %w", readErr)
		}
	}
	result.Duration = time.Since(started)
	result.RawSnippet = snippet.String()
	return result, nil
}

func shouldRetry(resp Response, err error) bool {
	if err != nil && resp.StatusCode == 0 {
		return true
	}
	return shouldRetryStatus(resp.StatusCode, http.Header(resp.Header))
}

func shouldRetryStatus(status int, headers http.Header) bool {
	if strings.EqualFold(strings.TrimSpace(headers.Get("x-should-retry")), "false") {
		return false
	}
	switch status {
	case 408, 429, 500, 502, 503, 504, 529:
		return true
	}
	return false
}

func retryDelay(resp Response, attempt int) time.Duration {
	if delay := retryAfter(http.Header(resp.Header)); delay > 0 {
		return delay
	}
	return time.Duration(1<<attempt) * 100 * time.Millisecond
}

func modelFromBody(body any) string {
	if m, ok := body.(map[string]any); ok {
		model, _ := m["model"].(string)
		return model
	}
	return ""
}

func (c *Client) applyHeaders(req *http.Request, contentType string) {
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for name, values := range c.headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
}

func joinURLPath(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

func requestID(h http.Header) string {
	for _, name := range []string{"request-id", "x-request-id", "openai-request-id", "x-correlation-id", "cf-ray"} {
		if value := h.Get(name); value != "" {
			return value
		}
	}
	return ""
}

func consumeStreamJSON(out *StreamResponse, root map[string]any) {
	if u := ParseUsage(root); u.TotalTokens > 0 || u.PromptTokens > 0 || u.CompletionTokens > 0 {
		out.Usage = u
	}
	choices, _ := root["choices"].([]any)
	for _, raw := range choices {
		choice, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if reason, ok := choice["finish_reason"].(string); ok && reason != "" {
			out.FinishReasons = append(out.FinishReasons, reason)
		}
		delta, _ := choice["delta"].(map[string]any)
		if delta == nil {
			continue
		}
		out.Content += contentString(delta["content"])
		if calls, ok := delta["tool_calls"].([]any); ok {
			for _, call := range calls {
				if m, ok := call.(map[string]any); ok {
					mergeStreamToolCall(out, m)
				}
			}
		}
	}
}

func mergeStreamToolCall(out *StreamResponse, delta map[string]any) {
	index := int(int64Value(delta["index"]))
	if index < 0 {
		index = len(out.ToolCalls)
	}
	for len(out.ToolCalls) <= index {
		out.ToolCalls = append(out.ToolCalls, map[string]any{"index": len(out.ToolCalls)})
	}
	target := out.ToolCalls[index]
	for _, key := range []string{"id", "type"} {
		if value, exists := delta[key]; exists && value != nil && stringValue(value) != "" {
			target[key] = value
		}
	}
	functionDelta, _ := delta["function"].(map[string]any)
	if functionDelta == nil {
		return
	}
	function, _ := target["function"].(map[string]any)
	if function == nil {
		function = map[string]any{}
		target["function"] = function
	}
	if name := stringValue(functionDelta["name"]); name != "" {
		function["name"] = name
	}
	if args, exists := functionDelta["arguments"]; exists {
		function["arguments"] = stringValue(function["arguments"]) + stringValue(args)
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
