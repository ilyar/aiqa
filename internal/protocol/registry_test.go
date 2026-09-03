package protocol

import (
	"context"
	"testing"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
)

func TestRegistryProvidesStableProtocolDispatch(t *testing.T) {
	r := NewRegistry()
	runner := func(context.Context, RunOptions) (RunResult, error) {
		return RunResult{Target: result.TargetReport{}}, nil
	}
	if err := r.Register(config.ProtocolGoogleGenAI, runner); err != nil {
		t.Fatal(err)
	}
	if got, ok := r.Runner(config.ProtocolGoogleGenAI); !ok || got == nil {
		t.Fatal("registered Google runner was not returned")
	}
	if err := r.Register(config.ProtocolGoogleGenAI, runner); err == nil {
		t.Fatal("duplicate protocol runner was accepted")
	}
	if got := r.Protocols(); len(got) != 1 || got[0] != config.ProtocolGoogleGenAI {
		t.Fatalf("unexpected protocols: %v", got)
	}
}
