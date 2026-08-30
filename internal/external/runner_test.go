package external

import (
	"context"
	"testing"
)

func TestExplicitExternalModeFailsWhenToolIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, requested := range []string{"binary", "docker"} {
		t.Run(requested, func(t *testing.T) {
			mode, _, reason := resolveMode(context.Background(), requested, "example.invalid/test:latest")
			if mode != "error" {
				t.Fatalf("mode=%q reason=%q, want error", mode, reason)
			}
		})
	}
}

func TestAutoExternalModeSkipsWhenNothingIsInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	mode, _, _ := resolveMode(context.Background(), "auto", "example.invalid/test:latest")
	if mode != "off" {
		t.Fatalf("mode=%q, want off", mode)
	}
}
