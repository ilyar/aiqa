package extsuite

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ilyar/aiqa/internal/result"
)

const (
	manifestTimeout  = 5 * time.Second
	maxManifestBytes = 1 << 20
	maxLineBytes     = 1 << 20
	maxStdoutBytes   = 16 << 20
	maxStderrBytes   = 64 << 10
	maxResultRecords = 2048
)

type Executable struct {
	path     string
	digest   string
	manifest Manifest
	host     HostInfo
}

func LoadExecutable(parent context.Context, path string, host HostInfo) (*Executable, error) {
	resolved, digest, err := resolveExecutable(path)
	if err != nil {
		return nil, err
	}
	request := ManifestRequest{SchemaVersion: ProtocolVersion, Type: "manifest_request", Host: host}
	ctx, cancel := context.WithTimeout(parent, manifestTimeout)
	defer cancel()
	stdout, stderr, _, err := executeJSON(ctx, resolved, request, maxManifestBytes)
	if err != nil {
		return nil, fmt.Errorf("read extension manifest from %s: %w: %s", resolved, err, sanitizeDiagnostic(stderr, ""))
	}
	line := bytes.TrimSpace(stdout)
	if bytes.Contains(line, []byte{'\n'}) {
		return nil, fmt.Errorf("extension %s emitted more than one manifest record", resolved)
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode extension manifest from %s: %w", resolved, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("decode extension manifest from %s: %w", resolved, err)
	}
	if err := ValidateManifest(&manifest); err != nil {
		return nil, fmt.Errorf("validate extension manifest from %s: %w", resolved, err)
	}
	return &Executable{path: resolved, digest: digest, manifest: manifest, host: host}, nil
}

func (e *Executable) Manifest() Manifest { return e.manifest }

func (e *Executable) Provenance() result.ExtensionProvenance {
	return result.ExtensionProvenance{ID: e.manifest.Extension.ID, Name: e.manifest.Extension.Name, Version: e.manifest.Extension.Version, Source: "executable", ProtocolVersion: ProtocolVersion, Path: e.path, SHA256: e.digest}
}

func (e *Executable) Run(ctx context.Context, request RunRequest, sink EventSink) (RunnerTelemetry, error) {
	started := time.Now()
	problems := []string{}
	telemetry, runErr := executeStreamingJSON(ctx, e.path, request, maxStdoutBytes, func(line []byte, lineNumber int) {
		record, err := decodeExtensionRecord(line)
		if err != nil {
			problems = append(problems, fmt.Sprintf("line %d: %v", lineNumber, err))
			return
		}
		switch value := record.(type) {
		case SuiteEvent:
			if err := sink.Emit(value); err != nil {
				problems = append(problems, fmt.Sprintf("line %d: %v", lineNumber, err))
			}
		case TraceEvent:
			traceSink, ok := sink.(TraceEventSink)
			if !ok {
				problems = append(problems, fmt.Sprintf("line %d: extension trace sink is unavailable", lineNumber))
				return
			}
			if err := traceSink.EmitTrace(value); err != nil {
				problems = append(problems, fmt.Sprintf("line %d: %v", lineNumber, err))
			}
		}
	})
	telemetry.TotalDuration = time.Since(started)
	if runErr != nil {
		problems = append(problems, runErr.Error())
	}
	if len(problems) > 0 {
		return telemetry, errors.New(strings.Join(problems, "; "))
	}
	return telemetry, nil
}

func decodeExtensionRecord(line []byte) (any, error) {
	if !utf8.Valid(line) {
		return nil, fmt.Errorf("record is not valid UTF-8")
	}
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return nil, err
	}
	switch envelope.Type {
	case "suite_result":
		return decodeSuiteEvent(line)
	case "trace_event":
		return decodeTraceEvent(line)
	default:
		return nil, fmt.Errorf("unsupported extension record type %q", envelope.Type)
	}
}

func decodeSuiteEvent(line []byte) (SuiteEvent, error) {
	if !utf8.Valid(line) {
		return SuiteEvent{}, fmt.Errorf("record is not valid UTF-8")
	}
	var event SuiteEvent
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return SuiteEvent{}, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return SuiteEvent{}, err
	}
	return event, nil
}

