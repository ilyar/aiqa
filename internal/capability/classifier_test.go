package capability

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGoogleAncillaryMetadataDoesNotOverrideMethodDeclaration(t *testing.T) {
	for _, policy := range []string{"inventory", "text"} {
		for _, methods := range []any{42, []any{"futureMethod"}, []any{"generateContent", 42}} {
			c := ClassifyGoogleGenAI("model", map[string]any{"supportedGenerationMethods": methods, "thinking": true, "vision": true}, Options{UnknownPolicy: policy})
			if !c.Unknown {
				t.Fatalf("%s: invalid/unrecognized methods became executable: %+v", policy, c)
			}
		}
		c := ClassifyGoogleGenAI("model", map[string]any{"supportedGenerationMethods": []any{}, "thinking": true}, Options{UnknownPolicy: policy})
		if c.Unknown || len(c.Capabilities) != 0 {
			t.Fatalf("%s: empty methods became executable: %+v", policy, c)
		}
	}
}

func TestVibecodeAdapterDistinguishesImageAndTokenModels(t *testing.T) {
	image := Classify("gpt-image-2", map[string]any{"is_image": true}, Options{BaseURL: "https://vibecode.moe/v1"})
	if !image.Has(ImagesGenerations) || image.Has(ChatText) {
		t.Fatalf("unexpected image classification: %+v", image)
	}
	chat := Classify("custom", map[string]any{"is_image": false, "pricing": map[string]any{"input_usd_per_m": 1.0}}, Options{BaseURL: "https://vibecode.moe/v1"})
	if !chat.Has(ChatText) {
		t.Fatalf("unexpected token-model classification: %+v", chat)
	}
}

func TestUnclassifiedModelDefaultsToLowConfidenceChat(t *testing.T) {
	got := Classify("custom-model", map[string]any{"id": "custom-model"}, Options{})
	if got.Unknown || !got.Has(ChatText) || len(got.Capabilities) != 1 {
		t.Fatalf("unexpected classification: %+v", got)
	}
	evidence := got.Capabilities[0]
	if evidence.Source != "default_policy" || evidence.Confidence != "low" {
		t.Fatalf("default assumption is not explicit: %+v", got)
	}

	strict := Classify("custom-model", nil, Options{UnknownPolicy: "inventory"})
	if !strict.Unknown || strict.Has(ChatText) {
		t.Fatalf("explicit inventory policy was ignored: %+v", strict)
	}
}

func TestProbePolicyMarksOnlyAbsentOpenAICapabilityMetadataEligible(t *testing.T) {
	eligible := Classify("opaque", map[string]any{"id": "opaque", "type": "model"}, Options{UnknownPolicy: "probe"})
	if eligible.Unknown || !eligible.ProbeEligible || len(eligible.Capabilities) != 0 {
		t.Fatalf("absent OpenAI metadata was not probe eligible: %+v", eligible)
	}
	for name, metadata := range map[string]map[string]any{
		"declared unknown":  {"capabilities": []any{"future-capability"}},
		"malformed":         {"capabilities": 42},
		"explicit negative": {"capabilities": map[string]any{"embeddings": false}},
		"foreign interface": {"interfaces": []any{"/v1/rerank"}},
		"negative image":    {"is_image": false},
	} {
		got := Classify("opaque", metadata, Options{UnknownPolicy: "probe"})
		if !got.Unknown || got.ProbeEligible {
			t.Fatalf("%s became probe eligible: %+v", name, got)
		}
	}
}

func TestBonsaiNestedTextModalitiesAreRecognized(t *testing.T) {
	got := Classify("Ternary-Bonsai-1.7B", map[string]any{
		"architecture": map[string]any{
			"input_modalities":  []any{"text"},
			"output_modalities": []any{"text"},
		},
	}, Options{})
	if got.Unknown || !got.Has(ChatText) {
		t.Fatalf("nested text modalities were not recognized: %+v", got)
	}
	if len(got.Capabilities) != 1 || got.Capabilities[0].Source != "vendor_metadata" || got.Capabilities[0].Confidence != "medium" {
		t.Fatalf("Bonsai classification did not preserve metadata evidence: %+v", got)
	}
}

