package capability

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
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
	if opts.UnknownPolicy == "chat" || opts.UnknownPolicy == "" {
		return fromValues(model, []string{ChatText}, "default_policy", "unclassified-default.chat", "low", "no evidence of a non-text capability; treated as text by default")
	}
	if opts.UnknownPolicy == "probe" && !openAICapabilityDeclared(metadata) {
		return Classification{Model: model, ProbeEligible: true, Reason: "capability metadata absent; support being tested"}
	}
	return Classification{Model: model, Unknown: true, Reason: "insufficient capability evidence"}
}

func openAICapabilityDeclared(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	for _, key := range []string{"capabilities", "provider_capabilities", "interfaces", "output_modalities", "modalities", "model_type", "task", "pipeline_tag", "is_image"} {
		if _, present := metadata[key]; present {
			return true
		}
	}
	if value, present := metadata["type"]; present {
		if text, ok := value.(string); !ok || !strings.EqualFold(strings.TrimSpace(text), "model") {
			return true
		}
	}
	if architecture, present := metadata["architecture"]; present {
		values, ok := architecture.(map[string]any)
		if !ok {
			return true
		}
		for _, key := range []string{"output_modalities", "modalities"} {
			if _, present := values[key]; present {
				return true
			}
		}
	}
	return false
}

// ClassifyAnthropic uses only target-local overrides and Anthropic model
// metadata. It deliberately does not reuse OpenAI provider or model-name rules.
func ClassifyAnthropic(model string, metadata map[string]any, opts Options) Classification {
	if values := opts.Overrides[model]; len(values) > 0 {
		return fromValues(model, values, "override", "explicit.anthropic-model-capability", "high", "explicit Anthropic capability override")
	}

	var evidence []Evidence
	textExplicitlyUnsupported := anthropicTextExplicitlyUnsupported(metadata)
	add := func(capability, rule, confidence, reason string) {
		evidence = append(evidence, Evidence{Capability: capability, Source: "vendor_metadata", Rule: rule, Confidence: confidence, Reason: reason})
	}
	checks := []struct {
		keys       []string
		capability string
	}{
		{[]string{"text", "messages", "messages_text"}, AnthropicMessagesText},
		{[]string{"image_input", "vision"}, AnthropicImageInput},
		{[]string{"tools", "tool_use"}, AnthropicTools},
		{[]string{"structured_output", "structured_outputs"}, AnthropicStructuredOutput},
		{[]string{"prompt_caching"}, AnthropicPromptCaching},
	}
	capabilities, _ := metadata["capabilities"].(map[string]any)
	for _, check := range checks {
		state := anthropicCapabilityState(metadata, capabilities, check.keys)
		if state.conflict {
			add(check.capability, "metadata.capabilities."+check.keys[0], "high", "Anthropic model metadata has contradictory "+check.keys[0]+" declarations")
			return Classification{Model: model, Unknown: true, Reason: "contradictory Anthropic " + check.capability + " metadata", Capabilities: deduplicate(evidence)}
		}
		if state.malformed {
			return Classification{Model: model, Unknown: true, Reason: "malformed Anthropic " + check.capability + " metadata"}
		}
		if state.supported {
			add(check.capability, "metadata.capabilities."+check.keys[0], "high", "Anthropic model metadata declares "+check.keys[0]+" support")
		}
	}
	adaptive, manual, thinkingState := anthropicThinkingCapabilities(metadata, capabilities)
	if thinkingState != "" {
		return Classification{Model: model, Unknown: true, Reason: thinkingState}
	}
	if adaptive.supported {
		add(AnthropicThinkingAdaptive, "metadata.capabilities.thinking.types.adaptive", "high", "Anthropic model metadata declares adaptive thinking support")
	}
	if manual.supported {
		add(AnthropicThinkingManual, "metadata.capabilities.thinking.types.enabled", "high", "Anthropic model metadata declares enabled thinking support")
	}
	if opts.UnknownPolicy == "probe" && len(evidence) == 0 && !textExplicitlyUnsupported {
		if !anthropicCapabilityDeclared(metadata) {
			return Classification{Model: model, ProbeEligible: true, Reason: "capability metadata absent; support being tested"}
		}
		return Classification{Model: model, Unknown: true, Reason: "insufficient Anthropic capability evidence"}
	}
	if !textExplicitlyUnsupported && anthropicModelInfoIsMessages(metadata) {
		messagesKnown := false
		for _, item := range evidence {
			if item.Capability == AnthropicMessagesText {
				messagesKnown = true
				break
			}
		}
		if !messagesKnown {
			evidence = append(evidence, Evidence{
				Capability: AnthropicMessagesText, Source: "vendor_metadata",
				Rule: "anthropic.model_info.messages", Confidence: "high",
				Reason: "complete Anthropic ModelInfo metadata identifies a Messages model",
			})
		}
	}
	evidence = deduplicate(evidence)
	if textExplicitlyUnsupported && hasCapability(evidence, AnthropicMessagesText) {
		return Classification{Model: model, Unknown: true, Reason: "contradictory Messages text capability metadata", Capabilities: evidence}
	}
	if textExplicitlyUnsupported && len(evidence) == 0 {
		return Classification{Model: model, Reason: "metadata explicitly declares Messages text unsupported"}
	}
	if opts.UnknownPolicy == "inventory" && len(evidence) == 0 {
		return Classification{Model: model, Unknown: true, Reason: "insufficient Anthropic capability evidence"}
	}
	if (opts.UnknownPolicy == "messages" || opts.UnknownPolicy == "") && !textExplicitlyUnsupported {
		// Models returned by the Anthropic inventory are treated as Messages text
		// models unless metadata or an override says otherwise. Optional tools and
		// structured-output modules remain probeable at low confidence so a clean
		// 4xx becomes UNSUP rather than N/A.
		defaults := []Evidence{
			{Capability: AnthropicMessagesText, Source: "default_policy", Rule: "anthropic.unclassified-default.messages", Confidence: "low", Reason: "Anthropic inventory model defaults to Messages text"},
			{Capability: AnthropicTools, Source: "default_policy", Rule: "anthropic.optional-probe.tools", Confidence: "low", Reason: "tools are safe to probe on a Messages text model"},
			{Capability: AnthropicStructuredOutput, Source: "default_policy", Rule: "anthropic.optional-probe.structured-output", Confidence: "low", Reason: "structured output is safe to probe on a Messages text model"},
		}
		for _, item := range defaults {
			if anthropicCapabilityState(metadata, capabilities, anthropicCapabilityKeys(item.Capability)).unsupported {
				continue
			}
			found := false
			for _, existing := range evidence {
				if existing.Capability == item.Capability {
					found = true
					break
				}
			}
			if !found {
				evidence = append(evidence, item)
			}
		}
	}
	if len(evidence) == 0 {
		return Classification{Model: model, Unknown: true, Reason: "insufficient Anthropic capability evidence"}
	}
	evidence = deduplicate(evidence)
	classification := Classification{Model: model, Capabilities: evidence}
	if opts.UnknownPolicy == "probe" {
		classification.ProbeCapabilities = anthropicProbeCapabilities(metadata, evidence)
	}
	return classification
}

