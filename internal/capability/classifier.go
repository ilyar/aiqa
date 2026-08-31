package capability

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
)

type Options struct {
	BaseURL       string
	Overrides     map[string][]string
	UnknownPolicy string
}

type OverrideFile struct {
	Models map[string][]string `json:"models"`
}

func LoadOverrideFile(path string) (map[string][]string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read capability file: %w", err)
	}
	var file OverrideFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode capability file: %w", err)
	}
	for model, capabilities := range file.Models {
		if strings.TrimSpace(model) == "" {
			return nil, fmt.Errorf("capability file contains an empty model ID")
		}
		if len(capabilities) == 0 {
			return nil, fmt.Errorf("model %q has no capabilities", model)
		}
		for _, capability := range capabilities {
			if !Known[capability] {
				return nil, fmt.Errorf("model %q has unknown capability %q", model, capability)
			}
		}
	}
	return file.Models, nil
}

func Classify(model string, metadata map[string]any, opts Options) Classification {
	if values := opts.Overrides[model]; len(values) > 0 {
		return fromValues(model, values, "override", "explicit.model-capability", "high", "explicit capability override")
	}

	var evidence []Evidence
	evidence = append(evidence, structuredMetadata(metadata)...)
	evidence = append(evidence, providerRules(opts.BaseURL, metadata)...)
	evidence = append(evidence, nameRules(model)...)
	evidence = deduplicate(evidence)
	if conflict(evidence) {
		return Classification{Model: model, Unknown: true, Reason: "classification conflict", Capabilities: evidence}
	}
	if len(evidence) > 0 {
		return Classification{Model: model, Capabilities: evidence}
	}
	if opts.UnknownPolicy != "inventory" {
		return fromValues(model, []string{ChatText}, "default_policy", "unclassified-default.chat", "low", "no evidence of a non-text capability; treated as text by default")
	}
	return Classification{Model: model, Unknown: true, Reason: "insufficient capability evidence"}
}

func fromValues(model string, values []string, source, rule, confidence, reason string) Classification {
	result := Classification{Model: model}
	seen := map[string]bool{}
	for _, value := range values {
		if Known[value] && !seen[value] {
			seen[value] = true
			result.Capabilities = append(result.Capabilities, Evidence{Capability: value, Source: source, Rule: rule, Confidence: confidence, Reason: reason})
		}
	}
	sort.Slice(result.Capabilities, func(i, j int) bool { return result.Capabilities[i].Capability < result.Capabilities[j].Capability })
	if len(result.Capabilities) == 0 {
		result.Unknown = true
		result.Reason = "override did not contain a known capability"
	}
	return result
}

func structuredMetadata(metadata map[string]any) []Evidence {
	var out []Evidence
	for _, key := range []string{"capabilities", "output_modalities", "modalities", "type", "model_type", "task", "pipeline_tag"} {
		out = append(out, metadataEvidence(key, metadata[key])...)
	}
	if architecture, ok := metadata["architecture"].(map[string]any); ok {
		for _, key := range []string{"output_modalities", "modalities"} {
			out = append(out, metadataEvidence("architecture."+key, architecture[key])...)
		}
	}
	return out
}

func metadataEvidence(key string, raw any) []Evidence {
	var out []Evidence
	for _, value := range stringValues(raw) {
		if capability := normalizeMetadataCapabilityForKey(key, value); capability != "" {
			out = append(out, Evidence{Capability: capability, Source: "vendor_metadata", Rule: "metadata." + key, Confidence: "medium", Reason: fmt.Sprintf("metadata %s=%q", key, value)})
		}
	}
	return out
}

func normalizeMetadataCapabilityForKey(key, value string) string {
	if capability := normalizeMetadataCapability(value); capability != "" {
		return capability
	}
	if !strings.HasSuffix(key, "output_modalities") {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "image":
		return ImagesGenerations
	case "audio":
		return AudioSpeech
	case "video":
		return VideosGenerations
	default:
		return ""
	}
}