func TestPositiveNonTextEvidencePreventsChatFallback(t *testing.T) {
	got := Classify("opaque-model", map[string]any{
		"architecture": map[string]any{"output_modalities": []any{"image"}},
	}, Options{})
	if got.Unknown || !got.Has(ImagesGenerations) || got.Has(ChatText) {
		t.Fatalf("non-text evidence was replaced by the chat fallback: %+v", got)
	}
}

func TestTextInputDoesNotOverrideEmbeddingEvidence(t *testing.T) {
	got := Classify("opaque-model", map[string]any{
		"input_modalities": []any{"text"},
		"pipeline_tag":     "feature-extraction",
	}, Options{})
	if got.Unknown || !got.Has(Embeddings) || got.Has(ChatText) {
		t.Fatalf("text input was mistaken for a chat capability: %+v", got)
	}
}

func TestExplicitRerankEvidencePreventsChatFallbackAndAllowsExplicitMultiCapability(t *testing.T) {
	rerank := Classify("opaque", map[string]any{"capabilities": map[string]any{"rerank": true}}, Options{})
	if rerank.Unknown || !rerank.Has(Rerank) || rerank.Has(ChatText) {
		t.Fatalf("rerank was classified as chat or unknown: %+v", rerank)
	}
	multi := Classify("opaque", map[string]any{"capabilities": map[string]any{"rerank": true, "chat": true}}, Options{})
	if multi.Unknown || !multi.Has(Rerank) || !multi.Has(ChatText) {
		t.Fatalf("explicit rerank/chat capabilities were not preserved: %+v", multi)
	}
}

func TestExplicitOverrideWins(t *testing.T) {
	got := Classify("image-name", map[string]any{"is_image": true}, Options{BaseURL: "https://vibecode.moe/v1", Overrides: map[string][]string{"image-name": {ChatText}}})
	if !got.Has(ChatText) || got.Has(ImagesGenerations) {
		t.Fatalf("override did not win: %+v", got)
	}
}

func TestModelCanHaveMultipleCapabilities(t *testing.T) {
	got := Classify("multimodal", nil, Options{Overrides: map[string][]string{
		"multimodal": {ChatText, ImagesGenerations},
	}})
	if got.Unknown || len(got.Capabilities) != 2 || !got.Has(ChatText) || !got.Has(ImagesGenerations) {
		t.Fatalf("multiple capabilities were not preserved: %+v", got)
	}
}

func TestConflictingPositiveRulesRemainUnknown(t *testing.T) {
	got := Classify("text-embedding-image", nil, Options{})
	if !got.Unknown || got.Has(ChatText) || got.Reason != "classification conflict" {
		t.Fatalf("conflict not preserved: %+v", got)
	}
}

func TestImageEditNameDoesNotAlsoInferGeneration(t *testing.T) {
	got := Classify("vendor-image-edit-v2", nil, Options{})
	if !got.Has(ImagesEdits) || got.Has(ImagesGenerations) || got.Unknown {
		t.Fatalf("image-edit classification is not specific: %+v", got)
	}
}

func TestStructuredCapabilityObjectIsRecognizedConservatively(t *testing.T) {
	got := Classify("opaque", map[string]any{"capabilities": map[string]any{
		"chat_completions": true,
		"vision":           true,
		"embeddings":       false,
	}}, Options{})
	if !got.Has(ChatText) || got.Has(Embeddings) || len(got.Capabilities) != 1 {
		t.Fatalf("unexpected structured metadata classification: %+v", got)
	}
}

func TestLoadOverrideFileValidatesCapabilityIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capabilities.json")
	if err := os.WriteFile(path, []byte(`{"models":{"custom":["chat.completions.text","images.generations"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	overrides, err := LoadOverrideFile(path)
	if err != nil || len(overrides["custom"]) != 2 {
		t.Fatalf("valid override file was not loaded: overrides=%v err=%v", overrides, err)
	}
	if err := os.WriteFile(path, []byte(`{"models":{"custom":["made.up"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOverrideFile(path); err == nil {
		t.Fatal("unknown capability in override file was accepted")
	}
}

func TestAnthropicClassificationUsesOnlyTargetMetadata(t *testing.T) {
	got := ClassifyAnthropic("image-looking-openai-name", map[string]any{
		"capabilities": map[string]any{
			"image_input":     map[string]any{"supported": true},
			"thinking_manual": map[string]any{"supported": true},
		},
	}, Options{UnknownPolicy: "messages"})
	for _, wanted := range []string{AnthropicMessagesText, AnthropicImageInput, AnthropicTools, AnthropicStructuredOutput, AnthropicThinkingManual} {
		if !got.Has(wanted) {
			t.Fatalf("missing Anthropic capability %q: %+v", wanted, got)
		}
	}
	if got.Has(ImagesGenerations) || got.Has(ChatText) {
		t.Fatalf("OpenAI capability leaked into Anthropic classification: %+v", got)
	}
}

func TestAnthropicInvalidModelInfoTimestampRemainsUnknown(t *testing.T) {
	got := ClassifyAnthropic("claude-invalid", map[string]any{
		"type": "model", "display_name": "Invalid Date", "created_at": "not-a-timestamp",
		"max_input_tokens": 1024, "max_tokens": 128,
	}, Options{UnknownPolicy: "inventory"})
	if !got.Unknown || got.Has(AnthropicMessagesText) {
		t.Fatalf("invalid Anthropic ModelInfo was treated as a trusted Messages route: %+v", got)
	}
}

func TestAnthropicInventoryPolicyDoesNotAssumeMessages(t *testing.T) {
	got := ClassifyAnthropic("opaque", map[string]any{"id": "opaque"}, Options{UnknownPolicy: "inventory"})
	if !got.Unknown || len(got.Capabilities) != 0 {
		t.Fatalf("inventory-only policy was ignored: %+v", got)
	}
}

func TestAnthropicProbePolicyAllowsPartialModelInfoOnlyWhenCapabilitiesAbsent(t *testing.T) {
	partial := map[string]any{"type": "model", "display_name": "Partial"}
	got := ClassifyAnthropic("partial", partial, Options{UnknownPolicy: "probe"})
	if got.Unknown || !got.ProbeEligible || got.Has(AnthropicMessagesText) {
		t.Fatalf("partial ModelInfo was not probe eligible: %+v", got)
	}
	negative := ClassifyAnthropic("negative", map[string]any{"capabilities": map[string]any{"text": map[string]any{"supported": false}}}, Options{UnknownPolicy: "probe"})
	if negative.Unknown || negative.ProbeEligible || negative.Has(AnthropicMessagesText) {
		t.Fatalf("explicit Anthropic text rejection became probe eligible: %+v", negative)
	}
}

func TestAnthropicProbePolicyDefersCompleteModelInfoAndForeignDeclarations(t *testing.T) {
	complete := map[string]any{
		"type": "model", "display_name": "Complete", "created_at": "2026-09-01T00:00:00Z",
		"max_input_tokens": 200000, "max_tokens": 8192,
	}
	if got := ClassifyAnthropic("complete", complete, Options{UnknownPolicy: "probe"}); got.Unknown || !got.ProbeEligible || got.Has(AnthropicMessagesText) {
		t.Fatalf("complete capability-free ModelInfo was not probe eligible: %+v", got)
	}
	for name, metadata := range map[string]map[string]any{
		"capabilities":          {"capabilities": []any{"rerank"}},
		"provider capabilities": {"provider_capabilities": []any{"rerank"}},
		"interfaces":            {"interfaces": []any{"/v1/rerank"}},
	} {
		for key, value := range complete {
			metadata[key] = value
		}
		got := ClassifyAnthropic("foreign", metadata, Options{UnknownPolicy: "probe"})
		if !got.Unknown || got.ProbeEligible || got.Has(AnthropicMessagesText) {
			t.Fatalf("%s declaration was inferred as Messages: %+v", name, got)
		}
	}
	override := ClassifyAnthropic("foreign", map[string]any{"capabilities": []any{"rerank"}}, Options{UnknownPolicy: "probe", Overrides: map[string][]string{"foreign": {AnthropicMessagesText}}})
	if override.Unknown || override.ProbeEligible || !override.Has(AnthropicMessagesText) {
		t.Fatalf("explicit Anthropic override lost precedence: %+v", override)
	}
}

func TestAnthropicCompleteModelInfoIdentifiesMessagesRoute(t *testing.T) {
	got := ClassifyAnthropic("claude-mock", map[string]any{
		"type": "model", "display_name": "Claude Mock", "created_at": "2026-09-01T00:00:00Z",
		"max_input_tokens": 200000, "max_tokens": 8192,
		"capabilities": map[string]any{"thinking": map[string]any{"supported": true}},
	}, Options{UnknownPolicy: "inventory"})
	if got.Unknown || !got.Has(AnthropicMessagesText) || len(got.Capabilities) != 1 {
		t.Fatalf("complete Anthropic ModelInfo was not trusted as Messages text: %+v", got)
	}
}

func TestAnthropicProbePolicyRoutesOnlyAbsentPartialCapabilities(t *testing.T) {
	metadata := map[string]any{
		"type": "model", "display_name": "Partial", "created_at": "2026-09-01T00:00:00Z",
		"max_input_tokens": 200000, "max_tokens": 8192,
		"capabilities": map[string]any{
			"structured_outputs": map[string]any{"supported": true},
			"image_input":        map[string]any{"supported": true},
		},
	}
	got := ClassifyAnthropic("partial", metadata, Options{UnknownPolicy: "probe"})
	if got.Unknown || got.ProbeEligible || !got.Has(AnthropicStructuredOutput) || !got.Has(AnthropicImageInput) || !got.ProbeHas(AnthropicTools) {
		t.Fatalf("partial capability probe plan=%+v", got)
	}
	for _, capability := range []string{AnthropicStructuredOutput, AnthropicImageInput} {
		if got.ProbeHas(capability) {
			t.Fatalf("declared capability %q became a probe: %+v", capability, got)
		}
	}
	for name, value := range map[string]any{
		"explicit false": map[string]any{"supported": false},
		"malformed":      []any{"tools"},
	} {
		metadata["capabilities"].(map[string]any)["tools"] = value
		got := ClassifyAnthropic("partial", metadata, Options{UnknownPolicy: "probe"})
		if got.ProbeHas(AnthropicTools) {
			t.Fatalf("%s tools metadata became probe eligible: %+v", name, got)
		}
	}
}

func TestAnthropicNestedThinkingTypesAreParsedWithoutProbePolicy(t *testing.T) {
	got := ClassifyAnthropic("thinking", map[string]any{"capabilities": map[string]any{
		"thinking": map[string]any{"types": map[string]any{
			"adaptive": map[string]any{"supported": true},
			"enabled":  map[string]any{"supported": true},
		}},
	}}, Options{UnknownPolicy: "inventory"})
	if got.Unknown || !got.Has(AnthropicThinkingAdaptive) || !got.Has(AnthropicThinkingManual) {
		t.Fatalf("nested thinking types were not classified: %+v", got)
	}
}

func TestAnthropicProbePolicyRejectsContradictoryAndMalformedFeatureMetadata(t *testing.T) {
	for name, metadata := range map[string]map[string]any{
		"tool aliases conflict": {"capabilities": map[string]any{"tools": false, "tool_use": true}},
		"thinking conflict":     {"capabilities": map[string]any{"thinking": map[string]any{"supported": false, "types": map[string]any{"adaptive": map[string]any{"supported": true}}}}},
		"malformed thinking":    {"capabilities": map[string]any{"thinking": []any{"adaptive"}}},
	} {
		got := ClassifyAnthropic("conflict", metadata, Options{UnknownPolicy: "probe"})
		if !got.Unknown || got.ProbeEligible || len(got.ProbeCapabilities) != 0 {
			t.Fatalf("%s became routable: %+v", name, got)
		}
	}
}

func TestAnthropicThinkingMetadataRejectsFlatNestedContradictions(t *testing.T) {
	for name, metadata := range map[string]map[string]any{
		"adaptive flat false nested true": {"capabilities": map[string]any{"thinking_adaptive": false, "thinking": map[string]any{"types": map[string]any{"adaptive": true}}}},
		"adaptive flat true nested false": {"capabilities": map[string]any{"thinking_adaptive": true, "thinking": map[string]any{"types": map[string]any{"adaptive": false}}}},
		"manual flat false nested true":   {"capabilities": map[string]any{"thinking_manual": false, "thinking": map[string]any{"types": map[string]any{"enabled": true}}}},
		"manual flat true nested false":   {"capabilities": map[string]any{"thinking_manual": true, "thinking": map[string]any{"types": map[string]any{"enabled": false}}}},
		"child internal conflict":         {"capabilities": map[string]any{"thinking": map[string]any{"types": map[string]any{"adaptive": map[string]any{"supported": false, "enabled": true}}}}},
	} {
		got := ClassifyAnthropic("thinking", metadata, Options{UnknownPolicy: "probe"})
		if !got.Unknown || got.Has(AnthropicThinkingAdaptive) || got.Has(AnthropicThinkingManual) || len(got.ProbeCapabilities) != 0 {
			t.Fatalf("%s became routable: %+v", name, got)
		}
	}
}

func TestAnthropicMessagesPolicyDoesNotOverrideExplicitToolDenial(t *testing.T) {
	got := ClassifyAnthropic("denied-tools", map[string]any{"capabilities": map[string]any{"tools": false}}, Options{UnknownPolicy: "messages"})
	if got.Has(AnthropicTools) {
		t.Fatalf("messages policy overrode explicit tools denial: %+v", got)
	}
}

func TestAnthropicPartialProbeRequiresMessagesRoute(t *testing.T) {
	got := ClassifyAnthropic("vision-only", map[string]any{"capabilities": map[string]any{
		"text":        map[string]any{"supported": false},
		"image_input": map[string]any{"supported": true},
	}}, Options{UnknownPolicy: "probe"})
	if got.Unknown || !got.Has(AnthropicImageInput) || got.ProbeHas(AnthropicMessagesText) || got.ProbeHas(AnthropicTools) {
		t.Fatalf("non-Messages image model gained probes: %+v", got)
	}
}

func TestAnthropicExplicitTextRejectionOverridesModelInfoAndDefault(t *testing.T) {
	metadata := map[string]any{
		"type": "model", "display_name": "Reranker", "created_at": "2026-09-01T00:00:00Z",
		"max_input_tokens": 200000, "max_tokens": 8192,
		"capabilities": map[string]any{"text": map[string]any{"supported": false}},
	}
	for _, policy := range []string{"inventory", "messages"} {
		got := ClassifyAnthropic("reranker", metadata, Options{UnknownPolicy: policy})
		if got.Unknown || got.Has(AnthropicMessagesText) {
			t.Fatalf("policy %q invented Messages text: %+v", policy, got)
		}
	}
}

func TestAnthropicContradictoryTextMetadataRemainsUnknown(t *testing.T) {
	cases := []map[string]any{
		{"capabilities": map[string]any{"text": false, "messages": true}},
		{"capabilities": map[string]any{"text": map[string]any{"supported": false, "enabled": true}}},
	}
	for _, metadata := range cases {
		for _, policy := range []string{"inventory", "messages"} {
			got := ClassifyAnthropic("contradictory", metadata, Options{UnknownPolicy: policy})
			if !got.Unknown || !got.Has(AnthropicMessagesText) {
				t.Fatalf("policy %q did not preserve contradictory text evidence: %+v", policy, got)
			}
		}
	}
}

func TestGoogleGenerationMethodsDoNotInferInteractions(t *testing.T) {
	got := ClassifyGoogleGenAI("gemini-custom", map[string]any{
		"name":                       "models/gemini-custom",
		"supportedGenerationMethods": []any{"generateContent", "countTokens"},
	}, Options{UnknownPolicy: "inventory"})
	if got.Unknown || !got.Has(GoogleGenerateText) || !got.Has(GoogleCountTokens) || got.Has(GoogleInteractionsText) {
		t.Fatalf("Google method routing inferred the wrong capabilities: %+v", got)
	}
}

func TestGoogleInteractionsMethodIdentifiesInteractionsOnly(t *testing.T) {
	got := ClassifyGoogleGenAI("local-interactions", map[string]any{
		"name":                       "models/local-interactions",
		"supportedGenerationMethods": []any{"interactions"},
	}, Options{UnknownPolicy: "inventory"})
	if got.Unknown || !got.Has(GoogleInteractionsText) || got.Has(GoogleGenerateText) {
		t.Fatalf("Interactions metadata was classified incorrectly: %+v", got)
	}
}

func TestGoogleUnknownPolicyAndMetadata(t *testing.T) {
	unknown := ClassifyGoogleGenAI("opaque", map[string]any{"name": "models/opaque"}, Options{UnknownPolicy: "inventory"})
	if !unknown.Unknown || unknown.Has(GoogleGenerateText) {
		t.Fatalf("inventory policy assumed a Google text model: %+v", unknown)
	}
	text := ClassifyGoogleGenAI("opaque", map[string]any{"name": "models/opaque"}, Options{UnknownPolicy: "text"})
	if text.Unknown || !text.Has(GoogleGenerateText) {
		t.Fatalf("text fallback did not classify Google model: %+v", text)
	}
	embed := ClassifyGoogleGenAI("embed", map[string]any{"supportedGenerationMethods": []any{"embedContent"}}, Options{UnknownPolicy: "text"})
	if embed.Unknown || !embed.Has(GoogleEmbedContent) || embed.Has(GoogleGenerateText) {
		t.Fatalf("embedding-only Google model was routed to generation: %+v", embed)
	}
}

func TestGoogleProbePolicyRequiresAbsentCapabilityMetadata(t *testing.T) {
	eligible := ClassifyGoogleGenAI("opaque", map[string]any{"name": "models/opaque"}, Options{UnknownPolicy: "probe"})
	if eligible.Unknown || !eligible.ProbeEligible || len(eligible.Capabilities) != 0 {
		t.Fatalf("absent Google metadata was not probe eligible: %+v", eligible)
	}
	for name, metadata := range map[string]map[string]any{
		"empty":     {"supportedGenerationMethods": []any{}},
		"malformed": {"supportedGenerationMethods": "generateContent"},
		"future":    {"supportedGenerationMethods": []any{"futureMethod"}},
		"negative":  {"thinking": false},
	} {
		got := ClassifyGoogleGenAI("opaque", metadata, Options{UnknownPolicy: "probe"})
		if got.ProbeEligible || name != "empty" && !got.Unknown {
			t.Fatalf("%s Google metadata became probe eligible: %+v", name, got)
		}
	}
}

func TestGoogleProbePolicyRejectsForeignCapabilityDeclarations(t *testing.T) {
	for name, metadata := range map[string]map[string]any{
		"capabilities":          {"capabilities": []any{"rerank"}},
		"provider capabilities": {"provider_capabilities": []any{"rerank"}},
		"interfaces":            {"interfaces": []any{"/v1/rerank"}},
	} {
		got := ClassifyGoogleGenAI("foreign", metadata, Options{UnknownPolicy: "probe"})
		if !got.Unknown || got.ProbeEligible || got.Has(GoogleGenerateText) {
			t.Fatalf("%s declaration became generation probe eligible: %+v", name, got)
		}
	}
	override := ClassifyGoogleGenAI("foreign", map[string]any{"capabilities": []any{"rerank"}}, Options{UnknownPolicy: "probe", Overrides: map[string][]string{"foreign": {GoogleGenerateText}}})
	if override.Unknown || override.ProbeEligible || !override.Has(GoogleGenerateText) {
		t.Fatalf("explicit Google override lost precedence: %+v", override)
	}
}

func TestGoogleGenerationMethodsEmptyMalformedAndFutureValues(t *testing.T) {
	empty := ClassifyGoogleGenAI("empty", map[string]any{"supportedGenerationMethods": []any{}}, Options{UnknownPolicy: "text"})
	if empty.Unknown || len(empty.Capabilities) != 0 || empty.Has(GoogleGenerateText) {
		t.Fatalf("empty method list invented generation support: %+v", empty)
	}
	for _, policy := range []string{"inventory", "text"} {
		malformed := ClassifyGoogleGenAI("bad", map[string]any{"supportedGenerationMethods": "generateContent"}, Options{UnknownPolicy: policy})
		if !malformed.Unknown || malformed.Has(GoogleGenerateText) {
			t.Fatalf("policy %q accepted malformed method list: %+v", policy, malformed)
		}
		future := ClassifyGoogleGenAI("future", map[string]any{"supportedGenerationMethods": []any{"futureMethod"}}, Options{UnknownPolicy: policy})
		if !future.Unknown || future.Has(GoogleGenerateText) {
			t.Fatalf("policy %q treated unrecognized future method as generation: %+v", policy, future)
		}
	}
}
