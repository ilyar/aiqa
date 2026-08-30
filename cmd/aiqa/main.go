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
  OPENAI_BASE_URL   base URL including /v1
  OPENAI_API_KEY    bearer token

selection and output:
  --model ID[,ID]          test selected models; default: discover all
  --timeout 120|2m         per-request timeout (default 120s)
  --request-interval 2s    initial adaptive request interval (default 2s)
  --format markdown,json   report formats (default both)
  --name report            output basename (default report)
  --concurrency 1          models tested concurrently (default 1)

compatibility:
  --external auto|off|binary|docker
                            optional official-SDK checker integration
  --external-image IMAGE    Docker image or digest for the checker
  --fail-on core|any|never exit-code policy (default core)

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