// anthropicProbeCapabilities identifies individual absent, low-risk Messages
// features. It never represents support: explicit negative, malformed, or
// contradictory declarations suppress only that feature's probe route.
func anthropicProbeCapabilities(metadata map[string]any, evidence []Evidence) []string {
	// Individual feature absence is meaningful only after the inventory has
	// established that this is a Messages model.  A non-text model must never
	// gain Messages probes merely because another feature was advertised.
	if !hasCapability(evidence, AnthropicMessagesText) {
		return nil
	}
	checks := []struct {
		capability string
		keys       []string
	}{
		{AnthropicMessagesText, []string{"text", "messages", "messages_text"}},
		{AnthropicImageInput, []string{"image_input", "vision"}},
		{AnthropicTools, []string{"tools", "tool_use"}},
		{AnthropicStructuredOutput, []string{"structured_output", "structured_outputs"}},
	}
	capabilities, capabilitiesOK := metadata["capabilities"].(map[string]any)
	if _, declared := metadata["capabilities"]; declared && !capabilitiesOK {
		return nil
	}
	var probe []string
	for _, check := range checks {
		if hasCapability(evidence, check.capability) {
			continue
		}
		present, safe := false, true
		for _, key := range check.keys {
			for _, source := range []map[string]any{metadata, capabilities} {
				if source == nil {
					continue
				}
				value, exists := source[key]
				if !exists {
					continue
				}
				present = true
				if capabilityExplicitlyUnsupported(value) || !capabilitySupported(value) {
					safe = false
				}
			}
		}
		if !present && safe {
			probe = append(probe, check.capability)
		}
	}
	return probe
}

