package workflowcontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDailyPerformanceSummaryUsesStableWindowMetrics verifies that the daily
// performance prompt keeps updatedAt pagination separate from lifecycle metrics.
// The tokens below are the stable contract surface; changing them requires
// updating both the workflow source and this test.
func TestDailyPerformanceSummaryUsesStableWindowMetrics(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "daily-performance-summary.md"))
	if err != nil {
		t.Fatalf("failed to read daily performance summary workflow source: %v", err)
	}
	text := string(content)

	for _, token := range []string{
		"window_start",
		"createdAt >= window_start",
		"mergedAt >= window_start",
		"closedAt >= window_start",
		"github-pr-query with state: \"all\", since: \"<window_start>\", jq: \".\"",
		"github-pr-query with state: \"open\", limit: 1000, jq: \".\"",
		"github-issue-query with state: \"all\", since: \"<window_start>\", jq: \".\"",
		"github-issue-query with state: \"open\", limit: 1000, jq: \".\"",
		"/tmp/gh-aw/python/data/open_prs.json",
		"/tmp/gh-aw/python/data/open_issues.json",
		"open_prs = load_json_data",
		"open_issues = load_json_data",
		"pd.to_datetime(pr_df['mergedAt'], utc=True)",
		"pd.to_datetime(issue_df['closedAt'], utc=True)",
		"pr_df['createdAt'] >= ninety_days_ago",
		"(pr_df['mergedAt'] >= ninety_days_ago)",
		"issue_df['createdAt'] >= ninety_days_ago",
		"(issue_df['closedAt'] >= ninety_days_ago)",
	} {
		if !strings.Contains(text, token) {
			t.Errorf("expected daily-performance-summary.md to contain token %q", token)
		}
	}
}

func TestGitHubQueryScriptsUseFileBackedPagination(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "shared", "github-queries-mcp-script.md"))
	if err != nil {
		t.Fatalf("failed to read GitHub query MCP scripts: %v", err)
	}
	text := string(content)

	if strings.Contains(text, "--argjson all") {
		t.Error("query scripts must not pass accumulated API data through command arguments")
	}
	if count := strings.Count(text, `jq -s '.[0] + .[1]'`); count != 3 {
		t.Errorf("expected all three paginated query scripts to merge data through files, got %d", count)
	}
}
