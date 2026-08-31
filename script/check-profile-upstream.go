// check-profile-upstream compares the profile's pinned OpenAPI commit with
// the latest commit reported by GitHub. It is a maintenance script, not part
// of the aiqa binary.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const defaultCommitsURL = "https://api.github.com/repos/openai/openai-openapi/commits?per_page=1"

type profileSource struct {
	SourceCommit string `json:"source_commit"`
}

type commit struct {
	SHA string `json:"sha"`
}

func main() {
	os.Exit(run())
}

func run() int {
	expected, err := pinnedCommit()
	if err != nil {
		fmt.Fprintf(os.Stderr, "read compatibility profile: %v\n", err)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, commitsURL(), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build OpenAI OpenAPI upstream request: %v\n", err)
		return 2
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "aiqa-profile-check/1.0")
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to query OpenAI OpenAPI upstream: %v\n", err)
		return 2
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		fmt.Fprintf(os.Stderr, "unable to query OpenAI OpenAPI upstream: HTTP %s\n", response.Status)
		return 2
	}
	var commits []commit
	if err := json.NewDecoder(response.Body).Decode(&commits); err != nil {
		fmt.Fprintf(os.Stderr, "decode OpenAI OpenAPI upstream response: %v\n", err)
		return 2
	}
	if len(commits) == 0 || strings.TrimSpace(commits[0].SHA) == "" {
		fmt.Fprintln(os.Stderr, "unexpected GitHub commits response")
		return 2
	}

	actual := commits[0].SHA
	if actual == expected {
		fmt.Printf("profile source is current: %s\n", actual)
		return 0
	}
	fmt.Println("OpenAI OpenAPI upstream moved; review the schema diff before updating the profile.")
	fmt.Printf("profile:  %s\n", expected)
	fmt.Printf("upstream: %s\n", actual)
	return 1
}

func pinnedCommit() (string, error) {
	data, err := os.ReadFile(profilePath())
	if err != nil {
		return "", err
	}
	var profile profileSource
	if err := json.Unmarshal(data, &profile); err != nil {
		return "", err
	}
	if profile.SourceCommit == "" {
		return "", fmt.Errorf("source_commit is missing")
	}
	return profile.SourceCommit, nil
}

func profilePath() string {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("profile", "openai-compat-2026-09.json")
	}
	return filepath.Join(filepath.Dir(filepath.Dir(source)), "profile", "openai-compat-2026-09.json")
}

func commitsURL() string {
	if value := strings.TrimSpace(os.Getenv("AIQA_OPENAPI_COMMITS_URL")); value != "" {
		return value
	}
	return defaultCommitsURL
}
