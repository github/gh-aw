//go:build !integration

package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRenderLogsTSVRowsAndAnnotations(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	renderLogsTSVToWriter(&buf, LogsData{
		Summary: LogsSummary{TotalRuns: 1, TotalDuration: "1m", TotalTokens: 12},
		Runs: []RunData{{
			RunID: 7, WorkflowName: "report", EngineID: "copilot", Status: "queued",
			TokenUsage: 12, AIC: 0.125, Turns: 2, ErrorCount: 1, Event: "push", Branch: "main",
			CreatedAt: time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC),
			URL:       "https://github.com/owner/repo/actions/runs/7",
		}},
		Observability: []ObservabilityInsight{{Severity: "warning", Title: "title", Summary: "summary"}},
	})
	expected := "# 1 runs | 1m duration | 12 tokens | 0 turns | 0 errors\n" +
		"run_id\tworkflow\tengine\tstatus\tduration\ttokens\taic\tturns\terrors\tevent\tbranch\tcreated_at\tclassification\turl\n" +
		"7\treport\tcopilot\tqueued\t-\t12\t0.125\t2\t1\tpush\tmain\t2026-01-02T03:04\t-\t/actions/runs/7\n" +
		"# insights:\n# [warning] title: summary\n"
	if got := buf.String(); got != expected {
		t.Fatalf("Unexpected TSV output:\n got: %q\nwant: %q", got, expected)
	}
}

func TestRenderLogsTSVVerboseRowFallbacks(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	renderLogsTSVVerboseToWriter(&buf, LogsData{
		Runs: []RunData{{
			RunID: 7, Status: "queued", AIC: 0.125, ActionMinutes: 1.4,
			CreatedAt:    time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC),
			DisplayTitle: strings.Repeat("x", 60), URL: "https://github.com/owner/repo/actions/runs/7",
		}},
	})
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	fields := strings.Split(lines[2], "\t")
	if len(fields) != 22 || fields[3] != "queued" || fields[4] != "-" ||
		fields[17] != "-" || fields[18] != "-" || fields[19] != "1" ||
		fields[21] != "https://github.com/owner/repo/actions/runs/7" {
		t.Fatalf("Unexpected verbose fields: %#v", fields)
	}
	if fields[20] == strings.Repeat("x", 60) {
		t.Fatal("Display title was not truncated")
	}
}

func TestRenderLogsTSVSummaryPreservesTokenField(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	renderLogsTSVToWriter(&buf, LogsData{
		Summary: LogsSummary{
			TotalRuns:     2,
			TotalDuration: "8m0s",
			TotalTurns:    5,
			TotalErrors:   1,
		},
	})
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
	renderLogsTSVVerboseToWriter(&buf, LogsData{
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
	output := buf.String()

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		t.Fatal("Expected TSV verbose output")
	}
	if got, want := lines[0], "# 2 runs | 8m0s duration | 1500 tokens | 5 turns | 1 errors | 2 missing_tools | 3 github_api_calls"; got != want {
		t.Fatalf("Unexpected TSV verbose summary line:\n got: %q\nwant: %q", got, want)
	}
}
