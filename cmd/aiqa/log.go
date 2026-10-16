package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ilyar/aiqa/internal/logview"
)

// runLog is deliberately independent from the runner configuration parser.
// Log flags are validated before resolveLogSource opens a file.
func runLog(args []string, stdin io.Reader, stdout, stderr io.Writer, cwd string) error {
	return runLogContext(context.Background(), args, stdin, stdout, stderr, cwd)
}

func runLogContext(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, cwd string) error {
	// The documented form places FILE before flags. flag.FlagSet stops at that
	// positional argument, so lift it out first while still accepting a trailing
	// FILE for shell-friendly flag-first invocations.
	leadingFile := ""
	if len(args) > 0 && (args[0] == "-" || !strings.HasPrefix(args[0], "-")) {
		leadingFile, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("aiqa log", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var name, level, only, statuses, target, model, job, event, format, color string
	var follow, failed bool
	fs.StringVar(&name, "name", "", "log basename")
	fs.StringVar(&level, "level", "", "minimum level")
	fs.StringVar(&only, "only-level", "", "exact levels")
	fs.BoolVar(&failed, "failed", false, "only failed outcomes")
	fs.StringVar(&statuses, "status", "", "exact outcomes")
	fs.StringVar(&target, "target", "", "target ID")
	fs.StringVar(&model, "model", "", "model ID")
	fs.StringVar(&job, "job", "", "job ID")
	fs.StringVar(&event, "event", "", "event regexp")
	fs.StringVar(&format, "format", "compact", "table, compact, or json")
	fs.StringVar(&color, "color", "auto", "auto, never, or always")
	fs.BoolVar(&follow, "follow", false, "follow a regular file")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printLogUsage(stdout)
			return nil
		}
		return err
	}
	positional := append([]string(nil), fs.Args()...)
	if leadingFile != "" {
		positional = append([]string{leadingFile}, positional...)
	}
	if len(positional) > 1 {
		return errors.New("at most one log FILE may be provided")
	}
	if name != "" && len(positional) != 0 {
		return errors.New("--name cannot be combined with FILE")
	}
	if format != "table" && format != "compact" && format != "json" {
		return fmt.Errorf("invalid --format %q", format)
	}
	if color != "auto" && color != "never" && color != "always" {
		return fmt.Errorf("invalid --color %q", color)
	}
	filter, err := makeLogFilter(level, only, failed, statuses, target, model, job, event)
	if err != nil {
		return err
	}
	source, label, closeSource, err := resolveLogSource(positional, name, stdin, cwd)
	if err != nil {
		return err
	}
	defer closeSource()
	if follow && label == "stdin" {
		// A pipe is naturally consumed until EOF. It must not acquire file polling.
		return renderLog(source, stdout, stderr, filter, format)
	}
	if follow {
		return followLogFileContext(ctx, label, stdout, stderr, filter, format)
	}
	return renderLog(source, stdout, stderr, filter, format)
}

func printLogUsage(w io.Writer) {
	fmt.Fprintln(w, `usage: aiqa log [FILE|-] [--name NAME] [--follow]
  [--level TRACE|DEBUG|INFO|WARN|ERROR|FATAL]
  [--only-level LEVEL[,LEVEL]] [--failed] [--status STATUS[,STATUS]]
  [--target ID] [--model ID] [--job ID] [--event REGEX]
  [--format table|compact|json] [--color auto|never|always]`)
}

// followLogFile is a small filesystem adapter around logview.Follower. The
// decoder owns all line-boundary, truncate and rotation semantics; this layer
// only supplies checked snapshots at the documented 200ms cadence.
func followLogFile(path string, stdout, stderr io.Writer, filter logview.Filter, format string) error {
	return followLogFileContext(context.Background(), path, stdout, stderr, filter, format)
}

func followLogFileContext(ctx context.Context, path string, stdout, stderr io.Writer, filter logview.Filter, format string) error {
	follow := logview.NewFollower()
	identity := fileGeneration{}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	header := false
	for {
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open followed log %q: %w", path, err)
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			_ = f.Close()
			if err != nil {
				return fmt.Errorf("stat followed log %q: %w", path, err)
			}
			return fmt.Errorf("followed log %q is not a regular file", path)
		}
		snap := logview.Snapshot{Identity: identity.next(info), Size: info.Size(), ReadAt: func(offset int64) ([]byte, error) {
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				return nil, err
			}
			// Bound each poll; Follower advances by returned bytes and catches up
			// on later ticks instead of retaining an arbitrarily large logfile.
			return io.ReadAll(io.LimitReader(f, 64*1024))
		}}
		items, pollErr := follow.Poll(snap)
		_ = f.Close()
		if pollErr != nil {
			return pollErr
		}
		_, header = writeLogItems(stdout, stderr, items, filter, format, header)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// fileGeneration assigns a stable identity to each file observed by a follow
// session. os.SameFile is portable, unlike direct inspection of syscall.Stat_t,
// and avoids treating an ordinary append (which changes ModTime) as rotation.
type fileGeneration struct {
	info       os.FileInfo
	generation uint64
}

func (f *fileGeneration) next(info os.FileInfo) string {
	if f.info == nil || !os.SameFile(f.info, info) {
		f.info = info
		f.generation++
	}
	return fmt.Sprintf("%d", f.generation)
}

