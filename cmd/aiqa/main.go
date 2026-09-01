package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/ilyar/aiqa/internal/app"
	"github.com/ilyar/aiqa/internal/config"
)

var (
	version = "1.0.0-alpha.0"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "aiqa:", err)
		printUsage(os.Stderr)
		os.Exit(2)
	}
	if cfg.HelpOnly {
		printUsage(os.Stdout)
		return
	}
	if cfg.VersionOnly {
		fmt.Printf("aiqa %s (commit %s, built %s)\n", version, commit, date)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	out, runErr := app.Run(ctx, cfg, app.BuildInfo{Version: version, Commit: commit, Date: date})
	for _, path := range out.Paths {
		fmt.Fprintf(os.Stderr, "aiqa: wrote %s\n", path)
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "aiqa:", runErr)
		if out.ExitCode == 0 {
			out.ExitCode = 1
		}
	}
	os.Exit(out.ExitCode)
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `usage:
  aiqa test=compat [options]
  aiqa test=model-param [options]
  aiqa test=compat,model-param [options]

environment:
  OPENAI_BASE_URL      OpenAI base URL including /v1
  OPENAI_API_KEY       OpenAI bearer token
  ANTHROPIC_BASE_URL   Anthropic prefix before /v1
  ANTHROPIC_API_KEY    Anthropic x-api-key value

targets:
  --target auto|openai|anthropic|openai,anthropic
                            select configured targets (default auto)
  --openai-model ID[,ID]    OpenAI-specific model selection
  --anthropic-model ID[,ID] Anthropic-specific model selection
  --openai-profile FILE     embedded OpenAI profile
  --anthropic-profile FILE  embedded Anthropic profile
  --report-schema auto|v1|v2
                            v1 for OpenAI-only auto; v2 for Anthropic/multi-target

selection and output:
  --model ID[,ID]          test selected models; default: discover all
  --timeout 120|2m         per-request timeout (default 120s)
  --request-interval 2s    initial adaptive request interval (default 2s)
  --format markdown,json   report formats (default both)
  --name report            output basename (default report)
  --concurrency 1          models tested concurrently (default 1)

compatibility:
  --module MODULE[,MODULE]
                            auto or explicit capability modules
  --model-capability MODEL=CAPABILITY[+CAPABILITY]
                            OpenAI-only legacy capability override
  --capability-file FILE   JSON file containing model capability overrides
  --openai-model-capability MODEL=CAPABILITY[+CAPABILITY]
  --anthropic-model-capability MODEL=CAPABILITY[+CAPABILITY]
                            target-qualified capability overrides
  --openai-capability-file FILE
  --anthropic-capability-file FILE
                            target-qualified capability override files
  --unknown-model inventory|chat
                            policy for unclassified models (default chat)
  --anthropic-unknown-model inventory|messages
                            Anthropic fallback policy (default messages)
  --allow-billable-artifacts
                            permit modules that generate billable media
  --max-artifacts-per-model N
                            artifact safety limit per model (default 1)
  --external auto|off|binary|docker
                            optional official-SDK checker integration
  --external-image IMAGE    Docker image or digest for the checker
  --openai-external MODE    OpenAI checker override
  --anthropic-external MODE Anthropic checker override
  --openai-external-image IMAGE
  --anthropic-external-image IMAGE
  --fail-on core|any|never exit-code policy (default core)
  --guidance full|summary|off
                            actionable guidance output (default full)
  --guidance-pack auto|generic|ID
                            versioned advice pack (default auto)
  --guidance-locale en      guidance language (default en)

model parameters:
  --samples 5              samples for statistical checks
  --context-max 1100k      largest context target to attempt
  --context-precision 512
                            desired boundary precision in tokens
  --context-budget 8m      prompt-token budget per model
  --context-start-bytes 1024
  --context-max-bytes 33554432

connection and diagnostics:
  --base-url URL           overrides OPENAI_BASE_URL
  --api-key KEY            overrides OPENAI_API_KEY
  --allow-http             permit plaintext outside loopback
  --verbose                print individual probe progress
  --version                print version
  --help, -h               print this help`)
}
