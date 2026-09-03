// check-profile-upstream validates pinned profile sources. It is a maintenance
// script and is not linked into the aiqa binary.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type profileSource struct {
	ID           string   `json:"id"`
	Source       string   `json:"source"`
	SourceCommit string   `json:"source_commit"`
	Sources      []source `json:"sources"`
}
type source struct {
	Kind       string `json:"kind"`
	URL        string `json:"url"`
	Revision   string `json:"revision"`
	ReviewedAt string `json:"reviewed_at"`
	Version    string `json:"version"`
	Commit     string `json:"commit"`
}
type commit struct {
	SHA string `json:"sha"`
}
type release struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}
type gitReference struct {
	Object struct {
		SHA  string `json:"sha"`
		Type string `json:"type"`
	} `json:"object"`
}
type gitTag struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	flags := flag.NewFlagSet("check-profile-upstream", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "profile/openai-compat-2026-09.json", "profile JSON file")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	p, err := loadProfile(*profilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read compatibility profile: %v\n", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if len(p.Sources) == 0 {
		return checkCommit(ctx, p.Source, p.SourceCommit)
	}
	changed := false
	for _, source := range p.Sources {
		switch source.Kind {
		case "official_sdk":
			state, err := checkRelease(ctx, source.URL, source.Version, source.Commit)
			if err != nil {
				fmt.Fprintf(os.Stderr, "check %s: %v\n", source.URL, err)
				return 2
			}
			changed = changed || state
		case "api_version":
			fmt.Printf("manual review required: %s revision %s (last reviewed %s)\n", source.URL, source.Revision, source.ReviewedAt)
		default:
			fmt.Fprintf(os.Stderr, "unsupported source kind %q\n", source.Kind)
			return 2
		}
	}
	if changed {
		return 1
	}
	return 0
}

func loadProfile(path string) (profileSource, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return profileSource{}, err
	}
	var p profileSource
	if err := json.Unmarshal(data, &p); err != nil {
		return p, err
	}
	if p.ID == "" {
		return p, fmt.Errorf("id is missing")
	}
	return p, nil
}

func checkCommit(ctx context.Context, repository, pinned string) int {
	ownerRepo, err := githubRepo(repository)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	endpoint := "https://api.github.com/repos/" + ownerRepo + "/commits?per_page=1"
	if override := strings.TrimSpace(os.Getenv("AIQA_OPENAPI_COMMITS_URL")); override != "" {
		endpoint = override
	}
	var commits []commit
	if err := getJSON(ctx, endpoint, &commits); err != nil {
		fmt.Fprintf(os.Stderr, "query profile upstream: %v\n", err)
		return 2
	}
	if len(commits) == 0 || commits[0].SHA == "" {
		fmt.Fprintln(os.Stderr, "unexpected GitHub commits response")
		return 2
	}
	if commits[0].SHA == pinned {
		fmt.Printf("profile source is current: %s\n", pinned)
		return 0
	}
	fmt.Printf("profile source moved; review the contract diff before updating\nprofile:  %s\nupstream: %s\n", pinned, commits[0].SHA)
	return 1
}

func checkRelease(ctx context.Context, repository, pinned, pinnedCommit string) (bool, error) {
	ownerRepo, err := githubRepo(repository)
	if err != nil {
		return false, err
	}
	var latest release
	if err := getJSON(ctx, "https://api.github.com/repos/"+ownerRepo+"/releases/latest", &latest); err != nil {
		return false, err
	}
	if latest.TagName == "" || latest.Draft || latest.Prerelease {
		return false, fmt.Errorf("latest stable release response is invalid")
	}
	changed := latest.TagName != pinned
	if pinnedCommit != "" {
		resolved, err := checkTagCommit(ctx, ownerRepo, pinned)
		if err != nil {
			return false, fmt.Errorf("resolve pinned SDK tag %s: %w", pinned, err)
		}
		if resolved != pinnedCommit {
			fmt.Printf("official SDK pinned tag changed; review generated API changes before updating\nprofile tag: %s\nprofile commit: %s\ntag commit: %s\n", pinned, pinnedCommit, resolved)
			changed = true
		}
	}
	if changed {
		fmt.Printf("official SDK moved; review generated API changes before updating\nprofile: %s\nupstream: %s\n", pinned, latest.TagName)
		return true, nil
	}
	fmt.Printf("profile SDK source is current: %s (%s)\n", pinned, pinnedCommit)
	return false, nil
}

func checkTagCommit(ctx context.Context, ownerRepo, tag string) (string, error) {
	var ref gitReference
	endpoint := "https://api.github.com/repos/" + ownerRepo + "/git/ref/tags/" + url.PathEscape(tag)
	if err := getJSON(ctx, endpoint, &ref); err != nil {
		return "", err
	}
	sha := ref.Object.SHA
	if ref.Object.Type == "tag" && sha != "" {
		var annotated gitTag
		if err := getJSON(ctx, "https://api.github.com/repos/"+ownerRepo+"/git/tags/"+url.PathEscape(sha), &annotated); err != nil {
			return "", err
		}
		sha = annotated.Object.SHA
	}
	if sha == "" {
		return "", fmt.Errorf("GitHub tag %q has no commit SHA", tag)
	}
	return sha, nil
}

func githubRepo(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host != "github.com" {
		return "", fmt.Errorf("source %q is not a GitHub repository", raw)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("source %q has no owner/repository", raw)
	}
	return parts[0] + "/" + strings.TrimSuffix(parts[1], ".git"), nil
}

func getJSON(ctx context.Context, endpoint string, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "aiqa-profile-check/1.0")
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("HTTP %s", response.Status)
	}
	return json.NewDecoder(response.Body).Decode(out)
}
