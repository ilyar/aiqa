package capability

import (
	"strings"

	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

const (
	ModuleChat       = "chat-completions"
	ModuleImages     = "images-generations"
	ModuleEmbeddings = "embeddings"
	ModuleResponses  = "responses"
	ModuleSpeech     = "audio-speech"
	ModuleTranscribe = "audio-transcriptions"
)

type RoutingOptions struct {
	Modules                []string
	AllowBillableArtifacts bool
	AllowHighTokenProbes   bool
}

func AssessModules(classification Classification, definitions []profile.Module, opts RoutingOptions) []result.ModuleAssessment {
	enabled := map[string]bool{}
	if len(opts.Modules) == 0 {
		opts.Modules = []string{"auto"}
	}
	for _, module := range opts.Modules {
		enabled[module] = true
	}
	auto := enabled["auto"]
	if len(definitions) == 0 {
		definitions = []profile.Module{{
			ID: ModuleChat, Subject: "model", DefaultEnabled: true, CostClass: "token", Capabilities: []string{ChatText},
		}}
	}
	modules := make([]result.ModuleAssessment, 0, len(definitions))
	for _, definition := range definitions {
		if definition.Subject != "model" {
			continue
		}
		assessment := result.ModuleAssessment{ID: definition.ID, Capability: strings.Join(definition.Capabilities, ",")}
		matchedCapability := firstMatchedCapability(classification, definition.Capabilities)
		switch {
		case classification.Unknown:
			assessment.Applicability = result.ApplicabilityUnknown
			assessment.Execution = result.ExecutionSkipped
			assessment.Reason = classification.Reason
		case matchedCapability != "":
			assessment.Applicability = result.ApplicabilityApplicable
			assessment.Reason, assessment.Source, assessment.Confidence = capabilityReason(classification, matchedCapability)
			moduleEnabled := enabled[definition.ID] || (auto && definition.DefaultEnabled)
			billable := strings.HasPrefix(definition.CostClass, "billable-")
			highToken := definition.CostClass == "high-token"
			switch {
			case !implementedModule(definition.ID):
				assessment.Execution = result.ExecutionSkipped
				assessment.Reason = "native module is not implemented"
			case billable && !opts.AllowBillableArtifacts && (enabled[definition.ID] || auto):
				assessment.Execution = result.ExecutionSkipped
				assessment.Reason = "billable artifact module was not enabled"
			case highToken && !opts.AllowHighTokenProbes:
				assessment.Execution = result.ExecutionSkipped
				assessment.Reason = "high-token probes require explicit authorization"
			case !moduleEnabled:
				assessment.Execution = result.ExecutionSkipped
				assessment.Reason = "module was not enabled"
			default:
				assessment.Execution = result.ExecutionExecuted
			}
		default:
			assessment.Applicability = result.ApplicabilityNotApplicable
			assessment.Reason = notApplicableReason(classification, definition.Capabilities)
		}
		modules = append(modules, assessment)
	}
	return modules
}

func firstMatchedCapability(classification Classification, capabilities []string) string {
	for _, id := range capabilities {
		if classification.Has(id) {
			return id
		}
	}
	return ""
}

func implementedModule(id string) bool {
	switch id {
	case ModuleChat, ModuleImages,
		"anthropic.messages", "anthropic.tools", "anthropic.token-counting",
		"anthropic.structured-output", "anthropic.vision", "anthropic.thinking":
		return true
	default:
		return false
	}
}

func capabilityReason(classification Classification, id string) (reason, source, confidence string) {
	for _, evidence := range classification.Capabilities {
		if evidence.Capability == id {
			return evidence.Reason, evidence.Source, evidence.Confidence
		}
	}
	return "capability classified as applicable", "classifier", "medium"
}

func notApplicableReason(classification Classification, capabilities []string) string {
	var known []string
	for _, evidence := range classification.Capabilities {
		known = append(known, evidence.Capability)
	}
	return "classified capabilities do not include " + strings.Join(capabilities, ", ") + ": " + strings.Join(known, ", ")
}

func Module(modules []result.ModuleAssessment, id string) *result.ModuleAssessment {
	for index := range modules {
		if modules[index].ID == id {
			return &modules[index]
		}
	}
	return nil
}
