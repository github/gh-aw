package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeModelRoutingFixture(t *testing.T, records ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "model-routing.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(records, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseModelRoutingSelectionAndCostAttribution(t *testing.T) {
	path := writeModelRoutingFixture(t,
		`{"_schema":"model-routing/v0.28.37","stage":"selection","objective":{"goal":"cost","mode":"auto"},"provider":"copilot","labels":{"task_type":"explain","scope":"local","task_complexity":"trivial"},"mode":"economy","classifier_model":"github-copilot/gpt-5.6-luna","classifier_effort":"high","classifier_attempts":1,"selected_id":"choice-0017","selected_model":"gpt-5.6-luna","selected_effort":"high","endpoint":"/responses","ranked_choices":[{"id":"choice-0017","model":"gpt-5.6-luna","effort":"high"},{"model":"gpt-5.4","effort":"medium"},{"model":"gpt-5.4-mini","effort":"low"},{"model":"ignored"}],"router":{"name":"gh-aw-router","version":"0.1.3"},"latency_ms":4900,"unknown_field":"ignored"}`,
		`{"_schema":"model-routing/v0.28.37","stage":"request","request_id":"selected-1","routed":"as_selected","outcome":"completed"}`,
		`{"_schema":"model-routing/v0.28.37","stage":"request","request_id":"deviated-1","routed":"deviated","deviations":["effort"],"requested_model":"gpt-5.6-luna","requested_effort":"low","outcome":"completed"}`,
		`{"_schema":"model-routing/v0.28.37","stage":"future-stage","new_field":true}`,
	)
	summary := parseModelRoutingFile(path, []TokenUsageEntry{
		{RequestID: "classifier-1", Purpose: "routing_classification", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 50}, AICreditsThisResponse: json.RawMessage("0.059")},
		{RequestID: "selected-1", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 100}, AICreditsThisResponse: json.RawMessage("0.4")},
		{RequestID: "deviated-1", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 75}, AICreditsThisResponse: json.RawMessage("0.3")},
	})

	if summary.Status != "selected" || summary.SelectedModel != "gpt-5.6-luna" || summary.SelectedEffort != "high" {
		t.Fatalf("unexpected route selection: %+v", summary)
	}
	if summary.Objective != "cost" || summary.ObjectiveMode != "auto" || summary.RouterVersion != "0.1.3" || summary.LatencyMs != 4900 {
		t.Fatalf("selection metadata missing: %+v", summary)
	}
	if len(summary.TopChoices) != 3 || summary.TopChoices[0].Model != "gpt-5.6-luna" {
		t.Fatalf("unexpected ranked choices: %+v", summary.TopChoices)
	}
	if summary.RoutedCounts["as_selected"] != 1 || summary.RoutedCounts["deviated"] != 1 ||
		len(summary.Deviations) != 1 || summary.Deviations[0].RequestedEffort != "low" {
		t.Fatalf("request outcomes or deviations missing: %+v", summary)
	}
	if summary.ClassifierCost.AIC != 0.059 || summary.SelectedModelCost.AIC != 0.4 || summary.DeviatedTrafficCost.AIC != 0.3 {
		t.Fatalf("costs were not attributed: classifier=%+v selected=%+v deviated=%+v",
			summary.ClassifierCost, summary.SelectedModelCost, summary.DeviatedTrafficCost)
	}
}

