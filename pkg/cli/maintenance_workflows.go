package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// toggleRemoteWorkflows selects registered agentic workflows without requiring a local checkout.
//
//nolint:largefunc // Validate every exact selection before ordered cancellation and toggle operations.
func toggleRemoteWorkflows(ctx context.Context, names []string, enable bool, repo string) error {
	workflows, err := fetchGitHubWorkflowsFiltered(ctx, repo, true, true)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	targets := make(map[string]*GitHubWorkflow)
	for name, wf := range workflows {
		if strings.HasSuffix(wf.Path, ".lock.yml") {
			targets[name] = wf
		}
	}
	if len(names) == 0 {
		for name := range targets {
			names = append(names, name)
		}
		slices.Sort(names)
	}
	if len(names) == 0 {
		return errors.New("no registered agentic workflows found in target repository")
	}
	// Resolve every selection before making any changes.
	for _, name := range names {
		target, ok := targets[name]
		if !ok {
			return fmt.Errorf("workflow %q not found in target repository %s", name, repo)
		}
		if target.ID <= 0 {
			return fmt.Errorf("workflow %q has no valid GitHub ID", name)
		}
	}
	action := "disable"
	if enable {
		action = "enable"
	}
	seen := make(map[string]struct{})
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		target := targets[name]
		id := strconv.FormatInt(target.ID, 10)
		if enable && target.State == "active" {
			fmt.Fprintf(os.Stderr, "Workflow %s is already enabled\n", name)
			continue
		}
		if !enable {
			if err := cancelMaintenanceWorkflowRuns(ctx, id, repo); err != nil {
				return fmt.Errorf("failed to cancel runs for workflow %s: %w", name, err)
			}
			if target.State == "disabled_manually" {
				fmt.Fprintf(os.Stderr, "Workflow %s is already disabled\n", name)
				continue
			}
		}
		cmd := workflow.ExecGHContext(ctx, "workflow", action, id, "--repo", repo)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("failed to %s workflow %s: %w", action, name, err)
		}
		fmt.Fprintf(os.Stderr, "%sd workflow: %s\n", strings.ToUpper(action[:1])+action[1:], name)
	}
	return ctx.Err()
}

// cancelMaintenanceWorkflowRuns paginates all targeted in-progress runs before disabling.
func cancelMaintenanceWorkflowRuns(ctx context.Context, id, repo string) error {
	ownerRepo := "{owner}/{repo}"
	hostname := ""
	if repo != "" {
		ownerRepo, hostname = repoutil.NormalizeRepoForAPI(repo)
	}
	endpoint := fmt.Sprintf("repos/%s/actions/workflows/%s/runs?status=in_progress&per_page=100", ownerRepo, url.PathEscape(id))
	args := []string{"api", "--paginate", "--slurp", endpoint}
	if hostname != "" {
		args = append(args, "--hostname", hostname)
	}
	cmd := workflow.ExecGHContext(ctx, args...)
	cmd.Stderr = os.Stderr
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("failed to list in-progress runs: %w", err)
	}
	var pages []struct {
		Runs []struct {
			ID int64 `json:"id"`
		} `json:"workflow_runs"`
	}
	if err := json.Unmarshal(output, &pages); err != nil {
		return fmt.Errorf("failed to parse in-progress runs: %w", err)
	}
	if len(pages) == 0 {
		return errors.New("in-progress runs response is missing pages")
	}
	for _, page := range pages {
		if page.Runs == nil {
			return errors.New("in-progress runs response is missing workflow_runs")
		}
		for _, run := range page.Runs {
			if run.ID <= 0 {
				return errors.New("in-progress runs response contains an invalid run ID")
			}
		}
	}
	for _, page := range pages {
		for _, run := range page.Runs {
			args := []string{"run", "cancel", strconv.FormatInt(run.ID, 10)}
			if repo != "" {
				args = append(args, "--repo", repo)
			}
			cmd := workflow.ExecGHContext(ctx, args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("failed to cancel run %d: %w", run.ID, err)
			}
		}
	}
	return ctx.Err()
}
