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
	"strings"
	"time"
)

const maxResponseBody = 64 << 20

type Client struct {
	baseURL   *url.URL
	apiKey    string
	http      *http.Client
	userAgent string
}

func NewClient(baseURL, apiKey, userAgent string) (*Client, error) {
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
	}, nil
}

func (c *Client) CloneWithKey(apiKey string) *Client {
	clone := *c
	clone.apiKey = apiKey
	return &clone
}

func (c *Client) BaseURL() string { return c.baseURL.String() }

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
	return c.doRawURL(ctx, http.MethodGet, endpoint, nil, "application/json")
}

func (c *Client) Chat(ctx context.Context, body map[string]any) (Response, error) {
	return c.DoJSON(ctx, http.MethodPost, "chat/completions", body)
}

func (c *Client) ChatRaw(ctx context.Context, raw []byte, contentType string) (Response, error) {
	return c.DoRaw(ctx, http.MethodPost, "chat/completions", raw, contentType)
}

func (c *Client) DoJSON(ctx context.Context, method, path string, body any) (Response, error) {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return Response{}, fmt.Errorf("marshal request: %w", err)
		}
	}
	return c.DoRaw(ctx, method, path, raw, "application/json")
}

func (c *Client) DoRaw(ctx context.Context, method, path string, body []byte, contentType string) (Response, error) {
	u := *c.baseURL
	u.Path = joinURLPath(c.baseURL.Path, path)
	return c.doRawURL(ctx, method, u.String(), body, contentType)
}

func (c *Client) doRawURL(ctx context.Context, method, endpoint string, body []byte, contentType string) (Response, error) {
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

func (c *Client) ChatStream(ctx context.Context, body map[string]any) (StreamResponse, error) {
	body = cloneMap(body)
	body["stream"] = true
	raw, err := json.Marshal(body)
	if err != nil {
		return StreamResponse{}, fmt.Errorf("marshal stream request: %w", err)
	}
	u := *c.baseURL
	u.Path = joinURLPath(c.baseURL.Path, "chat/completions")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(raw))
	if err != nil {
		return StreamResponse{}, err
	}
	c.applyHeaders(req, "application/json")
	started := time.Now()
	httpResp, err := c.http.Do(req)
	if err != nil {
		return StreamResponse{Duration: time.Since(started)}, err
	}
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

	reader := bufio.NewReaderSize(httpResp.Body, 64*1024)
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

func (c *Client) applyHeaders(req *http.Request, contentType string) {
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
}

func joinURLPath(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

func requestID(h http.Header) string {
	for _, name := range []string{"x-request-id", "openai-request-id", "x-correlation-id", "cf-ray"} {
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
