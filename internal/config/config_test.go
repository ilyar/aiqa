package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseExtensionSelectionAndPaths(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("OPENAI_API_KEY", "secret")
	envPath := filepath.Join(t.TempDir(), "aiqa-env-ext-suite")
	flagPath := filepath.Join(t.TempDir(), "aiqa-flag-ext-suite")
	t.Setenv("AIQA_EXT_SUITE_PATHS", strings.Join([]string{envPath}, string(os.PathListSeparator)))
	cfg, err := Parse([]string{"test=ext-suite", "--ext-suite=official-sdk.openai,acme.gateway", "--ext-suite=acme.gateway", "--ext-suite-path=" + flagPath})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.ExtSuites, ",") != "official-sdk.openai,acme.gateway" || len(cfg.ExtSuitePaths) != 2 || cfg.ExtSuitePaths[0] != envPath || cfg.ExtSuitePaths[1] != flagPath {
		t.Fatalf("extension controls were not preserved: suites=%v paths=%v", cfg.ExtSuites, cfg.ExtSuitePaths)
	}
}

func TestResolveOfficialSDKSelectionUsesConfiguredTargets(t *testing.T) {
	tests := []struct {
		name    string
		targets []TargetConfig
		want    string
	}{
		{name: "openai only", targets: []TargetConfig{{Protocol: ProtocolOpenAI}}, want: "official-sdk.openai"},
		{name: "anthropic only", targets: []TargetConfig{{Protocol: ProtocolAnthropic}}, want: "official-sdk.anthropic"},
		{name: "both", targets: []TargetConfig{{Protocol: ProtocolOpenAI}, {Protocol: ProtocolAnthropic}}, want: "official-sdk.openai,official-sdk.anthropic"},
		{name: "google", targets: []TargetConfig{{Protocol: ProtocolGoogleGenAI}}, want: "official-sdk.google-genai"},
		{name: "all", targets: []TargetConfig{{Protocol: ProtocolOpenAI}, {Protocol: ProtocolAnthropic}, {Protocol: ProtocolGoogleGenAI}}, want: "official-sdk.openai,official-sdk.anthropic,official-sdk.google-genai"},
		{name: "no target", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveExtSuiteSelection([]string{"official-sdk"}, tt.targets)
			if strings.Join(got, ",") != tt.want {
				t.Fatalf("resolved selection=%v, want %q", got, tt.want)
			}
		})
	}
}

