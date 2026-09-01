package guidance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

func findingFingerprint(p profile.Profile, code string, subject result.SubjectRef, details map[string]any) string {
	payload := struct {
		ProfileID      string            `json:"profile_id"`
		ProfileMajor   string            `json:"profile_major"`
		Code           string            `json:"code"`
		Subject        result.SubjectRef `json:"subject"`
		RootParameters map[string]any    `json:"root_parameters,omitempty"`
	}{
		ProfileID:      p.ID,
		ProfileMajor:   profileMajor(p.Revision),
		Code:           code,
		Subject:        result.SubjectRef{TargetID: strings.ToLower(strings.TrimSpace(subject.TargetID)), Protocol: strings.ToLower(strings.TrimSpace(subject.Protocol)), Kind: subject.Kind, ID: strings.ToLower(strings.TrimSpace(subject.ID))},
		RootParameters: stableDetails(details),
	}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func recommendationFingerprint(findingFingerprint string, source result.RecommendationSource) string {
	payload := struct {
		Finding string                      `json:"finding"`
		Source  result.RecommendationSource `json:"source"`
	}{Finding: findingFingerprint, Source: source}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func stableDetails(details map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range details {
		lower := strings.ToLower(key)
		if lower == "mode" || lower == "status_code" || strings.Contains(lower, "count") || strings.Contains(lower, "entries") || lower == "total" || lower == "affected" || strings.Contains(lower, "latency") || strings.Contains(lower, "request_id") {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func profileMajor(revision string) string {
	parts := strings.Split(strings.TrimSpace(revision), "-")
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return revision
}

func shortFingerprint(value string) string {
	value = strings.TrimPrefix(value, "sha256:")
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func findingCodeID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastSeparator := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastSeparator = false
			continue
		}
		if r == '.' || r == '_' || r == '-' {
			if !lastSeparator && b.Len() > 0 {
				b.WriteRune(r)
				lastSeparator = true
			}
			continue
		}
		if !lastSeparator && b.Len() > 0 {
			b.WriteByte('_')
			lastSeparator = true
		}
	}
	return strings.Trim(b.String(), "._-")
}
