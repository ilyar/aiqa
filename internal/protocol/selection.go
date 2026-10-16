package protocol

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/result"
)

// ValidateInventoryBudget rejects attacker-sized catalogues before their
// entries are copied into discovery reports, planning structures, or workers.
// Callers retain the discovery error as incomplete evidence rather than
// silently truncating a catalogue into an apparent successful run.
func ValidateInventoryBudget(models []api.Model, maxModels int, maxBytes int64) error {
	// Zero is the Go struct's legacy/default value in programmatic callers;
	// retain the secure CLI defaults rather than accidentally disabling limits.
	if maxModels == 0 {
		maxModels = 4096
	}
	if maxBytes == 0 {
		maxBytes = 16 * 1024 * 1024
	}
	if maxModels < 1 || maxBytes < 1 {
		return fmt.Errorf("invalid inventory budget")
	}
	if len(models) > maxModels {
		return fmt.Errorf("provider inventory has %d models; configured limit is %d", len(models), maxModels)
	}
	var size int64
	for _, model := range models {
		if err := ValidateModelID(model.ID); err != nil {
			return err
		}
		encoded, err := json.Marshal(model.Raw)
		if err != nil {
			return fmt.Errorf("encode provider inventory entry: %w", err)
		}
		size += int64(len(encoded))
		if size > maxBytes {
			return fmt.Errorf("provider inventory is %d bytes; configured limit is %d", size, maxBytes)
		}
	}
	return nil
}

func EffectiveInventoryByteBudget(maxBytes int64) int64 {
	if maxBytes == 0 {
		return 16 * 1024 * 1024
	}
	return maxBytes
}

// ValidateModelID preserves ordinary Unicode model names and punctuation while
// rejecting values that can alter terminal/Markdown framing or force
// attacker-sized map/report allocations.
func ValidateModelID(id string) error {
	if id == "" || len(id) > 1024 || !utf8.ValidString(id) {
		return fmt.Errorf("invalid provider model id")
	}
	for _, r := range id {
		if r <= 0x1f || (r >= 0x7f && r <= 0x9f) || r == 0x061c || r == 0x200e || r == 0x200f || (r >= 0x2028 && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return fmt.Errorf("invalid provider model id")
		}
	}
	return nil
}

// ValidateInventoryResponseBudget accounts for bytes received from the
// provider, not a re-serialized subset of parsed fields. Call it for every
// discovery page before retaining the decoded inventory.
func ValidateInventoryResponseBudget(models []api.Model, responseBytes int64, maxModels int, maxBytes int64) error {
	if maxBytes == 0 {
		maxBytes = 16 * 1024 * 1024
	}
	if responseBytes > maxBytes {
		return fmt.Errorf("provider inventory response is %d bytes; configured limit is %d", responseBytes, maxBytes)
	}
	return ValidateInventoryBudget(models, maxModels, maxBytes)
}

// ValidateSelectionBudget refuses excess work rather than silently dropping
// models. Raising the configured cap is an explicit operator decision.
func ValidateSelectionBudget(selected []string, maxModels int) error {
	if maxModels == 0 {
		maxModels = 64
	}
	if maxModels < 1 || len(selected) > maxModels {
		return fmt.Errorf("selected %d models; configured limit is %d", len(selected), maxModels)
	}
	return nil
}

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
