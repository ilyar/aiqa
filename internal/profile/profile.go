package profile

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"strings"
	"time"

	profiledata "github.com/ilyar/aiqa/profile"
)

const DefaultFile = "openai-compat-2026-09.json"

type Probe struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
	Executor    string `json:"executor,omitempty"`
}

type Module struct {
	ID               string   `json:"id"`
	Subject          string   `json:"subject"`
	AlwaysApplicable bool     `json:"always_applicable,omitempty"`
	DefaultEnabled   bool     `json:"default_enabled"`
	CostClass        string   `json:"cost_class"`
	Capabilities     []string `json:"capabilities,omitempty"`
	ProbeIDs         []string `json:"probe_ids"`
}

type Source struct {
	Kind       string `json:"kind"`
	URL        string `json:"url"`
	Revision   string `json:"revision,omitempty"`
	ReviewedAt string `json:"reviewed_at,omitempty"`
	Version    string `json:"version,omitempty"`
	Commit     string `json:"commit,omitempty"`
}

type ExtensionBinding struct {
	ExtensionID           string   `json:"extension_id"`
	SuiteID               string   `json:"suite_id"`
	RequirementID         string   `json:"requirement_id"`
	RelatedRequirementIDs []string `json:"related_requirement_ids,omitempty"`
	Subject               string   `json:"subject"`
	Module                string   `json:"module"`
}

type ModelSelection struct {
	InteractionsAllowlist []string              `json:"interactions_allowlist,omitempty"`
	Interactions          *InteractionSelection `json:"interactions,omitempty"`
}

// InteractionSelection is the versioned policy for selecting models for the
// modern Interactions surface.  The legacy flat field remains accepted for
// backwards-compatible embedded profiles.
type InteractionSelection struct {
	Strategy  string   `json:"strategy,omitempty"`
	Allowlist []string `json:"allowlist,omitempty"`
}

type Profile struct {
	SchemaVersion      string             `json:"schema_version,omitempty"`
	ID                 string             `json:"id"`
	Protocol           string             `json:"protocol,omitempty"`
	Revision           string             `json:"revision"`
	APIVersion         string             `json:"api_version,omitempty"`
	BaseURLSemantics   string             `json:"base_url_semantics,omitempty"`
	Source             string             `json:"source"`
	SourceCommit       string             `json:"source_commit"`
	SourceRefRevision  string             `json:"source_ref_revision"`
	Scope              string             `json:"scope"`
	Probes             []Probe            `json:"probes"`
	StandardParameters []string           `json:"standard_parameters"`
	VendorExtensions   []string           `json:"vendor_extensions"`
	Modules            []Module           `json:"modules,omitempty"`
	Sources            []Source           `json:"sources,omitempty"`
	ExtensionBindings  []ExtensionBinding `json:"extension_bindings,omitempty"`
	ModelSelection     ModelSelection     `json:"model_selection,omitempty"`
	byID               map[string]Probe
	bindingsByKey      map[string]ExtensionBinding
}

func Load() (Profile, error) {
	return LoadFile(DefaultFile)
}

// AvailableFiles returns the names of all profiles embedded in the binary.
func AvailableFiles() ([]string, error) {
	files, err := fs.Glob(profiledata.Files, "*.json")
	if err != nil {
		return nil, fmt.Errorf("list embedded compatibility profiles: %w", err)
	}
	return files, nil
}

// LoadFile loads and validates one embedded profile by filename.
func LoadFile(name string) (Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" || path.Base(name) != name || !strings.HasSuffix(name, ".json") {
		return Profile{}, fmt.Errorf("invalid compatibility profile filename %q", name)
	}
	data, err := profiledata.Files.ReadFile(name)
	if err != nil {
		return Profile{}, fmt.Errorf("read embedded compatibility profile %q: %w", name, err)
	}
	return parse(data)
}

func parse(data []byte) (Profile, error) {
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return Profile{}, fmt.Errorf("decode embedded compatibility profile: %w", err)
	}
	if err := p.validate(); err != nil {
		return Profile{}, fmt.Errorf("validate embedded compatibility profile: %w", err)
	}
	return p, nil
}

