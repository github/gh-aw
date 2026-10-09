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
		`{"_schema":"model-routing/v0.28.37","stage":"request","request_id":"unobserved-1","routed":"unobserved"}`,
		`{"_schema":"model-routing/v0.28.37","stage":"request","request_id":"unknown-1","routed":"unknown"}`,
		`{"_schema":"model-routing/v0.28.37","stage":"request","request_id":"empty-1","routed":""}`,
		`{"_schema":"model-routing/v0.28.37","stage":"future-stage","new_field":true}`,
	)
	summary := parseModelRoutingFile(path, []TokenUsageEntry{
		{RequestID: "classifier-1", Purpose: "routing_classification", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 50, OutputTokens: 10, CacheReadTokens: 5, CacheWriteTokens: 2}, AICreditsThisResponse: json.RawMessage("0.059")},
		{RequestID: "selected-1", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 10, CacheWriteTokens: 3}, AICreditsThisResponse: json.RawMessage("0.4")},
		{RequestID: "deviated-1", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 75, OutputTokens: 15, CacheReadTokens: 7, CacheWriteTokens: 1}, AICreditsThisResponse: json.RawMessage("0.3")},
		{RequestID: "unobserved-1", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 200}, AICreditsThisResponse: json.RawMessage("9")},
		{RequestID: "unknown-1", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 300}, AICreditsThisResponse: json.RawMessage("8")},
		{RequestID: "empty-1", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 400}, AICreditsThisResponse: json.RawMessage("7")},
		{RequestID: "unmatched", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 200}, AICreditsThisResponse: json.RawMessage("9")},
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
	if summary.ClassifierCost.Requests != 1 || summary.ClassifierCost.InputTokens != 50 || summary.ClassifierCost.OutputTokens != 10 ||
		summary.ClassifierCost.CacheReadTokens != 5 || summary.ClassifierCost.CacheWriteTokens != 2 {
		t.Errorf("classifier usage was not attributed: %+v", summary.ClassifierCost)
	}
	if summary.SelectedModelCost.Requests != 1 || summary.SelectedModelCost.InputTokens != 100 || summary.SelectedModelCost.OutputTokens != 20 ||
		summary.SelectedModelCost.CacheReadTokens != 10 || summary.SelectedModelCost.CacheWriteTokens != 3 {
		t.Errorf("selected-model usage was not attributed: %+v", summary.SelectedModelCost)
	}
	if summary.DeviatedTrafficCost.Requests != 1 || summary.DeviatedTrafficCost.InputTokens != 75 || summary.DeviatedTrafficCost.OutputTokens != 15 ||
		summary.DeviatedTrafficCost.CacheReadTokens != 7 || summary.DeviatedTrafficCost.CacheWriteTokens != 1 {
		t.Errorf("deviated usage was not attributed: %+v", summary.DeviatedTrafficCost)
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
		if summary != nil {
			t.Fatalf("expected missing routing file to be omitted, got %+v", summary)
		}
	})
}

func TestAnalyzeModelRoutingFromCompactUsageArtifact(t *testing.T) {
	runDir := t.TempDir()
	usageDir := filepath.Join(runDir, "usage", "agent")
	if err := os.MkdirAll(usageDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(usageDir, "model-routing.jsonl"), []byte(
		"{\"_schema\":\"model-routing/v0.28.37\",\"stage\":\"selection\",\"selected_model\":\"gpt-5.6-luna\"}\n"+
			"{\"_schema\":\"model-routing/v0.28.37\",\"stage\":\"request\",\"request_id\":\"usage-request\",\"routed\":\"as_selected\"}\n",
	), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(usageDir, "token_usage.jsonl"), []byte(
		"{\"request_id\":\"usage-request\",\"ai_credits_this_response\":0.4}\n",
	), 0600); err != nil {
		t.Fatal(err)
	}

	summary := analyzeModelRouting(runDir)
	if summary == nil || summary.SelectedModel != "gpt-5.6-luna" ||
		summary.SelectedModelCost.Requests != 1 || summary.SelectedModelCost.AIC != 0.4 {
		t.Fatalf("compact usage artifact routing was not analyzed: %+v", summary)
	}
}

