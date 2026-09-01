package capability

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestAnthropicInventoryPolicyDoesNotAssumeMessages(t *testing.T) {
	got := ClassifyAnthropic("opaque", map[string]any{"id": "opaque"}, Options{UnknownPolicy: "inventory"})
	if !got.Unknown || len(got.Capabilities) != 0 {
		t.Fatalf("inventory-only policy was ignored: %+v", got)
	}
}
