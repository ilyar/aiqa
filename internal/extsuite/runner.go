package extsuite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/probe"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

func RegisterExecutables(ctx context.Context, registry *Registry, cfg config.Config, host HostInfo) error {
	executables, err := DiscoverExecutables(ctx, cfg.ExtSuitePaths, os.Getenv("PATH"), host)
	if err != nil {
		return err
	}
	for _, extension := range executables {
		if err := registry.Register(extension); err != nil {
			return err
		}
	}
	return nil
}

func ValidateSelection(registry *Registry, selected []string) error {
	for _, id := range selected {
		if _, ok := registry.Get(id); !ok {
			return fmt.Errorf("selected extension %q is not installed", id)
		}
	}
	return nil
}

type invocation struct {
	extension Extension
	manifest  Manifest
	subject   result.SubjectRef
	suiteIDs  []string
}

const maxExtensionDeadline = time.Hour
const extensionRetryLimit = 2

func ExecuteTarget(ctx context.Context, registry *Registry, cfg config.Config, target config.TargetConfig, p profile.Profile, models []result.ModelReport, client *api.Client, executor *probe.Executor) []result.ExtensionRun {
	invocations := planTarget(registry, cfg, target, p, models)
	if len(invocations) == 0 {
		return nil
	}
	runs := make([]result.ExtensionRun, len(invocations))
	var wg sync.WaitGroup
	unsafeLocks := map[string]*sync.Mutex{}
	for _, planned := range invocations {
		if planned.extension.Provenance().Source == "executable" && hasUnsafeConcurrency(planned.manifest, planned.suiteIDs) {
			if unsafeLocks[planned.manifest.Extension.ID] == nil {
				unsafeLocks[planned.manifest.Extension.ID] = &sync.Mutex{}
			}
		}
	}
	for index, planned := range invocations {
		index, planned := index, planned
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowedConcurrency := cfg.Concurrency
			if planned.extension.Provenance().Source == "executable" {
				allowedConcurrency = declaredSlots(planned.manifest, planned.suiteIDs, cfg.Concurrency)
			}
			deadline := extensionDeadline(cfg.Timeout, len(planned.suiteIDs))
			pacing := api.PacingStats{}
			if client != nil {
				pacing = client.PacingConstraints()
			}
			request := RunRequest{
				SchemaVersion: ProtocolVersion,
				Type:          "run_request",
				RunID:         runID(target.ID, planned.manifest.Extension.ID, planned.subject),
				Target:        Target{ID: target.ID, Protocol: string(target.Protocol), BaseURL: target.BaseURL, APIVersion: target.APIVersion},
				Subject:       planned.subject,
				SuiteIDs:      append([]string(nil), planned.suiteIDs...),
				Profile:       ProfileRef{ID: p.ID, Revision: p.Revision},
				Limits: Limits{
					RequestTimeoutMS:              cfg.Timeout.Milliseconds(),
					DeadlineMS:                    deadline.Milliseconds(),
					MaxConcurrency:                allowedConcurrency,
					RequestIntervalMS:             target.RequestInterval.Milliseconds(),
					RetryLimit:                    extensionRetryLimit,
					ObservedSafeRequestsPerSecond: pacing.SafeRate,
					ObservedSafeConcurrency:       pacing.SafeConcurrency,
					MaxOutputBytes:                maxStdoutBytes,
				},
				Credential: Credential{Type: "api_key", Value: target.APIKey},
				Runtime:    RuntimeServices{Executor: executor},
			}
			if client != nil {
				request.Runtime.HTTPClient = client.PacedHTTPClient(planned.subject.ID)
			}
			collector, err := NewCollector(planned.manifest, request, p)
			started := time.Now()
			if err != nil {
				provenance := planned.extension.Provenance()
				pacingCompliance := "host_enforced"
				if provenance.Source == "executable" {
					pacingCompliance = "extension_required_unverified"
				}
				runs[index] = result.ExtensionRun{Provenance: provenance, TargetID: target.ID, Subject: planned.subject, Runner: result.ExtensionRunnerResult{Status: result.StatusError, PacingCompliance: pacingCompliance, Summary: "Extension invocation could not be validated", Error: err.Error()}}
				return
			}
			runCtx, cancel := context.WithTimeout(ctx, deadline)
			defer cancel()
			var telemetry RunnerTelemetry
			var runErr error
			if planned.extension.Provenance().Source == "executable" {
				slots := declaredSlots(planned.manifest, planned.suiteIDs, cfg.Concurrency)
				if lock := unsafeLocks[planned.manifest.Extension.ID]; lock != nil {
					lock.Lock()
					defer lock.Unlock()
				}
				executor.DoN(slots, func() { telemetry, runErr = planned.extension.Run(runCtx, request, collector) })
			} else {
				telemetry, runErr = planned.extension.Run(runCtx, request, collector)
			}
			if runCtx.Err() != nil {
				runErr = fmt.Errorf("extension deadline exceeded: %w", runCtx.Err())
			}
			runs[index] = collector.Finish(planned.extension.Provenance(), telemetry, runErr, started)
		}()
	}
	wg.Wait()
	return runs
}

