package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/workflow"
)

type valueOutput struct {
	PR        int       `json:"pr"`
	URL       string    `json:"url"`
	RunID     int64     `json:"run_id"`
	Delivered time.Time `json:"delivered"`
	Merged    time.Time `json:"merged,omitzero"`
	SHA       string    `json:"merge_sha,omitempty"`
	Files     []string  `json:"changed_files,omitempty"`
	base      string    // Verified whole-PR baseline, not necessarily inferable from a linear merge SHA.
}

type valuePullRequest struct {
	Number    int       `json:"number"`
	URL       string    `json:"html_url"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	PullRequest struct {
		MergedAt time.Time `json:"merged_at"`
	} `json:"pull_request"`
}

var valueProducerFooter = regexp.MustCompile(`(?m)^<!-- gh-aw-agentic-workflow: [^\r\n]*, workflow_id: ([^,\r\n]+), run: (https://[^ \r\n]+/actions/runs/([0-9]+)) -->\r?$`)

func valueGitHubJSON(ctx context.Context, host, endpoint string, target any, fields ...string) error {
	args := append([]string{"api", "--hostname", host, "--method", "GET", endpoint}, fields...)
	output, err := valueCommandOutput(workflow.ExecGHContext(ctx, args...), valueCommandLimit)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("read %s: %w: %s", endpoint, err, valueDisplayText(strings.TrimSpace(string(exit.Stderr))))
		}
		return fmt.Errorf("read %s: %w", endpoint, err)
	}
	if err := json.Unmarshal(output, target); err != nil {
		return fmt.Errorf("decode %s: %w", endpoint, err)
	}
	return nil
}

func collectValueOutputs(ctx context.Context, definition valueMeasurementDefinition, recipes ...*valueRecipe) ([]valueOutput, error) {
	var recipe *valueRecipe
	if len(recipes) > 0 {
		recipe = recipes[0]
	}
	query := fmt.Sprintf(`q=repo:%s is:pr "workflow_id: %s" in:body`, definition.Repository, definition.Workflow)
	if recipe != nil && recipe.Author != "" {
		query += " author:" + recipe.Author
	}
	outputs := []valueOutput{}
	seen := make(map[int]bool)
	total := -1
	for page := 1; ; page++ {
		var response struct {
			TotalCount int                `json:"total_count"`
			Incomplete bool               `json:"incomplete_results"`
			Items      []valuePullRequest `json:"items"`
		}
		err := valueGitHubJSON(ctx, definition.Host, "search/issues", &response,
			"-f", query,
			"-f", "per_page=100", "-f", fmt.Sprintf("page=%d", page), "-f", "sort=created", "-f", "order=asc")
		if err != nil {
			return nil, err
		}
		if response.Incomplete || response.TotalCount > 1000 || total >= 0 && total != response.TotalCount {
			return nil, errors.New("incomplete or changing value-report PR search; retry with a stable cohort (maximum 1000 results)")
		}
		total = response.TotalCount
		for _, item := range response.Items {
			if seen[item.Number] {
				return nil, fmt.Errorf("PR %d has duplicate or invalid producer evidence", item.Number)
			}
			seen[item.Number] = true // Pagination counts search hits, not verified producers.
			output, err := parseValueOutput(item, definition, recipe)
			if err != nil {
				return nil, err
			}
			if output != nil {
				outputs = append(outputs, *output)
			}
		}
		if len(seen) == total {
			break
		}
		if len(seen) > total || len(response.Items) == 0 {
			return nil, fmt.Errorf("value-report search returned %d of %d pull requests", len(seen), total)
		}
	}
	slices.SortFunc(outputs, func(a, b valueOutput) int {
		if comparison := a.Delivered.Compare(b.Delivered); comparison != 0 {
			return comparison
		}
		return a.PR - b.PR
	})
	return outputs, nil
}

func parseValueOutput(item valuePullRequest, definition valueMeasurementDefinition, recipe *valueRecipe) (*valueOutput, error) {
	matches := valueProducerFooter.FindAllStringSubmatch(item.Body, -1)
	runBase := fmt.Sprintf("https://%s/%s/actions/runs/", definition.Host, definition.Repository)
	if !slices.ContainsFunc(matches, func(m []string) bool {
		return m[1] == definition.Workflow && m[2] == runBase+m[3] //nolint:uncheckedsliceindex // The regexp has exactly three captures.
	}) {
		return nil, nil // A text match is not a producer.
	}
	if item.Number <= 0 || item.User.Login == "" || len(matches) != 1 || item.CreatedAt.IsZero() ||
		item.URL != fmt.Sprintf("https://%s/%s/pull/%d", definition.Host, definition.Repository, item.Number) {
		return nil, fmt.Errorf("PR %d has duplicate or invalid producer evidence", item.Number)
	}
	if recipe != nil && recipe.AuthorLogin != "" && item.User.Login != recipe.AuthorLogin {
		return nil, fmt.Errorf("PR %d does not match recipe author_login", item.Number)
	}
	match := matches[0] //nolint:uncheckedsliceindex // Exactly one regexp match is required above.
	run := match[3]     //nolint:uncheckedsliceindex // The constant regexp has exactly three captures.
	runID, err := strconv.ParseInt(run, 10, 64)
	if err != nil || runID <= 0 || run != strconv.FormatInt(runID, 10) {
		return nil, fmt.Errorf("PR %d has an invalid producer run ID", item.Number)
	}
	if !item.PullRequest.MergedAt.IsZero() && item.PullRequest.MergedAt.Before(item.CreatedAt) {
		return nil, fmt.Errorf("PR %d merges before delivery", item.Number)
	}
	return &valueOutput{
		PR: item.Number, URL: item.URL, RunID: runID, Delivered: item.CreatedAt, Merged: item.PullRequest.MergedAt,
	}, nil
}

func resolveValueMerge(git valueGit, definition valueMeasurementDefinition, output *valueOutput) error {
	var pull struct {
		URL      string    `json:"html_url"`
		MergedAt time.Time `json:"merged_at"`
		SHA      string    `json:"merge_commit_sha"`
		Commits  int       `json:"commits"`
	}
	if err := valueGitHubJSON(git.ctx, definition.Host, fmt.Sprintf("repos/%s/pulls/%d", definition.Repository, output.PR), &pull); err != nil {
		return err
	}
	if pull.URL != output.URL || !pull.MergedAt.Equal(output.Merged) || !gitutil.IsValidFullSHA(pull.SHA) {
		return fmt.Errorf("PR %d has conflicting merge identity or clocks", output.PR)
	}
	result, err := git.run("rev-list", "--parents", "-1", pull.SHA)
	if err != nil {
		return err
	}
	output.SHA = pull.SHA
	output.base, output.Files = "", nil
	parents := strings.Fields(result)
	// A single-parent tip of a multi-commit PR could be the last rebased commit.
	// Without proof of the whole delta, do not treat that parent as the PR baseline.
	if len(parents) == 3 || (len(parents) == 2 && pull.Commits == 1) {
		output.base = parents[1] //nolint:uncheckedsliceindex // Both supported shapes include a first parent.
	}
	return nil
}
