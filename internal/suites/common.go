package suites

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

type Environment struct {
	Client  *api.Client
	Config  config.Config
	Profile profile.Profile
	Logf    func(format string, args ...any)
}

func (e Environment) logf(format string, args ...any) {
	if e.Logf != nil {
		e.Logf(format, args...)
	}
}

func (e Environment) probeContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, e.Config.Timeout)
}

func testBase(p profile.Profile, id, suite string) result.TestResult {
	spec := p.Probe(id)
	return result.TestResult{
		ID:            id,
		RequirementID: id,
		ProbeID:       "aiqa.native." + id,
		Name:          spec.Name,
		Suite:         suite,
		Category:      spec.Category,
		Required:      spec.Required,
		Expected:      spec.Description,
		Metrics:       map[string]any{},
	}
}

func httpObservation(resp api.Response) result.HTTPObservation {
	headers := map[string]string{}
	for _, name := range []string{"server", "content-encoding", "openai-processing-ms", "x-ratelimit-limit-requests", "x-ratelimit-remaining-requests"} {
		for key, values := range resp.Header {
			if strings.EqualFold(key, name) && len(values) > 0 {
				headers[strings.ToLower(name)] = values[0]
			}
		}
	}
	if len(headers) == 0 {
		headers = nil
	}
	return result.HTTPObservation{
		StatusCode:  resp.StatusCode,
		ContentType: resp.ContentType,
		RequestID:   resp.RequestID,
		Headers:     headers,
		Snippet:     sanitizeSnippet(resp.Snippet(1200)),
	}
}

func streamObservation(resp api.StreamResponse) result.HTTPObservation {
	return result.HTTPObservation{
		StatusCode:  resp.StatusCode,
		ContentType: resp.ContentType,
		RequestID:   resp.RequestID,
		Snippet:     sanitizeSnippet(truncate(resp.RawSnippet, 1200)),
	}
}

func sanitizeSnippet(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return ""
	}
	var compact any
	if json.Unmarshal([]byte(s), &compact) == nil {
		if b, err := json.Marshal(compact); err == nil {
			s = string(b)
		}
	}
	return truncate(s, 1200)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return truncate(err.Error(), 1000)
}

func statusForOptionalHTTP(resp api.Response, err error) result.Status {
	if err != nil && resp.StatusCode == 0 {
		return result.StatusError
	}
	if resp.Is2xx() {
		return result.StatusPass
	}
	switch {
	case resp.StatusCode == 400, resp.StatusCode == 404, resp.StatusCode == 405, resp.StatusCode == 409, resp.StatusCode == 415, resp.StatusCode == 422, resp.StatusCode == 501:
		return result.StatusUnsupported
	case resp.StatusCode == 408, resp.StatusCode == 429, resp.StatusCode >= 500:
		return result.StatusError
	default:
		return result.StatusFail
	}
}

func providerFailureStatusCode(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

func providerFailureSummary(subject string, status int) string {
	switch status {
	case http.StatusRequestTimeout:
		return subject + " timed out at the provider; compatibility outcome is inconclusive"
	case http.StatusTooManyRequests:
		return subject + " remained rate limited after adaptive retries; compatibility outcome is inconclusive"
	default:
		return fmt.Sprintf("%s returned provider HTTP %d; compatibility outcome is inconclusive", subject, status)
	}
}

func boolWord(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func metricString(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return d.Round(time.Millisecond).String()
}
