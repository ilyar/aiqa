// qa-check contains lightweight local QA checks implemented in Go.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ilyar/aiqa/internal/schema"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "qa-check:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: qa-check schemas | sbom PATH")
	}
	switch args[0] {
	case "schemas":
		if len(args) != 1 {
			return fmt.Errorf("usage: qa-check schemas")
		}
		root, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
		return schema.CheckSchemas(root)
	case "sbom":
		if len(args) != 2 {
			return fmt.Errorf("usage: qa-check sbom PATH")
		}
		path, err := filepath.Abs(args[1])
		if err != nil {
			return fmt.Errorf("resolve SBOM path: %w", err)
		}
		return schema.CheckSBOM(path)
	default:
		return fmt.Errorf("unknown command %q; usage: qa-check schemas | sbom PATH", args[0])
	}
}
