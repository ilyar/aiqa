package protocol

import (
	"sort"
	"strings"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/result"
)

// SelectModels intersects one global requested list with a target-local
// inventory. The normalizer is deliberately protocol-owned: OpenAI and
// Anthropic use exact IDs, while Google removes only its documented
// models/ resource prefix. A requested ID is never materialized when the
// inventory does not contain it.
//
// When requested is empty, only trusted entries are scheduled. Entries that
// have an ID but do not satisfy the protocol's inventory shape remain visible
// in Discovery.Models, but are inventory-only and receive no model probes.
func SelectModels(requested []string, discovered []api.Model, normalize func(string) string, trusted func(map[string]any) bool) ([]string, map[string]map[string]any) {
	if normalize == nil {
		normalize = strings.TrimSpace
	}
	metadata := make(map[string]map[string]any, len(discovered))
	trustedIDs := make(map[string]bool, len(discovered))
	metadataTrusted := make(map[string]bool, len(discovered))
	for _, model := range discovered {
		id := normalize(model.ID)
		if id == "" {
			continue
		}
		entryTrusted := trusted == nil || trusted(model.Raw)
		if _, exists := metadata[id]; !exists || entryTrusted && !metadataTrusted[id] {
			metadata[id] = model.Raw
			metadataTrusted[id] = entryTrusted
		}
		if entryTrusted {
			trustedIDs[id] = true
		}
	}

	if len(requested) > 0 {
		selected := make([]string, 0, len(requested))
		seen := map[string]bool{}
		for _, raw := range requested {
			id := normalize(raw)
			if id == "" || seen[id] {
				continue
			}
			if _, exists := metadata[id]; !exists {
				continue
			}
			seen[id] = true
			selected = append(selected, id)
		}
		return selected, metadata
	}

	selected := make([]string, 0, len(trustedIDs))
	for id := range trustedIDs {
		selected = append(selected, id)
	}
	sort.Strings(selected)
	return selected, metadata
}

func ExactModelID(value string) string { return strings.TrimSpace(value) }

// RateLimitStats converts transport-owned observations into the public report
// shape. Safe pacing values intentionally remain zero when no successful
// response exists; report JSON omits those zero observations.
func RateLimitStats(stats api.PacingStats) *result.RateLimitStats {
	out := &result.RateLimitStats{
		LogicalRequests:    stats.LogicalRequests,
		PhysicalAttempts:   stats.PhysicalAttempts,
		SuccessfulRequests: stats.SuccessfulRequests,
		RateLimited:        stats.RateLimited,
		RetryAttempts:      stats.RetryAttempts,
		RetriesRecovered:   stats.RetriesRecovered,
		RetriesExhausted:   stats.RetriesExhausted,
		BackoffMS:          stats.Backoff.Milliseconds(),
		SafeRate:           stats.SafeRate,
		SafeConcurrency:    stats.SafeConcurrency,
	}
	latencies := append([]int64(nil), stats.LatencySamples...)
	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		var total int64
		for _, latency := range latencies {
			total += latency
		}
		out.LatencySamples = len(latencies)
		out.MeanLatencyMS = total / int64(len(latencies))
		out.P50LatencyMS = latencies[(len(latencies)-1)/2]
		out.P95LatencyMS = latencies[(95*len(latencies)-1)/100]
	}
	return out
}
