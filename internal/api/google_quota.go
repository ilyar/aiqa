package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxGoogleQuotaInspectionBytes = 64 << 10

const (
	googleQuotaFailureType    = "type.googleapis.com/google.rpc.QuotaFailure"
	googleGenerateQuotaMetric = "generativelanguage.googleapis.com/generate_content_free_tier_requests"
)

// IsGoogleDailyQuota identifies Google's structured per-day generate-content
// quota response. It intentionally does not classify generic 429s or other
// QuotaFailure violations.
func IsGoogleDailyQuota(status int, body map[string]any) bool {
	if status != http.StatusTooManyRequests {
		return false
	}
	errorBody, _ := body["error"].(map[string]any)
	details, _ := errorBody["details"].([]any)
	for _, detail := range details {
		detailMap, _ := detail.(map[string]any)
		if stringValue(detailMap["@type"]) != googleQuotaFailureType {
			continue
		}
		violations, _ := detailMap["violations"].([]any)
		for _, violation := range violations {
			item, _ := violation.(map[string]any)
			quotaID := stringValue(item["quotaId"])
			metric := stringValue(item["quotaMetric"])
			if strings.Contains(quotaID, "PerDay") && metric == googleGenerateQuotaMetric {
				return true
			}
		}
	}
	return false
}

func isGoogleGeneratePath(path string) bool {
	u, err := url.Parse(path)
	if err != nil {
		return false
	}
	segments := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	if len(segments) < 2 || segments[len(segments)-2] != "models" {
		return false
	}
	modelMethod := segments[len(segments)-1]
	return strings.HasSuffix(modelMethod, ":generateContent") || strings.HasSuffix(modelMethod, ":streamGenerateContent")
}

// isGoogleDailyQuotaHTTPResponse consumes at most a fixed prefix and restores
// it before returning, so SDK error parsing sees exactly the original body.
func isGoogleDailyQuotaHTTPResponse(response *http.Response) bool {
	if response == nil || response.StatusCode != http.StatusTooManyRequests || response.Body == nil {
		return false
	}
	prefix, err := io.ReadAll(io.LimitReader(response.Body, maxGoogleQuotaInspectionBytes+1))
	response.Body = &replayReadCloser{prefix: bytes.NewReader(prefix), body: response.Body, pending: err}
	if err != nil || len(prefix) > maxGoogleQuotaInspectionBytes {
		return false
	}
	var body map[string]any
	if err := json.Unmarshal(prefix, &body); err != nil {
		return false
	}
	return IsGoogleDailyQuota(response.StatusCode, body)
}

type replayReadCloser struct {
	prefix  *bytes.Reader
	body    io.ReadCloser
	pending error
}

func (r *replayReadCloser) Read(p []byte) (int, error) {
	if r.prefix.Len() > 0 {
		return r.prefix.Read(p)
	}
	if r.pending != nil {
		err := r.pending
		r.pending = nil
		return 0, err
	}
	return r.body.Read(p)
}

func (r *replayReadCloser) Close() error { return r.body.Close() }
