package config

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Protocol string

const (
	ProtocolOpenAI      Protocol = "openai"
	ProtocolAnthropic   Protocol = "anthropic"
	ProtocolGoogleGenAI Protocol = "google-genai"
)

type TargetConfig struct {
	ID                string
	Protocol          Protocol
	BaseURL           string
	BaseURLNormalized bool
	APIKey            string
	Models            []string
	ProfileFile       string
	Modules           []string
	ModelCapabilities []string
	CapabilityFile    string
	UnknownModel      string
	RequestInterval   time.Duration
	APIVersion        string
}

type Config struct {
	Targets                      []TargetConfig
	BaseURL                      string
	APIKey                       string
	Tests                        []string
	Models                       []string
	Timeout                      time.Duration
	RequestInterval              time.Duration
	Formats                      []string
	Name                         string
	Concurrency                  int
	ExtSuites                    []string
	ExtSuitePaths                []string
	Modules                      []string
	ModelCapabilities            []string
	CapabilityFile               string
	UnknownModel                 string
	AllowBillableArtifacts       bool
	MaxArtifactsPerModel         int
	FailOn                       string
	Guidance                     string
	GuidancePack                 string
	GuidanceLocale               string
	AllowHTTP                    bool
	OTelPropagationTo            []string
	Verbosity                    int
	Samples                      int
	ContextMaxTokens             int64
	ContextPrecision             int64
	ContextBudget                int64
	ContextStartBytes            int
	ContextMaxBytes              int
	VersionOnly                  bool
	HelpOnly                     bool
	TargetSelection              string
	OpenAIModelCapabilities      []string
	AnthropicModelCapabilities   []string
	OpenAICapabilityFile         string
	AnthropicCapabilityFile      string
	AnthropicUnknownModel        string
	OpenAIProfile                string
	AnthropicProfile             string
	AllowHighTokenProbes         bool
	AllowPersistentResources     bool
	GoogleGenAIBaseURL           string
	GoogleGenAIAPIKey            string
	GoogleGenAIAPIVersion        string
	GoogleGenAIProfile           string
	GoogleGenAIModelCapabilities []string
	GoogleGenAICapabilityFile    string
	GoogleGenAIUnknownModel      string
	googleGenAIBaseURLFlag       bool
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

func Parse(args []string) (Config, error) {
	openAIBaseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	openAIAPIKey := os.Getenv("OPENAI_API_KEY")
	anthropicBaseURL := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL"))
	anthropicAPIKey := os.Getenv("ANTHROPIC_API_KEY")
	googleGenAIBaseURL := strings.TrimSpace(os.Getenv("GOOGLE_GENAI_BASE_URL"))
	if googleGenAIBaseURL == "" {
		googleGenAIBaseURL = strings.TrimSpace(os.Getenv("GOOGLE_GEMINI_BASE_URL"))
	}
	googleGenAIAPIKey := strings.TrimSpace(os.Getenv("GOOGLE_GENAI_API_KEY"))
	googleGenAIAPIVersion := strings.TrimSpace(os.Getenv("GOOGLE_GENAI_API_VERSION"))
	googleGenAIProfile := strings.TrimSpace(os.Getenv("GOOGLE_GENAI_PROFILE"))
	if googleGenAIAPIVersion == "" {
		googleGenAIAPIVersion = "v1beta"
	}
	if googleGenAIProfile == "" {
		// The public Gemini Developer API currently exposes GenerateContent;
		// Interactions remains an opt-in contract for compatible gateways. Do
		// not make a public Google target fail because it lacks that surface.
		googleGenAIProfile = "google-genai-generate-content-compat-2026-09.json"
	}
	googleGenAIModelEnv := strings.TrimSpace(os.Getenv("GOOGLE_GENAI_MODEL"))
	cfg := Config{
		BaseURL:                 openAIBaseURL,
		APIKey:                  openAIAPIKey,
		Tests:                   []string{"compat"},
		Timeout:                 120 * time.Second,
		RequestInterval:         2 * time.Second,
		Formats:                 []string{"markdown", "json"},
		Name:                    "report",
		Concurrency:             1,
		ExtSuitePaths:           splitPathList(os.Getenv("AIQA_EXT_SUITE_PATHS")),
		Modules:                 []string{"auto"},
		UnknownModel:            "inventory",
		AnthropicUnknownModel:   "inventory",
		MaxArtifactsPerModel:    1,
		FailOn:                  "core",
		Guidance:                "full",
		GuidancePack:            "auto",
		GuidanceLocale:          "en",
		Samples:                 5,
		ContextMaxTokens:        1_100_000,
		ContextPrecision:        512,
		ContextBudget:           8_000_000,
		ContextStartBytes:       1024,
		ContextMaxBytes:         32 * 1024 * 1024,
		TargetSelection:         "auto",
		OpenAIProfile:           "openai-compat-2026-09.json",
		AnthropicProfile:        "anthropic-compat-2026-09.json",
		GoogleGenAIBaseURL:      googleGenAIBaseURL,
		GoogleGenAIAPIKey:       googleGenAIAPIKey,
		GoogleGenAIAPIVersion:   googleGenAIAPIVersion,
		GoogleGenAIProfile:      googleGenAIProfile,
		GoogleGenAIUnknownModel: "inventory",
	}

	var normalizeErr error
	args, normalizeErr = normalizeArguments(args)
	if normalizeErr != nil {
		return Config{}, normalizeErr
	}
	if removed := removedModelFlag(args); removed != "" {
		return Config{}, fmt.Errorf("--%s was removed; use the global --model option", removed)
	}
	modelFlagProvided := flagProvided(args, "model")
	fs := flag.NewFlagSet("aiqa", flag.ContinueOnError)
	fs.SetOutput(new(strings.Builder))

	var tests, models, formats, modules, modelCapabilities, otelPropagationTo string
	var openAIModelCapabilities, anthropicModelCapabilities, googleGenAIModelCapabilities string
	extSuitePaths := multiValue{values: append([]string(nil), cfg.ExtSuitePaths...)}
	extSuiteValues := multiValue{}
	tests = strings.Join(cfg.Tests, ",")
	formats = strings.Join(cfg.Formats, ",")
	modules = strings.Join(cfg.Modules, ",")
	fs.StringVar(&tests, "test", tests, "comma-separated suites: compat,model-param,sdk,ext-suite")
	fs.StringVar(&models, "model", "", "one or more comma-separated model IDs; default: discover via /models")
	fs.StringVar(&cfg.TargetSelection, "target", cfg.TargetSelection, "targets: auto,openai,anthropic,google-genai or comma-separated")
	fs.StringVar(&cfg.OpenAIProfile, "openai-profile", cfg.OpenAIProfile, "embedded OpenAI profile filename")
	fs.StringVar(&cfg.AnthropicProfile, "anthropic-profile", cfg.AnthropicProfile, "embedded Anthropic profile filename")
	fs.StringVar(&cfg.GoogleGenAIProfile, "google-genai-profile", cfg.GoogleGenAIProfile, "embedded Google GenAI profile filename")
	fs.StringVar(&cfg.GoogleGenAIBaseURL, "google-genai-base-url", cfg.GoogleGenAIBaseURL, "Google GenAI base URL; defaults to GOOGLE_GENAI_BASE_URL")
	fs.StringVar(&cfg.GoogleGenAIAPIKey, "google-genai-api-key", cfg.GoogleGenAIAPIKey, "Google GenAI API key; defaults to GOOGLE_GENAI_API_KEY")
	fs.StringVar(&cfg.GoogleGenAIAPIVersion, "google-genai-api-version", cfg.GoogleGenAIAPIVersion, "Google GenAI API version (default v1beta)")
	fs.Var(durationValue{&cfg.Timeout}, "timeout", "per HTTP-attempt timeout after pacing, e.g. 120 or 2m")
	fs.Var(durationValue{&cfg.RequestInterval}, "request-interval", "initial adaptive request interval, e.g. 2s")
	fs.StringVar(&formats, "format", formats, "comma-separated report formats: markdown,json")
	fs.StringVar(&cfg.Name, "name", cfg.Name, "output basename without extension")
	fs.IntVar(&cfg.Concurrency, "concurrency", cfg.Concurrency, "global ceiling for concurrent native and extension probe work")
	fs.Var(&extSuiteValues, "ext-suite", "extension IDs to run; repeatable or comma-separated; official-sdk selects configured SDKs; default: all applicable extensions")
	fs.Var(&extSuitePaths, "ext-suite-path", "absolute extension executable path; may be repeated")
	fs.BoolVar(&cfg.AllowHighTokenProbes, "allow-high-token-probes", false, "allow opt-in high-token probes")
	fs.BoolVar(&cfg.AllowPersistentResources, "allow-persistent-resources", false, "allow stateful probes that create retained provider resources")
	fs.StringVar(&modules, "module", modules, "capability modules: auto, chat-completions, images-generations, or anthropic.*")
	fs.StringVar(&modelCapabilities, "model-capability", "", "model capability overrides: model=capability[+capability]")
	fs.StringVar(&cfg.CapabilityFile, "capability-file", "", "JSON file with model capability overrides")
	fs.StringVar(&cfg.UnknownModel, "unknown-model", cfg.UnknownModel, "unknown model policy: inventory or chat")
	fs.StringVar(&openAIModelCapabilities, "openai-model-capability", "", "OpenAI model capability overrides")
	fs.StringVar(&anthropicModelCapabilities, "anthropic-model-capability", "", "Anthropic model capability overrides")
	fs.StringVar(&googleGenAIModelCapabilities, "google-genai-model-capability", "", "Google GenAI model capability overrides")
	fs.StringVar(&cfg.OpenAICapabilityFile, "openai-capability-file", "", "OpenAI capability override file")
	fs.StringVar(&cfg.AnthropicCapabilityFile, "anthropic-capability-file", "", "Anthropic capability override file")
	fs.StringVar(&cfg.GoogleGenAICapabilityFile, "google-genai-capability-file", "", "Google GenAI capability override file")
	fs.StringVar(&cfg.AnthropicUnknownModel, "anthropic-unknown-model", cfg.AnthropicUnknownModel, "unknown Anthropic model policy: messages or inventory")
	fs.StringVar(&cfg.GoogleGenAIUnknownModel, "google-genai-unknown-model", cfg.GoogleGenAIUnknownModel, "unknown Google GenAI model policy: inventory or text")
	fs.BoolVar(&cfg.AllowBillableArtifacts, "allow-billable-artifacts", false, "allow modules that create billable media")
	fs.IntVar(&cfg.MaxArtifactsPerModel, "max-artifacts-per-model", cfg.MaxArtifactsPerModel, "maximum billable artifacts generated per model")
	fs.StringVar(&cfg.FailOn, "fail-on", cfg.FailOn, "exit policy: core,any,never")
	fs.StringVar(&cfg.Guidance, "guidance", cfg.Guidance, "guidance output: full,summary,off")
	fs.StringVar(&cfg.GuidancePack, "guidance-pack", cfg.GuidancePack, "guidance advice pack: auto,generic,PACK_ID")
	fs.StringVar(&cfg.GuidanceLocale, "guidance-locale", cfg.GuidanceLocale, "guidance locale: en")
	fs.BoolVar(&cfg.AllowHTTP, "allow-http", false, "allow non-TLS endpoints outside loopback")
	fs.StringVar(&otelPropagationTo, "otel-propagate-to", "", "comma-separated exact origins allowed to receive OTel trace context")
	fs.IntVar(&cfg.Verbosity, "verbose", 0, "log verbosity: 0=errors, 1=warnings, 2=info, 3=debug, 4=trace")
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
	var modelErr error
	cfg.Models, modelErr = parseModelList(models)
	if modelErr != nil {
		return Config{}, modelErr
	}
	if modelFlagProvided && len(cfg.Models) == 0 {
		return Config{}, errors.New("--model contains an empty model ID at position 1")
	}
	cfg.Formats = normalizeFormats(splitUnique(formats))
	cfg.Modules = splitUnique(modules)
	cfg.ExtSuites = splitUnique(strings.Join(extSuiteValues.values, ","))
	if duplicate := duplicateCleanPath(extSuitePaths.values); duplicate != "" {
		return Config{}, fmt.Errorf("extension path %q is configured more than once", duplicate)
	}
	cfg.ExtSuitePaths = uniqueStrings(extSuitePaths.values)
	cfg.ModelCapabilities = splitUnique(modelCapabilities)
	cfg.OTelPropagationTo = splitUnique(otelPropagationTo)
	cfg.OpenAIModelCapabilities = splitUnique(openAIModelCapabilities)
	cfg.AnthropicModelCapabilities = splitUnique(anthropicModelCapabilities)
	cfg.GoogleGenAIModelCapabilities = splitUnique(googleGenAIModelCapabilities)
	cfg.FailOn = strings.ToLower(strings.TrimSpace(cfg.FailOn))
	cfg.Guidance = strings.ToLower(strings.TrimSpace(cfg.Guidance))
	cfg.GuidancePack = strings.ToLower(strings.TrimSpace(cfg.GuidancePack))
	cfg.GuidanceLocale = strings.ToLower(strings.TrimSpace(cfg.GuidanceLocale))
	cfg.UnknownModel = strings.ToLower(strings.TrimSpace(cfg.UnknownModel))
	cfg.AnthropicUnknownModel = strings.ToLower(strings.TrimSpace(cfg.AnthropicUnknownModel))
	cfg.CapabilityFile = strings.TrimSpace(cfg.CapabilityFile)
	cfg.OpenAICapabilityFile = strings.TrimSpace(cfg.OpenAICapabilityFile)
	cfg.AnthropicCapabilityFile = strings.TrimSpace(cfg.AnthropicCapabilityFile)
	cfg.GoogleGenAICapabilityFile = strings.TrimSpace(cfg.GoogleGenAICapabilityFile)
	cfg.Name = strings.TrimSpace(cfg.Name)
	cfg.TargetSelection = strings.ToLower(strings.TrimSpace(cfg.TargetSelection))
	cfg.OpenAIProfile = strings.TrimSpace(cfg.OpenAIProfile)
	cfg.AnthropicProfile = strings.TrimSpace(cfg.AnthropicProfile)
	cfg.GoogleGenAIBaseURL = strings.TrimSpace(cfg.GoogleGenAIBaseURL)
	cfg.GoogleGenAIAPIKey = strings.TrimSpace(cfg.GoogleGenAIAPIKey)
	cfg.GoogleGenAIAPIVersion = strings.TrimSpace(cfg.GoogleGenAIAPIVersion)
	cfg.GoogleGenAIProfile = strings.TrimSpace(cfg.GoogleGenAIProfile)
	cfg.GoogleGenAIUnknownModel = strings.ToLower(strings.TrimSpace(cfg.GoogleGenAIUnknownModel))
	for _, arg := range args {
		if arg == "--google-genai-base-url" || strings.HasPrefix(arg, "--google-genai-base-url=") {
			cfg.googleGenAIBaseURLFlag = true
			break
		}
	}

	if err := cfg.configureTargets(openAIBaseURL, openAIAPIKey, anthropicBaseURL, anthropicAPIKey); err != nil {
		return Config{}, err
	}
	if googleGenAIModelEnv != "" && hasProtocolTarget(cfg.Targets, ProtocolGoogleGenAI) && !cfg.HelpOnly && !cfg.VersionOnly {
		return Config{}, errors.New("GOOGLE_GENAI_MODEL was removed; use the global --model option")
	}

	if cfg.VersionOnly || cfg.HelpOnly {
		return cfg, nil
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

type multiValue struct{ values []string }

func (v *multiValue) String() string { return strings.Join(v.values, string(os.PathListSeparator)) }
func (v *multiValue) Set(value string) error {
	v.values = append(v.values, strings.TrimSpace(value))
	return nil
}

func splitPathList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return uniqueStrings(filepath.SplitList(value))
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func duplicateCleanPath(values []string) string {
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		cleaned := filepath.Clean(value)
		if seen[cleaned] {
			return value
		}
		seen[cleaned] = true
	}
	return ""
}

func validStableID(value string) bool {
	if value == "" {
		return false
	}
	for index, r := range value {
		letterOrDigit := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if !letterOrDigit && r != '.' && r != '-' {
			return false
		}
		if (index == 0 || index == len(value)-1) && !letterOrDigit {
			return false
		}
	}
	return true
}

func normalizeArguments(args []string) ([]string, error) {
	out := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if strings.HasPrefix(arg, "test=") {
			out = append(out, "--test="+strings.TrimPrefix(arg, "test="))
			continue
		}
		if arg == "--verbose" {
			if index+1 < len(args) && isVerbosityValue(args[index+1]) {
				out = append(out, "--verbose="+args[index+1])
				index++
			} else {
				out = append(out, "--verbose=1")
			}
			continue
		}
		if strings.HasPrefix(arg, "-v") && !strings.HasPrefix(arg, "--") {
			short := strings.TrimPrefix(arg, "-")
			if short == "" || strings.Trim(short, "v") != "" {
				return nil, fmt.Errorf("invalid short verbose form %q; use -v through -vvvv", arg)
			}
			level := len(arg) - 1
			if level < 1 || level > 4 {
				return nil, fmt.Errorf("invalid short verbose form %q; use -v through -vvvv", arg)
			}
			out = append(out, "--verbose="+strconv.Itoa(level))
			continue
		}
		out = append(out, arg)
	}
	return out, nil
}

func isVerbosityValue(value string) bool {
	_, err := strconv.Atoi(value)
	return err == nil
}

func removedModelFlag(args []string) string {
	for _, arg := range args {
		for _, name := range []string{"openai-model", "anthropic-model", "google-genai-model"} {
			if arg == "--"+name || strings.HasPrefix(arg, "--"+name+"=") {
				return name
			}
		}
	}
	return ""
}

func flagProvided(args []string, name string) bool {
	prefix := "--" + name
	for _, arg := range args {
		if arg == prefix || strings.HasPrefix(arg, prefix+"=") {
			return true
		}
	}
	return false
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

func parseModelList(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	seen := map[string]bool{}
	models := make([]string, 0)
	for index, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			return nil, fmt.Errorf("--model contains an empty model ID at position %d", index+1)
		}
		if seen[item] {
			continue
		}
		seen[item] = true
		models = append(models, item)
	}
	return models, nil
}

