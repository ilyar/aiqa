//go:build ignore

// Command http-client-private-env creates a private HTTP client environment
// file by resolving $env.NAME placeholders from http-client.env.json.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var placeholderName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func main() {
	if err := generatePrivateEnv("http-client.env.json", "http-client.private.env.json", os.LookupEnv); err != nil {
		fmt.Fprintf(os.Stderr, "create private HTTP client environment: %v\n", err)
		os.Exit(1)
	}
}

func generatePrivateEnv(inputPath, outputPath string, lookupEnv func(string) (string, bool)) error {
	input, err := readEnvironmentFile(inputPath)
	if err != nil {
		return err
	}

	private, err := resolveEnvironment(input, lookupEnv)
	if err != nil {
		return err
	}
	return writePrivateEnvironment(outputPath, private)
}

func readEnvironmentFile(path string) (map[string]json.RawMessage, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	var environments map[string]json.RawMessage
	if err := decoder.Decode(&environments); err != nil {
		return nil, fmt.Errorf("decode input: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("decode input: trailing JSON value")
		}
		return nil, fmt.Errorf("decode input: trailing data: %w", err)
	}
	if environments == nil {
		return nil, errors.New("decode input: top-level value must be an object")
	}
	return environments, nil
}

func resolveEnvironment(environments map[string]json.RawMessage, lookupEnv func(string) (string, bool)) (map[string]map[string]string, error) {
	private := make(map[string]map[string]string, len(environments))
	for name, raw := range environments {
		var environment map[string]json.RawMessage
		if err := json.Unmarshal(raw, &environment); err != nil || environment == nil {
			return nil, fmt.Errorf("environment %q must be an object", name)
		}
		rawKey, ok := environment["key"]
		if !ok {
			return nil, fmt.Errorf("environment %q is missing key", name)
		}
		var placeholder string
		if err := json.Unmarshal(rawKey, &placeholder); err != nil {
			return nil, fmt.Errorf("environment %q key must be a string placeholder", name)
		}
		const prefix = "$env."
		if len(placeholder) <= len(prefix) || placeholder[:len(prefix)] != prefix {
			return nil, fmt.Errorf("environment %q key must use a $env.NAME placeholder", name)
		}
		variable := placeholder[len(prefix):]
		if !placeholderName.MatchString(variable) {
			return nil, fmt.Errorf("environment %q has an invalid placeholder name", name)
		}
		value, ok := lookupEnv(variable)
		if !ok {
			continue
		}
		private[name] = map[string]string{"key": value}
	}
	return private, nil
}

func writePrivateEnvironment(path string, private map[string]map[string]string) error {
	data, err := json.MarshalIndent(private, "", "  ")
	if err != nil {
		return fmt.Errorf("encode output: %w", err)
	}
	data = append(data, '\n')

	directory := filepath.Dir(filepath.Clean(path))
	temporary, err := os.CreateTemp(directory, ".http-client.private.env-*")
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("set output permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace output: %w", err)
	}
	return nil
}
