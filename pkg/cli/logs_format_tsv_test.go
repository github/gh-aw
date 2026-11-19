//go:build !integration

package cli

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

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

func TestRenderLogsTSVDistinguishesUnavailableAICFromMeasuredZero(t *testing.T) {
	t.Parallel()
	data := LogsData{Runs: []RunData{{RunID: 1}, {RunID: 2, AIC: new(0.0)}, {RunID: 3, AIC: new(0.25)}}}
	for name, render := range map[string]func(*bytes.Buffer, LogsData){
		"compact": func(buf *bytes.Buffer, data LogsData) { renderLogsTSVToWriter(buf, data) },
		"verbose": func(buf *bytes.Buffer, data LogsData) { renderLogsTSVVerboseToWriter(buf, data) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			render(&buf, data)
			lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
			if len(lines) < 5 {
				t.Fatalf("Expected summary, header and three run rows, got:\n%s", buf.String())
			}
			column := slices.Index(strings.Split(lines[1], "\t"), "aic")
			if column < 0 {
				t.Fatalf("Expected aic column in header %q", lines[1])
			}
			for i, want := range []string{"-", "0.000", "0.250"} {
				if got := strings.Split(lines[i+2], "\t")[column]; got != want {
					t.Errorf("run %d aic = %q, want %q", i+1, got, want)
				}
			}
		})
	}
}
