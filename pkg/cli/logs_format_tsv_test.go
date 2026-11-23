//go:build !integration

package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderLogsTSVSummaryPreservesTokenField(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := renderLogsTSVToWriter(&buf, LogsData{
		Summary: LogsSummary{
			TotalRuns:     2,
			TotalDuration: "8m0s",
			TotalTurns:    5,
			TotalErrors:   1,
		},
	})
	if err != nil {
		t.Fatalf("renderLogsTSVToWriter: %v", err)
	}
	output := buf.String()

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		t.Fatal("Expected TSV output")
	}
	if got, want := lines[0], "# 2 runs | 8m0s duration | n/a tokens | 5 turns | 1 errors"; got != want {
		t.Fatalf("Unexpected TSV summary line:\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderLogsTSVVerboseSummaryPreservesTokenField(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := renderLogsTSVVerboseToWriter(&buf, LogsData{
		Summary: LogsSummary{
			TotalRuns:           2,
			TotalDuration:       "8m0s",
			TotalTokens:         1500,
			TotalTurns:          5,
			TotalErrors:         1,
			TotalMissingTools:   2,
			TotalGitHubAPICalls: 3,
		},
	})
	if err != nil {
		t.Fatalf("renderLogsTSVVerboseToWriter: %v", err)
	}
	output := buf.String()

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		t.Fatal("Expected TSV verbose output")
	}
	if got, want := lines[0], "# 2 runs | 8m0s duration | 1500 tokens | 5 turns | 1 errors | 2 missing_tools | 3 github_api_calls"; got != want {
		t.Fatalf("Unexpected TSV verbose summary line:\n got: %q\nwant: %q", got, want)
	}
}
