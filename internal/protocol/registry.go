// Package protocol contains the target runner contract shared by native
// protocol implementations.  Keeping the registration boundary here prevents
// protocol-specific clients from leaking into configuration, reporting, or
// extension planning code.
package protocol

import (
	"context"
	"fmt"
	"sort"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

// RunOptions is the protocol-neutral portion of a native target invocation.
// Protocol packages may carry a typed runtime through Runtime; the app owns
// its lifecycle and passes it back to extensions after the run.
type RunOptions struct {
	Target    config.TargetConfig
	Config    config.Config
	Profile   profile.Profile
	UserAgent string
	Logf      func(string, ...any)
	Runtime   any
}

// Runner is the adapter used by the application registry.  Concrete protocol
// packages can convert RunOptions.Runtime to their own typed runtime while
// retaining a single dispatch contract.
type Runner func(context.Context, RunOptions) (result.TargetReport, error)

// Registry maps stable protocol IDs to native runners.
type Registry struct {
	runners map[config.Protocol]Runner
}

func NewRegistry() *Registry { return &Registry{runners: make(map[config.Protocol]Runner)} }

func (r *Registry) Register(protocol config.Protocol, runner Runner) error {
	if r == nil {
		return fmt.Errorf("nil protocol registry")
	}
	if protocol == "" || runner == nil {
		return fmt.Errorf("protocol and runner are required")
	}
	if _, exists := r.runners[protocol]; exists {
		return fmt.Errorf("duplicate protocol runner %q", protocol)
	}
	r.runners[protocol] = runner
	return nil
}

func (r *Registry) Runner(protocol config.Protocol) (Runner, bool) {
	if r == nil {
		return nil, false
	}
	runner, ok := r.runners[protocol]
	return runner, ok
}

func (r *Registry) Protocols() []config.Protocol {
	if r == nil {
		return nil
	}
	protocols := make([]config.Protocol, 0, len(r.runners))
	for protocol := range r.runners {
		protocols = append(protocols, protocol)
	}
	sort.Slice(protocols, func(i, j int) bool { return protocols[i] < protocols[j] })
	return protocols
}