type anthropicCapabilityMetadataState struct {
	supported   bool
	unsupported bool
	conflict    bool
	malformed   bool
}

func anthropicCapabilityState(metadata, capabilities map[string]any, keys []string) anthropicCapabilityMetadataState {
	var state anthropicCapabilityMetadataState
	for _, key := range keys {
		for _, source := range []map[string]any{metadata, capabilities} {
			if source == nil {
				continue
			}
			value, present := source[key]
			if !present {
				continue
			}
			state.observe(value)
		}
	}
	return state
}

func anthropicCapabilityKeys(capability string) []string {
	switch capability {
	case AnthropicMessagesText:
		return []string{"text", "messages", "messages_text"}
	case AnthropicImageInput:
		return []string{"image_input", "vision"}
	case AnthropicTools:
		return []string{"tools", "tool_use"}
	case AnthropicStructuredOutput:
		return []string{"structured_output", "structured_outputs"}
	}
	return nil
}

func (state *anthropicCapabilityMetadataState) observe(value any) {
	supported := capabilitySupported(value)
	unsupported := capabilityExplicitlyUnsupported(value)
	if !supported && !unsupported {
		state.malformed = true
		return
	}
	state.supported = state.supported || supported
	state.unsupported = state.unsupported || unsupported
	state.conflict = state.supported && state.unsupported
}

func anthropicThinkingCapabilities(metadata, capabilities map[string]any) (anthropicCapabilityMetadataState, anthropicCapabilityMetadataState, string) {
	adaptive := anthropicCapabilityState(metadata, capabilities, []string{"thinking_adaptive", "adaptive_thinking"})
	manual := anthropicCapabilityState(metadata, capabilities, []string{"thinking_manual", "manual_thinking"})
	raw, present := capabilities["thinking"]
	if !present {
		return validatedAnthropicThinking(adaptive, manual)
	}
	thinking, ok := raw.(map[string]any)
	if !ok {
		return adaptive, manual, "malformed Anthropic thinking metadata"
	}
	parent, parentPresent := thinking["supported"]
	if parentPresent {
		supported, ok := parent.(bool)
		if !ok {
			return adaptive, manual, "malformed Anthropic thinking metadata"
		}
		if !supported {
			adaptive.observe(false)
			manual.observe(false)
		}
	}
	rawTypes, typesPresent := thinking["types"]
	if !typesPresent {
		return validatedAnthropicThinking(adaptive, manual)
	}
	types, ok := rawTypes.(map[string]any)
	if !ok {
		return adaptive, manual, "malformed Anthropic thinking types metadata"
	}
	for _, item := range []struct {
		key   string
		state *anthropicCapabilityMetadataState
	}{
		{"adaptive", &adaptive},
		{"enabled", &manual},
	} {
		value, present := types[item.key]
		if !present {
			continue
		}
		if !capabilitySupported(value) && !capabilityExplicitlyUnsupported(value) {
			return adaptive, manual, "malformed Anthropic thinking types metadata"
		}
		item.state.observe(value)
	}
	return validatedAnthropicThinking(adaptive, manual)
}

func validatedAnthropicThinking(adaptive, manual anthropicCapabilityMetadataState) (anthropicCapabilityMetadataState, anthropicCapabilityMetadataState, string) {
	if adaptive.conflict || manual.conflict {
		return adaptive, manual, "contradictory Anthropic thinking metadata"
	}
	if adaptive.malformed || manual.malformed {
		return adaptive, manual, "malformed Anthropic thinking types metadata"
	}
	return adaptive, manual, ""
}

func anthropicCapabilityDeclared(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	for _, key := range []string{"capabilities", "provider_capabilities", "interfaces"} {
		if _, present := metadata[key]; present {
			return true
		}
	}
	for _, key := range []string{"text", "messages", "messages_text", "image_input", "vision", "tools", "tool_use", "structured_output", "structured_outputs", "thinking_adaptive", "adaptive_thinking", "thinking_manual", "manual_thinking", "prompt_caching"} {
		if _, present := metadata[key]; present {
			return true
		}
	}
	return false
}

