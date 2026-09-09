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
	version = "1.0.0-alpha.10"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	// Keep signal ownership at the CLI boundary.  The application receives a
	// cancellable context and remains responsible for writing a partial report.
	// A second signal is intentionally handled by runCLI as the operator's
	// request to stop waiting for that bounded cleanup.
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	code := runCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, signals, app.Run)
	signal.Stop(signals)
	os.Exit(code)
}

type applicationRunner func(context.Context, config.Config, app.BuildInfo) (app.RunResult, error)

type applicationResult struct {
	out app.RunResult
	err error
}

// runCLI is deliberately separate from main so signal behaviour is testable
// without delivering process signals to the Go test binary.
func runCLI(args []string, stdin io.Reader, stdout, stderr io.Writer, signals <-chan os.Signal, runner applicationRunner) int {
	if len(args) > 0 && args[0] == "log" {
		if err := runLog(args[1:], stdin, stdout, stderr, "."); err != nil {
			fmt.Fprintln(stderr, "aiqa log:", err)
			return 2
		}
		return 0
	}
	cfg, err := config.Parse(args)
	if err != nil {
		fmt.Fprintln(stderr, "aiqa: [FATAL]", err)
		printUsage(stderr)
		return 2
	}
	if cfg.HelpOnly {
		printUsage(stdout)
		return 0
	}
	if cfg.VersionOnly {
		fmt.Fprintf(stdout, "aiqa %s (commit %s, built %s)\n", version, commit, date)
		return 0
	}
	out, runErr, exitCode, interrupted := runApplication(context.Background(), cfg, signals, runner)
	if len(out.Stdout) > 0 {
		if _, err := stdout.Write(out.Stdout); err != nil {
			fmt.Fprintln(stderr, "aiqa: [FATAL] write stdout:", err)
			return 2
		}
	}
	if runErr != nil && !interrupted {
		fmt.Fprintln(stderr, "aiqa: [FATAL]", runErr)
		// An error returned by app.Run is an operational failure, never a
		// compatibility verdict.  In particular --fail-on=never must not turn
		// an aborted or failed finalization into success.
		out.ExitCode = 2
	}
	if interrupted {
		return exitCode
	}
	return out.ExitCode
}

// runApplication maps the first cancellation signal to the documented CLI
// exit status while still allowing app.Run to finish its cancellation-aware
// finalization.  A second signal stops that wait; main then exits, which is
// the only portable hard termination available to this process.
func runApplication(parent context.Context, cfg config.Config, signals <-chan os.Signal, runner applicationRunner) (app.RunResult, error, int, bool) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	done := make(chan applicationResult, 1)
	go func() {
		out, err := runner(ctx, cfg, app.BuildInfo{Version: version, Commit: commit, Date: date})
		done <- applicationResult{out: out, err: err}
	}()
	if signals == nil {
		result := <-done
		return result.out, result.err, 0, false
	}
	select {
	case result := <-done:
		return result.out, result.err, 0, false
	case first := <-signals:
		code := signalExitCode(first)
		cancel()
		select {
		case result := <-done:
			return result.out, result.err, code, true
		case second := <-signals:
			return app.RunResult{}, nil, signalExitCode(second), true
		}
	}
}

func signalExitCode(sig os.Signal) int {
	if sig == syscall.SIGTERM {
		return 143
	}
	return 130
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `usage:
	  aiqa log [FILE|-] [log options]
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
  GOOGLE_GENAI_PROFILE  Google profile filename (default GenerateContent)
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
  --timeout 120|2m         per HTTP-attempt timeout after pacing (default 120s)
  --request-interval 2s    initial adaptive request interval (default 2s)
  --format markdown,json   report formats (default both)
  --name report            output basename (default report)
  --concurrency 1          global native and extension request ceiling (default 1)
  --target-concurrency auto|N
                            concurrent target workflows; auto activates selected targets
  --pacing-group TARGET=ALIAS
                            share a safe provider-quota alias between targets; repeatable
  --request-memory-budget 128MiB
                            run-owned request buffer admission budget
  --queue-timeout DURATION  bound scheduler queue wait (0 disables)
  --run-timeout DURATION    bound the whole run (0 disables)
  --progress auto|tty|plain|off
                            stderr progress rendering mode (default auto)
  --no-progress             suppress progress rendering
  --color auto|always|never terminal color policy (default auto)
  --unicode auto|always|never terminal glyph policy (default auto)
  --stdout json|markdown    additionally write exactly one final report to stdout

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
                            levels 1-4 write a private JSONL sidecar; progress is always shown on stderr
                            level 4 writes private trace/verdict diagnostics; they do not change compatibility verdict or exit policy
  --version                print version
  --help, -h               print this help`)
}
