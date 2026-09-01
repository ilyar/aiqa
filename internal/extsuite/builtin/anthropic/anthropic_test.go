package anthropic

import (
	"reflect"
	"runtime/debug"
	"testing"

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