func providerRules(baseURL string, metadata map[string]any) []Evidence {
	u, _ := url.Parse(baseURL)
	if u == nil || !strings.EqualFold(u.Hostname(), "vibecode.moe") {
		return nil
	}
	isImage, exists := metadata["is_image"].(bool)
	if !exists {
		return nil
	}
	if isImage {
		return []Evidence{{Capability: ImagesGenerations, Source: "provider_adapter", Rule: "vibecode.is_image.true", Confidence: "high", Reason: "Vibecode metadata is_image=true"}}
	}
	pricing, _ := metadata["pricing"].(map[string]any)
	if pricing != nil && (numberPresent(pricing["input_usd_per_m"]) || numberPresent(pricing["output_usd_per_m"])) {
		return []Evidence{{Capability: ChatText, Source: "provider_adapter", Rule: "vibecode.token-pricing", Confidence: "high", Reason: "Vibecode metadata identifies a non-image token-priced model"}}
	}
	return nil
}

var rules = []struct {
	re   *regexp.Regexp
	id   string
	rule string
}{
	{regexp.MustCompile(`(?i)(^|[-_.])text[-_.]?embedding|(^|[-_.])embedding(s)?($|[-_.])`), Embeddings, "name.embeddings"},
	{regexp.MustCompile(`(?i)(^|[-_.])(asr|transcription|whisper)($|[-_.])`), AudioTranscriptions, "name.audio-transcriptions"},
	{regexp.MustCompile(`(?i)(^|[-_.])(tts|speech)($|[-_.])`), AudioSpeech, "name.audio-speech"},
	{regexp.MustCompile(`(?i)(image[-_.]?edit|edit[-_.]?image)`), ImagesEdits, "name.images-edits"},
	{regexp.MustCompile(`(?i)(^|[-_.])(image|dall[-_.]?e|flux)($|[-_.])`), ImagesGenerations, "name.images-generations"},
	{regexp.MustCompile(`(?i)(^|[-_.])(video|sora)($|[-_.])`), VideosGenerations, "name.videos-generations"},
}

func nameRules(model string) []Evidence {
	var out []Evidence
	hasImageEdit := false
	for _, rule := range rules {
		if rule.re.MatchString(model) {
			if rule.id == ImagesEdits {
				hasImageEdit = true
			}
			out = append(out, Evidence{Capability: rule.id, Source: "model_id", Rule: rule.rule, Confidence: "medium", Reason: "model ID matched a conservative capability rule"})
		}
	}
	if hasImageEdit {
		filtered := out[:0]
		for _, item := range out {
			if item.Capability != ImagesGenerations {
				filtered = append(filtered, item)
			}
		}
		out = filtered
	}
	return out
}

func normalizeMetadataCapability(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer("_", ".", "-", ".", " ", ".")
	normalized = replacer.Replace(normalized)
	aliases := map[string]string{
		"chat": ChatText, "chat.completion": ChatText, "chat.completions": ChatText, "text.generation": ChatText, "text": ChatText,
		"image.generation": ImagesGenerations, "text.to.image": ImagesGenerations,
		"embedding": Embeddings, "embeddings": Embeddings, "text.embedding": Embeddings, "feature.extraction": Embeddings,
		"automatic.speech.recognition": AudioTranscriptions,
		"text.to.speech":               AudioSpeech,
	}
	if Known[normalized] {
		return normalized
	}
	return aliases[normalized]
}

func stringValues(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		var out []string
		for _, item := range typed {
			if value, ok := item.(string); ok {
				out = append(out, value)
			}
		}
		return out
	case []string:
		return typed
	case map[string]any:
		var out []string
		for key, raw := range typed {
			if enabled, ok := raw.(bool); ok && enabled {
				out = append(out, key)
			}
		}
		sort.Strings(out)
		return out
	default:
		return nil
	}
}

func numberPresent(value any) bool {
	switch value.(type) {
	case float64, float32, int, int64, json.Number:
		return true
	default:
		return false
	}
}

func deduplicate(in []Evidence) []Evidence {
	seen := map[string]bool{}
	var out []Evidence
	for _, item := range in {
		key := item.Capability + "\x00" + item.Rule
		if !seen[key] {
			seen[key] = true
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Capability == out[j].Capability {
			return out[i].Rule < out[j].Rule
		}
		return out[i].Capability < out[j].Capability
	})
	return out
}

func conflict(evidence []Evidence) bool {
	hasEmbedding, hasGeneration := false, false
	for _, item := range evidence {
		switch item.Capability {
		case Embeddings, AudioSpeech, AudioTranscriptions:
			hasEmbedding = true
		case ImagesGenerations, ImagesEdits, VideosGenerations:
			hasGeneration = true
		}
	}
	return hasEmbedding && hasGeneration
}
