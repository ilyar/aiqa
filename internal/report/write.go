package report

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ilyar/aiqa/internal/config"
)

var (
	verboseURLPattern        = regexp.MustCompile(`https?://[^\s]+`)
	verboseCredentialPattern = regexp.MustCompile(`(?i)\b(authorization|x-api-key|api[_-]?key|token|secret|password)\s*[:=]\s*[^\s]+`)
)

func reportSecrets(cfg config.Config) []string {
	out := []string{cfg.APIKey}
	for _, target := range cfg.Targets {
		out = append(out, target.APIKey)
	}
	return out
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".aiqa-report-*")
	if err != nil {
		return fmt.Errorf("create temporary report: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary report: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temporary report: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temporary report: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary report: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace report %s: %w", path, err)
	}
	return nil
}

func sanitizeVerboseLogLine(line string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			line = strings.ReplaceAll(line, secret, "[REDACTED]")
		}
	}
	line = verboseURLPattern.ReplaceAllString(line, "[URL REDACTED]")
	return verboseCredentialPattern.ReplaceAllString(line, "$1: [REDACTED]")
}
