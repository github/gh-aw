// This file provides command-line interface functionality for gh-aw.
// This file (logs_github_rate_limit_usage.go) parses the github_rate_limits.jsonl artifact
// produced by workflow runs and computes per-run GitHub API quota consumption metrics.
//
// Key responsibilities:
//   - Locating and parsing the github_rate_limits.jsonl file within a run's artifact directory
//   - Aggregating per-resource API usage statistics
//   - Providing a measure of how much GitHub API quota each agentic workflow consumes

package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var gitHubRateLimitUsageLog = logger.New("cli:github_rate_limit_usage")

// GitHubRateLimitEntry is a single line from the github_rate_limits.jsonl file.
// Each entry records either a rate-limit header captured from a REST response
// (source="response_headers") or a snapshot from the rate-limit API
// (source="rate_limit_api").
type GitHubRateLimitEntry struct {
	Timestamp string `json:"timestamp"`
	Source    string `json:"source"`             // "response_headers" or "rate_limit_api"
	Operation string `json:"operation"`          // e.g. "issues.listComments"
	Resource  string `json:"resource,omitempty"` // e.g. "core", "search", "graphql"
	Limit     int    `json:"limit,omitempty"`
	Remaining int    `json:"remaining,omitempty"`
	Used      int    `json:"used,omitempty"`
	Reset     string `json:"reset,omitempty"`
}

// GitHubRateLimitResourceUsage summarizes API usage for a single GitHub rate-limit
// resource category (e.g. "core", "search", "graphql") over a workflow run.
type GitHubRateLimitResourceUsage struct {
	Resource       string `json:"resource" console:"header:Resource"`
	RequestsMade   int    `json:"requests_made" console:"header:Requests Made,format:number"`
	QuotaConsumed  int    `json:"quota_consumed" console:"header:Quota Consumed,format:number"`
	FinalRemaining int    `json:"final_remaining" console:"header:Remaining,format:number"`
	Limit          int    `json:"limit" console:"header:Limit,format:number"`
}

// GitHubRateLimitUsage provides an aggregated view of GitHub API quota consumed
// by a single workflow run.  It is populated by parsing the github_rate_limits.jsonl
// artifact produced during the run.
type GitHubRateLimitUsage struct {
	TotalRequestsMade  int                             `json:"total_requests_made" console:"header:Total GitHub API Calls,format:number"`
	CoreConsumed       int                             `json:"core_consumed" console:"header:Core Quota Consumed,format:number"`
	CoreConsumedSource string                          `json:"core_consumed_source,omitempty" console:"-"`
	CoreRemaining      int                             `json:"core_remaining" console:"header:Core Remaining,format:number"`
	CoreLimit          int                             `json:"core_limit" console:"header:Core Limit,format:number"`
	Resources          []*GitHubRateLimitResourceUsage `json:"resources,omitempty"`
}

// ResourceRows returns per-resource rows sorted by total requests made descending,
// suitable for console table rendering.
func (u *GitHubRateLimitUsage) ResourceRows() []*GitHubRateLimitResourceUsage {
	rows := make([]*GitHubRateLimitResourceUsage, len(u.Resources))
	copy(rows, u.Resources)
	slices.SortFunc(rows, func(a, b *GitHubRateLimitResourceUsage) int {
		if a.RequestsMade > b.RequestsMade {
			return -1
		}
		if a.RequestsMade < b.RequestsMade {
			return 1
		}
		return 0
	})
	return rows
}

// findGitHubRateLimitsFile locates the github_rate_limits.jsonl file within a run directory.
// It checks the root of the run directory first (after artifact flattening) and then
// performs a directory walk as a fallback to handle non-standard artifact structures.
func findGitHubRateLimitsFile(runDir string) string {
	filename := constants.GithubRateLimitsFilename.String()

	// Primary location: root of the run directory (after flattenUnifiedArtifact)
	primary := filepath.Join(runDir, filename)
	if _, err := os.Stat(primary); err == nil {
		return primary
	}

	// Fallback: walk the run directory looking for the file by name
	found := ""
	if walkErr := filepath.Walk(runDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			gitHubRateLimitUsageLog.Printf("walk error at %s: %v", path, err)
			return nil
		}
		if info == nil || info.IsDir() {
			return nil
		}
		if info.Name() == filename {
			found = path
			return filepath.SkipAll
		}
		return nil
	}); walkErr != nil && !errors.Is(walkErr, filepath.SkipAll) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("filesystem error walking %s: %v", runDir, walkErr)))
	}
	if found != "" {
		gitHubRateLimitUsageLog.Printf("Found rate limits file via walk: %s", found)
		return found
	}

	gitHubRateLimitUsageLog.Print("No github_rate_limits.jsonl file found")
	return ""
}