func anthropicTextExplicitlyUnsupported(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	capabilities, _ := metadata["capabilities"].(map[string]any)
	for _, key := range []string{"text", "messages", "messages_text"} {
		if capabilityExplicitlyUnsupported(metadata[key]) || capabilityExplicitlyUnsupported(capabilities[key]) {
			return true
		}
	}
	return false
}

func capabilityExplicitlyUnsupported(value any) bool {
	switch typed := value.(type) {
	case bool:
		return !typed
	case map[string]any:
		for _, key := range []string{"supported", "enabled", "available"} {
			if enabled, ok := typed[key].(bool); ok && !enabled {
				return true
			}
		}
	}
	return false
}

func anthropicModelInfoIsMessages(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	typeValue, _ := metadata["type"].(string)
	displayName, _ := metadata["display_name"].(string)
	createdAt, _ := metadata["created_at"].(string)
	maxInput, inputOK := positiveNumber(metadata["max_input_tokens"])
	maxOutput, outputOK := positiveNumber(metadata["max_tokens"])
	_, createdOK := time.Parse(time.RFC3339, strings.TrimSpace(createdAt))
	return strings.TrimSpace(typeValue) == "model" && strings.TrimSpace(displayName) != "" && createdOK == nil && inputOK && outputOK && maxInput > 0 && maxOutput > 0
}

func positiveNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), number > 0
	case int64:
		return float64(number), number > 0
	case float64:
		return number, number > 0
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil && parsed > 0
	default:
		return 0, false
	}
}

// ClassifyGoogleGenAI uses only Google model metadata and explicit target-local
// overrides. In particular, a generateContent method is not evidence that the
// model supports the distinct Interactions API.
func ClassifyGoogleGenAI(model string, metadata map[string]any, opts Options) Classification {
	if values := opts.Overrides[model]; len(values) > 0 {
		return fromValues(model, values, "override", "explicit.google-genai-model-capability", "high", "explicit Google GenAI capability override")
	}
	var evidence []Evidence
	methods, methodsPresent, methodsValid := googleGenerationMethods(metadata)
	if methodsPresent && !methodsValid {
		return Classification{Model: model, Unknown: true, Reason: "supported generation methods metadata is malformed"}
	}
	if methodsPresent && len(methods) == 0 {
		return Classification{Model: model, Reason: "metadata explicitly declares no supported generation methods"}
	}
	has := func(want string) bool {
		for _, method := range methods {
			if strings.EqualFold(strings.TrimSpace(method), want) {
				return true
			}
		}
		return false
	}
	if has("generateContent") {
		evidence = append(evidence,
			Evidence{Capability: GoogleGenerateText, Source: "vendor_metadata", Rule: "metadata.supportedGenerationMethods.generateContent", Confidence: "high", Reason: "Google metadata declares generateContent support"},
			Evidence{Capability: GoogleGenerateTools, Source: "vendor_metadata", Rule: "metadata.supportedGenerationMethods.generateContent", Confidence: "medium", Reason: "generateContent is eligible for client-side tool probing"},
			Evidence{Capability: GoogleGenerateStructured, Source: "vendor_metadata", Rule: "metadata.supportedGenerationMethods.generateContent", Confidence: "medium", Reason: "generateContent is eligible for local structured-output probing"},
		)
	}
	if has("interactions") {
		evidence = append(evidence,
			Evidence{Capability: GoogleInteractionsText, Source: "vendor_metadata", Rule: "metadata.supportedGenerationMethods.interactions", Confidence: "high", Reason: "Google metadata declares Interactions support"},
		)
	}
	if has("countTokens") {
		evidence = append(evidence, Evidence{Capability: GoogleCountTokens, Source: "vendor_metadata", Rule: "metadata.supportedGenerationMethods.countTokens", Confidence: "high", Reason: "Google metadata declares countTokens support"})
	}
	if has("embedContent") {
		evidence = append(evidence, Evidence{Capability: GoogleEmbedContent, Source: "vendor_metadata", Rule: "metadata.supportedGenerationMethods.embedContent", Confidence: "high", Reason: "Google metadata declares embedContent support"})
	}
	if methodsPresent && len(evidence) == 0 {
		return Classification{Model: model, Unknown: true, Reason: "supported generation methods are unrecognized"}
	}
	if capabilitySupported(metadata["thinking"]) {
		evidence = append(evidence,
			Evidence{Capability: GoogleGenerateThinking, Source: "vendor_metadata", Rule: "metadata.thinking", Confidence: "high", Reason: "Google metadata declares thinking support"},
			Evidence{Capability: GoogleInteractionsThinking, Source: "vendor_metadata", Rule: "metadata.thinking", Confidence: "medium", Reason: "thinking metadata is eligible for Interactions thinking probes only when the surface is selected"},
		)
	}
	if capabilitySupported(metadata["vision"]) || containsString(stringValues(metadata["inputModalities"]), "image") || containsString(stringValues(metadata["input_modalities"]), "image") {
		evidence = append(evidence,
			Evidence{Capability: GoogleGenerateImage, Source: "vendor_metadata", Rule: "metadata.inputModalities.image", Confidence: "high", Reason: "Google metadata declares image input"},
			Evidence{Capability: GoogleInteractionsImage, Source: "vendor_metadata", Rule: "metadata.inputModalities.image", Confidence: "medium", Reason: "image input is eligible for Interactions vision probes only when the surface is selected"},
		)
	}
	evidence = deduplicate(evidence)
	if len(evidence) == 0 {
		if opts.UnknownPolicy == "text" {
			return fromValues(model, []string{GoogleGenerateText}, "default_policy", "google-genai.unclassified-default.text", "low", "no contrary metadata; treated as a text generation model")
		}
		if opts.UnknownPolicy == "probe" && !googleCapabilityDeclared(metadata) {
			return Classification{Model: model, ProbeEligible: true, Reason: "capability metadata absent; support being tested"}
		}
		return Classification{Model: model, Unknown: true, Reason: "insufficient Google GenAI capability evidence"}
	}
	return Classification{Model: model, Capabilities: evidence}
}

