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
