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
	version = "1.0.0-alpha.6"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "aiqa: [FATAL]", err)
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
	if runErr != nil {
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
  aiqa test=sdk [options]
  aiqa test=ext-suite [options]
  aiqa test=compat,model-param [options]
  aiqa test=compat,sdk [options]
  aiqa test=compat,ext-suite [options]

environment:
  OPENAI_BASE_URL      OpenAI base URL including /v1
  OPENAI_API_KEY       OpenAI bearer token
  ANTHROPIC_BASE_URL   Anthropic prefix before /v1
  ANTHROPIC_API_KEY    Anthropic x-api-key value
  GOOGLE_GENAI_BASE_URL Google Gemini Developer API prefix before v1beta
  GOOGLE_GENAI_API_KEY Google Gemini API key (x-goog-api-key)
  GOOGLE_GENAI_PROFILE  Google profile filename (default modern Interactions)
  GOOGLE_GENAI_API_VERSION API version (default v1beta)
  GOOGLE_GEMINI_BASE_URL, GOOGLE_API_KEY, GEMINI_API_KEY are accepted aliases
  AIQA_EXT_SUITE_PATHS OS path-list of trusted extension executables

targets:
  --target auto|openai|anthropic|google-genai|...
                            select configured targets (default auto)
  --openai-profile FILE     embedded OpenAI profile
  --anthropic-profile FILE  embedded Anthropic profile
  --google-genai-profile FILE embedded Google GenAI profile
selection and output:
  --model ID[,ID]          test selected models; default: discover all
  --timeout 120|2m         per-request timeout (default 120s)
  --request-interval 2s    initial adaptive request interval (default 2s)
  --format markdown,json   report formats (default both)
  --name report            output basename (default report)
  --concurrency 1          global native and extension request ceiling (default 1)

compatibility:
  --module MODULE[,MODULE]
                            auto or explicit capability modules
  --model-capability MODEL=CAPABILITY[+CAPABILITY]
                            capability override for a single active target
  --capability-file FILE   JSON file containing model capability overrides
  --openai-model-capability MODEL=CAPABILITY[+CAPABILITY]
  --anthropic-model-capability MODEL=CAPABILITY[+CAPABILITY]
                            target-qualified capability overrides
  --openai-capability-file FILE
  --anthropic-capability-file FILE
                            target-qualified capability override files
  --google-genai-model-capability MODEL=CAPABILITY[+CAPABILITY]
  --google-genai-capability-file FILE
  --google-genai-unknown-model inventory|text
                            Google target-qualified capability controls
  --unknown-model inventory|chat
                            policy for unclassified OpenAI models (default inventory)
  --anthropic-unknown-model inventory|messages
                            Anthropic fallback policy (default inventory)
  --allow-billable-artifacts
                            permit modules that generate billable media
  --allow-persistent-resources
                            permit stateful Google resources with cleanup
  --max-artifacts-per-model N
                            artifact safety limit per model (default 1)
  --ext-suite ID[,ID]      restrict extension execution to exact IDs
  --ext-suite-path PATH    trusted absolute extension executable; repeatable
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
  --verbose[=N], -v...     diagnostics: 0 quiet; 1 WARN; 2 INFO; 3 DEBUG; 4 TRACE
                            --verbose is level 1; -v through -vvvv select levels 1 through 4
  --version                print version
  --help, -h               print this help`)
}
