package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseVerboseLevels(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("OPENAI_API_KEY", "secret")
	for _, test := range []struct {
		name string
		args []string
		want int
	}{
		{"quiet by default", []string{"test=compat"}, 0},
		{"long default", []string{"test=compat", "--verbose"}, 1},
		{"long equals", []string{"test=compat", "--verbose=2"}, 2},
		{"long separate", []string{"test=compat", "--verbose", "3"}, 3},
		{"short one", []string{"test=compat", "-v"}, 1},
		{"short four", []string{"test=compat", "-vvvv"}, 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := Parse(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Verbosity != test.want {
				t.Fatalf("verbosity = %d, want %d", cfg.Verbosity, test.want)
			}
		})
	}
}

func TestParseRejectsInvalidVerboseLevels(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("OPENAI_API_KEY", "secret")
	for _, args := range [][]string{
		{"test=compat", "--verbose=-1"},
		{"test=compat", "--verbose=5"},
		{"test=compat", "--verbose=verbose"},
		{"test=compat", "-vvvvv"},
		{"test=compat", "-vvx"},
	} {
		if _, err := Parse(args); err == nil {
			t.Fatalf("invalid verbose argument was accepted: %v", args)
		}
	}
}

func TestValidateRejectsInvalidVerbosity(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("OPENAI_API_KEY", "secret")
	cfg, err := Parse([]string{"test=compat", "--verbose=1"})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Verbosity = 5
	if err := cfg.Validate(); err == nil {
		t.Fatal("out-of-range programmatic verbosity was accepted")
	}
}

func TestParseExtensionSelectionAndPaths(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("OPENAI_API_KEY", "secret")
	envPath := filepath.Join(t.TempDir(), "aiqa-env-ext-suite")
	flagPath := filepath.Join(t.TempDir(), "aiqa-flag-ext-suite")
	t.Setenv("AIQA_EXT_SUITE_PATHS", strings.Join([]string{envPath}, string(os.PathListSeparator)))
	cfg, err := Parse([]string{"test=ext-suite", "--ext-suite=acme.gateway,fixture.gateway", "--ext-suite=acme.gateway", "--ext-suite-path=" + flagPath})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.ExtSuites, ",") != "acme.gateway,fixture.gateway" || len(cfg.ExtSuitePaths) != 2 || cfg.ExtSuitePaths[0] != envPath || cfg.ExtSuitePaths[1] != flagPath {
		t.Fatalf("extension controls were not preserved: suites=%v paths=%v", cfg.ExtSuites, cfg.ExtSuitePaths)
	}
}

