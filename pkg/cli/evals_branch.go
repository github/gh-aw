package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var errRunStateCommitNotFound = errors.New("run-specific state commit not found")

func ensureEvalsResultsFromBranch(ctx context.Context, run WorkflowRun, runDir, owner, repo, hostname string, verbose bool) bool {
	if runHasEvals(runDir, verbose) {
		return true
	}

	repoOverride, host := resolveRepoOverrideForRun(run, owner, repo, hostname)
	if repoOverride == "" {
		return false
	}
	run = populateWorkflowPathForRun(ctx, run, repoOverride, host)

	workflowID := workflowIDFromRunPath(run.WorkflowPath)
	if workflowID == "" {
		return false
	}
	branchName := workflow.WorkflowStateBranchName(constants.EvalsBranchPrefix, workflowID)
	refName, err := resolveRunStateBranchRef(ctx, repoOverride, branchName, run.DatabaseID, host, "evals results")
	if err != nil {
		if !errors.Is(err, errRunStateCommitNotFound) && !isRemoteFileNotFound(err) {
			logsOrchestratorLog.Printf("Failed to resolve evals branch ref for run %d: branch=%s repo=%s err=%v", run.DatabaseID, branchName, repoOverride, err)
		}
		return false
	}
	decoded, err := readRemoteRepoBranchFileContext(ctx, repoOverride, refName, constants.EvalsResultFilename.String(), host)
	if err != nil {
		if !isRemoteFileNotFound(err) {
			logsOrchestratorLog.Printf("Failed to fetch evals branch file for run %d: branch=%s ref=%s repo=%s err=%v", run.DatabaseID, branchName, refName, repoOverride, err)
		}
		return false
	}

	if mkdirErr := os.MkdirAll(runDir, constants.DirPermPublic); mkdirErr != nil {
		logsOrchestratorLog.Printf("Failed to create run directory for evals branch file: run=%d dir=%s err=%v", run.DatabaseID, runDir, mkdirErr)
		return false
	}

	dest := filepath.Join(runDir, constants.EvalsResultFilename.String())
	if writeErr := os.WriteFile(dest, decoded, constants.FilePermPublic); writeErr != nil {
		logsOrchestratorLog.Printf("Failed to write evals branch file for run %d: %v", run.DatabaseID, writeErr)
		return false
	}
	logsOrchestratorLog.Printf("Loaded evals results from branch %s (ref=%s) into %s for run %d", branchName, refName, dest, run.DatabaseID)
	return true
}

func workflowIDFromRunPath(workflowPath string) string {
	if workflowPath == "" {
		return ""
	}
	base := filepath.Base(workflowPath)
	base = stringutil.NormalizeWorkflowName(base)
	if before, ok := strings.CutSuffix(base, ".yml"); ok {
		base = before
	}
	if before, ok := strings.CutSuffix(base, ".yaml"); ok {
		base = before
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	return workflow.SanitizeWorkflowIDForCacheKey(base)
}

func resolveRepoOverrideForRun(run WorkflowRun, owner, repo, hostname string) (string, string) {
	runOwner, runRepo, runHost := owner, repo, hostname
	if runOwner == "" && run.URL != "" {
		if c, err := parser.ParseRunURLExtended(run.URL); err == nil && c.Owner != "" {
			runOwner, runRepo, runHost = c.Owner, c.Repo, c.Host
		}
	}
	if runOwner == "" || runRepo == "" {
		return "", runHost
	}
	return fmt.Sprintf("%s/%s", runOwner, runRepo), runHost
}

func populateWorkflowPathForRun(ctx context.Context, run WorkflowRun, repoOverride, hostname string) WorkflowRun {
	if run.WorkflowPath != "" || run.DatabaseID <= 0 || repoOverride == "" {
		return run
	}
	ownerRepo := strings.SplitN(repoOverride, "/", 2)
	if len(ownerRepo) != 2 {
		return run
	}
	metadata, err := fetchWorkflowRunMetadata(ctx, run.DatabaseID, ownerRepo[0], ownerRepo[1], hostname, false)
	if err != nil {
		logsOrchestratorLog.Printf("Failed to fetch workflow metadata for run %d: repo=%s err=%v", run.DatabaseID, repoOverride, err)
		return run
	}
	if metadata.WorkflowPath != "" {
		run.WorkflowPath = metadata.WorkflowPath
	}
	return run
}

type branchCommitInfo struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
	} `json:"commit"`
}

func resolveRunStateBranchRef(ctx context.Context, repoOverride, branchName string, runID int64, hostname, stateLabel string) (string, error) {
	if repoOverride == "" || branchName == "" || runID <= 0 {
		return "", errRunStateCommitNotFound
	}
	targetMessage := fmt.Sprintf("Update %s from workflow run %d", stateLabel, runID)
	const perPage = 100
	const maxPages = 10
	for page := 1; page <= maxPages; page++ {
		endpoint := fmt.Sprintf("commits?sha=%s&per_page=%d&page=%d", url.QueryEscape(branchName), perPage, page)
		args, err := repositoryAPIArgs(repoOverride, hostname, endpoint, "--method", "GET")
		if err != nil {
			return "", fmt.Errorf("invalid state branch endpoint: %w", err)
		}
		cmd := workflow.ExecGHContext(ctx, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			if errorutil.IsNotFoundOutput(string(out)) {
				return "", os.ErrNotExist
			}
			return "", fmt.Errorf("failed to list commits for state branch %s: %w", branchName, err)
		}
		var commits []branchCommitInfo
		if err := json.Unmarshal(out, &commits); err != nil {
			return "", fmt.Errorf("failed to parse commits for state branch %s: %w", branchName, err)
		}
		for _, commit := range commits {
			if strings.TrimSpace(commit.Commit.Message) == targetMessage && strings.TrimSpace(commit.SHA) != "" {
				return strings.TrimSpace(commit.SHA), nil
			}
		}
		if len(commits) < perPage {
			break
		}
	}
	return "", errRunStateCommitNotFound
}
