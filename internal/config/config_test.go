package config

import (
	"strings"
	"testing"
	"time"
)

func TestParseExpectedCLI(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("OPENAI_API_KEY", "secret")
	cfg, err := Parse([]string{"test=compat,model-param", "--timeout=120", "--format=markdown,json", "--name=report"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tests) != 2 || cfg.Tests[0] != "compat" || cfg.Tests[1] != "model-param" {
		t.Fatalf("unexpected tests: %#v", cfg.Tests)
	}
	if cfg.Timeout != 120*time.Second {
		t.Fatalf("unexpected timeout: %s", cfg.Timeout)
	}
	if cfg.RequestInterval != 2*time.Second {
		t.Fatalf("unexpected request interval: %s", cfg.RequestInterval)
	}
	if cfg.UnknownModel != "chat" {
		t.Fatalf("unexpected unknown-model policy: %s", cfg.UnknownModel)
	}
	if cfg.Guidance != "full" || cfg.GuidancePack != "auto" || cfg.GuidanceLocale != "en" {
		t.Fatalf("unexpected guidance defaults: mode=%q pack=%q locale=%q", cfg.Guidance, cfg.GuidancePack, cfg.GuidanceLocale)
	}
}

func TestParseGuidanceControls(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("OPENAI_API_KEY", "secret")
	cfg, err := Parse([]string{"test=compat", "--guidance=summary", "--guidance-pack=generic", "--guidance-locale=en"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Guidance != "summary" || cfg.GuidancePack != "generic" || cfg.GuidanceLocale != "en" {
		t.Fatalf("guidance controls were not preserved: %+v", cfg)
	}
}

func TestParseRejectsInvalidGuidanceControls(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("OPENAI_API_KEY", "secret")
	for _, args := range [][]string{
		{"test=compat", "--guidance=verbose"},
		{"test=compat", "--guidance-pack="},
		{"test=compat", "--guidance-locale=ru"},
	} {
		if _, err := Parse(args); err == nil {
			t.Fatalf("expected validation error for %v", args)
		}
	}
}

func TestParseCapabilityRoutingFlags(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("OPENAI_API_KEY", "secret")
	cfg, err := Parse([]string{
		"test=compat",
		"--module=auto,images-generations",
		"--model-capability=image-model=images.generations+images.edits",
		"--unknown-model=inventory",
		"--allow-billable-artifacts",
		"--max-artifacts-per-model=2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Modules) != 2 || len(cfg.ModelCapabilities) != 1 || !cfg.AllowBillableArtifacts || cfg.MaxArtifactsPerModel != 2 {
		t.Fatalf("capability flags were not preserved: %+v", cfg)
	}
}

func TestParseTimeoutWithSpace(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:8080/v1")
	t.Setenv("OPENAI_API_KEY", "secret")
	cfg, err := Parse([]string{"test=model-param", "--timeout", "2m"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timeout != 2*time.Minute {
		t.Fatalf("unexpected timeout: %s", cfg.Timeout)
	}
}

func TestParseUsesPreciseContextBoundaryByDefault(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:8080/v1")
	t.Setenv("OPENAI_API_KEY", "secret")
	cfg, err := Parse([]string{"test=model-param"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ContextPrecision != 512 {
		t.Fatalf("unexpected default context precision: %d", cfg.ContextPrecision)
	}
	if cfg.ContextStartBytes != 1024 {
		t.Fatalf("unexpected default context start bytes: %d", cfg.ContextStartBytes)
	}
}

func TestParseHelpWithoutCredentials(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	cfg, err := Parse([]string{"--help"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.HelpOnly {
		t.Fatal("help flag was not preserved")
	}
}

func TestParseDualTargetsInDeterministicOrder(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:8080/openai/v1")
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:8080/anthropic/v1")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-secret")
	cfg, err := Parse([]string{"test=compat", "--openai-model=o", "--anthropic-model=a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Targets) != 2 || cfg.Targets[0].Protocol != ProtocolOpenAI || cfg.Targets[1].Protocol != ProtocolAnthropic {
		t.Fatalf("unexpected targets: %+v", cfg.Targets)
	}
	if cfg.Targets[1].BaseURL != "http://127.0.0.1:8080/anthropic" || cfg.Targets[1].APIVersion != "2023-06-01" {
		t.Fatalf("unexpected Anthropic target: %+v", cfg.Targets[1])
	}
	if cfg.Targets[0].APIKey == cfg.Targets[1].APIKey {
		t.Fatal("target credentials were conflated")
	}
}

func TestParsePreservesExplicitTargetOrder(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:8080/openai/v1")
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:8080/anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-secret")
	cfg, err := Parse([]string{"test=compat", "--target=anthropic,openai", "--openai-model=o", "--anthropic-model=a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Targets) != 2 || cfg.Targets[0].Protocol != ProtocolAnthropic || cfg.Targets[1].Protocol != ProtocolOpenAI {
		t.Fatalf("explicit target order was not preserved: %+v", cfg.Targets)
	}
}

func TestParseActivatesTargetsIndependently(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "unused-key")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:8080/anthropic/v1/")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-secret")
	cfg, err := Parse([]string{"test=compat", "--anthropic-model=a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Targets) != 1 || cfg.Targets[0].Protocol != ProtocolAnthropic || cfg.Targets[0].BaseURL != "http://127.0.0.1:8080/anthropic" || !cfg.Targets[0].BaseURLNormalized {
		t.Fatalf("unexpected Anthropic-only target: %+v", cfg.Targets)
	}
}

func TestParseKeyWithoutBaseDoesNotActivateTarget(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:8080/v1")
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "unused-anthropic-key")
	cfg, err := Parse([]string{"test=compat"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Targets) != 1 || cfg.Targets[0].Protocol != ProtocolOpenAI {
		t.Fatalf("API key without base URL activated a target: %+v", cfg.Targets)
	}
}

func TestParseTargetQualifiedAnthropicCapabilityControls(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:8080/anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-secret")
	cfg, err := Parse([]string{"test=compat", "--anthropic-model=a", "--anthropic-model-capability=a=anthropic.messages.text+anthropic.messages.tools", "--anthropic-capability-file=anthropic.json", "--anthropic-unknown-model=inventory"})
	if err != nil {
		t.Fatal(err)
	}
	target := cfg.Targets[0]
	if target.UnknownModel != "inventory" || target.CapabilityFile != "anthropic.json" || len(target.ModelCapabilities) != 1 {
		t.Fatalf("Anthropic capability controls were not target-qualified: %+v", target)
	}
}

func TestParseRejectsUnqualifiedCapabilityControlsForMultipleTargets(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:8080/openai/v1")
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:8080/anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-secret")
	if _, err := Parse([]string{"test=compat", "--model-capability=same=chat.completions.text"}); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous capability override error, got %v", err)
	}
}

func TestParseRejectsAmbiguousModelWithDualTargets(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:8080/openai/v1")
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:8080/anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-secret")
	if _, err := Parse([]string{"test=compat", "--model=ambiguous"}); err == nil {
		t.Fatal("expected ambiguous --model to fail")
	}
}

func TestParseRejectsActiveTargetWithoutKey(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:8080/anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "")
	if _, err := Parse([]string{"test=compat"}); err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseRejectsCredentialsEmbeddedInBaseURL(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://user:secret@example.test/v1")
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	if _, err := Parse([]string{"test=compat"}); err == nil || !strings.Contains(err.Error(), "must not contain user information") {
		t.Fatalf("unexpected embedded-credential validation: %v", err)
	}
}
