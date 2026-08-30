package profile

import (
	"strings"
	"testing"
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

func TestEmbeddedProfileFilesCanBeListedAndLoaded(t *testing.T) {
	files, err := AvailableFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != DefaultFile {
		t.Fatalf("unexpected embedded profile files: %#v", files)
	}
	if _, err := LoadFile(DefaultFile); err != nil {
		t.Fatalf("load default profile: %v", err)
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
