package config

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const DefaultExternalImage = "ghcr.io/beranekio/openai-compatibility-tester:latest"

type Config struct {
	BaseURL           string
	APIKey            string
	Tests             []string
	Models            []string
	Timeout           time.Duration
	Formats           []string
	Name              string
	Concurrency       int
	ExternalMode      string
	ExternalImage     string
	FailOn            string
	AllowHTTP         bool
	Verbose           bool
	Samples           int
	ContextMaxTokens  int64
	ContextPrecision  int64
	ContextBudget     int64
	ContextStartBytes int
	ContextMaxBytes   int
	VersionOnly       bool
	HelpOnly          bool
}

type durationValue struct{ d *time.Duration }

func (v durationValue) String() string {
	if v.d == nil {
		return ""
	}
	return v.d.String()
}

func (v durationValue) Set(s string) error {
	if s == "" {
		return errors.New("timeout must not be empty")
	}
	if n, err := strconv.Atoi(s); err == nil {
		if n <= 0 {
			return errors.New("timeout must be positive")
		}
		*v.d = time.Duration(n) * time.Second
		return nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("parse timeout %q: use seconds (120) or a Go duration (2m): %w", s, err)
	}
	if d <= 0 {
		return errors.New("timeout must be positive")
	}
	*v.d = d
	return nil
}

type int64Value struct{ value *int64 }

func (v int64Value) String() string {
	if v.value == nil {
		return ""
	}
	return strconv.FormatInt(*v.value, 10)
}

func (v int64Value) Set(s string) error {
	n, err := parseHumanInt(s)
	if err != nil {
		return err
	}
	if n <= 0 {
		return errors.New("value must be positive")
	}
	*v.value = n
	return nil
}

func parseHumanInt(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, errors.New("empty number")
	}
	mult := int64(1)
	switch s[len(s)-1] {
	case 'k':
		mult = 1_000
		s = strings.TrimSpace(s[:len(s)-1])
	case 'm':
		mult = 1_000_000
		s = strings.TrimSpace(s[:len(s)-1])
	case 'g':
		mult = 1_000_000_000
		s = strings.TrimSpace(s[:len(s)-1])
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse number %q: %w", s, err)
	}
	return n * mult, nil
}

func externalImageFromEnv() string {
	if image := strings.TrimSpace(os.Getenv("AIQA_EXTERNAL_IMAGE")); image != "" {
		return image
	}
	return DefaultExternalImage
}

func Parse(args []string) (Config, error) {
	cfg := Config{
		BaseURL:           strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")),
		APIKey:            os.Getenv("OPENAI_API_KEY"),
		Tests:             []string{"compat"},
		Timeout:           120 * time.Second,
		Formats:           []string{"markdown", "json"},
		Name:              "report",
		Concurrency:       1,
		ExternalMode:      "auto",
		ExternalImage:     externalImageFromEnv(),
		FailOn:            "core",
		Samples:           5,
		ContextMaxTokens:  1_100_000,
		ContextPrecision:  512,
		ContextBudget:     8_000_000,
		ContextStartBytes: 8 * 1024,
		ContextMaxBytes:   32 * 1024 * 1024,
	}

	args = normalizeTestArgument(args)
	fs := flag.NewFlagSet("aiqa", flag.ContinueOnError)
	fs.SetOutput(new(strings.Builder))

	var tests, models, formats string
	tests = strings.Join(cfg.Tests, ",")
	formats = strings.Join(cfg.Formats, ",")
	fs.StringVar(&tests, "test", tests, "comma-separated suites: compat,model-param")
	fs.StringVar(&models, "model", "", "one or more comma-separated model IDs; default: discover via /models")
	fs.Var(durationValue{&cfg.Timeout}, "timeout", "per-request timeout, e.g. 120 or 2m")
	fs.StringVar(&formats, "format", formats, "comma-separated report formats: markdown,json")
	fs.StringVar(&cfg.Name, "name", cfg.Name, "output basename without extension")
	fs.IntVar(&cfg.Concurrency, "concurrency", cfg.Concurrency, "number of models tested concurrently")
	fs.StringVar(&cfg.ExternalMode, "external", cfg.ExternalMode, "external checker: auto,off,binary,docker")
	fs.StringVar(&cfg.ExternalImage, "external-image", cfg.ExternalImage, "container image for --external=docker")
	fs.StringVar(&cfg.FailOn, "fail-on", cfg.FailOn, "exit policy: core,any,never")
	fs.BoolVar(&cfg.AllowHTTP, "allow-http", false, "allow non-TLS endpoints outside loopback")
	fs.BoolVar(&cfg.Verbose, "verbose", false, "show probe progress on stderr")
	fs.IntVar(&cfg.Samples, "samples", cfg.Samples, "samples used for semantic parameter checks")
	fs.Var(int64Value{&cfg.ContextMaxTokens}, "context-max", "maximum context target to probe, e.g. 1100k")
	fs.Var(int64Value{&cfg.ContextPrecision}, "context-precision", "desired context boundary precision in tokens")
	fs.Var(int64Value{&cfg.ContextBudget}, "context-budget", "maximum prompt-token budget per model for context probing")
	fs.IntVar(&cfg.ContextStartBytes, "context-start-bytes", cfg.ContextStartBytes, "initial context payload size")
	fs.IntVar(&cfg.ContextMaxBytes, "context-max-bytes", cfg.ContextMaxBytes, "hard request-payload ceiling")
	fs.BoolVar(&cfg.VersionOnly, "version", false, "print version and exit")
	fs.BoolVar(&cfg.HelpOnly, "help", false, "print help and exit")
	fs.BoolVar(&cfg.HelpOnly, "h", false, "print help and exit")
	fs.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, "OpenAI-compatible base URL; defaults to OPENAI_BASE_URL")
	fs.StringVar(&cfg.APIKey, "api-key", cfg.APIKey, "API key; defaults to OPENAI_API_KEY")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if len(fs.Args()) > 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	cfg.Tests = splitUnique(tests)
	cfg.Models = splitUnique(models)
	cfg.Formats = normalizeFormats(splitUnique(formats))
	cfg.ExternalMode = strings.ToLower(strings.TrimSpace(cfg.ExternalMode))
	cfg.ExternalImage = strings.TrimSpace(cfg.ExternalImage)
	cfg.FailOn = strings.ToLower(strings.TrimSpace(cfg.FailOn))
	cfg.Name = strings.TrimSpace(cfg.Name)

	if cfg.VersionOnly || cfg.HelpOnly {
		return cfg, nil
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func normalizeTestArgument(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.HasPrefix(arg, "test=") {
			out = append(out, "--test="+strings.TrimPrefix(arg, "test="))
			continue
		}
		out = append(out, arg)
	}
	return out
}