func extensionDeadline(timeout time.Duration, suites int) time.Duration {
	count := time.Duration(max(1, suites))
	if timeout >= maxExtensionDeadline/count {
		return maxExtensionDeadline
	}
	return timeout * count
}

func hasUnsafeConcurrency(manifest Manifest, suiteIDs []string) bool {
	wanted := map[string]bool{}
	for _, id := range suiteIDs {
		wanted[id] = true
	}
	for _, suite := range manifest.Suites {
		if wanted[suite.ID] && suite.InternalConcurrency == 0 {
			return true
		}
	}
	return false
}

func planTarget(registry *Registry, cfg config.Config, target config.TargetConfig, p profile.Profile, models []result.ModelReport) []invocation {
	selected := map[string]bool{}
	for _, id := range cfg.ExtSuites {
		selected[id] = true
	}
	artifactBudgets := make([]int, len(models))
	for index, model := range models {
		artifactBudgets[index] = cfg.MaxArtifactsPerModel
		if cfg.HasTest("compat") {
			artifactBudgets[index] -= nativeArtifactReservations(p, model)
			if artifactBudgets[index] < 0 {
				artifactBudgets[index] = 0
			}
		}
	}
	var out []invocation
	for _, extension := range registry.All() {
		manifest := extension.Manifest()
		if (len(selected) > 0 && !selected[manifest.Extension.ID]) || !supportsProtocol(manifest, string(target.Protocol)) || !contains(manifest.ProfileContracts, p.SchemaVersion) {
			continue
		}
		providerSuites := applicableSuites(manifest, cfg, p, nil, nil)
		if len(providerSuites) > 0 {
			out = append(out, invocation{extension: extension, manifest: manifest, subject: result.SubjectRef{TargetID: target.ID, Protocol: string(target.Protocol), Kind: "provider", ID: strings.TrimRight(target.BaseURL, "/")}, suiteIDs: providerSuites})
		}
		for index := range models {
			model := &models[index]
			suites := applicableSuites(manifest, cfg, p, model, &artifactBudgets[index])
			if len(suites) == 0 {
				continue
			}
			out = append(out, invocation{extension: extension, manifest: manifest, subject: result.SubjectRef{TargetID: target.ID, Protocol: string(target.Protocol), Kind: "model", ID: model.ID}, suiteIDs: suites})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.manifest.Extension.ID != b.manifest.Extension.ID {
			return a.manifest.Extension.ID < b.manifest.Extension.ID
		}
		if a.subject.Kind != b.subject.Kind {
			return a.subject.Kind < b.subject.Kind
		}
		return a.subject.ID < b.subject.ID
	})
	return out
}

func applicableSuites(manifest Manifest, cfg config.Config, p profile.Profile, model *result.ModelReport, artifactBudget *int) []string {
	var out []string
	for _, suite := range manifest.Suites {
		if model == nil && suite.Subject != "provider" || model != nil && suite.Subject != "model" {
			continue
		}
		binding, bound := p.ExtensionBinding(manifest.Extension.ID, suite.ID)
		if model != nil {
			if !hasCapabilities(*model, suite.Capabilities) {
				continue
			}
			if bound && !moduleExecuted(*model, binding.Module) {
				continue
			}
		}
		if suite.Cost == "billable_artifact" {
			if !cfg.AllowBillableArtifacts || artifactBudget == nil || *artifactBudget < 1 {
				continue
			}
			*artifactBudget = *artifactBudget - 1
		}
		out = append(out, suite.ID)
	}
	sort.Strings(out)
	return out
}

func nativeArtifactReservations(p profile.Profile, model result.ModelReport) int {
	reserved := 0
	for _, assessment := range model.Modules {
		if assessment.Execution != result.ExecutionExecuted {
			continue
		}
		module, ok := p.Module(assessment.ID)
		if ok && module.CostClass == "billable-artifact" {
			reserved++
		}
	}
	return reserved
}

func hasCapabilities(model result.ModelReport, required []string) bool {
	available := map[string]bool{}
	for _, capability := range model.Capabilities {
		available[capability.ID] = true
	}
	for _, capability := range required {
		if !available[capability] {
			return false
		}
	}
	return true
}

func moduleExecuted(model result.ModelReport, moduleID string) bool {
	for _, module := range model.Modules {
		if module.ID == moduleID {
			return module.Execution == result.ExecutionExecuted
		}
	}
	return false
}

func declaredSlots(manifest Manifest, suiteIDs []string, ceiling int) int {
	slots := 1
	wanted := map[string]bool{}
	for _, id := range suiteIDs {
		wanted[id] = true
	}
	for _, suite := range manifest.Suites {
		if wanted[suite.ID] && suite.InternalConcurrency > slots {
			slots = suite.InternalConcurrency
		}
	}
	if slots > ceiling {
		return ceiling
	}
	return slots
}

func runID(parts ...any) string {
	hash := sha256.New()
	_, _ = fmt.Fprint(hash, time.Now().UnixNano())
	for _, part := range parts {
		_, _ = fmt.Fprint(hash, "\x00", part)
	}
	return hex.EncodeToString(hash.Sum(nil)[:16])
}
