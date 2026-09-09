package protocol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/progress"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/scheduler"
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

func TestPreparedTargetRejectsFrozenPlanDriftBeforeExecution(t *testing.T) {
	called := false
	prepared := PreparedTarget{
		Target: config.TargetConfig{ID: "target-a"},
		Jobs:   []Job{{ID: "native:target-a::inventory", Target: "target-a"}},
		Execute: func(context.Context, progress.Sink) (RunResult, error) {
			called = true
			return RunResult{}, nil
		},
	}
	_, err := prepared.ExecutePlan(context.Background(), &frozenRecorder{}, TargetExecutionPlan{
		TargetID: "target-a", Binding: TargetBinding{TargetID: "target-a"},
		Jobs: []JobSpec{{ID: "different", TargetID: "target-a"}},
	})
	if err == nil || called {
		t.Fatalf("plan drift err=%v called=%t", err, called)
	}
}

func TestConfigureClientSchedulerUsesOnlyLocalTargetAndAlias(t *testing.T) {
	broker, err := scheduler.New(scheduler.Config{Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	client, err := api.NewClient("https://provider.example/v1", "key", "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := ConfigureClientScheduler(client, RunOptions{
		Target:      config.TargetConfig{ID: "target_a"},
		Scheduler:   broker,
		PacingGroup: "shared-quota",
	}); err != nil {
		t.Fatalf("configure scheduler: %v", err)
	}
	if err := ConfigureClientScheduler(client, RunOptions{
		Target:      config.TargetConfig{ID: "https://provider.example"},
		Scheduler:   broker,
		PacingGroup: "shared-quota",
	}); err == nil {
		t.Fatal("unsafe target ID was accepted")
	}
	if err := ConfigureClientScheduler(client, RunOptions{PacingGroup: "shared-quota"}); err == nil {
		t.Fatal("pacing alias without run broker was accepted")
	}
}

func TestConfigureClientObservabilityAttachesDeclaredPreparationOwner(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	client, err := api.NewClient(server.URL, "", "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var events []api.ExecutionEvent
	options := RunOptions{
		Target: config.TargetConfig{ID: "target-a", Protocol: config.ProtocolOpenAI},
		ExecutionHooks: ExecutionHooks{
			Observe:          func(event api.ExecutionEvent) { mu.Lock(); events = append(events, event); mu.Unlock() },
			PreparationOwner: api.ExecutionOwner{TargetID: "target-a", PreparationID: "target-a-discovery"},
		},
	}
	if err := ConfigureClientObservability(client, options); err != nil {
		t.Fatal(err)
	}
	if _, err := client.DoJSON(PreparationContext(context.Background(), options), http.MethodGet, "models", nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 3 || events[0].PreparationID != "target-a-discovery" || events[0].JobID != "" || events[2].Kind != api.ExecutionRequestTerminal {
		t.Fatalf("preparation lifecycle=%#v", events)
	}
}

func TestConfigureClientObservabilityRejectsRuntimeJobAsPreparationOwner(t *testing.T) {
	client, err := api.NewClient("https://provider.example", "", "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	err = ConfigureClientObservability(client, RunOptions{
		Target:         config.TargetConfig{ID: "target-a"},
		ExecutionHooks: ExecutionHooks{PreparationOwner: api.ExecutionOwner{TargetID: "target-a", PreparationID: "discovery", JobID: "not-frozen"}},
	})
	if err == nil {
		t.Fatal("runtime JobID was accepted as preparation owner")
	}
}
