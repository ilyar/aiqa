package anthropic

import (
	"encoding/json"
	"reflect"
	"runtime/debug"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/ilyar/aiqa/internal/profile"
)

func TestManifestRetainsLegacyCheckerCoverage(t *testing.T) {
	var got []string
	for _, suite := range New().Manifest().Suites {
		got = append(got, suite.ID)
	}
	want := []string{"count-tokens", "error.responses", "messages", "messages.multi-turn", "messages.stream", "messages.system", "models.get", "models.list", "structured-output", "tools", "tools.stream"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Anthropic Official SDK parity changed: got %v, want %v", got, want)
	}
}

func TestManifestSuitesAreProfileBoundAndVersionMatchesDependency(t *testing.T) {
	p, err := profile.LoadFile("anthropic-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, suite := range New().Manifest().Suites {
		if _, ok := p.ExtensionBinding(extensionID, suite.ID); !ok {
			t.Errorf("suite %q has no explicit profile binding", suite.ID)
		}
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("Go build information is unavailable")
	}
	for _, dependency := range info.Deps {
		if dependency.Path == "github.com/anthropics/anthropic-sdk-go" {
			if dependency.Version != sdkVersion {
				t.Fatalf("embedded SDK version=%q, manifest=%q", dependency.Version, sdkVersion)
			}
			return
		}
	}
	t.Fatal("embedded Anthropic SDK dependency is missing")
}

func TestToolSuitePromptSchemaAndValidatorShareOneContract(t *testing.T) {
	if toolPrompt != "Call get_status for resource-alpha." {
		t.Fatalf("tool prompt=%q", toolPrompt)
	}
	definition := toolDefinition()
	if definition["name"] != "get_status" {
		t.Fatalf("tool name=%v", definition["name"])
	}
	schema, ok := definition["input_schema"].(map[string]any)
	if !ok || schema["type"] != "object" || schema["additionalProperties"] != false || !reflect.DeepEqual(schema["required"], []string{"resource_id"}) {
		t.Fatalf("tool input schema=%#v", definition["input_schema"])
	}
	properties, ok := schema["properties"].(map[string]any)
	resource, _ := properties["resource_id"].(map[string]any)
	if !ok || resource["type"] != "string" {
		t.Fatalf("resource_id schema=%#v", properties["resource_id"])
	}
	valid := &anthropic.Message{Content: []anthropic.ContentBlockUnion{{Type: "tool_use", ID: "toolu_test", Name: "get_status", Input: json.RawMessage(`{"resource_id":"resource-alpha"}`)}}}
	if _, err := sdkToolUse(valid); err != nil {
		t.Fatalf("valid tool use rejected: %v", err)
	}
	invalid := &anthropic.Message{Content: []anthropic.ContentBlockUnion{{Type: "tool_use", ID: "toolu_test", Name: "get_status", Input: json.RawMessage(`{"resource_id":"resource-beta"}`)}}}
	if _, err := sdkToolUse(invalid); err == nil {
		t.Fatal("validator accepted a tool use for the wrong resource")
	}
}