func (p *Profile) validate() error {
	p.SchemaVersion = strings.TrimSpace(p.SchemaVersion)
	p.ID = strings.TrimSpace(p.ID)
	p.Protocol = strings.TrimSpace(p.Protocol)
	p.Revision = strings.TrimSpace(p.Revision)
	p.APIVersion = strings.TrimSpace(p.APIVersion)
	p.BaseURLSemantics = strings.TrimSpace(p.BaseURLSemantics)
	p.Source = strings.TrimSpace(p.Source)
	p.SourceCommit = strings.TrimSpace(p.SourceCommit)
	p.SourceRefRevision = strings.TrimSpace(p.SourceRefRevision)
	p.Scope = strings.TrimSpace(p.Scope)
	allowlist := p.ModelSelection.InteractionsAllowlist
	if p.ModelSelection.Interactions != nil {
		selection := p.ModelSelection.Interactions
		selection.Strategy = strings.TrimSpace(selection.Strategy)
		if selection.Strategy != "" && selection.Strategy != "explicit_or_allowlist" {
			return fmt.Errorf("model_selection.interactions.strategy %q is unsupported", selection.Strategy)
		}
		allowlist = append(allowlist, selection.Allowlist...)
	}
	seenAllowlist := map[string]bool{}
	for index, model := range allowlist {
		model = strings.TrimSpace(model)
		if model == "" {
			return fmt.Errorf("model_selection.interactions_allowlist[%d] must not be empty", index)
		}
		if seenAllowlist[model] {
			return fmt.Errorf("model_selection interactions allowlist contains duplicate %q", model)
		}
		seenAllowlist[model] = true
		if index < len(p.ModelSelection.InteractionsAllowlist) {
			p.ModelSelection.InteractionsAllowlist[index] = model
		} else if p.ModelSelection.Interactions != nil {
			p.ModelSelection.Interactions.Allowlist[index-len(p.ModelSelection.InteractionsAllowlist)] = model
		}
	}
	if p.SchemaVersion == "" {
		p.SchemaVersion = "aiqa.profile.v1"
	}
	if p.Protocol == "" {
		p.Protocol = "openai"
	}
	if p.SchemaVersion != "aiqa.profile.v1" && p.SchemaVersion != "aiqa.profile.v2" && p.SchemaVersion != "aiqa.profile.v3" {
		return fmt.Errorf("unsupported schema_version %q", p.SchemaVersion)
	}
	if p.Protocol != "openai" && p.Protocol != "anthropic" && p.Protocol != "google-genai" {
		return fmt.Errorf("unsupported protocol %q", p.Protocol)
	}
	if p.SchemaVersion == "aiqa.profile.v2" || p.SchemaVersion == "aiqa.profile.v3" {
		if (p.Protocol == "anthropic" || p.Protocol == "google-genai") && p.APIVersion == "" {
			return fmt.Errorf("%s profile requires api_version", p.Protocol)
		}
		if p.BaseURLSemantics == "" {
			return fmt.Errorf("profile v2 or v3 requires base_url_semantics")
		}
		if p.Protocol == "anthropic" && p.BaseURLSemantics != "prefix_before_v1" {
			return fmt.Errorf("Anthropic profile requires base_url_semantics=prefix_before_v1")
		}
		if p.Protocol == "google-genai" && p.BaseURLSemantics != "prefix_before_api_version" {
			return fmt.Errorf("Google GenAI profile requires base_url_semantics=prefix_before_api_version")
		}
		if len(p.Sources) == 0 {
			return fmt.Errorf("profile v2 or v3 requires sources")
		}
	}
	if p.ID == "" || p.Revision == "" || p.Source == "" || p.SourceCommit == "" || p.Scope == "" {
		return fmt.Errorf("id, revision, source, source_commit, and scope are required")
	}
	if len(p.SourceCommit) != 40 || !isLowerHex(p.SourceCommit) {
		return fmt.Errorf("source_commit %q must be a 40-character lowercase Git SHA", p.SourceCommit)
	}
	if _, err := time.Parse("2006-01-02", p.Revision); err != nil {
		return fmt.Errorf("revision %q must use YYYY-MM-DD: %w", p.Revision, err)
	}
	u, err := url.Parse(p.Source)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("source %q must be an absolute HTTP(S) URL", p.Source)
	}
	if len(p.Probes) == 0 {
		return fmt.Errorf("at least one probe is required")
	}

	p.byID = make(map[string]Probe, len(p.Probes))
	for index := range p.Probes {
		probe := &p.Probes[index]
		probe.ID = strings.TrimSpace(probe.ID)
		probe.Name = strings.TrimSpace(probe.Name)
		probe.Category = strings.TrimSpace(probe.Category)
		probe.Description = strings.TrimSpace(probe.Description)
		if probe.ID == "" || probe.Name == "" || probe.Category == "" || probe.Description == "" {
			return fmt.Errorf("probe %d must define id, name, category, and description", index)
		}
		probe.Executor = strings.TrimSpace(probe.Executor)
		if probe.Executor == "" {
			probe.Executor = "native"
		}
		if probe.Executor != "native" && probe.Executor != "extension" {
			return fmt.Errorf("probe %q has unsupported executor %q", probe.ID, probe.Executor)
		}
		if _, exists := p.byID[probe.ID]; exists {
			return fmt.Errorf("duplicate probe id %q", probe.ID)
		}
		if p.Protocol == "anthropic" && !strings.HasPrefix(probe.ID, "anthropic.") {
			return fmt.Errorf("Anthropic probe id %q must be protocol-qualified", probe.ID)
		}
		if p.Protocol == "google-genai" && !strings.HasPrefix(probe.ID, "google.") && !strings.HasPrefix(probe.ID, "official-sdk.google-genai.") {
			return fmt.Errorf("Google GenAI probe id %q must be protocol-qualified", probe.ID)
		}
		p.byID[probe.ID] = *probe
	}

	standard, err := validateNames("standard parameter", p.StandardParameters, nil)
	if err != nil {
		return err
	}
	if _, err := validateNames("vendor extension", p.VendorExtensions, standard); err != nil {
		return err
	}
	moduleIDs := map[string]struct{}{}
	moduleSubjects := map[string]string{}
	assignedProbes := map[string]string{}
	for index := range p.Modules {
		module := &p.Modules[index]
		module.ID, module.Subject, module.CostClass = strings.TrimSpace(module.ID), strings.TrimSpace(module.Subject), strings.TrimSpace(module.CostClass)
		if module.ID == "" || (module.Subject != "provider" && module.Subject != "model") || module.CostClass == "" {
			return fmt.Errorf("module %d must define id, provider/model subject, and cost_class", index)
		}
		if _, exists := moduleIDs[module.ID]; exists {
			return fmt.Errorf("duplicate module id %q", module.ID)
		}
		if p.Protocol == "anthropic" && !strings.HasPrefix(module.ID, "anthropic.") {
			return fmt.Errorf("Anthropic module id %q must be protocol-qualified", module.ID)
		}
		if p.Protocol == "google-genai" && !strings.HasPrefix(module.ID, "google-genai.") {
			return fmt.Errorf("Google GenAI module id %q must be protocol-qualified", module.ID)
		}
		moduleIDs[module.ID] = struct{}{}
		moduleSubjects[module.ID] = module.Subject
		if len(module.ProbeIDs) == 0 {
			return fmt.Errorf("module %q must reference at least one probe", module.ID)
		}
		for _, probeID := range module.ProbeIDs {
			if _, exists := p.byID[probeID]; !exists {
				return fmt.Errorf("module %q references unknown probe %q", module.ID, probeID)
			}
			if previous, exists := assignedProbes[probeID]; exists {
				return fmt.Errorf("probe %q belongs to both %q and %q", probeID, previous, module.ID)
			}
			assignedProbes[probeID] = module.ID
		}
		if module.Subject == "model" && len(module.Capabilities) == 0 {
			return fmt.Errorf("model module %q must define capabilities", module.ID)
		}
		if _, err := validateNames("module "+module.ID+" capability", module.Capabilities, nil); err != nil {
			return err
		}
		if p.Protocol == "anthropic" {
			for _, capability := range module.Capabilities {
				if !strings.HasPrefix(capability, "anthropic.") {
					return fmt.Errorf("Anthropic capability %q must be protocol-qualified", capability)
				}
			}
		}
		if p.Protocol == "google-genai" {
			for _, capability := range module.Capabilities {
				if !strings.HasPrefix(capability, "google-genai.") {
					return fmt.Errorf("Google GenAI capability %q must be protocol-qualified", capability)
				}
			}
			if module.CostClass == "persistent-resource" && module.Subject != "model" {
				return fmt.Errorf("Google GenAI persistent resources must be model-scoped")
			}
		}
	}
	p.bindingsByKey = make(map[string]ExtensionBinding, len(p.ExtensionBindings))
	boundProbes := map[string]bool{}
	for index := range p.ExtensionBindings {
		binding := &p.ExtensionBindings[index]
		binding.ExtensionID = strings.TrimSpace(binding.ExtensionID)
		binding.SuiteID = strings.TrimSpace(binding.SuiteID)
		binding.RequirementID = strings.TrimSpace(binding.RequirementID)
		binding.Subject = strings.TrimSpace(binding.Subject)
		binding.Module = strings.TrimSpace(binding.Module)
		if binding.ExtensionID == "" || binding.SuiteID == "" || binding.RequirementID == "" || binding.Module == "" || (binding.Subject != "provider" && binding.Subject != "model") {
			return fmt.Errorf("extension binding %d must define extension_id, suite_id, requirement_id, provider/model subject, and module", index)
		}
		key := binding.ExtensionID + "\x00" + binding.SuiteID
		if _, exists := p.bindingsByKey[key]; exists {
			return fmt.Errorf("duplicate extension binding %q/%q", binding.ExtensionID, binding.SuiteID)
		}
		probe, exists := p.byID[binding.RequirementID]
		if !exists {
			return fmt.Errorf("extension binding %q/%q references unknown requirement %q", binding.ExtensionID, binding.SuiteID, binding.RequirementID)
		}
		if probe.Executor != "extension" {
			return fmt.Errorf("extension binding %q/%q references non-extension probe %q", binding.ExtensionID, binding.SuiteID, binding.RequirementID)
		}
		if _, exists := moduleIDs[binding.Module]; !exists {
			return fmt.Errorf("extension binding %q/%q references unknown module %q", binding.ExtensionID, binding.SuiteID, binding.Module)
		}
		if moduleSubjects[binding.Module] != binding.Subject {
			return fmt.Errorf("extension binding %q/%q subject %q does not match module %q subject %q", binding.ExtensionID, binding.SuiteID, binding.Subject, binding.Module, moduleSubjects[binding.Module])
		}
		if previous, exists := assignedProbes[binding.RequirementID]; exists {
			return fmt.Errorf("extension probe %q is also assigned to native module %q", binding.RequirementID, previous)
		}
		for relatedIndex, relatedID := range binding.RelatedRequirementIDs {
			relatedID = strings.TrimSpace(relatedID)
			if relatedID == "" {
				return fmt.Errorf("extension binding %q/%q has an empty related requirement", binding.ExtensionID, binding.SuiteID)
			}
			if _, exists := p.byID[relatedID]; !exists {
				return fmt.Errorf("extension binding %q/%q references unknown related requirement %q", binding.ExtensionID, binding.SuiteID, relatedID)
			}
			if p.byID[relatedID].Executor != "native" {
				return fmt.Errorf("extension binding %q/%q related requirement %q is not native", binding.ExtensionID, binding.SuiteID, relatedID)
			}
			binding.RelatedRequirementIDs[relatedIndex] = relatedID
		}
		boundProbes[binding.RequirementID] = true
		p.bindingsByKey[key] = *binding
	}
	if p.SchemaVersion == "aiqa.profile.v3" && len(p.ExtensionBindings) == 0 {
		return fmt.Errorf("profile v3 requires extension_bindings")
	}
	sourceKinds := map[string]bool{}
	for index, source := range p.Sources {
		if strings.TrimSpace(source.Kind) == "" || strings.TrimSpace(source.URL) == "" {
			return fmt.Errorf("source %d requires kind and url", index)
		}
		u, err := url.Parse(source.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("source %d URL must be absolute HTTPS", index)
		}
		if source.Commit != "" && (len(source.Commit) != 40 || !isLowerHex(source.Commit)) {
			return fmt.Errorf("source %d commit must be a 40-character lowercase Git SHA", index)
		}
		if sourceKinds[source.Kind] {
			return fmt.Errorf("duplicate source kind %q", source.Kind)
		}
		sourceKinds[source.Kind] = true
		switch source.Kind {
		case "api_version":
			if source.Revision == "" || source.ReviewedAt == "" {
				return fmt.Errorf("api_version source requires revision and reviewed_at")
			}
			if _, err := time.Parse("2006-01-02", source.ReviewedAt); err != nil {
				return fmt.Errorf("api_version source reviewed_at must use YYYY-MM-DD: %w", err)
			}
			if (p.Protocol == "anthropic" || p.Protocol == "google-genai") && source.Revision != p.APIVersion {
				return fmt.Errorf("api_version source revision %q does not match profile api_version %q", source.Revision, p.APIVersion)
			}
		case "official_sdk":
			if source.Version == "" || source.Commit == "" {
				return fmt.Errorf("official_sdk source requires version and commit")
			}
			if (p.Protocol == "anthropic" || p.Protocol == "google-genai") && source.Commit != p.SourceCommit {
				return fmt.Errorf("official_sdk source commit does not match source_commit")
			}
		}
	}
	if len(p.Modules) > 0 && len(assignedProbes)+len(boundProbes) != len(p.Probes) {
		for _, probe := range p.Probes {
			_, assigned := assignedProbes[probe.ID]
			_, bound := boundProbes[probe.ID]
			if !assigned && !bound {
				return fmt.Errorf("probe %q is not assigned to a native module or extension binding", probe.ID)
			}
		}
	}
	return nil
}

