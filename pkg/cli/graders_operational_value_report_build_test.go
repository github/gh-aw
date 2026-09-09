package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildOperationalValueReportIncludesEveryPointAndDeduplicatesWeeklyMean(t *testing.T) {
	baseline := 0.25
	evaluator := &operationalValueReportEvaluator{
		WorkflowID: "daily-file-diet", EvaluatorDigest: strings.Repeat("a", 64),
		Definition: operationalValueReportDefinition{
			Repository: "github/gh-aw", WorkflowName: "Daily File Diet", SourcePath: ".github/workflows/daily-file-diet.md",
			OperationalValue: "Improve the assigned file.", Adoption: operationalValueReportAdoption{AdoptedAt: "2026-08-01T00:00:00Z"},
			DiagnosticMetrics: []operationalValueReportDiagnosticMetric{{ID: "repository-health", Name: "Repository health", Formula: "health", Direction: "higher_is_better", Aggregation: "latest"}},
			Baseline:          operationalValueReportBaseline{Mode: "baseline-comparable", Value: &baseline},
			Raw:               json.RawMessage(`{"schemaVersion":4,"grader":"operational-value"}`),
		},
	}
	valueOne, valueTwo, valueThree := 0.2, 0.6, 0.8
	observations := []operationalValueReportObservation{
		{Run: operationalValueReportRun{ID: "1", CreatedAt: time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)}, Value: &valueOne, Status: "pass", OpportunityKey: "issue:1", Mature: true, Diagnostics: map[string]any{"repository-health": 0.3}},
		{Run: operationalValueReportRun{ID: "2", CreatedAt: time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)}, Value: &valueTwo, Status: "pass", OpportunityKey: "issue:1", Mature: true, Diagnostics: map[string]any{"repository-health": 0.4}},
		{Run: operationalValueReportRun{ID: "3", CreatedAt: time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)}, Value: &valueThree, Status: "pass", OpportunityKey: "issue:2", Mature: true, Diagnostics: map[string]any{"repository-health": 0.7}},
	}

	report := buildOperationalValueReport(evaluator, observations, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC), operationalValueReportBackfillStats{CacheHits: 2, Evaluated: 1})
	require.Len(t, report.Observations, 3)
	assert.Equal(t, "github/gh-aw:daily-file-diet:1:0:"+strings.Repeat("a", 64), report.Observations[0].ID)
	require.Len(t, report.Weekly, 1)
	assert.Equal(t, "2026-08-01T00:00:00Z", report.Window.StartAt)
	assert.Equal(t, 2, report.Weekly[0].DistinctOpportunityCount)
	assert.InDelta(t, 0.7, *report.Weekly[0].Mean, 0.000001)
	assert.Equal(t, 1, report.Coverage.DuplicateOpportunityCount)
	assert.InDelta(t, 0.55, *report.Summary.LatestDeltaFromBaseline, 0.000001)
	require.Len(t, report.Diagnostics, 1)
	assert.InDelta(t, 0.7, *report.Diagnostics[0].Weekly[0].Value, 0.000001)
	assert.InDelta(t, 0.7, *report.Diagnostics[0].Summary.Latest, 0.000001)
}

