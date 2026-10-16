package capability

import (
	"testing"

	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

func routingProfile(t *testing.T) []profile.Module {
	t.Helper()
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	return p.Modules
}

func TestImageRoutingIsNAForChatAndSkippedWithoutBillableOptIn(t *testing.T) {
	classification := fromValues("image", []string{ImagesGenerations}, "test", "test", "high", "image")
	modules := AssessModules(classification, routingProfile(t), RoutingOptions{Modules: []string{"auto", ModuleImages}})
	if got := Module(modules, ModuleChat); got == nil || got.Applicability != result.ApplicabilityNotApplicable {
		t.Fatalf("chat routing: %+v", got)
	}
	if got := Module(modules, ModuleImages); got == nil || got.Applicability != result.ApplicabilityApplicable || got.Execution != result.ExecutionSkipped {
		t.Fatalf("image routing: %+v", got)
	}
}

func TestBillableImageRoutingCanExecute(t *testing.T) {
	classification := fromValues("image", []string{ImagesGenerations}, "test", "test", "high", "image")
	modules := AssessModules(classification, routingProfile(t), RoutingOptions{Modules: []string{ModuleImages}, AllowBillableArtifacts: true})
	if got := Module(modules, ModuleImages); got == nil || got.Execution != result.ExecutionExecuted {
		t.Fatalf("image routing: %+v", got)
	}
}

func TestRoutingUsesModulesDeclaredByProfile(t *testing.T) {
	classification := fromValues("chat", []string{ChatText}, "test", "test", "high", "chat")
	definitions := []profile.Module{{
		ID: "future-chat-surface", Subject: "model", DefaultEnabled: true, CostClass: "token", Capabilities: []string{ChatText},
	}}
	modules := AssessModules(classification, definitions, RoutingOptions{Modules: []string{"auto"}})
	if len(modules) != 1 || modules[0].ID != "future-chat-surface" || modules[0].Applicability != result.ApplicabilityApplicable || modules[0].Execution != result.ExecutionSkipped {
		t.Fatalf("profile module was not used by router: %+v", modules)
	}
}

func TestRerankCapabilityIsNotApplicableToChatAndRoutesToDeclaredModule(t *testing.T) {
	definitions := []profile.Module{
		{ID: ModuleChat, Subject: "model", DefaultEnabled: true, CostClass: "token", Capabilities: []string{ChatText}},
		{ID: "rerank", Subject: "model", DefaultEnabled: true, CostClass: "token", Capabilities: []string{Rerank}},
	}
	classification := fromValues("reranker", []string{Rerank}, "test", "test", "high", "rerank")
	modules := AssessModules(classification, definitions, RoutingOptions{Modules: []string{"auto"}})
	if got := Module(modules, ModuleChat); got == nil || got.Applicability != result.ApplicabilityNotApplicable {
		t.Fatalf("chat routing: %+v", got)
	}
	if got := Module(modules, "rerank"); got == nil || got.Applicability != result.ApplicabilityApplicable || got.Execution != result.ExecutionSkipped {
		t.Fatalf("rerank routing: %+v", got)
	}
}

func TestProbeEligibleRoutingExecutesRequestedImplementedModulesAndPreservesGates(t *testing.T) {
	classification := Classification{Model: "opaque", ProbeEligible: true}
	definitions := []profile.Module{
		{ID: ModuleChat, Subject: "model", DefaultEnabled: true, CostClass: "token", Capabilities: []string{ChatText}},
		{ID: ModuleImages, Subject: "model", DefaultEnabled: false, CostClass: "billable-artifact", Capabilities: []string{ImagesGenerations}},
		{ID: "anthropic.thinking", Subject: "model", DefaultEnabled: false, CostClass: "high-token", Capabilities: []string{AnthropicThinkingManual}},
		{ID: "google-genai.interactions-stateful", Subject: "model", DefaultEnabled: false, CostClass: "persistent-resource", Capabilities: []string{GoogleInteractionsStateful}},
	}
	auto := AssessModules(classification, definitions, RoutingOptions{Modules: []string{"auto"}})
	if got := Module(auto, ModuleChat); got == nil || got.Execution != result.ExecutionExecuted || got.Source != "probe_policy" || got.Confidence != "low" {
		t.Fatalf("chat probe routing=%+v", got)
	}
	if got := Module(auto, ModuleImages); got == nil || got.Applicability != result.ApplicabilityNotApplicable {
		t.Fatalf("unrequested billable probe routing=%+v", got)
	}
	costs := AssessModules(classification, definitions, RoutingOptions{Modules: []string{ModuleImages, "anthropic.thinking", "google-genai.interactions-stateful"}})
	if got := Module(costs, ModuleImages); got == nil || got.Execution != result.ExecutionSkipped || got.Reason != "billable artifact module was not enabled" {
		t.Fatalf("billable probe bypassed gate=%+v", got)
	}
	if got := Module(costs, "anthropic.thinking"); got == nil || got.Execution != result.ExecutionSkipped || got.Reason != "high-token probes require explicit authorization" {
		t.Fatalf("high-token probe bypassed gate=%+v", got)
	}
	if got := Module(costs, "google-genai.interactions-stateful"); got == nil || got.Execution != result.ExecutionSkipped || got.Reason != "persistent-resource probes require explicit authorization" {
		t.Fatalf("persistent probe bypassed gate=%+v", got)
	}
}

func TestAnthropicThinkingRequiresCapabilityAndHighTokenAuthorization(t *testing.T) {
	definitions := []profile.Module{{ID: "anthropic.thinking", Subject: "model", DefaultEnabled: false, CostClass: "high-token", Capabilities: []string{AnthropicThinkingManual}}}
	classification := fromValues("claude", []string{AnthropicThinkingManual}, "test", "test", "high", "declared")
	withoutBudget := AssessModules(classification, definitions, RoutingOptions{Modules: []string{"anthropic.thinking"}})
	if got := Module(withoutBudget, "anthropic.thinking"); got == nil || got.Execution != result.ExecutionSkipped {
		t.Fatalf("thinking ran without high-token authorization: %+v", got)
	}
	withBudget := AssessModules(classification, definitions, RoutingOptions{Modules: []string{"anthropic.thinking"}, AllowHighTokenProbes: true})
	if got := Module(withBudget, "anthropic.thinking"); got == nil || got.Execution != result.ExecutionExecuted {
		t.Fatalf("authorized applicable thinking did not run: %+v", got)
	}
}

func TestPartialProbeCapabilityRoutesOnlyItsRequestedAnthropicModule(t *testing.T) {
	classification := Classification{Model: "partial", Capabilities: []Evidence{{Capability: AnthropicStructuredOutput}}, ProbeCapabilities: []string{AnthropicTools}}
	definitions := []profile.Module{
		{ID: "anthropic.structured-output", Subject: "model", DefaultEnabled: true, CostClass: "token", Capabilities: []string{AnthropicStructuredOutput}},
		{ID: "anthropic.tools", Subject: "model", DefaultEnabled: true, CostClass: "token", Capabilities: []string{AnthropicTools}},
		{ID: "anthropic.vision", Subject: "model", DefaultEnabled: true, CostClass: "token-media-input", Capabilities: []string{AnthropicImageInput}},
	}
	modules := AssessModules(classification, definitions, RoutingOptions{Modules: []string{"auto"}})
	if got := Module(modules, "anthropic.structured-output"); got == nil || got.Execution != result.ExecutionExecuted || got.Source == "probe_policy" {
		t.Fatalf("declared structured routing=%+v", got)
	}
	if got := Module(modules, "anthropic.tools"); got == nil || got.Execution != result.ExecutionExecuted || got.Source != "probe_policy" {
		t.Fatalf("absent tools were not probe-routed=%+v", got)
	}
	if got := Module(modules, "anthropic.vision"); got == nil || got.Applicability != result.ApplicabilityNotApplicable {
		t.Fatalf("unlisted vision became probe-routed=%+v", got)
	}
}

func TestAdvertisedCapabilityWinsOverProbeMarker(t *testing.T) {
	classification := Classification{Model: "known", Capabilities: []Evidence{{Capability: AnthropicTools, Source: "vendor_metadata", Confidence: "high", Reason: "declared"}}, ProbeCapabilities: []string{AnthropicTools}}
	modules := AssessModules(classification, []profile.Module{{ID: "anthropic.tools", Subject: "model", DefaultEnabled: true, CostClass: "token", Capabilities: []string{AnthropicTools}}}, RoutingOptions{Modules: []string{"auto"}})
	if got := Module(modules, "anthropic.tools"); got == nil || got.Source != "vendor_metadata" || got.Confidence != "high" {
		t.Fatalf("advertised capability lost to probe marker: %+v", got)
	}
}
