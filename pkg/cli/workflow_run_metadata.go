package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/goccy/go-yaml"
)

// fetchWorkflowRunMetadata fetches metadata for a single workflow run
func fetchWorkflowRunMetadata(ctx context.Context, runID int64, owner, repo, hostname string, verbose bool) (WorkflowRun, error) {
	args := buildWorkflowRunMetadataArgs(runID, owner, repo, hostname)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Executing: gh "+strings.Join(args, " ")))
	}
	output, err := workflow.RunGHCombinedContext(ctx, "Fetching run metadata...", args...)
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(string(output)))
		}
		return WorkflowRun{}, classifyWorkflowRunMetadataError(runID, err, output)
	}
	var run WorkflowRun
	if err := json.Unmarshal(output, &run); err != nil {
		return WorkflowRun{}, fmt.Errorf("failed to parse run metadata: %w", err)
	}
	resolveWorkflowRunDisplayName(ctx, &run, owner, repo, hostname)
	return run, nil
}

func buildWorkflowRunMetadataArgs(runID int64, owner, repo, hostname string) []string {
	args := buildWorkflowRunAPIArgs(runID, owner, repo, hostname)
	return append(args, "--jq", "{databaseId: .id, number: .run_number, url: .html_url, status: .status, conclusion: .conclusion, workflowName: .name, workflowPath: .path, createdAt: .created_at, startedAt: .run_started_at, updatedAt: .updated_at, event: .event, headBranch: .head_branch, headSha: .head_sha, displayTitle: .display_title, attempt: .run_attempt, repository: .repository.full_name, actor: .actor.login}")
}

func buildWorkflowRunAPIArgs(runID int64, owner, repo, hostname string) []string {
	endpoint := fmt.Sprintf("repos/{owner}/{repo}/actions/runs/%d", runID)
	if owner != "" && repo != "" {
		endpoint = fmt.Sprintf("repos/%s/%s/actions/runs/%d", owner, repo, runID)
	}
	args := []string{"api"}
	if hostname != "" && hostname != "github.com" {
		args = append(args, "--hostname", hostname)
	}
	return append(args, endpoint)
}

func classifyWorkflowRunMetadataError(runID int64, err error, output []byte) error {
	outputStr := string(output)
	if errorutil.IsNotFoundError(err) ||
		errorutil.IsNotFoundOutput(outputStr) ||
		strings.Contains(outputStr, "Could not resolve") {
		return fmt.Errorf("workflow run %d not found. Please verify the run ID is correct and that you have access to the repository", runID)
	}
	return fmt.Errorf("failed to fetch run metadata: %w", err)
}

func resolveWorkflowRunDisplayName(ctx context.Context, run *WorkflowRun, owner, repo, hostname string) {
	if !strings.HasPrefix(run.WorkflowName, constants.GithubDir) {
		return
	}
	if displayName := resolveWorkflowDisplayName(ctx, run.WorkflowPath, owner, repo, hostname); displayName != "" {
		auditLog.Printf("Resolved workflow display name: %q -> %q", run.WorkflowName, displayName)
		run.WorkflowName = displayName
	}
}

// resolveWorkflowDisplayName returns the human-readable display name for a workflow file.
// It first attempts to read the YAML file from the local filesystem (resolving the path
// relative to the git repository root so that it works from any working directory inside
// the repo); if that fails it falls back to a GitHub API call.  An empty string is
// returned on any error so that callers can gracefully keep the original value.
func resolveWorkflowDisplayName(ctx context.Context, workflowPath, owner, repo, hostname string) string {
	// Try local file first.  workflowPath is a repo-relative path like
	// ".github/workflows/foo.lock.yml", so we resolve it against the git root to
	// produce a correct absolute path regardless of the current working directory.
	if gitRoot, err := gitutil.FindGitRoot(); err == nil {
		absPath := filepath.Join(gitRoot, workflowPath)
		if content, err := os.ReadFile(absPath); err == nil {
			if name := extractWorkflowNameFromYAML(content); name != "" {
				return name
			}
		}
	}

	// Fall back to the GitHub Actions workflows API.
	filename := filepath.Base(workflowPath)
	var endpoint string
	if owner != "" && repo != "" {
		endpoint = fmt.Sprintf("repos/%s/%s/actions/workflows/%s", owner, repo, filename)
	} else {
		endpoint = "repos/{owner}/{repo}/actions/workflows/" + filename
	}

	args := []string{"api"}
	if hostname != "" && hostname != "github.com" {
		args = append(args, "--hostname", hostname)
	}
	args = append(args, endpoint, "--jq", ".name")

	out, err := workflow.RunGHCombinedContext(ctx, "Fetching workflow name...", args...)
	if err != nil {
		auditLog.Printf("Failed to fetch workflow display name for %q: %v", workflowPath, err)
		return ""
	}

	return strings.TrimSpace(string(out))
}

// extractWorkflowNameFromYAML parses a GitHub Actions workflow YAML document and
// returns the value of its top-level "name:" field.  An empty string is returned
// when the field is absent or the document cannot be parsed.
func extractWorkflowNameFromYAML(content []byte) string {
	var wf struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal(content, &wf); err != nil {
		auditLog.Printf("Failed to parse workflow YAML for name extraction (file may be malformed): %v", err)
		return ""
	}
	return wf.Name
}
