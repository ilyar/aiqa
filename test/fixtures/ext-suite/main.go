// Command ext-suite-fixture is a minimal conforming aiqa.ext.v1 executable.
// It is intentionally provider-independent: contract tests use it to exercise
// manifest negotiation, credential transport, JSONL results, and exit codes.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type envelope struct {
	SchemaVersion string          `json:"schema_version"`
	Type          string          `json:"type"`
	RunID         string          `json:"run_id"`
	Subject       json.RawMessage `json:"subject"`
	SuiteIDs      []string        `json:"suite_ids"`
	Credential    struct {
		Value string `json:"value"`
	} `json:"credential"`
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		fmt.Fprintln(os.Stderr, "missing protocol request")
		os.Exit(2)
	}
	var request envelope
	if err := json.Unmarshal(scanner.Bytes(), &request); err != nil || request.SchemaVersion != "aiqa.ext.v1" {
		fmt.Fprintln(os.Stderr, "invalid protocol request")
		os.Exit(2)
	}
	encoder := json.NewEncoder(os.Stdout)
	switch request.Type {
	case "manifest_request":
		_ = encoder.Encode(map[string]any{
			"schema_version": "aiqa.ext.v1", "type": "manifest",
			"extension":        map[string]any{"id": "fixture.contract", "name": "Contract fixture", "version": "1.0.0"},
			"report_contracts": []string{"aiqa.report.v3"}, "profile_contracts": []string{"aiqa.profile.v3"}, "protocols": []string{"openai", "anthropic"},
			"suites": []map[string]any{
				{"id": "after", "subject": "model", "cost": "text", "internal_concurrency": 1},
				{"id": "echo", "subject": "model", "cost": "text", "internal_concurrency": 1},
				{"id": "partial", "subject": "model", "cost": "text", "internal_concurrency": 1},
				{"id": "timeout", "subject": "model", "cost": "text", "internal_concurrency": 1},
			},
		})
	case "run_request":
		if request.Credential.Value == "" {
			fmt.Fprintln(os.Stderr, "credential was not supplied through stdin")
			os.Exit(3)
		}
		for _, suiteID := range request.SuiteIDs {
			if suiteID == "partial" {
				fmt.Fprintln(os.Stdout, "{malformed")
				continue
			}
			if suiteID == "timeout" {
				time.Sleep(10 * time.Second)
				continue
			}
			_ = encoder.Encode(map[string]any{
				"schema_version": "aiqa.ext.v1", "type": "suite_result", "run_id": request.RunID,
				"extension_id": "fixture.contract", "suite_id": suiteID, "subject": json.RawMessage(request.Subject),
				"outcome": "pass", "duration_ms": 1, "summary": "Fixture accepted the request",
			})
		}
		for _, suiteID := range request.SuiteIDs {
			if suiteID == "partial" {
				os.Exit(4)
			}
		}
	default:
		fmt.Fprintln(os.Stderr, "unsupported protocol request")
		os.Exit(2)
	}
}