func splitUnique(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func normalizeFormats(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range in {
		f = strings.ToLower(strings.TrimSpace(f))
		if f == "md" {
			f = "markdown"
		}
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

func (c Config) Validate() error {
	if c.BaseURL == "" {
		return errors.New("OPENAI_BASE_URL or --base-url is required")
	}
	if c.APIKey == "" {
		return errors.New("OPENAI_API_KEY or --api-key is required")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid base URL %q", c.BaseURL)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return errors.New("base URL must not contain query parameters or a fragment")
	}
	if u.Scheme != "https" && !c.AllowHTTP && !isLoopbackHost(u.Hostname()) {
		return errors.New("refusing non-HTTPS base URL outside loopback; use --allow-http to override")
	}
	if len(c.Tests) == 0 {
		return errors.New("at least one test suite is required")
	}
	for _, test := range c.Tests {
		if test != "compat" && test != "model-param" {
			return fmt.Errorf("unknown test suite %q; supported: compat,model-param", test)
		}
	}
	if len(c.Formats) == 0 {
		return errors.New("at least one report format is required")
	}
	for _, format := range c.Formats {
		if format != "markdown" && format != "json" {
			return fmt.Errorf("unknown format %q; supported: markdown,json", format)
		}
	}
	if c.Name == "" {
		return errors.New("--name must not be empty")
	}
	if filepath.Base(c.Name) != c.Name {
		return errors.New("--name must be a basename, not a path")
	}
	if c.Concurrency < 1 || c.Concurrency > 32 {
		return errors.New("--concurrency must be between 1 and 32")
	}
	if c.Samples < 2 || c.Samples > 30 {
		return errors.New("--samples must be between 2 and 30")
	}
	if c.ContextStartBytes < 1024 || c.ContextMaxBytes < c.ContextStartBytes {
		return errors.New("invalid context byte limits")
	}
	if c.ContextPrecision > c.ContextMaxTokens {
		return errors.New("--context-precision must not exceed --context-max")
	}
	switch c.ExternalMode {
	case "auto", "off", "binary", "docker":
	default:
		return fmt.Errorf("unknown --external mode %q", c.ExternalMode)
	}
	if c.ExternalImage == "" {
		return errors.New("--external-image must not be empty")
	}
	switch c.FailOn {
	case "core", "any", "never":
	default:
		return fmt.Errorf("unknown --fail-on policy %q", c.FailOn)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(host)
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func (c Config) HasTest(name string) bool {
	for _, test := range c.Tests {
		if test == name {
			return true
		}
	}
	return false
}
