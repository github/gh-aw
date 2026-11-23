package workflowcontract

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
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
		"github-pr-query with state: \"all\", since: \"<batch_start>\", until: \"<batch_end>\", limit: 1000, jq: \"[.[] | {number, author, createdAt, updatedAt, mergedAt, closedAt, reviewDecision, labels}]\"",
		"github-pr-query with state: \"open\", limit: 1000, jq: \".\"",
		"github-issue-query with state: \"all\", since: \"<batch_start>\", until: \"<batch_end>\", limit: 1000, jq: \"[.[] | {number, author, createdAt, updatedAt, closedAt, labels}]\"",
		"combine the results by item number",
		"split that batch into smaller ranges",
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
	parts := strings.SplitN(string(content), "---", 3)
	if len(parts) != 3 {
		t.Fatal("query scripts must have YAML frontmatter")
	}
	var config struct {
		Scripts map[string]struct {
			Run string `yaml:"run"`
		} `yaml:"mcp-scripts"`
	}
	if err := yaml.Unmarshal([]byte(parts[1]), &config); err != nil {
		t.Fatalf("failed to parse GitHub query MCP scripts: %v", err)
	}

	fileMerge := regexp.MustCompile(`jq -s '\.\[0\] \+ \.\[1\]' "\$[A-Z_]+_FILE" "\$[A-Z_]+_FILE" > "\$[A-Z_]+_FILE"`)
	for _, name := range []string{"github-issue-query", "github-pr-query", "github-discussion-query"} {
		t.Run(name, func(t *testing.T) {
			run := config.Scripts[name].Run
			if run == "" {
				t.Fatal("missing query script run body")
			}
			if strings.Contains(run, "--argjson all") {
				t.Error("query script must not pass accumulated API data through command arguments")
			}
			if !fileMerge.MatchString(run) {
				t.Error("query script must merge paginated data from input files into an output file")
			}
		})
	}
}