func TestParseModelRoutingFailureAndLegacyEndpointDeviation(t *testing.T) {
	t.Run("failure", func(t *testing.T) {
		path := writeModelRoutingFixture(t, `{"_schema":"model-routing/v0.28.37","stage":"failure","code":"no_route","detail":"no eligible model"}`)
		summary := parseModelRoutingFile(path, nil)
		if summary.Status != "failed" || summary.Failure == nil || summary.Failure.Code != "no_route" {
			t.Fatalf("failure record was not reported: %+v", summary)
		}
	})

	t.Run("legacy endpoint-only deviation", func(t *testing.T) {
		path := writeModelRoutingFixture(t,
			`{"_schema":"model-routing/v0.28.35","stage":"selection","selected_model":"claude-opus-5","selected_effort":"max"}`,
			`{"_schema":"model-routing/v0.28.35","stage":"request","request_id":"old-request","routed":"deviated","deviations":["endpoint"],"requested_model":"claude-opus-5","requested_effort":"max"}`,
		)
		summary := parseModelRoutingFile(path, []TokenUsageEntry{
			{RequestID: "old-request", AICreditsThisResponse: json.RawMessage("1.25")},
		})
		if !summary.EndpointOnlyDeviationNormalized || summary.RoutedCounts["as_selected"] != 1 ||
			summary.SelectedModelCost.AIC != 1.25 || summary.DeviatedTrafficCost.AIC != 0 {
			t.Fatalf("legacy endpoint-only deviation was not normalized: %+v", summary)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		summary := analyzeModelRouting(t.TempDir())
		if summary.Status != "not_routed" {
			t.Fatalf("expected missing routing file to be non-fatal and not_routed, got %+v", summary)
		}
	})
}

func TestModelRoutingComparisonDetectsRouteChanges(t *testing.T) {
	before := &AuditComparisonRoute{Model: "gpt-5.6-luna", Effort: "medium", Mode: "economy", RouterVersion: "0.1.2"}
	after := &AuditComparisonRoute{Model: "gpt-5.6-luna", Effort: "high", Mode: "economy", RouterVersion: "0.1.3"}
	if sameModelRoutingRoute(before, after) {
		t.Fatal("route changes should be detected even when the model is unchanged")
	}
	if !sameModelRoutingRoute(before, &AuditComparisonRoute{Model: "gpt-5.6-luna", Effort: "medium", Mode: "economy", RouterVersion: "0.1.2"}) {
		t.Fatal("identical routes should compare as equal")
	}
	comparison := buildAuditComparison("success",
		auditComparisonSnapshot{ModelRouting: after},
		&WorkflowRun{DatabaseID: 12},
		&auditComparisonSnapshot{ModelRouting: before},
	)
	if comparison.Classification.Label != "changed" || !comparison.Delta.ModelRouting.Changed {
		t.Fatalf("route-only change should be visible in comparison: %+v", comparison)
	}
}

func TestBuildModelRoutingLogsSummary(t *testing.T) {
	routing := &ModelRoutingSummary{
		Status: "selected", Labels: ModelRoutingLabels{TaskType: "explain", Scope: "local", TaskComplexity: "trivial"},
		Mode: "economy", SelectedModel: "gpt-5.6-luna", SelectedEffort: "high", RouterVersion: "0.1.3",
		RoutedCounts:   map[string]int{"as_selected": 3, "deviated": 1},
		ClassifierCost: ModelRoutingCost{AIC: 0.05}, SelectedModelCost: ModelRoutingCost{AIC: 0.60},
		DeviatedTrafficCost: ModelRoutingCost{AIC: 0.10},
	}
	summary := buildModelRoutingLogsSummary([]ProcessedRun{
		{ModelRouting: routing},
		{ModelRouting: routing},
	})
	if summary == nil || summary.TotalRequests != 8 || summary.DeviatedRequests != 2 ||
		summary.DeviatedTrafficShare != 0.25 || summary.ClassifierAIC != 0.1 {
		t.Fatalf("unexpected cross-run routing summary: %+v", summary)
	}
	if len(summary.Routes) != 1 || summary.Routes[0].RunCount != 2 ||
		summary.Routes[0].TotalAIC != 1.5 || summary.Routes[0].AverageAIC != 0.75 {
		t.Fatalf("unexpected route aggregate: %+v", summary.Routes)
	}
}

func TestModelRoutingTextReports(t *testing.T) {
	routing := &ModelRoutingSummary{
		Status: "failed", Objective: "cost", Failure: &ModelRoutingFailure{Code: "no_route", Detail: "no eligible choice"},
		ClassifierCost: ModelRoutingCost{AIC: 0.059}, SelectedModelCost: ModelRoutingCost{AIC: 0.4},
		DeviatedTrafficCost: ModelRoutingCost{AIC: 0.3},
	}
	oldStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = writer
	renderConsoleModelRouting(routing)
	writer.Close()
	os.Stderr = oldStderr
	var output bytes.Buffer
	if _, err := io.Copy(&output, reader); err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	for _, expected := range []string{"status=failed objective=cost", "failure: code=no_route", "classifier=0.059 selected_model=0.400 deviated=0.300"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("console routing report missing %q: %s", expected, output.String())
		}
	}

	var markdown bytes.Buffer
	renderMarkdownModelRoutingToWriter(&markdown, &ModelRoutingLogsSummary{
		ClassifierAIC: 0.059,
		Routes: []ModelRoutingRouteSummary{{
			TaskType: "explain", Scope: "local", Complexity: "trivial", Mode: "economy",
			Model: "gpt-5.6-luna", Effort: "high", RouterVersion: "0.1.3",
			RunCount: 1, TotalAIC: 0.76, AverageAIC: 0.76,
		}},
	})
	if !strings.Contains(markdown.String(), "gpt-5.6-luna:high") ||
		!strings.Contains(markdown.String(), "Classifier AIC: 0.059") {
		t.Fatalf("markdown routing report missing route mix or classifier cost: %s", markdown.String())
	}
}
