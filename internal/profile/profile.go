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

const DefaultFile = "openai-compat-2026-08.json"

type Probe struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
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
	return Probe{ID: id, Name: id, Category: "unknown"}
}
