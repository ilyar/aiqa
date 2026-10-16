package logging

import (
	"strings"
	"testing"
)

func TestLoggerRedactsNonScalarAllowedFields(t *testing.T) {
	const secret = "nested-test-credential"
	for _, value := range []any{[]string{secret}, map[string]string{secret: secret}, struct{ Value string }{secret}} {
		logger := New(4, secret)
		logger.Log(Trace, "safe.event", Fields{"model": value})
		line := logger.Lines()[0]
		if strings.Contains(line, secret) || !strings.Contains(line, `"model":"[REDACTED]"`) {
			t.Fatal("non-scalar allowed field was not redacted")
		}
	}
}

func TestLoggerPreservesAndRedactsNamedStrings(t *testing.T) {
	type status string
	logger := New(4, "enum-secret")
	logger.Log(Trace, "safe.event", Fields{"status": status("error"), "model": status("enum-secret")})
	line := logger.Lines()[0]
	if !strings.Contains(line, `"status":"error"`) || strings.Contains(line, "enum-secret") {
		t.Fatal("named string scalar was not safely preserved")
	}
}
