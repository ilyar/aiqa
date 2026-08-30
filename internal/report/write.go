package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
)

func Write(r result.Report, cfg config.Config) ([]string, error) {
	var paths []string
	for _, format := range cfg.Formats {
		var data []byte
		var ext string
		var err error
		switch format {
		case "json":
			data, err = json.MarshalIndent(r, "", "  ")
			ext = ".json"
		case "markdown":
			data = []byte(Markdown(r))
			ext = ".md"
		default:
			return paths, fmt.Errorf("unsupported format %q", format)
		}
		if err != nil {
			return paths, err
		}
		data = append(data, '\n')
		path := cfg.Name + ext
		if err := atomicWrite(path, data, 0o644); err != nil {
			return paths, err
		}
		paths = append(paths, path)
	}
	return paths, nil
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
