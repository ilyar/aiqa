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

func TestUnknownDoesNotBecomeChatWithoutPolicy(t *testing.T) {
	got := Classify("custom-model", map[string]any{"id": "custom-model"}, Options{})
	if !got.Unknown || got.Has(ChatText) {
		t.Fatalf("unexpected classification: %+v", got)
	}
	legacy := Classify("custom-model", nil, Options{UnknownPolicy: "chat"})
	if !legacy.Has(ChatText) {
		t.Fatalf("explicit legacy policy was ignored: %+v", legacy)
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
	if !got.Unknown || got.Reason != "classification conflict" {
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