func decodeTraceEvent(line []byte) (TraceEvent, error) {
	var event TraceEvent
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return TraceEvent{}, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return TraceEvent{}, err
	}
	return event, nil
}

func DiscoverExecutables(ctx context.Context, explicit []string, pathEnv string, host HostInfo) ([]*Executable, error) {
	candidates, err := executableCandidates(explicit, pathEnv)
	if err != nil {
		return nil, err
	}
	var out []*Executable
	byID := map[string]string{}
	for _, candidate := range candidates {
		extension, err := LoadExecutable(ctx, candidate, host)
		if err != nil {
			return nil, err
		}
		id := extension.manifest.Extension.ID
		if previous, exists := byID[id]; exists {
			return nil, fmt.Errorf("duplicate extension id %q from %s and %s", id, previous, extension.path)
		}
		byID[id] = extension.path
		out = append(out, extension)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].manifest.Extension.ID < out[j].manifest.Extension.ID })
	return out, nil
}

func executableCandidates(explicit []string, pathEnv string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(candidate string, requireAbsolute bool) error {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return nil
		}
		if requireAbsolute && !filepath.IsAbs(candidate) {
			return fmt.Errorf("extension path %q must be absolute", candidate)
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			return err
		}
		if !seen[absolute] {
			seen[absolute] = true
			out = append(out, absolute)
		}
		return nil
	}
	for _, candidate := range explicit {
		if err := add(candidate, true); err != nil {
			return nil, err
		}
	}
	for _, directory := range filepath.SplitList(pathEnv) {
		if directory == "" {
			continue
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			name := entry.Name()
			logicalName := strings.TrimSuffix(name, ".exe")
			if entry.IsDir() || !strings.HasPrefix(logicalName, "aiqa-") || !strings.HasSuffix(logicalName, "-ext-suite") {
				continue
			}
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			candidate := filepath.Join(directory, name)
			if seenBaseInCandidates(out, name) {
				continue
			}
			if err := add(candidate, false); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func seenBaseInCandidates(candidates []string, name string) bool {
	for _, candidate := range candidates {
		if filepath.Base(candidate) == name {
			return true
		}
	}
	return false
}

func resolveExecutable(path string) (string, string, error) {
	if !filepath.IsAbs(path) {
		return "", "", fmt.Errorf("extension path %q must be absolute", path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", fmt.Errorf("resolve extension executable %s: %w", path, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", "", err
	}
	if !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return "", "", fmt.Errorf("extension path %s is not an executable regular file", resolved)
	}
	file, err := os.Open(resolved)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", "", err
	}
	return resolved, hex.EncodeToString(hash.Sum(nil)), nil
}

func executeJSON(ctx context.Context, path string, request any, stdoutLimit int64) ([]byte, []byte, RunnerTelemetry, error) {
	input, err := json.Marshal(request)
	if err != nil {
		return nil, nil, RunnerTelemetry{}, err
	}
	input = append(input, '\n')
	cmd := exec.CommandContext(ctx, path)
	configureProcess(cmd)
	cmd.Env = cleanEnvironment()
	cmd.Stdin = bytes.NewReader(input)
	stdout := &boundedBuffer{limit: stdoutLimit}
	stderr := &boundedBuffer{limit: maxStderrBytes}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	started := time.Now()
	err = cmd.Start()
	startupDuration := time.Since(started)
	if err == nil {
		err = cmd.Wait()
	}
	telemetry := RunnerTelemetry{StartupDuration: startupDuration, TotalDuration: time.Since(started), ExitCode: processExitCode(err)}
	if stdout.overflow || stderr.overflow {
		err = errors.Join(err, fmt.Errorf("extension output exceeded configured limit"))
	}
	if ctx.Err() != nil {
		err = errors.Join(err, ctx.Err())
	}
	return stdout.Bytes(), stderr.Bytes(), telemetry, err
}

// executeStreamingJSON consumes stdout while the child is still running. A
// result or trace event parsed before a timeout/exit is retained by Collector;
// the old buffered implementation could not make that guarantee.
func executeStreamingJSON(ctx context.Context, path string, request any, stdoutLimit int64, consume func([]byte, int)) (RunnerTelemetry, error) {
	input, err := json.Marshal(request)
	if err != nil {
		return RunnerTelemetry{}, err
	}
	input = append(input, '\n')
	cmd := exec.CommandContext(ctx, path)
	configureProcess(cmd)
	cmd.Env = cleanEnvironment()
	cmd.Stdin = bytes.NewReader(input)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return RunnerTelemetry{}, err
	}
	stderr := &boundedBuffer{limit: maxStderrBytes}
	cmd.Stderr = stderr
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return RunnerTelemetry{StartupDuration: time.Since(started), TotalDuration: time.Since(started), ExitCode: -1}, err
	}
	startupDuration := time.Since(started)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), maxLineBytes)
	lineNumber := 0
	var outputBytes int64
	var scanErr error
	for scanner.Scan() {
		lineNumber++
		outputBytes += int64(len(scanner.Bytes())) + 1
		if outputBytes > stdoutLimit {
			scanErr = fmt.Errorf("extension output exceeded configured limit")
			continue
		}
		if lineNumber > maxResultRecords {
			scanErr = fmt.Errorf("extension emitted too many result records")
			continue
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) > 0 {
			consume(append([]byte(nil), line...), lineNumber)
		}
	}
	if err := scanner.Err(); err != nil {
		scanErr = err
	}
	err = cmd.Wait()
	if stderr.overflow {
		err = errors.Join(err, fmt.Errorf("extension output exceeded configured limit"))
	}
	if scanErr != nil {
		err = errors.Join(err, scanErr)
	}
	if ctx.Err() != nil {
		err = errors.Join(err, ctx.Err())
	}
	return RunnerTelemetry{StartupDuration: startupDuration, TotalDuration: time.Since(started), ExitCode: processExitCode(err), Stderr: sanitizeDiagnostic(stderr.Bytes(), credentialFromRequest(request))}, err
}

