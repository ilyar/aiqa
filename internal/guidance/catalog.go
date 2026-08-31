package guidance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"

	guidanceassets "github.com/ilyar/aiqa/guidance"
	"github.com/ilyar/aiqa/internal/result"
)

const EngineVersion = "1.0.0"

type Catalog struct {
	Revision string         `json:"revision"`
	Entries  []CatalogEntry `json:"entries"`
	byCode   map[string]CatalogEntry
}

type CatalogEntry struct {
	ID                  string                      `json:"id"`
	Revision            string                      `json:"revision"`
	FindingCode         string                      `json:"finding_code"`
	FindingTitle        string                      `json:"finding_title"`
	RecommendationTitle string                      `json:"recommendation_title"`
	DefaultPriority     string                      `json:"default_priority"`
	ActionKind          string                      `json:"action_kind"`
	GuidanceLevel       int                         `json:"guidance_level"`
	Audiences           []string                    `json:"audiences"`
	Why                 string                      `json:"why"`
	Attribution         result.Attribution          `json:"attribution"`
	Steps               []result.RecommendationStep `json:"steps"`
	Verification        []result.Verification       `json:"verification"`
	References          []result.Reference          `json:"references"`
	Workarounds         []string                    `json:"workarounds,omitempty"`
	Caveats             []string                    `json:"caveats,omitempty"`
}

type Pack struct {
	ID        string         `json:"id"`
	Revision  string         `json:"revision"`
	Overrides []PackOverride `json:"overrides"`
}

// PackOverride is deliberately limited to non-normative guidance fields.
// Status, requirement level, priority, verdict, and impact cannot be overlaid.
type PackOverride struct {
	FindingCode   string                      `json:"finding_code"`
	GuidanceLevel int                         `json:"guidance_level,omitempty"`
	Steps         []result.RecommendationStep `json:"steps,omitempty"`
	Workarounds   []string                    `json:"workarounds,omitempty"`
	Caveats       []string                    `json:"caveats,omitempty"`
	References    []result.Reference          `json:"references,omitempty"`
}

func LoadCatalog() (Catalog, error) {
	data, err := guidanceassets.Files.ReadFile("catalog/generic.json")
	if err != nil {
		return Catalog{}, fmt.Errorf("read guidance catalog: %w", err)
	}
	var catalog Catalog
	if err := decodeStrict(data, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode guidance catalog: %w", err)
	}
	if err := catalog.validate(); err != nil {
		return Catalog{}, fmt.Errorf("validate guidance catalog: %w", err)
	}
	return catalog, nil
}

