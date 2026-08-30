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
