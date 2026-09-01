package openai

import (
	"reflect"
	"runtime/debug"
	"testing"

	"github.com/ilyar/aiqa/internal/profile"
)

func TestManifestRetainsUsefulLegacyCheckerCoverage(t *testing.T) {
	var got []string
	for _, suite := range New().Manifest().Suites {
		got = append(got, suite.ID)
	}
	want := []string{"chat.completions", "chat.json", "chat.multi-turn", "chat.stream", "chat.stream.usage", "chat.tools", "chat.tools.stream", "error.responses", "images.generations", "models.get", "models.list"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("OpenAI Official SDK parity changed: got %v, want %v", got, want)
	}
}

func TestManifestSuitesAreProfileBoundAndVersionMatchesDependency(t *testing.T) {
	p, err := profile.LoadFile("openai-compat-2026-09.json")
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
		if dependency.Path == "github.com/openai/openai-go/v3" {
			if dependency.Version != sdkVersion {
				t.Fatalf("embedded SDK version=%q, manifest=%q", dependency.Version, sdkVersion)
			}
			return
		}
	}
	t.Fatal("embedded OpenAI SDK dependency is missing")
}
