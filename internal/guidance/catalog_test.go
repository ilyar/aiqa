package guidance

import (
	"strings"
	"testing"

	"github.com/ilyar/aiqa/internal/result"
)

func TestEmbeddedCatalogAndPackValidate(t *testing.T) {
	catalog, err := LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if catalog.Revision == "" || len(catalog.Entries) == 0 {
		t.Fatal("embedded catalog must have a revision and entries")
	}
	if _, _, err := LoadPack("generic"); err != nil {
		t.Fatalf("LoadPack: %v", err)
	}
	if _, ok := catalog.Entry("anthropic.stream.sequence_invalid"); !ok {
		t.Fatal("Anthropic guidance catalog was not merged")
	}
	if _, ok := catalog.Entry("contract.google.interactions.stateful.cleanup"); !ok {
		t.Fatal("Google stateful cleanup guidance catalog was not merged")
	}
	if pack, _, err := LoadPack("generic-anthropic-facade"); err != nil || pack.ID != "generic-anthropic-facade" {
		t.Fatalf("Anthropic advice pack is invalid: pack=%+v err=%v", pack, err)
	}
	if pack, info, err := LoadPack("generic-google-genai-gateway"); err != nil || pack.ID != "generic-google-genai-gateway" || info.ID != pack.ID {
		t.Fatalf("Google advice pack is invalid: pack=%+v info=%+v err=%v", pack, info, err)
	}
}

func TestPackCannotDeclareNormativeFields(t *testing.T) {
	input := []byte(`{
		"id":"unsafe",
		"revision":"1",
		"overrides":[{
			"finding_code":"contract.models.list.entry.created_missing",
			"priority":"p3"
		}]
	}`)
	var pack Pack
	err := decodeStrict(input, &pack)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("normative pack field must be rejected, got %v", err)
	}
}

func TestCatalogRejectsNonHTTPSReferences(t *testing.T) {
	catalog := Catalog{
		Revision: "1",
		Entries: []CatalogEntry{{
			ID:                  "bad-link",
			Revision:            "1",
			FindingCode:         "example.bad_link",
			FindingTitle:        "Bad link",
			RecommendationTitle: "Replace the link",
			DefaultPriority:     "p2",
			ActionKind:          "document",
			GuidanceLevel:       1,
			Why:                 "References must be safe.",
			References:          []result.Reference{{Title: "Example", URL: "http://example.com", SourceKind: "official"}},
		}},
	}
	if err := catalog.validate(); err == nil {
		t.Fatal("HTTP reference must be rejected")
	}
}

func TestCatalogRejectsVerificationCommands(t *testing.T) {
	catalog := Catalog{
		Revision: "1",
		Entries: []CatalogEntry{{
			ID:                  "broad-rerun",
			Revision:            "1",
			FindingCode:         "example.broad_rerun",
			FindingTitle:        "Broad rerun",
			RecommendationTitle: "Verify the requirement",
			DefaultPriority:     "p2",
			ActionKind:          "rerun",
			GuidanceLevel:       1,
			Audiences:           []string{"qa_operator"},
			Why:                 "Verification must name an executable target.",
			Attribution:         result.Attribution{Component: "aiqa_runner", Confidence: "high", Basis: "catalog"},
			Steps:               []result.RecommendationStep{{ID: "verify", Text: "Verify the affected requirement."}},
			Verification:        []result.Verification{{Kind: "command", IDs: []string{"aiqa test=compat"}}},
		}},
	}
	err := catalog.validate()
	if err == nil || !strings.Contains(err.Error(), "unsupported verification kind") {
		t.Fatalf("command verification must be rejected, got %v", err)
	}
}