func TestApplyAwInfoModelRoutingAddsEndpointMetadata(t *testing.T) {
	runDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(runDir, "aw_info.json"), []byte(
		`{"model_routing":{"status":"selected","endpoint":"/v1/messages","selected_endpoint":"/chat/completions"}}`,
	), 0o600); err != nil {
		t.Fatal(err)
	}
	routing := &ModelRoutingSummary{Status: "selected", Endpoint: "/chat/completions"}

	result := applyAwInfoModelRouting(routing, runDir)
	if result == routing || result.Endpoint != "/chat/completions" ||
		result.EffectiveEndpoint != "/v1/messages" || result.SelectedEndpoint != "/chat/completions" {
		t.Fatalf("aw_info endpoint metadata was not merged into the AWF routing summary: %+v", result)
	}
}

func TestBuildAuditComparisonCandidateFromLegacySummary(t *testing.T) {
	runDir := t.TempDir()
	routingDir := filepath.Join(runDir, "sandbox", "firewall", "logs", "api-proxy-logs")
	if err := os.MkdirAll(routingDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(routingDir, "model-routing.jsonl"), []byte(
		"{\"_schema\":\"model-routing/v0.28.37\",\"stage\":\"selection\",\"selected_model\":\"gpt-5.6-luna\",\"selected_effort\":\"high\"}\n",
	), 0600); err != nil {
		t.Fatal(err)
	}

	candidate := buildAuditComparisonCandidateFromSummary(&RunSummary{}, runDir)
	if candidate.Snapshot.ModelRouting == nil || candidate.Snapshot.ModelRouting.Model != "gpt-5.6-luna" ||
		candidate.Snapshot.ModelRouting.Effort != "high" {
		t.Fatalf("legacy summary did not recover routing from its artifact: %+v", candidate.Snapshot.ModelRouting)
	}
}

func TestResolveEffectiveModelAttributionSuppressesUnselectedRouteModels(t *testing.T) {
	info := &AwInfo{
		Model: "auto",
		ModelRouting: &AwInfoModelRouting{
			Status: "failed",
		},
	}
	usage := &TokenUsageSummary{
		ByModel: map[string]*ModelTokenUsage{
			"router-classifier": {AIC: 1},
		},
	}

	attribution := resolveEffectiveModelAttribution(info, nil, usage)
	if attribution.Model != "" {
		t.Fatalf("failed routing must not attribute classifier usage as the agent model: %+v", attribution)
	}

	legacyInfo := &AwInfo{Model: "configured-alias"}
	attribution = resolveEffectiveModelAttribution(legacyInfo, nil, usage)
	if attribution.Model != "configured-alias" {
		t.Fatalf("non-routed workflows must retain configured model attribution: %+v", attribution)
	}
}

func TestAwInfoModelRoutingPreservesSelectedEndpoint(t *testing.T) {
	var info AwInfo
	if err := json.Unmarshal([]byte(`{"model_routing":{"status":"selected","endpoint":"/v1/messages","selected_endpoint":"/chat/completions"}}`), &info); err != nil {
		t.Fatalf("failed to unmarshal model routing metadata: %v", err)
	}
	if info.ModelRouting == nil || info.ModelRouting.Endpoint != "/v1/messages" || info.ModelRouting.SelectedEndpoint != "/chat/completions" {
		t.Fatalf("model routing endpoints were not preserved: %+v", info.ModelRouting)
	}
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
		Endpoint: "/chat/completions", EffectiveEndpoint: "/v1/messages", SelectedEndpoint: "/chat/completions",
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
	if summary.Routes[0].Endpoint != "/chat/completions" ||
		summary.Routes[0].EffectiveEndpoint != "/v1/messages" ||
		summary.Routes[0].SelectedEndpoint != "/chat/completions" {
		t.Fatalf("route endpoint metadata was not retained: %+v", summary.Routes[0])
	}
}

func TestModelRoutingTextReports(t *testing.T) {
	routing := &ModelRoutingSummary{
		Status: "failed", Objective: "cost", Failure: &ModelRoutingFailure{Code: "no_route", Detail: "no eligible choice"},
		Endpoint: "/chat/completions", EffectiveEndpoint: "/v1/messages", SelectedEndpoint: "/chat/completions",
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
	for _, expected := range []string{"status=failed objective=cost", "endpoint=/chat/completions effective_endpoint=/v1/messages selected_endpoint=/chat/completions", "failure: code=no_route", "classifier=0.059 selected_model=0.400 deviated=0.300"} {
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