func TestRenderOperationalValueReportArtifactsContainRichReport(t *testing.T) {
	value, diagnosticFirst, diagnosticValue, diagnosticChange := 0.75, 0.4, 0.9, 0.5
	report := operationalValueReport{
		SchemaVersion: 1, WorkflowID: "example", WorkflowName: "Example <Workflow>", Repository: "owner/repo",
		Window:    operationalValueReportWindow{StartAt: "2026-08-01T00:00:00Z", EndAt: "2026-08-31T00:00:00Z"},
		Evaluator: operationalValueReportEvaluatorReference{SHA256: strings.Repeat("a", 64), Definition: json.RawMessage(`{"evidence":{},"primaryMetric":{}}`)},
		Coverage:  operationalValueReportCoverage{RunCount: 1, NumericCount: 1}, Summary: operationalValueReportSummary{Latest: &value},
		Weekly: []operationalValueReportWeek{
			{
				WeekStart: "2026-08-03T00:00:00Z", WeekEnd: "2026-08-10T00:00:00Z", RunCount: 1,
				NumericCount: 1, DistinctOpportunityCount: 1, Mean: &value, Minimum: &value, Maximum: &value,
			},
			{
				WeekStart: "2026-08-10T00:00:00Z", WeekEnd: "2026-08-17T00:00:00Z", RunCount: 1,
				NumericCount: 1, DistinctOpportunityCount: 1, Mean: &diagnosticFirst, Minimum: &diagnosticFirst, Maximum: &diagnosticFirst,
			},
		},
		Diagnostics: []operationalValueReportDiagnosticSeries{{
			Metric:  operationalValueReportDiagnosticMetric{ID: "health", Name: "Repository health", Formula: "healthy / total", Direction: "higher_is_better", Aggregation: "latest"},
			Summary: operationalValueReportSummary{First: &diagnosticFirst, Latest: &diagnosticValue, Change: &diagnosticChange},
			Weekly: []operationalValueReportDiagnosticWeek{
				{WeekStart: "2026-08-03T00:00:00Z", WeekEnd: "2026-08-10T00:00:00Z", NumericCount: 1, Value: &diagnosticFirst},
				{WeekStart: "2026-08-10T00:00:00Z", WeekEnd: "2026-08-17T00:00:00Z", NumericCount: 1, Value: &diagnosticValue},
			},
		}},
		Observations: []operationalValueReportObservation{{Run: operationalValueReportRun{ID: "1", CreatedAt: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)}, Value: &value}},
	}
	svg := string(renderOperationalValueReportSVG(report))
	assert.Contains(t, svg, "Example &lt;Workflow&gt;")
	assert.Contains(t, svg, "Repository health improved since adoption")
	assert.Contains(t, svg, "40.0% → 90.0%")
	assert.Contains(t, svg, "+50.0 pts since adoption")
	assert.Contains(t, svg, "Outcome change since adoption")
	assert.Contains(t, svg, "Percentage-point change from each metric's first observed value")
	assert.Contains(t, svg, "0 pts = first observed value")
	assert.Contains(t, svg, ">+50 pts</text>")
	assert.Contains(t, svg, ">-50 pts</text>")
	assert.Contains(t, svg, "class=\"gain-area\"")
	assert.NotContains(t, svg, "class=\"loss-area\"")
	assert.Contains(t, svg, "class=\"metric-change endpoint-label-1\">+50.0 pts</text>")
	assert.Contains(t, svg, "Per-run operational attainment")
	assert.Contains(t, svg, "4-week rolling mean (bold)")
	assert.Contains(t, svg, "class=\"primary-trend\"")
	assert.Contains(t, svg, "4-week avg 57.5%")
	markdown := string(renderOperationalValueReportMarkdown(report, "example.json", "example.svg"))
	assert.Contains(t, markdown, "![Example <Workflow> operational value timeline](example.svg)")
	assert.Contains(t, markdown, "Weekly History")
	assert.Contains(t, markdown, "Diagnostic, Repository health")
	assert.Contains(t, markdown, "| 2026-08-03 | 1 | 1 | 0.75 | 0.75-0.75 | 0.4 |")

	decliningLatest, decliningChange := 0.2, -0.2
	report.Diagnostics[0].Summary.Latest = &decliningLatest
	report.Diagnostics[0].Summary.Change = &decliningChange
	report.Diagnostics[0].Weekly[1].Value = &decliningLatest
	decliningSVG := string(renderOperationalValueReportSVG(report))
	assert.NotContains(t, decliningSVG, "class=\"gain-area\"")
	assert.Contains(t, decliningSVG, "class=\"loss-area\"")
	assert.Contains(t, decliningSVG, "class=\"metric-change endpoint-label-1\">-20.0 pts</text>")
}

func TestRenderOperationalValueReportSVGWithoutDiagnostics(t *testing.T) {
	first, latest, change := 0.25, 0.75, 0.5
	report := operationalValueReport{
		WorkflowName: "Example Workflow",
		Window:       operationalValueReportWindow{StartAt: "2026-08-03T00:00:00Z", EndAt: "2026-08-17T00:00:00Z"},
		Evaluator:    operationalValueReportEvaluatorReference{SHA256: "abcdef1234567890"},
		Summary:      operationalValueReportSummary{First: &first, Latest: &latest, Change: &change},
		Weekly: []operationalValueReportWeek{
			{WeekStart: "2026-08-03T00:00:00Z", WeekEnd: "2026-08-10T00:00:00Z", Mean: &first},
			{WeekStart: "2026-08-10T00:00:00Z", WeekEnd: "2026-08-17T00:00:00Z", Mean: &latest},
		},
	}

	svg := string(renderOperationalValueReportSVG(report))
	assert.Contains(t, svg, "Operational attainment improved since adoption")
	assert.Contains(t, svg, "25.0% → 75.0%")
	assert.Contains(t, svg, "class=\"primary\"")
	assert.NotContains(t, svg, "Repository outcome health")
	assert.NotContains(t, svg, "Per-run operational attainment")
}
