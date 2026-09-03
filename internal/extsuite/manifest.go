package extsuite

import (
	"fmt"
	"regexp"
	"strings"
)

var stableID = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)

const maxManifestSuites = 1024

func ValidateManifest(manifest *Manifest) error {
	if manifest == nil {
		return fmt.Errorf("extension manifest is nil")
	}
	if manifest.SchemaVersion != ProtocolVersion || manifest.Type != "manifest" {
		return fmt.Errorf("unsupported extension manifest envelope %q/%q", manifest.SchemaVersion, manifest.Type)
	}
	extensionID := strings.TrimSpace(manifest.Extension.ID)
	name := strings.TrimSpace(manifest.Extension.Name)
	version := strings.TrimSpace(manifest.Extension.Version)
	if extensionID != manifest.Extension.ID || name != manifest.Extension.Name || version != manifest.Extension.Version || !stableID.MatchString(extensionID) || name == "" || version == "" {
		return fmt.Errorf("extension manifest requires a stable id, name, and version")
	}
	if !contains(manifest.ReportContracts, "aiqa.report.v3") || !contains(manifest.ProfileContracts, "aiqa.profile.v3") {
		return fmt.Errorf("extension %s does not support aiqa.report.v3 and aiqa.profile.v3", manifest.Extension.ID)
	}
	if len(manifest.Protocols) == 0 || len(manifest.Suites) == 0 {
		return fmt.Errorf("extension %s must declare protocols and suites", manifest.Extension.ID)
	}
	if len(manifest.Suites) > maxManifestSuites {
		return fmt.Errorf("extension %s declares too many suites", manifest.Extension.ID)
	}
	protocols := map[string]bool{}
	for _, protocol := range manifest.Protocols {
		if protocol != "openai" && protocol != "anthropic" && protocol != "google-genai" {
			return fmt.Errorf("extension %s declares unsupported protocol %q", manifest.Extension.ID, protocol)
		}
		if protocols[protocol] {
			return fmt.Errorf("extension %s declares duplicate protocol %q", manifest.Extension.ID, protocol)
		}
		protocols[protocol] = true
	}
	suites := map[string]bool{}
	for index := range manifest.Suites {
		suite := manifest.Suites[index]
		if strings.TrimSpace(suite.ID) != suite.ID || !stableID.MatchString(suite.ID) {
			return fmt.Errorf("extension %s has invalid suite id %q", manifest.Extension.ID, suite.ID)
		}
		if suites[suite.ID] {
			return fmt.Errorf("extension %s has duplicate suite %q", manifest.Extension.ID, suite.ID)
		}
		suites[suite.ID] = true
		if suite.Subject != "provider" && suite.Subject != "model" {
			return fmt.Errorf("extension %s suite %s has invalid subject %q", manifest.Extension.ID, suite.ID, suite.Subject)
		}
		if suite.Cost != "text" && suite.Cost != "billable_artifact" {
			return fmt.Errorf("extension %s suite %s has invalid cost %q", manifest.Extension.ID, suite.ID, suite.Cost)
		}
		if suite.Cost == "billable_artifact" && suite.Subject != "model" {
			return fmt.Errorf("extension %s suite %s declares a billable artifact without a model subject", manifest.Extension.ID, suite.ID)
		}
		if suite.InternalConcurrency < 0 || suite.InternalConcurrency > 32 {
			return fmt.Errorf("extension %s suite %s has invalid internal_concurrency", manifest.Extension.ID, suite.ID)
		}
		capabilities := map[string]bool{}
		for _, capability := range suite.Capabilities {
			if !stableID.MatchString(capability) {
				return fmt.Errorf("extension %s suite %s has invalid capability %q", manifest.Extension.ID, suite.ID, capability)
			}
			if capabilities[capability] {
				return fmt.Errorf("extension %s suite %s has duplicate capability %q", manifest.Extension.ID, suite.ID, capability)
			}
			capabilities[capability] = true
		}
	}
	return nil
}

func supportsProtocol(manifest Manifest, protocol string) bool {
	return contains(manifest.Protocols, protocol)
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