func TestParseGoogleGenAIEnvironmentAndAliases(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_GENAI_BASE_URL", "https://gateway.example/google/v1beta/")
	t.Setenv("GOOGLE_GEMINI_BASE_URL", "https://gateway.example/google/v1beta")
	t.Setenv("GOOGLE_GENAI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "google-key")
	t.Setenv("GEMINI_API_KEY", "google-key")
	t.Setenv("GOOGLE_GENAI_MODEL", "gemini-a,gemini-b,gemini-a")
	cfg, err := Parse([]string{"test=compat,ext-suite", "--ext-suite=official-sdk"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Targets) != 1 || cfg.Targets[0].Protocol != ProtocolGoogleGenAI {
		t.Fatalf("unexpected Google target: %+v", cfg.Targets)
	}
	target := cfg.Targets[0]
	if target.BaseURL != "https://gateway.example/google" || !target.BaseURLNormalized || target.APIVersion != "v1beta" || target.APIKey != "google-key" {
		t.Fatalf("unexpected Google target normalization: %+v", target)
	}
	if strings.Join(target.Models, ",") != "gemini-a,gemini-b" {
		t.Fatalf("Google model env was not preserved: %v", target.Models)
	}
	if got := strings.Join(ResolveExtSuiteSelection(cfg.ExtSuites, cfg.Targets), ","); got != "official-sdk.google-genai" {
		t.Fatalf("unexpected Google SDK selection: %q", got)
	}
}

func TestParseGoogleGenAICanonicalKeyWinsAndAliasConflictFails(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_GENAI_BASE_URL", "http://127.0.0.1:8080/google")
	t.Setenv("GOOGLE_GENAI_API_KEY", "canonical")
	t.Setenv("GOOGLE_API_KEY", "alias-one")
	t.Setenv("GEMINI_API_KEY", "alias-two")
	cfg, err := Parse([]string{"test=compat"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Targets[0].APIKey != "canonical" {
		t.Fatalf("canonical key did not win: %+v", cfg.Targets[0])
	}
	t.Setenv("GOOGLE_GENAI_API_KEY", "")
	if _, err := Parse([]string{"test=compat"}); err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("conflicting generic aliases were accepted: %v", err)
	}
}

func TestParseGoogleGenAIBaseURLValidation(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_GENAI_BASE_URL", "http://gateway.example/google")
	t.Setenv("GOOGLE_GENAI_API_KEY", "key")
	if _, err := Parse([]string{"test=compat"}); err == nil || !strings.Contains(err.Error(), "non-HTTPS") {
		t.Fatalf("non-loopback HTTP Google URL was accepted: %v", err)
	}
	t.Setenv("GOOGLE_GENAI_BASE_URL", "http://127.0.0.1:8080/google?key=leak")
	if _, err := Parse([]string{"test=compat"}); err == nil || !strings.Contains(err.Error(), "query") {
		t.Fatalf("Google URL query was accepted: %v", err)
	}
	t.Setenv("GOOGLE_GENAI_BASE_URL", "http://127.0.0.1:8080/google")
	if _, err := Parse([]string{"test=compat", "--google-genai-api-version=v2gamma"}); err == nil || !strings.Contains(err.Error(), "API version") {
		t.Fatalf("invalid Google API version was accepted: %v", err)
	}
}

func TestParseGoogleGenAIBaseAliasConflictFails(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_GENAI_BASE_URL", "http://127.0.0.1:8080/one")
	t.Setenv("GOOGLE_GEMINI_BASE_URL", "http://127.0.0.1:8080/two")
	t.Setenv("GOOGLE_GENAI_API_KEY", "key")
	if _, err := Parse([]string{"test=compat"}); err == nil || !strings.Contains(err.Error(), "BASE_URL") || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("conflicting Google base aliases were accepted: %v", err)
	}
	if _, err := Parse([]string{"test=compat", "--google-genai-base-url=http://127.0.0.1:8080/explicit"}); err != nil {
		t.Fatalf("explicit Google base URL should override environment aliases: %v", err)
	}
}

func TestResolveOfficialSDKSelectionDeduplicatesExplicitIDs(t *testing.T) {
	got := ResolveExtSuiteSelection([]string{"official-sdk", "official-sdk.openai", "acme.gateway"}, []TargetConfig{{Protocol: ProtocolOpenAI}})
	if want := "official-sdk.openai,acme.gateway"; strings.Join(got, ",") != want {
		t.Fatalf("resolved selection=%v, want %q", got, want)
	}
}

func TestParseRejectsExtensionFilterWithoutExtensionTest(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("OPENAI_API_KEY", "secret")
	if _, err := Parse([]string{"test=compat", "--ext-suite=official-sdk.openai"}); err == nil {
		t.Fatal("extension filter without test=ext-suite was accepted")
	}
}

func TestParseRejectsDuplicateExplicitExtensionPath(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("OPENAI_API_KEY", "secret")
	path := filepath.Join(t.TempDir(), "aiqa-demo-ext-suite")
	if _, err := Parse([]string{"test=ext-suite", "--ext-suite-path=" + path, "--ext-suite-path=" + path}); err == nil || !strings.Contains(err.Error(), "configured more than once") {
		t.Fatalf("duplicate explicit path was not rejected: %v", err)
	}
}

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

func TestParsePreservesExplicitTargetSelectionOrderBeforePlanning(t *testing.T) {
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

func TestParseIgnoresGoogleAliasConflictWhenGoogleIsNotActive(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:8080/v1")
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_GENAI_BASE_URL", "")
	t.Setenv("GOOGLE_GEMINI_BASE_URL", "")
	t.Setenv("GOOGLE_GENAI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "google-one")
	t.Setenv("GEMINI_API_KEY", "google-two")
	cfg, err := Parse([]string{"test=compat"})
	if err != nil {
		t.Fatalf("inactive Google alias conflict should not affect auto target selection: %v", err)
	}
	if len(cfg.Targets) != 1 || cfg.Targets[0].Protocol != ProtocolOpenAI {
		t.Fatalf("unexpected targets after ignoring inactive Google aliases: %+v", cfg.Targets)
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
