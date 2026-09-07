package protocol

import (
	"context"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/progress"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/telemetry"
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

type frozenRecorder struct{ starts, completes []string }

func (*frozenRecorder) ObserveLifecycle(telemetry.LifecycleEvent) {}
func (*frozenRecorder) RunStarted(int)                            {}
func (*frozenRecorder) Planning(string)                           {}
func (*frozenRecorder) DiscoveryStarted(string)                   {}
func (*frozenRecorder) Plan(string, int)                          {}
func (*frozenRecorder) RevisePlan(string, int)                    {}
func (*frozenRecorder) AddPlan(string, int)                       {}
func (*frozenRecorder) Pacing(string, bool, time.Duration)        {}
func (*frozenRecorder) TargetFinished(string, string)             {}
func (*frozenRecorder) RunFinished()                              {}
func (*frozenRecorder) Message(string)                            {}
func (*frozenRecorder) Close()                                    {}
func (f *frozenRecorder) Freeze([]progress.Job)                   {}
func (f *frozenRecorder) Start(id string)                         { f.starts = append(f.starts, id) }
func (f *frozenRecorder) Complete(id, _ string)                   { f.completes = append(f.completes, id) }

func TestStartJobCompletesAtItsBoundary(t *testing.T) {
	sink := &frozenRecorder{}
	done := StartJob(sink, "target-a", "model-a", "compat")
	if len(sink.starts) != 1 || len(sink.completes) != 0 {
		t.Fatalf("unexpected pre-boundary lifecycle: %+v", sink)
	}
	done("completed")
	if len(sink.completes) != 1 || sink.completes[0] != JobID("target-a", "model-a", "compat") {
		t.Fatalf("unexpected completion: %+v", sink.completes)
	}
}

func TestRegistryProvidesPreparedDispatch(t *testing.T) {
	r := NewRegistry()
	preparer := func(context.Context, RunOptions) (PreparedTarget, error) { return PreparedTarget{}, nil }
	if err := r.RegisterPreparer(config.ProtocolGoogleGenAI, preparer); err != nil {
		t.Fatal(err)
	}
	if got, ok := r.Preparer(config.ProtocolGoogleGenAI); !ok || got == nil {
		t.Fatal("registered preparer was not returned")
	}
	if err := r.RegisterPreparer(config.ProtocolGoogleGenAI, preparer); err == nil {
		t.Fatal("duplicate protocol preparer was accepted")
	}
}