func LoadPack(requested string) (Pack, result.GuidancePackInfo, error) {
	requested = strings.TrimSpace(strings.ToLower(requested))
	if requested == "" || requested == "auto" || requested == "generic" {
		requested = "generic-openai-gateway"
	}
	path := "packs/" + requested + ".json"
	data, err := guidanceassets.Files.ReadFile(path)
	if err != nil {
		return Pack{}, result.GuidancePackInfo{}, fmt.Errorf("read guidance pack %q: %w", requested, err)
	}
	var pack Pack
	if err := decodeStrict(data, &pack); err != nil {
		return Pack{}, result.GuidancePackInfo{}, fmt.Errorf("decode guidance pack %q: %w", requested, err)
	}
	if err := pack.validate(); err != nil {
		return Pack{}, result.GuidancePackInfo{}, fmt.Errorf("validate guidance pack %q: %w", requested, err)
	}
	info := result.GuidancePackInfo{ID: pack.ID, Revision: pack.Revision, Source: "builtin"}
	if strings.TrimSpace(strings.ToLower(requested)) == "generic-openai-gateway" {
		info.DetectionSource, info.Confidence = "default", "high"
	}
	return pack, info, nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func (c Catalog) Entry(code string) (CatalogEntry, bool) {
	entry, ok := c.byCode[code]
	return entry, ok
}

func (c *Catalog) validate() error {
	c.Revision = strings.TrimSpace(c.Revision)
	if c.Revision == "" {
		return fmt.Errorf("revision is required")
	}
	c.byCode = make(map[string]CatalogEntry, len(c.Entries))
	ids := map[string]bool{}
	for index := range c.Entries {
		entry := &c.Entries[index]
		entry.ID = strings.TrimSpace(entry.ID)
		entry.Revision = strings.TrimSpace(entry.Revision)
		entry.FindingCode = strings.TrimSpace(entry.FindingCode)
		if entry.ID == "" || entry.Revision == "" || entry.FindingCode == "" || entry.FindingTitle == "" || entry.RecommendationTitle == "" || entry.Why == "" {
			return fmt.Errorf("entry %d is missing required fields", index)
		}
		if ids[entry.ID] {
			return fmt.Errorf("duplicate catalog entry ID %q", entry.ID)
		}
		if _, exists := c.byCode[entry.FindingCode]; exists {
			return fmt.Errorf("duplicate finding code %q", entry.FindingCode)
		}
		ids[entry.ID] = true
		c.byCode[entry.FindingCode] = *entry
		if !validPriority(entry.DefaultPriority) || !validActionKind(entry.ActionKind) {
			return fmt.Errorf("entry %q has invalid priority or action kind", entry.ID)
		}
		if entry.GuidanceLevel < 1 || entry.GuidanceLevel > 3 {
			return fmt.Errorf("entry %q has invalid guidance level", entry.ID)
		}
		if !validAttributionComponent(entry.Attribution.Component) || !validConfidence(entry.Attribution.Confidence) || strings.TrimSpace(entry.Attribution.Basis) == "" {
			return fmt.Errorf("entry %q has invalid attribution", entry.ID)
		}
		if len(entry.Audiences) == 0 || len(entry.Steps) == 0 {
			return fmt.Errorf("entry %q requires audiences and steps", entry.ID)
		}
		if (entry.DefaultPriority == "p0" || entry.DefaultPriority == "p1") && len(entry.Verification) == 0 {
			return fmt.Errorf("entry %q requires verification", entry.ID)
		}
		if entry.GuidanceLevel == 3 && len(entry.Verification) == 0 {
			return fmt.Errorf("level 3 entry %q requires verification", entry.ID)
		}
		if err := validateReferences(entry.References); err != nil {
			return fmt.Errorf("entry %q: %w", entry.ID, err)
		}
	}
	sort.Slice(c.Entries, func(i, j int) bool { return c.Entries[i].ID < c.Entries[j].ID })
	return nil
}

func (p *Pack) validate() error {
	p.ID, p.Revision = strings.TrimSpace(p.ID), strings.TrimSpace(p.Revision)
	if p.ID == "" || p.Revision == "" {
		return fmt.Errorf("id and revision are required")
	}
	seen := map[string]bool{}
	for _, override := range p.Overrides {
		if override.FindingCode == "" || seen[override.FindingCode] {
			return fmt.Errorf("pack overrides require unique finding_code values")
		}
		seen[override.FindingCode] = true
		if override.GuidanceLevel < 0 || override.GuidanceLevel > 3 {
			return fmt.Errorf("override %q has invalid guidance level", override.FindingCode)
		}
		if err := validateReferences(override.References); err != nil {
			return fmt.Errorf("override %q: %w", override.FindingCode, err)
		}
	}
	return nil
}

func validateReferences(references []result.Reference) error {
	for _, reference := range references {
		if reference.Title == "" || reference.URL == "" || reference.SourceKind == "" {
			return fmt.Errorf("references require title, URL, and source_kind")
		}
		u, err := url.Parse(reference.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("reference %q must use an absolute HTTPS URL", reference.URL)
		}
	}
	return nil
}

func validPriority(value string) bool {
	return value == "p0" || value == "p1" || value == "p2" || value == "p3"
}

func validActionKind(value string) bool {
	switch value {
	case "fix", "configure", "workaround", "document", "investigate", "rerun", "manual_review":
		return true
	default:
		return false
	}
}

func validConfidence(value string) bool {
	return value == "low" || value == "medium" || value == "high"
}

func validAttributionComponent(value string) bool {
	switch value {
	case "provider_gateway", "model_adapter", "upstream_model", "client_configuration", "network_or_runtime", "aiqa_runner", "external_checker", "unknown":
		return true
	default:
		return false
	}
}
