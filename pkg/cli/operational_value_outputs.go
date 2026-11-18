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

var valueProducerFooter = regexp.MustCompile(`<!-- gh-aw-agentic-workflow: [^\r\n]*workflow_id: ([^,\r\n]+), run: (https://[^ \r\n]+/actions/runs/([0-9]+)) -->`)

func valueGitHubJSON(ctx context.Context, host, endpoint string, target any, fields ...string) error {
	args := append([]string{"api", "--hostname", host, "--method", "GET", endpoint}, fields...)
	output, err := workflow.ExecGHContext(ctx, args...).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("read %s: %w: %s", endpoint, err, strings.TrimSpace(string(exit.Stderr)))
		}
		return fmt.Errorf("read %s: %w", endpoint, err)
	}
	if err := json.Unmarshal(output, target); err != nil {
		return fmt.Errorf("decode %s: %w", endpoint, err)
	}
	return nil
}

func collectValueOutputs(ctx context.Context, definition valueMeasurementDefinition) ([]valueOutput, error) {
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
			"-f", fmt.Sprintf(`q=repo:%s is:pr "workflow_id: %s" in:body`, definition.Repository, definition.Workflow),
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
			output, err := parseValueOutput(item, definition)
			if err != nil {
				return nil, err
			}
			seen[item.Number] = true
			outputs = append(outputs, output)
		}
		if len(outputs) == total {
			break
		}
		if len(outputs) > total || len(response.Items) == 0 {
			return nil, fmt.Errorf("value-report search returned %d of %d pull requests", len(outputs), total)
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

func parseValueOutput(item valuePullRequest, definition valueMeasurementDefinition) (valueOutput, error) {
	matches := valueProducerFooter.FindAllStringSubmatch(item.Body, -1)
	if item.Number <= 0 || item.User.Login == "" || len(matches) != 1 || item.CreatedAt.IsZero() ||
		item.URL != fmt.Sprintf("https://%s/%s/pull/%d", definition.Host, definition.Repository, item.Number) {
		return valueOutput{}, fmt.Errorf("PR %d has duplicate or invalid producer evidence", item.Number)
	}
	match := matches[0]                                   //nolint:uncheckedsliceindex // Exactly one regexp match is required above.
	workflow, runURL, run := match[1], match[2], match[3] //nolint:uncheckedsliceindex // The constant regexp has exactly three captures.
	runID, err := strconv.ParseInt(run, 10, 64)
	if err != nil || runID <= 0 {
		return valueOutput{}, fmt.Errorf("PR %d has an invalid producer run ID", item.Number)
	}
	if workflow != definition.Workflow || runURL != fmt.Sprintf("https://%s/%s/actions/runs/%d", definition.Host, definition.Repository, runID) {
		return valueOutput{}, fmt.Errorf("PR %d has duplicate or invalid producer evidence", item.Number)
	}
	if !item.PullRequest.MergedAt.IsZero() && item.PullRequest.MergedAt.Before(item.CreatedAt) {
		return valueOutput{}, fmt.Errorf("PR %d merges before delivery", item.Number)
	}
	return valueOutput{
		PR: item.Number, URL: item.URL, RunID: runID, Delivered: item.CreatedAt, Merged: item.PullRequest.MergedAt,
	}, nil
}

func resolveValueMerge(ctx context.Context, definition valueMeasurementDefinition, output valueOutput) (string, error) {
	var pull struct {
		URL      string    `json:"html_url"`
		MergedAt time.Time `json:"merged_at"`
		SHA      string    `json:"merge_commit_sha"`
	}
	if err := valueGitHubJSON(ctx, definition.Host, fmt.Sprintf("repos/%s/pulls/%d", definition.Repository, output.PR), &pull); err != nil {
		return "", err
	}
	if pull.URL != output.URL || !pull.MergedAt.Equal(output.Merged) || !gitutil.IsValidFullSHA(pull.SHA) {
		return "", fmt.Errorf("PR %d has conflicting merge identity or clocks", output.PR)
	}
	return pull.SHA, nil
}