func credentialFromRequest(request any) string {
	if value, ok := request.(RunRequest); ok {
		return value.Credential.Value
	}
	return ""
}

type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int64
	written  int64
	overflow bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	original := len(data)
	remaining := b.limit - b.written
	if remaining <= 0 {
		b.overflow = true
		return original, nil
	}
	if int64(len(data)) > remaining {
		data = data[:remaining]
		b.overflow = true
	}
	_, _ = b.buffer.Write(data)
	b.written += int64(len(data))
	return original, nil
}

func (b *boundedBuffer) Bytes() []byte { return b.buffer.Bytes() }

func cleanEnvironment() []string {
	allowed := map[string]bool{"PATH": true, "TMPDIR": true, "TMP": true, "TEMP": true, "LANG": true, "LC_ALL": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true}
	var out []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if allowed[name] {
			out = append(out, entry)
		}
	}
	return out
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func processExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func sanitizeDiagnostic(value []byte, secret string) string {
	text := string(value)
	if secret != "" {
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	text = diagnosticBearerSecret.ReplaceAllString(text, `${1}[REDACTED]`)
	text = diagnosticCookieSecret.ReplaceAllString(text, `${1}[REDACTED]`)
	text = diagnosticJSONSecret.ReplaceAllString(text, `${1}[REDACTED]${2}`)
	text = diagnosticSignedURL.ReplaceAllString(text, `${1}?[REDACTED]`)
	text = diagnosticLongBase64.ReplaceAllString(text, `[REDACTED BASE64]`)
	return strings.TrimSpace(text)
}

var (
	diagnosticCookieSecret = regexp.MustCompile(`(?i)((?:cookie|set-cookie)\s*:\s*)[^\r\n]+`)
	diagnosticBearerSecret = regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)[^\s]+`)
	diagnosticJSONSecret   = regexp.MustCompile(`(?i)("(?:api[_-]?key|authorization|access[_-]?token|cookie|tool[_-]?arguments|arguments)"\s*:\s*")[^"]+(")`)
	diagnosticSignedURL    = regexp.MustCompile(`(https?://[^\s"'<>?]+)\?[^\s"'<>)]*`)
	diagnosticLongBase64   = regexp.MustCompile(`[A-Za-z0-9+/]{256,}={0,2}`)
)