func validateNames(kind string, values []string, disallow map[string]struct{}) (map[string]struct{}, error) {
	seen := make(map[string]struct{}, len(values))
	for index, rawValue := range values {
		value := strings.TrimSpace(rawValue)
		if value == "" {
			return nil, fmt.Errorf("%s %d must not be empty", kind, index)
		}
		if _, exists := seen[value]; exists {
			return nil, fmt.Errorf("duplicate %s %q", kind, value)
		}
		if _, exists := disallow[value]; exists {
			return nil, fmt.Errorf("%s %q also appears in standard parameters", kind, value)
		}
		values[index] = value
		seen[value] = struct{}{}
	}
	return seen, nil
}

func isLowerHex(value string) bool {
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func (p Profile) Probe(id string) Probe {
	if probe, ok := p.byID[id]; ok {
		return probe
	}
	// The modular profile prefixes endpoint-specific error probes. Keep the
	// immutable 2026-08 profile usable by mapping the new chat IDs to its
	// historical unprefixed IDs at lookup time.
	if strings.HasPrefix(id, "chat.error.") {
		if probe, ok := p.byID[strings.TrimPrefix(id, "chat.")]; ok {
			probe.ID = id
			return probe
		}
	}
	return Probe{ID: id, Name: id, Category: "unknown"}
}

func (p Profile) HasProbe(id string) bool {
	_, ok := p.byID[id]
	return ok
}

// Module returns a profile module by its stable ID.
func (p Profile) Module(id string) (Module, bool) {
	for _, module := range p.Modules {
		if module.ID == id {
			return module, true
		}
	}
	return Module{}, false
}

// InteractionModels returns the normalized profile-maintained Interactions
// model allowlist.  Explicit target model selection is handled by the runner
// separately and is intentionally not folded into this policy list.
func (p Profile) InteractionModels() []string {
	values := append([]string(nil), p.ModelSelection.InteractionsAllowlist...)
	if p.ModelSelection.Interactions != nil {
		values = append(values, p.ModelSelection.Interactions.Allowlist...)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func (p Profile) ExtensionBinding(extensionID, suiteID string) (ExtensionBinding, bool) {
	binding, ok := p.bindingsByKey[extensionID+"\x00"+suiteID]
	return binding, ok
}
