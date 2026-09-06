// Package logging provides the private, buffered diagnostic log used by aiqa.
package logging

import (
	"fmt"
	"strings"
	"sync"
)

// Level identifies a diagnostic category. The ordered names are deliberately
// stable because they are written into the optional private log sidecar.
type Level string

const (
	Trace Level = "TRACE"
	Debug Level = "DEBUG"
	Info  Level = "INFO"
	Warn  Level = "WARN"
	Error Level = "ERROR"
	Fatal Level = "FATAL"
)

// Verbosity returns the minimum --verbose value that includes this level.
func (level Level) Verbosity() int {
	switch level {
	case Trace:
		return 4
	case Debug:
		return 3
	case Info:
		return 2
	case Warn:
		return 1
	case Error, Fatal:
		return 0
	default:
		return -1
	}
}

// Logger retains formatted private diagnostic lines. It is safe for concurrent
// probes. It never writes to a stream and does not terminate for Fatal.
type Logger struct {
	verbosity int
	mu        sync.Mutex
	lines     []string
}

// New constructs a logger. Invalid values are clamped to the documented range.
func New(verbosity int) *Logger {
	if verbosity < 0 {
		verbosity = 0
	}
	if verbosity > 4 {
		verbosity = 4
	}
	return &Logger{verbosity: verbosity}
}

// Enabled reports whether a category will be retained.
func (l *Logger) Enabled(level Level) bool {
	return level.Verbosity() >= 0 && level.Verbosity() <= l.verbosity
}

// Logf formats and records a line if its category is enabled. It returns false
// when the line was filtered, allowing callers to avoid related work.
func (l *Logger) Logf(level Level, format string, args ...any) bool {
	if !l.Enabled(level) {
		return false
	}
	line := "[" + string(level) + "] " + strings.TrimRight(fmt.Sprintf(format, args...), "\n")
	l.mu.Lock()
	l.lines = append(l.lines, line)
	l.mu.Unlock()
	return true
}

// Lines returns a snapshot of the retained formatted lines.
func (l *Logger) Lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}
