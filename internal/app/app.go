package app

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/external"
	"github.com/ilyar/aiqa/internal/profile"
	reportpkg "github.com/ilyar/aiqa/internal/report"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/suites"
)

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

type RunResult struct {
	Report   result.Report
	Paths    []string
	ExitCode int
}

func Run(ctx context.Context, cfg config.Config, build BuildInfo) (RunResult, error) {
	p, err := profile.Load()
	if err != nil {
		return RunResult{}, err
	}
	client, err := api.NewClient(cfg.BaseURL, cfg.APIKey, "aiqa/"+build.Version)
	if err != nil {
		return RunResult{}, err
	}
	r := result.Report{
		SchemaVersion: "aiqa.report.v1",
		GeneratedAt:   time.Now().UTC(),
		Tool: result.ToolInfo{
			Name:      "aiqa",
			Version:   build.Version,
			Commit:    build.Commit,
			BuildDate: build.Date,
			GoVersion: runtime.Version(),
		},
		Profile: result.ProfileInfo{
			ID:                p.ID,
			Revision:          p.Revision,
			Source:            p.Source,
			SourceCommit:      p.SourceCommit,
			SourceRefRevision: p.SourceRefRevision,
			Scope:             p.Scope,
		},
		Config: result.PublicConfig{
			BaseURL:          cfg.BaseURL,
			Tests:            append([]string(nil), cfg.Tests...),
			RequestedModels:  append([]string(nil), cfg.Models...),
			Timeout:          cfg.Timeout.String(),
			Formats:          append([]string(nil), cfg.Formats...),
			ExternalMode:     cfg.ExternalMode,
			FailOn:           cfg.FailOn,
			ContextMaxTokens: cfg.ContextMaxTokens,
			ContextPrecision: cfg.ContextPrecision,
			ContextBudget:    cfg.ContextBudget,
			Samples:          cfg.Samples,
		},
	}

	logf := func(format string, args ...any) {
		if cfg.Verbose {
			fmt.Fprintf(os.Stderr, "aiqa: "+format+"\n", args...)
		}
	}
	env := suites.Environment{Client: client, Config: cfg, Profile: p, Logf: logf}

	listCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	models, listResp, listErr := client.ListModels(listCtx)
	cancel()
	r.Discovery.ModelCount = len(models)
	for _, model := range models {
		r.Discovery.Models = append(r.Discovery.Models, cloneMap(model.Raw))
	}
	if cfg.HasTest("compat") {
		r.Discovery.Tests = append(r.Discovery.Tests, suites.DiscoveryTests(p, listResp, models, listErr)...)
	}

	selected, metadata := selectModels(cfg.Models, models)
	if len(selected) == 0 {
		// model-param still needs a visible discovery failure even when compat was
		// not selected.
		if !cfg.HasTest("compat") {
			r.Discovery.Tests = append(r.Discovery.Tests, suites.DiscoveryTests(p, listResp, models, listErr)...)
		}
		reportpkg.Finalize(&r)
		paths, writeErr := reportpkg.Write(r, cfg)
		if writeErr != nil {
			return RunResult{Report: r, ExitCode: 2}, writeErr
		}
		if listErr != nil {
			return RunResult{Report: r, Paths: paths, ExitCode: 1}, fmt.Errorf("no models available: %w", listErr)
		}
		return RunResult{Report: r, Paths: paths, ExitCode: 1}, fmt.Errorf("no models returned by endpoint")
	}
	fmt.Fprintf(os.Stderr, "aiqa: testing %d model(s): %s\n", len(selected), joinModels(selected))
	if cfg.HasTest("model-param") {
		fmt.Fprintf(os.Stderr, "aiqa: context probing is enabled; the per-model prompt budget is %d tokens\n", cfg.ContextBudget)
	}

	if cfg.HasTest("compat") {
		r.Discovery.Tests = append(r.Discovery.Tests, suites.RunEndpointCompat(ctx, env, selected[0])...)
		suites.SortTests(r.Discovery.Tests)
	}

	type item struct {
		index  int
		report result.ModelReport
	}
	jobs := make(chan int)
	results := make(chan item, len(selected))
	workers := cfg.Concurrency
	if workers > len(selected) {
		workers = len(selected)
	}
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				model := selected[index]
				fmt.Fprintf(os.Stderr, "aiqa: [%s] start\n", model)
				mr := result.ModelReport{ID: model, Metadata: cloneMap(metadata[model])}
				if cfg.HasTest("compat") {
					mr.Compatibility = suites.RunModelCompat(ctx, env, model)
					suites.SortTests(mr.Compatibility)
				}
				if cfg.HasTest("model-param") {
					mr.Parameters, mr.Context = suites.RunModelParameters(ctx, env, model, metadata[model])
				}
				if cfg.HasTest("compat") && cfg.ExternalMode != "off" {
					mr.External = append(mr.External, external.Run(ctx, cfg, model)...)
				}
				fmt.Fprintf(os.Stderr, "aiqa: [%s] done\n", model)
				results <- item{index: index, report: mr}
			}
		}()
	}
	go func() {
		for i := range selected {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	ordered := make([]result.ModelReport, len(selected))
	for item := range results {
		ordered[item.index] = item.report
	}
	r.Models = ordered
	reportpkg.Finalize(&r)
	paths, err := reportpkg.Write(r, cfg)
	if err != nil {
		return RunResult{Report: r, ExitCode: 2}, err
	}
	return RunResult{Report: r, Paths: paths, ExitCode: exitCode(cfg, r.Summary)}, nil
}

func selectModels(requested []string, discovered []api.Model) ([]string, map[string]map[string]any) {
	metadata := make(map[string]map[string]any, len(discovered))
	for _, model := range discovered {
		metadata[model.ID] = model.Raw
	}
	if len(requested) > 0 {
		selected := append([]string(nil), requested...)
		for _, id := range selected {
			if metadata[id] == nil {
				metadata[id] = map[string]any{"id": id, "object": "model", "source": "--model"}
			}
		}
		return selected, metadata
	}
	selected := make([]string, 0, len(discovered))
	for _, model := range discovered {
		selected = append(selected, model.ID)
	}
	sort.Strings(selected)
	return selected, metadata
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func joinModels(models []string) string {
	const max = 5
	if len(models) <= max {
		return fmt.Sprint(models)
	}
	return fmt.Sprintf("%v and %d more", models[:max], len(models)-max)
}

func exitCode(cfg config.Config, s result.Summary) int {
	switch cfg.FailOn {
	case "never":
		return 0
	case "any":
		if s.Fail > 0 || s.Error > 0 || s.Warn > 0 || s.Unsupported > 0 {
			return 1
		}
		return 0
	case "core":
		if s.RequiredFail > 0 || s.Fail > 0 || s.Error > 0 {
			return 1
		}
		return 0
	default:
		return 2
	}
}
