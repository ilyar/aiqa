package config

import (
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