func makeLogFilter(level, only string, failed bool, statuses, target, model, job, event string) (logview.Filter, error) {
	f := logview.Filter{Failed: failed, Statuses: csvSet(statuses), Target: target, Model: model, Job: job, Event: event}
	if level != "" {
		v := logview.Level(strings.ToUpper(strings.TrimSpace(level)))
		f.Minimum = &v
	}
	if only != "" {
		f.Only = map[logview.Level]bool{}
		for _, value := range strings.Split(only, ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				return logview.Filter{}, errors.New("--only-level contains an empty level")
			}
			f.Only[logview.Level(strings.ToUpper(value))] = true
		}
	}
	if err := f.Normalize(); err != nil {
		return logview.Filter{}, err
	}
	return f, nil
}

func csvSet(value string) map[string]bool {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	values := map[string]bool{}
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values[item] = true
		}
	}
	return values
}

func resolveLogSource(args []string, name string, stdin io.Reader, cwd string) (io.Reader, string, func(), error) {
	if len(args) == 1 {
		if args[0] == "-" {
			return stdin, "stdin", func() {}, nil
		}
		return openLogFile(args[0])
	}
	if name != "" {
		return openLogFile(filepath.Join(cwd, name+".log"))
	}
	if file, ok := stdin.(*os.File); ok {
		if info, err := file.Stat(); err == nil && info.Mode()&os.ModeNamedPipe != 0 {
			return stdin, "stdin", func() {}, nil
		}
	}
	entries, err := filepath.Glob(filepath.Join(cwd, "*.log"))
	if err != nil {
		return nil, "", nil, err
	}
	var candidates []string
	for _, path := range entries {
		if isAIQALog(path) {
			candidates = append(candidates, path)
		}
	}
	sort.Strings(candidates)
	switch len(candidates) {
	case 0:
		return nil, "", nil, errors.New("no AIQA log was created; pass FILE, -, or --name")
	case 1:
		return openLogFile(candidates[0])
	default:
		return nil, "", nil, fmt.Errorf("multiple AIQA logs found: %s; select FILE or --name", strings.Join(candidates, ", "))
	}
}

func openLogFile(path string) (io.Reader, string, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", nil, fmt.Errorf("open log %q: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		if err != nil {
			return nil, "", nil, fmt.Errorf("stat log %q: %w", path, err)
		}
		return nil, "", nil, fmt.Errorf("log %q is not a regular file", path)
	}
	return f, path, func() { _ = f.Close() }, nil
}

func isAIQALog(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	return bytes.Contains(buf[:n], []byte(`"schema_version":"aiqa.trace.v1"`))
}

func renderLog(source io.Reader, stdout, stderr io.Writer, filter logview.Filter, format string) error {
	decoder := logview.NewDecoder()
	buf := make([]byte, 32*1024)
	printedHeader, invalid := false, false
	for {
		n, err := source.Read(buf)
		if n > 0 {
			items := decoder.Feed(buf[:n], false)
			bad, header := writeLogItems(stdout, stderr, items, filter, format, printedHeader)
			invalid, printedHeader = invalid || bad, header
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read log: %w", err)
		}
	}
	bad, _ := writeLogItems(stdout, stderr, decoder.Feed(nil, true), filter, format, printedHeader)
	if invalid || bad {
		return errors.New("log contains invalid or unsupported records")
	}
	return nil
}

func writeLogItems(stdout, stderr io.Writer, items []logview.Item, filter logview.Filter, format string, header bool) (bool, bool) {
	bad := false
	for _, item := range items {
		if item.Diagnostic != nil {
			bad = true
			if format == "json" {
				fmt.Fprintf(stdout, `{"diagnostic":%q,"detail":%q}`+"\n", item.Diagnostic.Code, item.Diagnostic.Text)
			} else {
				fmt.Fprintf(stderr, "aiqa log: %s\n", item.Diagnostic.Text)
			}
			continue
		}
		if item.Record == nil || !filter.Match(*item.Record) {
			continue
		}
		if format == "table" && !header {
			fmt.Fprintln(stdout, "SEQ   TIME          LEVEL  TARGET        JOB                    OUTCOME  DETAIL")
			header = true
		}
		writeLogRecord(stdout, *item.Record, format)
	}
	return bad, header
}

func writeLogRecord(w io.Writer, r logview.Record, format string) {
	if format == "json" {
		encoded, _ := json.Marshal(r.Raw)
		fmt.Fprintln(w, string(encoded))
		return
	}
	detail := logview.SafeText(r.String("message"))
	if format == "table" {
		fmt.Fprintf(w, "%-5d %-13s %-6s %-13s %-22s %-8s %s\n", r.Seq, r.Time.Format("15:04:05.000"), r.Level, logview.SafeText(r.Target()), logview.SafeText(r.Job()), logview.SafeText(r.Outcome()), detail)
		return
	}
	fmt.Fprintf(w, "%d %s %s %s target=%s model=%s job=%s outcome=%s %s\n", r.Seq, r.Time.Format("15:04:05.000"), r.Level, r.Event, logview.SafeText(r.Target()), logview.SafeText(r.Model()), logview.SafeText(r.Job()), logview.SafeText(r.Outcome()), detail)
}
