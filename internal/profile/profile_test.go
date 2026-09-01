package profile

import (
	"strings"
	"testing"

	profiledata "github.com/ilyar/aiqa/profile"
)

func TestEmbeddedProfileIsValid(t *testing.T) {
	p, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == "" || len(p.Probes) == 0 {
		t.Fatalf("unexpected profile: %+v", p)
	}
	for _, probe := range p.Probes {
		if got := p.Probe(probe.ID); got.ID != probe.ID {
			t.Fatalf("probe lookup failed for %q: %+v", probe.ID, got)
		}
	}
}

func TestAnthropicProfileV2RejectsMissingVersionAndUnqualifiedCapability(t *testing.T) {
	data, err := profiledata.Files.ReadFile("anthropic-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	withoutVersion := strings.Replace(string(data), `  "api_version": "2023-06-01",`+"\n", "", 1)
	if _, err := parse([]byte(withoutVersion)); err == nil || !strings.Contains(err.Error(), "api_version") {
		t.Fatalf("missing Anthropic API version was accepted: %v", err)
	}
	unqualified := strings.Replace(string(data), `"anthropic.messages.text"`, `"chat.completions.text"`, 1)
	if _, err := parse([]byte(unqualified)); err == nil || !strings.Contains(err.Error(), "protocol-qualified") {
		t.Fatalf("unqualified Anthropic capability was accepted: %v", err)
	}
}

func TestAnthropicProfileV2RejectsDuplicateModules(t *testing.T) {
	data, err := profiledata.Files.ReadFile("anthropic-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	duplicate := strings.Replace(string(data), `"id": "anthropic.messages"`, `"id": "anthropic.discovery"`, 1)
	if _, err := parse([]byte(duplicate)); err == nil || !strings.Contains(err.Error(), "duplicate module") {
		t.Fatalf("duplicate Anthropic module was accepted: %v", err)
	}
}

func TestEmbeddedProfileFilesCanBeListedAndLoaded(t *testing.T) {
	files, err := AvailableFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || files[0] != "anthropic-compat-2026-09.json" || files[1] != "openai-compat-2026-08.json" || files[2] != DefaultFile {
		t.Fatalf("unexpected embedded profile files: %#v", files)
	}
	if _, err := LoadFile(DefaultFile); err != nil {
		t.Fatalf("load default profile: %v", err)
	}
	anthropic, err := LoadFile("anthropic-compat-2026-09.json")
	if err != nil {
		t.Fatalf("load Anthropic profile: %v", err)
	}
	if anthropic.Protocol != "anthropic" || anthropic.APIVersion != "2023-06-01" {
		t.Fatalf("unexpected Anthropic profile: %+v", anthropic)
	}
	if _, err := LoadFile("../" + DefaultFile); err == nil {
		t.Fatal("expected invalid profile filename to be rejected")
	}
}

func TestProfileRejectsDuplicateProbeIDs(t *testing.T) {
	data := []byte(`{
		"id":"test","revision":"2026-08-30","source":"https://example.test/spec","source_commit":"0123456789abcdef0123456789abcdef01234567",
		"scope":"test","probes":[
			{"id":"same","name":"one","category":"core","description":"one"},
			{"id":"same","name":"two","category":"core","description":"two"}
		],"standard_parameters":[],"vendor_extensions":[]
	}`)
	_, err := parse(data)
	if err == nil || !strings.Contains(err.Error(), "duplicate probe id") {
		t.Fatalf("expected duplicate probe error, got %v", err)
	}
}

func TestProfileRejectsParameterNamespaceOverlap(t *testing.T) {
	data := []byte(`{
		"id":"test","revision":"2026-08-30","source":"https://example.test/spec","source_commit":"0123456789abcdef0123456789abcdef01234567",
		"scope":"test","probes":[
			{"id":"one","name":"one","category":"core","description":"one"}
		],"standard_parameters":["thinking"],"vendor_extensions":["thinking"]
	}`)
	_, err := parse(data)
	if err == nil || !strings.Contains(err.Error(), "also appears") {
		t.Fatalf("expected namespace overlap error, got %v", err)
	}
}
