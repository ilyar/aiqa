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