// parseGitHubRateLimitsFile reads and parses the github_rate_limits.jsonl file,
// returning an aggregated GitHubRateLimitUsage summary.
func parseGitHubRateLimitsFile(filePath string) (*GitHubRateLimitUsage, error) {
	gitHubRateLimitUsageLog.Printf("Parsing GitHub rate limits file: %s", filePath)

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open rate limits file: %w", err)
	}
	defer file.Close()

	type resourceState struct {
		requestsMade      int
		firstRemaining    int
		lastRemaining     int
		firstUsed         int
		lastUsed          int
		firstSnapshotUsed int
		lastSnapshotUsed  int
		snapshotCount     int
		limit             int
		firstEntrySet     bool
	}
	byResource := make(map[string]*resourceState)

	totalRequests := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry GitHubRateLimitEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			gitHubRateLimitUsageLog.Printf("Skipping invalid JSON at line %d: %v", lineNum, err)
			continue
		}

		resource := entry.Resource
		if resource == "" {
			resource = "core"
		}

		// Only count actual API calls (not rate-limit-API snapshots) toward request totals
		switch entry.Source {
		case "response_headers":
			totalRequests++

			state, ok := byResource[resource]
			if !ok {
				state = &resourceState{}
				byResource[resource] = state
			}
			state.requestsMade++

			if !state.firstEntrySet {
				state.firstRemaining = entry.Remaining
				state.firstUsed = entry.Used
				state.firstEntrySet = true
			}
			// Always update last values
			state.lastRemaining = entry.Remaining
			state.lastUsed = entry.Used
			if entry.Limit > 0 {
				state.limit = entry.Limit
			}
		case "rate_limit_api":
			// Use rate-limit API snapshots to fill in limit and remaining, and to
			// derive core_consumed via firstSnapshotUsed/lastSnapshotUsed when no
			// response-header entries are present for this resource.
			state, ok := byResource[resource]
			if !ok {
				state = &resourceState{}
				byResource[resource] = state
			}
			state.snapshotCount++
			if state.snapshotCount == 1 {
				state.firstSnapshotUsed = entry.Used
			}
			state.lastSnapshotUsed = entry.Used
			if entry.Limit > 0 && state.limit == 0 {
				state.limit = entry.Limit
			}
			// If we have no response-header entries, record the snapshot as an approximation
			if !state.firstEntrySet {
				state.lastRemaining = entry.Remaining
				state.lastUsed = entry.Used
				if entry.Limit > 0 {
					state.limit = entry.Limit
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading rate limits file: %w", err)
	}

	if len(byResource) == 0 && totalRequests == 0 {
		// File was empty or had no parseable entries
		return nil, nil
	}

	usage := &GitHubRateLimitUsage{
		TotalRequestsMade: totalRequests,
	}

	for resource, state := range byResource {
		// Compute quota consumed within the same rate-limit window.
		// If used values suggest a window reset occurred (lastUsed < firstUsed),
		// fall back to using the absolute lastUsed value as the consumption metric.
		var consumed int
		consumedSource := ""
		if state.requestsMade > 0 {
			diff := state.lastUsed - state.firstUsed
			if diff >= 0 {
				consumed = diff
			} else {
				// Window reset mid-run; use lastUsed as a lower-bound estimate
				consumed = state.lastUsed
			}
			consumedSource = "response_headers_delta"
		} else if state.snapshotCount >= 2 {
			diff := state.lastSnapshotUsed - state.firstSnapshotUsed
			if diff >= 0 {
				consumed = diff
			} else {
				// Window reset across snapshots; use last snapshot used as a lower-bound estimate
				consumed = state.lastSnapshotUsed
			}
			consumedSource = "rate_limit_api_delta"
		} else if state.snapshotCount == 1 {
			consumedSource = "rate_limit_api_single_snapshot"
		}

		row := &GitHubRateLimitResourceUsage{
			Resource:       resource,
			RequestsMade:   state.requestsMade,
			QuotaConsumed:  consumed,
			FinalRemaining: state.lastRemaining,
			Limit:          state.limit,
		}
		usage.Resources = append(usage.Resources, row)

		if resource == "core" {
			usage.CoreConsumed = consumed
			usage.CoreConsumedSource = consumedSource
			usage.CoreRemaining = state.lastRemaining
			usage.CoreLimit = state.limit
		}
	}

	// Sort resources for deterministic output
	slices.SortFunc(usage.Resources, func(a, b *GitHubRateLimitResourceUsage) int {
		if a.RequestsMade > b.RequestsMade {
			return -1
		}
		if a.RequestsMade < b.RequestsMade {
			return 1
		}
		return 0
	})

	return usage, nil
}

// analyzeGitHubRateLimits locates and parses the github_rate_limits.jsonl file within
// a run directory.  Returns nil without error when the file is absent (e.g., for runs
// that pre-date the feature or where the activation job made no GitHub API calls).
func analyzeGitHubRateLimits(runDir string, verbose bool) (*GitHubRateLimitUsage, error) {
	gitHubRateLimitUsageLog.Printf("Analyzing GitHub rate limits in: %s", runDir)

	filePath := findGitHubRateLimitsFile(runDir)
	if filePath == "" {
		return nil, nil
	}

	if verbose {
		if info, err := os.Stat(filePath); err == nil {
			fmt.Fprintf(os.Stderr, "  Found GitHub rate limits file: %s (%d bytes)\n", filepath.Base(filePath), info.Size())
		}
	}

	return parseGitHubRateLimitsFile(filePath)
}
