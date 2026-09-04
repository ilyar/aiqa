package protocol

import (
	"testing"

	"github.com/ilyar/aiqa/internal/api"
)

func TestSelectModelsUsesExactTargetInventoryAndPreservesRequestedOrder(t *testing.T) {
	discovered := []api.Model{
		{ID: "gpt-5.4-mini", Raw: map[string]any{"id": "gpt-5.4-mini", "object": "model", "created": 1}},
		{ID: "claude-sonnet-5", Raw: map[string]any{"id": "claude-sonnet-5", "object": "model", "created": 1}},
		{ID: "nano-banana-2-lite", Raw: map[string]any{"id": "nano-banana-2-lite", "object": "model", "created": 1, "is_image": true}},
		{ID: "ambiguous", Raw: map[string]any{"id": "ambiguous"}},
	}
	requested := []string{"nano-banana-2-lite", "missing", "gpt-5.4-mini", "nano-banana-2-lite", "ambiguous"}
	selected, metadata := SelectModels(requested, discovered, ExactModelID, func(raw map[string]any) bool {
		_, object := raw["object"].(string)
		_, created := raw["created"]
		return object && created
	})
	want := []string{"nano-banana-2-lite", "gpt-5.4-mini", "ambiguous"}
	if len(selected) != len(want) {
		t.Fatalf("selected=%v, want %v", selected, want)
	}
	for i := range want {
		if selected[i] != want[i] {
			t.Fatalf("selected=%v, want %v", selected, want)
		}
	}
	if _, ok := metadata["missing"]; ok {
		t.Fatal("missing requested model received synthetic metadata")
	}

	auto, _ := SelectModels(nil, discovered, ExactModelID, func(raw map[string]any) bool {
		_, object := raw["object"].(string)
		_, created := raw["created"]
		return object && created
	})
	if got := len(auto); got != 3 {
		t.Fatalf("trusted auto selection=%v, want three trusted entries", auto)
	}
	for _, id := range auto {
		if id == "ambiguous" {
			t.Fatalf("ambiguous unrequested entry was scheduled: %v", auto)
		}
	}
}

func TestSelectModelsKeepsGoogleResourcePrefixAsTheOnlyNormalization(t *testing.T) {
	discovered := []api.Model{{ID: "nano-banana-2-lite", Raw: map[string]any{"name": "models/nano-banana-2-lite"}}}
	normalize := func(id string) string {
		return trimGoogleResource(id)
	}
	selected, _ := SelectModels([]string{"models/nano-banana-2-lite", "nano-banana-2-lite"}, discovered, normalize, func(raw map[string]any) bool {
		return raw["name"] == "models/nano-banana-2-lite"
	})
	if len(selected) != 1 || selected[0] != "nano-banana-2-lite" {
		t.Fatalf("Google normalized selection=%v", selected)
	}
	selected, _ = SelectModels([]string{"Nano-Banana-2-Lite"}, discovered, normalize, nil)
	if len(selected) != 0 {
		t.Fatalf("Google matching became case-insensitive: %v", selected)
	}
}

func trimGoogleResource(id string) string {
	if len(id) >= len("models/") && id[:len("models/")] == "models/" {
		return id[len("models/"):]
	}
	return id
}
