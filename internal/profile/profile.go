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

type Profile struct {
	ID                 string   `json:"id"`
	Revision           string   `json:"revision"`
	Source             string   `json:"source"`
	SourceCommit       string   `json:"source_commit"`
	SourceRefRevision  string   `json:"source_ref_revision"`
	Scope              string   `json:"scope"`
	Probes             []Probe  `json:"probes"`
	StandardParameters []string `json:"standard_parameters"`
	VendorExtensions   []string `json:"vendor_extensions"`
	Modules            []Module `json:"modules,omitempty"`
	byID               map[string]Probe
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
	p.ID = strings.TrimSpace(p.ID)
	p.Revision = strings.TrimSpace(p.Revision)
	p.Source = strings.TrimSpace(p.Source)
	p.SourceCommit = strings.TrimSpace(p.SourceCommit)
	p.SourceRefRevision = strings.TrimSpace(p.SourceRefRevision)
	p.Scope = strings.TrimSpace(p.Scope)
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
		if _, exists := p.byID[probe.ID]; exists {
			return fmt.Errorf("duplicate probe id %q", probe.ID)
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
		moduleIDs[module.ID] = struct{}{}
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
	}
	if len(p.Modules) > 0 && len(assignedProbes) != len(p.Probes) {
		for _, probe := range p.Probes {
			if _, exists := assignedProbes[probe.ID]; !exists {
				return fmt.Errorf("probe %q is not assigned to a module", probe.ID)
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
