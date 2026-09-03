package googlegenai

import (
	"reflect"
	"runtime/debug"
	"testing"

	"github.com/ilyar/aiqa/internal/profile"
)

func TestManifestListsOnlyPublicSDKSurfaces(t *testing.T) {
	var got []string
	for _, suite := range New().Manifest().Suites {
		got = append(got, suite.ID)
	}
	want := []string{"models.list", "models.get", "generate-content", "generate-content-stream", "count-tokens", "embeddings", "tools", "structured-output", "multimodal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Google Gen AI Official SDK suite list changed: got %v, want %v", got, want)
	}
	for _, suite := range New().Manifest().Suites {
		if suite.ID == "interactions" {
			t.Fatal("pinned SDK must not advertise an Interactions suite")
		}
	}
}

func TestManifestSuitesAreProfileBoundAndVersionMatchesDependency(t *testing.T) {
	p, err := profile.LoadFile("google-genai-compat-2026-09.json")
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
		if dependency.Path == "google.golang.org/genai" {
			if dependency.Version != sdkVersion {
				t.Fatalf("embedded SDK version=%q, manifest=%q", dependency.Version, sdkVersion)
			}
			return
		}
	}
	t.Fatal("embedded Google Gen AI SDK dependency is missing")
}