func TestParseSDKTestSelector(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("OPENAI_API_KEY", "secret")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_GENAI_BASE_URL", "")
	t.Setenv("GOOGLE_GEMINI_BASE_URL", "")
	t.Setenv("GOOGLE_GENAI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("AIQA_EXT_SUITE_PATHS", "")

	cfg, err := Parse([]string{"test=compat,sdk"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tests) != 2 || cfg.Tests[0] != "compat" || cfg.Tests[1] != "sdk" {
		t.Fatalf("unexpected tests: %#v", cfg.Tests)
	}
	if !cfg.HasExtensionTests() || cfg.HasTest("ext-suite") || len(cfg.ExtSuites) != 0 {
		t.Fatalf("SDK selector was treated as a generic extension selection: %+v", cfg)
	}
}

func TestParseGoogleGenAIEnvironmentAndGlobalModelList(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_GENAI_BASE_URL", "https://gateway.example/google/v1beta/")
	t.Setenv("GOOGLE_GEMINI_BASE_URL", "https://gateway.example/google/v1beta")
	t.Setenv("GOOGLE_GENAI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "google-key")
	t.Setenv("GEMINI_API_KEY", "google-key")
	t.Setenv("GOOGLE_GENAI_MODEL", "")
	cfg, err := Parse([]string{"test=compat,ext-suite", "--ext-suite=fixture.google", "--model=gemini-a,gemini-b,gemini-a"})
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
	if strings.Join(cfg.Models, ",") != "gemini-a,gemini-b" || len(target.Models) != 0 {
		t.Fatalf("global model list was not preserved without a target-local assignment: cfg=%v target=%v", cfg.Models, target.Models)
	}
	if got := strings.Join(cfg.ExtSuites, ","); got != "fixture.google" {
		t.Fatalf("extension selector was unexpectedly rewritten: %q", got)
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

func TestParseRejectsRemovedModelSelectorsAndGoogleModelEnvironment(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:8080/v1")
	t.Setenv("OPENAI_API_KEY", "key")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	for _, argument := range []string{"--openai-model=x", "--anthropic-model=x", "--google-genai-model=x"} {
		if _, err := Parse([]string{"test=compat", argument}); err == nil || !strings.Contains(err.Error(), "removed") {
			t.Fatalf("removed selector %q was accepted: %v", argument, err)
		}
	}
	t.Setenv("GOOGLE_GENAI_BASE_URL", "http://127.0.0.1:8080/google")
	t.Setenv("GOOGLE_GENAI_API_KEY", "key")
	t.Setenv("GOOGLE_GENAI_MODEL", "legacy-model")
	if _, err := Parse([]string{"test=compat"}); err == nil || !strings.Contains(err.Error(), "GOOGLE_GENAI_MODEL") {
		t.Fatalf("removed Google model environment was accepted: %v", err)
	}
}

func TestParseGlobalModelListRejectsEmptyComponentsAndDeduplicates(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:8080/v1")
	t.Setenv("OPENAI_API_KEY", "key")
	for _, value := range []string{"", " ", "one,,two", "one, ,two", ",one", "one,"} {
		if _, err := Parse([]string{"test=compat", "--model=" + value}); err == nil || !strings.Contains(err.Error(), "empty model ID") {
			t.Fatalf("empty model component %q was accepted: %v", value, err)
		}
	}
	cfg, err := Parse([]string{"test=compat", "--model= one, two,one ,two "})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Models, ",") != "one,two" {
		t.Fatalf("global model list was not trimmed/deduplicated: %v", cfg.Models)
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

func TestParseRejectsExtensionFilterWithoutExtensionTest(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("OPENAI_API_KEY", "secret")
	if _, err := Parse([]string{"test=compat", "--ext-suite=fixture.openai"}); err == nil {
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
	if cfg.GoogleGenAIProfile != "google-genai-generate-content-compat-2026-09.json" {
		t.Fatalf("unexpected Google GenAI default profile: %q", cfg.GoogleGenAIProfile)
	}
	if cfg.UnknownModel != "inventory" {
		t.Fatalf("unexpected unknown-model policy: %s", cfg.UnknownModel)
	}
	if cfg.AnthropicUnknownModel != "inventory" {
		t.Fatalf("unexpected Anthropic unknown-model policy: %s", cfg.AnthropicUnknownModel)
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
	cfg, err := Parse([]string{"test=compat", "--model=o,a"})
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
	if strings.Join(cfg.Models, ",") != "o,a" || len(cfg.Targets[0].Models) != 0 || len(cfg.Targets[1].Models) != 0 {
		t.Fatalf("global models were not kept separate from target configuration: cfg=%v targets=%+v", cfg.Models, cfg.Targets)
	}
}

func TestParsePreservesExplicitTargetSelectionOrderBeforePlanning(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:8080/openai/v1")
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:8080/anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-secret")
	cfg, err := Parse([]string{"test=compat", "--target=anthropic,openai", "--model=o,a"})
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
	cfg, err := Parse([]string{"test=compat", "--model=a"})
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
	cfg, err := Parse([]string{"test=compat", "--model=a", "--anthropic-model-capability=a=anthropic.messages.text+anthropic.messages.tools", "--anthropic-capability-file=anthropic.json", "--anthropic-unknown-model=inventory"})
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

func TestParseAppliesGlobalModelToEveryActiveTarget(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:8080/openai/v1")
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:8080/anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-secret")
	cfg, err := Parse([]string{"test=compat", "--model=ambiguous"})
	if err != nil {
		t.Fatalf("global --model was rejected for multiple targets: %v", err)
	}
	if len(cfg.Targets) != 2 || len(cfg.Models) != 1 || cfg.Models[0] != "ambiguous" {
		t.Fatalf("global model was not retained across targets: %+v", cfg)
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
