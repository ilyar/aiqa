package extsuite

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/result"
)

// DiagnosticLogger receives private, already-structured extension diagnostics.
// Callers decide whether a level is retained. Diagnostic fields deliberately
// exclude request/response content, URLs, credentials, and headers.
type DiagnosticLogger interface {
	Log(logging.Level, string, logging.Fields) bool
}

type contextDiagnosticLogger interface {
	LogContext(context.Context, logging.Level, string, logging.Fields) bool
}

func logDiagnostic(logger DiagnosticLogger, level logging.Level, event string, fields logging.Fields) {
	if logger != nil {
		logger.Log(level, event, fields)
	}
}

func logDiagnosticContext(ctx context.Context, logger DiagnosticLogger, level logging.Level, event string, fields logging.Fields) {
	if contextual, ok := logger.(contextDiagnosticLogger); ok {
		contextual.LogContext(ctx, level, event, fields)
		return
	}
	logDiagnostic(logger, level, event, fields)
}

// diagnosticSubjectRef identifies a subject without placing a base URL or an
// arbitrary model identifier in the private log. The report retains the human
// readable identifier needed to correlate this stable reference.
func diagnosticSubjectRef(subject result.SubjectRef) string {
	sum := sha256.Sum256([]byte(subject.TargetID + "\x00" + subject.Protocol + "\x00" + subject.Kind + "\x00" + subject.ID))
	return fmt.Sprintf("%x", sum[:6])
}

func diagnosticSignals(signals []result.EvidenceSignal) string {
	if len(signals) == 0 {
		return "-"
	}
	seen := make(map[string]bool, len(signals))
	for _, signal := range signals {
		seen[signal.Code] = true
	}
	codes := make([]string, 0, len(seen))
	for code := range seen {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return strings.Join(codes, ",")
}

func diagnosticUsageFields(usage result.Usage) string {
	fields := make([]string, 0, len(usage.ProviderFields))
	for field := range usage.ProviderFields {
		fields = append(fields, field)
	}
	if len(fields) == 0 {
		return "-"
	}
	sort.Strings(fields)
	return strings.Join(fields, ",")
}