func hasProtocolTarget(targets []TargetConfig, protocol Protocol) bool {
	for _, target := range targets {
		if target.Protocol == protocol {
			return true
		}
	}
	return false
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
	if c.Verbosity < 0 || c.Verbosity > 4 {
		return fmt.Errorf("--verbose must be between 0 and 4, got %d", c.Verbosity)
	}
	if len(c.Targets) == 0 {
		return errors.New("no API target is configured; set an OPENAI, ANTHROPIC, or GOOGLE_GENAI base URL and API key")
	}
	for _, target := range c.Targets {
		u, err := url.Parse(target.BaseURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("invalid %s base URL %q", target.Protocol, target.BaseURL)
		}
		if u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("%s base URL must not contain query parameters or a fragment", target.Protocol)
		}
		if u.User != nil {
			return fmt.Errorf("%s base URL must not contain user information or credentials", target.Protocol)
		}
		if u.Scheme != "https" && !c.AllowHTTP && !isLoopbackHost(u.Hostname()) {
			return fmt.Errorf("refusing non-HTTPS %s base URL outside loopback; use --allow-http to override", target.Protocol)
		}
		if target.Protocol == ProtocolGoogleGenAI && !validGoogleAPIVersion(target.APIVersion) {
			return fmt.Errorf("invalid Google GenAI API version %q", target.APIVersion)
		}
	}
	for _, raw := range c.OTelPropagationTo {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
			return fmt.Errorf("--otel-propagate-to requires an exact origin, got %q", raw)
		}
		origin := u.Scheme + "://" + u.Host
		allowed := false
		for _, target := range c.Targets {
			targetURL, _ := url.Parse(target.BaseURL)
			if targetURL.Scheme+"://"+targetURL.Host == origin {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("--otel-propagate-to origin %q is not a configured target", origin)
		}
	}
	if len(c.Tests) == 0 {
		return errors.New("at least one test suite is required")
	}
	for _, test := range c.Tests {
		if test != "compat" && test != "model-param" && test != "sdk" && test != "ext-suite" {
			return fmt.Errorf("unknown test suite %q; supported: compat,model-param,sdk,ext-suite", test)
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
	if c.ContextStartBytes < 256 || c.ContextMaxBytes < c.ContextStartBytes {
		return errors.New("invalid context byte limits")
	}
	if c.ContextPrecision > c.ContextMaxTokens {
		return errors.New("--context-precision must not exceed --context-max")
	}
	for _, path := range c.ExtSuitePaths {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("--ext-suite-path must be absolute: %q", path)
		}
	}
	for _, id := range c.ExtSuites {
		if !validStableID(id) {
			return fmt.Errorf("invalid --ext-suite id %q", id)
		}
	}
	if len(c.ExtSuites) > 0 && !c.HasTest("ext-suite") {
		return errors.New("--ext-suite requires test=ext-suite; use test=sdk for built-in SDK checks")
	}
	if len(c.Modules) == 0 {
		return errors.New("at least one --module is required")
	}
	for _, module := range c.Modules {
		switch module {
		case "auto", "chat-completions", "images-generations", "anthropic.discovery", "anthropic.messages", "anthropic.tools", "anthropic.token-counting", "anthropic.structured-output", "anthropic.vision", "anthropic.thinking",
			"google-genai.discovery", "google-genai.interactions", "google-genai.interactions-stream", "google-genai.interactions-tools", "google-genai.interactions-structured-output", "google-genai.interactions-vision", "google-genai.interactions-thinking", "google-genai.interactions-stateful", "google-genai.generate-content", "google-genai.generate-content-stream", "google-genai.generate-content-tools", "google-genai.generate-content-structured-output", "google-genai.token-counting", "google-genai.embeddings", "google-genai.vision", "google-genai.thinking":
		default:
			return fmt.Errorf("unknown module %q", module)
		}
	}
	if contains(c.Modules, "anthropic.thinking") && !c.AllowHighTokenProbes {
		return errors.New("--module=anthropic.thinking requires --allow-high-token-probes")
	}
	if c.UnknownModel != "inventory" && c.UnknownModel != "chat" {
		return errors.New("--unknown-model must be inventory or chat")
	}
	if c.AnthropicUnknownModel != "inventory" && c.AnthropicUnknownModel != "messages" {
		return errors.New("--anthropic-unknown-model must be inventory or messages")
	}
	if c.GoogleGenAIUnknownModel != "inventory" && c.GoogleGenAIUnknownModel != "text" {
		return errors.New("--google-genai-unknown-model must be inventory or text")
	}
	if c.MaxArtifactsPerModel < 1 || c.MaxArtifactsPerModel > 10 {
		return errors.New("--max-artifacts-per-model must be between 1 and 10")
	}
	for _, override := range append(append(append(append([]string(nil), c.ModelCapabilities...), c.OpenAIModelCapabilities...), c.AnthropicModelCapabilities...), c.GoogleGenAIModelCapabilities...) {
		parts := strings.SplitN(override, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return fmt.Errorf("invalid --model-capability %q", override)
		}
	}
	switch c.FailOn {
	case "core", "any", "never":
	default:
		return fmt.Errorf("unknown --fail-on policy %q", c.FailOn)
	}
	switch c.Guidance {
	case "full", "summary", "off":
	default:
		return fmt.Errorf("unknown --guidance mode %q", c.Guidance)
	}
	if c.GuidancePack == "" {
		return errors.New("--guidance-pack must not be empty")
	}
	if c.GuidanceLocale != "en" {
		return errors.New("--guidance-locale currently supports en")
	}
	return nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (c *Config) configureTargets(openAIBaseURL, openAIAPIKey, anthropicBaseURL, anthropicAPIKey string) error {
	// The legacy flags are OpenAI aliases and override their environment pair.
	openAIBaseURL, openAIAPIKey = strings.TrimSpace(c.BaseURL), c.APIKey
	requested := splitUnique(c.TargetSelection)
	if c.TargetSelection == "auto" {
		requested = nil
	}
	wants := func(protocol string) bool {
		if len(requested) == 0 {
			return true
		}
		for _, item := range requested {
			if item == protocol {
				return true
			}
		}
		return false
	}
	for _, item := range requested {
		if item != string(ProtocolOpenAI) && item != string(ProtocolAnthropic) && item != string(ProtocolGoogleGenAI) {
			return fmt.Errorf("unknown target %q; supported: openai,anthropic,google-genai", item)
		}
	}
	googleBaseURL := strings.TrimSpace(c.GoogleGenAIBaseURL)
	googleAPIKey := c.GoogleGenAIAPIKey
	// Generic Google aliases are intentionally consulted only after the Google
	// target is active (a base URL or an explicit target selection). In auto
	// mode, a key alone must never activate Google or make unrelated alias
	// conflicts fatal. The canonical GOOGLE_GENAI_API_KEY wins over both
	// aliases when it is present.
	explicitGoogle := len(requested) > 0 && wants(string(ProtocolGoogleGenAI))
	if googleAPIKey == "" && (googleBaseURL != "" || explicitGoogle) {
		var err error
		googleAPIKey, err = googleAliasKey()
		if err != nil {
			return err
		}
	}
	if openAIBaseURL != "" && openAIAPIKey == "" && wants("openai") {
		return errors.New("OPENAI_API_KEY or --api-key is required when the OpenAI base URL is configured")
	}
	if anthropicBaseURL != "" && anthropicAPIKey == "" && wants("anthropic") {
		return errors.New("ANTHROPIC_API_KEY is required when ANTHROPIC_BASE_URL is configured")
	}
	if googleBaseURL != "" && googleAPIKey == "" && wants(string(ProtocolGoogleGenAI)) {
		return errors.New("GOOGLE_GENAI_API_KEY, GOOGLE_API_KEY, or GEMINI_API_KEY is required when GOOGLE_GENAI_BASE_URL is configured")
	}
	if googleBaseURL != "" && wants(string(ProtocolGoogleGenAI)) && !c.googleGenAIBaseURLFlag {
		if err := validateGoogleBaseAliases(c.GoogleGenAIAPIVersion); err != nil {
			return err
		}
	}
	configured := map[string]TargetConfig{}
	if wants("openai") && openAIBaseURL != "" && openAIAPIKey != "" {
		capabilities, capabilityFile := c.OpenAIModelCapabilities, c.OpenAICapabilityFile
		if len(requested) <= 1 && len(capabilities) == 0 {
			capabilities = c.ModelCapabilities
		}
		if len(requested) <= 1 && capabilityFile == "" {
			capabilityFile = c.CapabilityFile
		}
		configured["openai"] = TargetConfig{ID: "openai", Protocol: ProtocolOpenAI, BaseURL: openAIBaseURL, APIKey: openAIAPIKey, ProfileFile: c.OpenAIProfile, Modules: append([]string(nil), c.Modules...), ModelCapabilities: append([]string(nil), capabilities...), CapabilityFile: capabilityFile, UnknownModel: c.UnknownModel, RequestInterval: c.RequestInterval}
	}
	if wants("anthropic") && anthropicBaseURL != "" && anthropicAPIKey != "" {
		capabilities, capabilityFile := c.AnthropicModelCapabilities, c.AnthropicCapabilityFile
		if len(requested) <= 1 && len(capabilities) == 0 {
			capabilities = c.ModelCapabilities
		}
		if len(requested) <= 1 && capabilityFile == "" {
			capabilityFile = c.CapabilityFile
		}
		normalized := normalizeAnthropicBaseURL(anthropicBaseURL)
		configured["anthropic"] = TargetConfig{ID: "anthropic", Protocol: ProtocolAnthropic, BaseURL: normalized, BaseURLNormalized: normalized != strings.TrimRight(strings.TrimSpace(anthropicBaseURL), "/"), APIKey: anthropicAPIKey, ProfileFile: c.AnthropicProfile, Modules: append([]string(nil), c.Modules...), ModelCapabilities: append([]string(nil), capabilities...), CapabilityFile: capabilityFile, UnknownModel: c.AnthropicUnknownModel, RequestInterval: c.RequestInterval, APIVersion: "2023-06-01"}
	}
	if wants(string(ProtocolGoogleGenAI)) && googleBaseURL != "" && googleAPIKey != "" {
		version := strings.TrimSpace(c.GoogleGenAIAPIVersion)
		if version == "" {
			version = "v1beta"
		}
		if !validGoogleAPIVersion(version) {
			return fmt.Errorf("invalid Google GenAI API version %q", version)
		}
		normalized, changed, err := normalizeGoogleBaseURL(googleBaseURL, version)
		if err != nil {
			return err
		}
		capabilities, capabilityFile := c.GoogleGenAIModelCapabilities, c.GoogleGenAICapabilityFile
		if len(requested) <= 1 && len(capabilities) == 0 {
			capabilities = c.ModelCapabilities
		}
		if len(requested) <= 1 && capabilityFile == "" {
			capabilityFile = c.CapabilityFile
		}
		configured[string(ProtocolGoogleGenAI)] = TargetConfig{ID: "google-genai", Protocol: ProtocolGoogleGenAI, BaseURL: normalized, BaseURLNormalized: changed, APIKey: googleAPIKey, ProfileFile: c.GoogleGenAIProfile, Modules: append([]string(nil), c.Modules...), ModelCapabilities: append([]string(nil), capabilities...), CapabilityFile: capabilityFile, UnknownModel: c.GoogleGenAIUnknownModel, RequestInterval: c.RequestInterval, APIVersion: version}
	}
	order := requested
	if len(order) == 0 {
		order = []string{"openai", "anthropic", "google-genai"}
	}
	for _, id := range order {
		if target, ok := configured[id]; ok {
			c.Targets = append(c.Targets, target)
		}
	}
	if len(requested) > 0 {
		for _, item := range requested {
			found := false
			for _, target := range c.Targets {
				if string(target.Protocol) == item {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("selected target %s is not configured", item)
			}
		}
	}
	if len(c.Targets) > 1 && (len(c.ModelCapabilities) > 0 || c.CapabilityFile != "") {
		return errors.New("--model-capability and --capability-file are ambiguous with multiple targets; use protocol-specific flags")
	}
	if len(c.Targets) == 1 {
		c.BaseURL, c.APIKey = c.Targets[0].BaseURL, c.Targets[0].APIKey
	}
	return nil
}

var googleAPIVersionPattern = regexp.MustCompile(`^v[0-9]+(?:beta[0-9]*|alpha[0-9]*)?$`)

func validGoogleAPIVersion(version string) bool {
	return googleAPIVersionPattern.MatchString(strings.TrimSpace(version))
}

func googleAliasKey() (string, error) {
	canonical := strings.TrimSpace(os.Getenv("GOOGLE_GENAI_API_KEY"))
	if canonical != "" {
		return canonical, nil
	}
	google := strings.TrimSpace(os.Getenv("GOOGLE_API_KEY"))
	gemini := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if google != "" && gemini != "" && google != gemini {
		return "", errors.New("GOOGLE_API_KEY and GEMINI_API_KEY conflict; set GOOGLE_GENAI_API_KEY explicitly")
	}
	if google != "" {
		return google, nil
	}
	return gemini, nil
}

func validateGoogleBaseAliases(version string) error {
	canonical := strings.TrimSpace(os.Getenv("GOOGLE_GENAI_BASE_URL"))
	alias := strings.TrimSpace(os.Getenv("GOOGLE_GEMINI_BASE_URL"))
	if canonical == "" || alias == "" {
		return nil
	}
	canonicalNormalized, _, err := normalizeGoogleBaseURL(canonical, version)
	if err != nil {
		return err
	}
	aliasNormalized, _, err := normalizeGoogleBaseURL(alias, version)
	if err != nil {
		return err
	}
	if canonicalNormalized != aliasNormalized {
		return fmt.Errorf("GOOGLE_GENAI_BASE_URL and GOOGLE_GEMINI_BASE_URL conflict; set --google-genai-base-url explicitly")
	}
	return nil
}

func normalizeGoogleBaseURL(raw, version string) (string, bool, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false, fmt.Errorf("invalid google-genai base URL %q", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", false, errors.New("google-genai base URL must not contain query parameters or a fragment")
	}
	if u.User != nil {
		return "", false, errors.New("google-genai base URL must not contain user information or credentials")
	}
	trimmed := strings.TrimRight(u.Path, "/")
	versionPath := "/" + strings.Trim(version, "/")
	if strings.HasSuffix(trimmed, versionPath) {
		u.Path = strings.TrimSuffix(trimmed, versionPath)
		if u.Path == "" {
			u.Path = "/"
		}
		u.RawPath = ""
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if u.Path == "" {
		u.Path = "/"
	}
	normalized := strings.TrimRight(u.String(), "/")
	return normalized, normalized != raw, nil
}

func normalizeAnthropicBaseURL(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if strings.HasSuffix(raw, "/v1") {
		return strings.TrimSuffix(raw, "/v1")
	}
	return raw
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

func (c Config) HasExtensionTests() bool {
	return c.HasTest("sdk") || c.HasTest("ext-suite")
}