func googleCapabilityDeclared(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	for _, key := range []string{"supportedGenerationMethods", "capabilities", "provider_capabilities", "interfaces", "thinking", "vision", "inputModalities", "input_modalities"} {
		if _, present := metadata[key]; present {
			return true
		}
	}
	return false
}

func hasCapability(evidence []Evidence, capability string) bool {
	for _, item := range evidence {
		if item.Capability == capability {
			return true
		}
	}
	return false
}

func googleGenerationMethods(metadata map[string]any) ([]string, bool, bool) {
	if metadata == nil {
		return nil, false, false
	}
	raw, present := metadata["supportedGenerationMethods"]
	if !present {
		return nil, false, false
	}
	switch methods := raw.(type) {
	case []string:
		return methods, true, true
	case []any:
		values := make([]string, 0, len(methods))
		for _, method := range methods {
			value, ok := method.(string)
			if !ok {
				return nil, true, false
			}
			values = append(values, value)
		}
		return values, true, true
	default:
		return nil, true, false
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), wanted) {
			return true
		}
	}
	return false
}

func capabilitySupported(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "supported", "enabled", "yes":
			return true
		}
	case map[string]any:
		for _, key := range []string{"supported", "enabled", "available"} {
			if enabled, ok := typed[key].(bool); ok && enabled {
				return true
			}
		}
	}
	return false
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
	// is_image=true is direct model metadata, not a Vibecode naming
	// convention. Honor the affirmative declaration for every provider.
	if isImage, exists := metadata["is_image"].(bool); exists && isImage {
		return []Evidence{{Capability: ImagesGenerations, Source: "vendor_metadata", Rule: "metadata.is_image.true", Confidence: "high", Reason: "model metadata declares image generation"}}
	}
	u, _ := url.Parse(baseURL)
	if u == nil || !strings.EqualFold(u.Hostname(), "vibecode.moe") {
		return nil
	}
	isImage, exists := metadata["is_image"].(bool)
	if !exists {
		return nil
	}
	if isImage {
		return nil
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
		"rerank": Rerank, "reranking": Rerank, "text.reranking": Rerank,
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
		case Embeddings, Rerank, AudioSpeech, AudioTranscriptions:
			hasEmbedding = true
		case ImagesGenerations, ImagesEdits, VideosGenerations:
			hasGeneration = true
		}
	}
	return hasEmbedding && hasGeneration
}
