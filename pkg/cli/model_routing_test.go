package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
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
	if err := os.WriteFile(filepath.Join(runDir, "usage", "aw_session.jsonl"), []byte(
		"{\"type\":\"firewall.token_usage\",\"data\":{\"requestId\":\"usage-request\",\"aic\":0.4,\"usage\":{\"inputTokens\":1,\"outputTokens\":2}},\"timestamp\":\"2026-10-08T10:00:00Z\",\"provenance\":{\"component\":\"firewall\",\"phase\":\"agent\"}}\n",
	), 0600); err != nil {
		t.Fatal(err)
	}

	summary := analyzeModelRouting(runDir)
	if summary == nil || summary.SelectedModel != "gpt-5.6-luna" ||
		summary.SelectedModelCost.Requests != 1 || summary.SelectedModelCost.AIC != 0.4 {
		t.Fatalf("compact usage artifact routing was not analyzed: %+v", summary)
	}
}

func TestAnalyzeModelRoutingFromSessionWithoutRawFile(t *testing.T) {
	runDir := t.TempDir()
	sessionDir := filepath.Join(runDir, "usage")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "aw_session.jsonl"), []byte(
		`{"type":"workflow.info","data":{"modelRouting":{"status":"selected","wireModel":"claude-sonnet-5","model":"claude-sonnet-5","effort":"medium"}},"provenance":{"component":"workflow","phase":"conclusion","path":"usage/aw_info.json"}}`+"\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}

	summary := analyzeModelRouting(runDir)
	if summary == nil || summary.Status != "selected" || summary.SelectedModel != "claude-sonnet-5" ||
		summary.SelectedEffort != "medium" {
		t.Fatalf("session-only routing was not used when the raw routing file is absent: %+v", summary)
	}
}

func TestAnalyzeRoutedClassifierUsageFromUnifiedSession(t *testing.T) {
	for _, test := range []struct {
		name   string
		legacy bool
		rawLog bool
	}{
		{name: "current unified fields", rawLog: false},
		{name: "legacy fields with raw proxy fallback", legacy: true, rawLog: true},
		{name: "legacy fields with model-routing timing fallback", legacy: true, rawLog: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			runDir := writeRoutedUnifiedAuditFixture(t, test.legacy, test.rawLog, "/responses", "/responses")
			usageEntries, err := readUnifiedTokenUsageEntries(runDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(usageEntries) != 3 || usageEntries[0].Purpose != "routing_classification" ||
				usageEntries[0].Path != "/responses" || usageEntries[0].XInitiator != "agent" {
				t.Fatalf("unified usage metadata was not preserved or recovered: %+v", usageEntries)
			}
			routing := analyzeModelRouting(runDir)
			if routing == nil || routing.ClassifierCost.Requests != 1 || math.Abs(routing.ClassifierCost.AIC-0.323) > 0.000001 {
				t.Fatalf("classifier cost was not attributed: %+v", routing)
			}
			total := routing.ClassifierCost.AIC + routing.SelectedModelCost.AIC + routing.DeviatedTrafficCost.AIC
			if math.Abs(total-17.341) > 0.000001 {
				t.Fatalf("routed cost buckets total %.3f, want proxy total 17.341", total)
			}

			summary := &TokenUsageSummary{AICFound: true, TotalAIC: 17.341}
			augmentSubagentModelAttribution(runDir, summary)
			if len(summary.Warnings) != 0 {
				t.Fatalf("per-agent reconciliation emitted warnings: %v", summary.Warnings)
			}
		})
	}
}

func TestReconcileRoutedClaudeClassifierNamesAgentEndpoint(t *testing.T) {
	runDir := writeRoutedUnifiedAuditFixture(t, true, true, "/chat/completions", "/v1/messages")
	summary := &TokenUsageSummary{AICFound: true, TotalAIC: 17.401}
	augmentSubagentModelAttribution(runDir, summary)
	if len(summary.Warnings) != 1 {
		t.Fatalf("expected one reconciliation warning, got %v", summary.Warnings)
	}
	if !strings.Contains(summary.Warnings[0], "/v1/messages") || strings.Contains(summary.Warnings[0], "/chat/completions") {
		t.Fatalf("warning used classifier endpoint instead of agent endpoint: %s", summary.Warnings[0])
	}
}

func writeRoutedUnifiedAuditFixture(t *testing.T, legacy, includeRaw bool, classifierPath, agentPath string) string {
	t.Helper()
	runDir := t.TempDir()
	usageDir := filepath.Join(runDir, "usage", "agent")
	if err := os.MkdirAll(usageDir, 0700); err != nil {
		t.Fatal(err)
	}
	routingRecords := []map[string]any{
		{"_schema": "model-routing/v0.28.44", "stage": "classification", "attempt": 1, "classifier_model": "github-copilot/claude-sonnet-5", "endpoint": classifierPath, "x_initiator": "agent"},
		{"_schema": "model-routing/v0.28.44", "stage": "selection", "classifier_model": "github-copilot/claude-sonnet-5", "classifier_attempts": 1, "selected_model": "claude-sonnet-5", "endpoint": agentPath},
		{"_schema": "model-routing/v0.28.44", "stage": "request", "request_id": "main-1", "routed": "as_selected"},
		{"_schema": "model-routing/v0.28.44", "stage": "request", "request_id": "subagent-1", "routed": "deviated"},
	}
	writeAuditJSONLines(t, filepath.Join(usageDir, "model-routing.jsonl"), routingRecords...)

	routingEvents := []map[string]any{
		{"type": "firewall.model_routing", "data": routingRecords[0], "timestamp": "2026-10-08T00:00:00Z", "provenance": map[string]string{"component": "firewall", "phase": "agent"}},
		{"type": "firewall.token_usage", "data": routedUsageEvent("classifier-1", "claude-sonnet-5", 0.323, classifierPath, "routing_classification", legacy), "timestamp": "2026-10-08T00:00:00.200Z", "provenance": map[string]string{"component": "firewall", "phase": "agent"}},
		{"type": "firewall.model_routing", "data": routingRecords[1], "timestamp": "2026-10-08T00:00:00.500Z", "provenance": map[string]string{"component": "firewall", "phase": "agent"}},
		{"type": "firewall.model_routing", "data": routingRecords[2], "timestamp": "2026-10-08T00:00:00.600Z", "provenance": map[string]string{"component": "firewall", "phase": "agent"}},
		{"type": "firewall.model_routing", "data": routingRecords[3], "timestamp": "2026-10-08T00:00:00.700Z", "provenance": map[string]string{"component": "firewall", "phase": "agent"}},
		{"type": "firewall.token_usage", "data": routedUsageEvent("main-1", "claude-sonnet-5", 15.227, agentPath, "agent", legacy), "timestamp": "2026-10-08T00:00:01Z", "provenance": map[string]string{"component": "firewall", "phase": "agent"}},
		{"type": "firewall.token_usage", "data": routedUsageEvent("subagent-1", "claude-haiku-4.5", 1.791, agentPath, "subagent", legacy), "timestamp": "2026-10-08T00:00:02Z", "provenance": map[string]string{"component": "firewall", "phase": "agent"}},
		{"type": "session.shutdown", "data": map[string]any{"agentMetrics": map[string]any{
			"main":  map[string]any{"totalNanoAiu": 15_227_000_000, "modelMetrics": map[string]any{"claude-sonnet-5": map[string]any{"requests": map[string]int{"count": 1}, "usage": map[string]int{"inputTokens": 100, "outputTokens": 10}, "totalNanoAiu": 15_227_000_000}}},
			"child": map[string]any{"agentName": "quick-checker", "agentDisplayName": "quick-checker", "totalNanoAiu": 1_791_000_000, "modelMetrics": map[string]any{"claude-haiku-4.5": map[string]any{"requests": map[string]int{"count": 1}, "usage": map[string]int{"inputTokens": 20, "outputTokens": 2}, "totalNanoAiu": 1_791_000_000}}},
		}}, "timestamp": "2026-10-08T00:00:03Z", "provenance": map[string]string{"component": "agent", "phase": "agent"}},
	}
	session := append([]map[string]any{{"type": "session.format", "data": map[string]int{"version": 1}, "provenance": map[string]string{"component": "collector"}}}, routingEvents...)
	writeAuditJSONLines(t, filepath.Join(runDir, "usage", "aw_session.jsonl"), session...)

	if includeRaw {
		rawDir := filepath.Join(runDir, "sandbox", "firewall", "logs", "api-proxy-logs")
		if err := os.MkdirAll(rawDir, 0700); err != nil {
			t.Fatal(err)
		}
		writeAuditJSONLines(t, filepath.Join(rawDir, "token-usage.jsonl"),
			routedRawUsage("classifier-1", "claude-sonnet-5", 0.323, classifierPath, "routing_classification"),
			routedRawUsage("main-1", "claude-sonnet-5", 15.227, agentPath, "agent"),
			routedRawUsage("subagent-1", "claude-haiku-4.5", 1.791, agentPath, "subagent"),
		)
	}
	return runDir
}

func routedUsageEvent(requestID, model string, credits float64, path, purpose string, legacy bool) map[string]any {
	data := map[string]any{
		"provider": "github-copilot", "model": model, "requestId": requestID, "aic": credits,
		"durationMs": 50, "usage": map[string]int{"inputTokens": 100, "outputTokens": 10},
	}
	if !legacy {
		data["purpose"] = purpose
		data["path"] = path
		data["xInitiator"] = "agent"
	}
	return data
}

func routedRawUsage(requestID, model string, credits float64, path, purpose string) map[string]any {
	return map[string]any{
		"event": "token_usage", "timestamp": "2026-10-08T00:00:00Z", "request_id": requestID,
		"purpose": purpose, "provider": "github-copilot", "model": model, "path": path,
		"x_initiator": "agent", "status": 200, "input_tokens": 100, "output_tokens": 10,
		"duration_ms": 50, "ai_credits_this_response": credits,
	}
}

func writeAuditJSONLines(t *testing.T, path string, records ...map[string]any) {
	t.Helper()
	var content strings.Builder
	for _, record := range records {
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		content.Write(encoded)
		content.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(content.String()), 0600); err != nil {
		t.Fatal(err)
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

func TestApplyAwInfoModelRoutingPrefersAgentArtifactCopy(t *testing.T) {
	runDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(runDir, "agent"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "aw_info.json"), []byte(`{"model":"agent"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "agent", "aw_info.json"), []byte(
		`{"model":"claude-opus-5","model_routing":{"status":"selected","endpoint":"/v1/messages","selected_endpoint":"/chat/completions"}}`,
	), 0o600); err != nil {
		t.Fatal(err)
	}

	result := applyAwInfoModelRouting(&ModelRoutingSummary{Status: "selected", Endpoint: "/chat/completions"}, runDir)
	if result.EffectiveEndpoint != "/v1/messages" || result.SelectedEndpoint != "/chat/completions" {
		t.Fatalf("agent artifact endpoint metadata was not preferred: %+v", result)
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

	attribution := resolveEffectiveModelAttribution("", info, nil, usage)
	if attribution.Model != "" {
		t.Fatalf("failed routing must not attribute classifier usage as the agent model: %+v", attribution)
	}

	legacyInfo := &AwInfo{Model: "configured-alias"}
	attribution = resolveEffectiveModelAttribution("", legacyInfo, nil, usage)
	if attribution.Model != "configured-alias" {
		t.Fatalf("non-routed workflows must retain configured model attribution: %+v", attribution)
	}
}

func TestUnifiedSessionModelRoutingAttribution(t *testing.T) {
	runDir := t.TempDir()
	sessionDir := filepath.Join(runDir, "usage")
	routingDir := filepath.Join(runDir, "sandbox", "firewall", "logs", "api-proxy-logs")
	if err := os.MkdirAll(sessionDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(routingDir, 0700); err != nil {
		t.Fatal(err)
	}
	session := strings.Join([]string{
		`{"type":"session.format","data":{"version":1},"provenance":{"component":"collector","phase":"conclusion","path":"usage/aw_session.jsonl","index":0}}`,
		`{"type":"workflow.info","data":{"engineId":"claude","model":"claude-sonnet-5","requestedModel":"agent","modelRouting":{"status":"selected","source":"awf-routing","provider":"anthropic","wireModel":"claude-sonnet-5","model":"claude-sonnet-5","effort":"medium","appliedEffort":"max","effectiveEndpoint":"/v1/messages","selectedEndpoint":"/chat/completions","mode":"awf-routed","selectedId":"sonnet","routerVersion":"0.28.49"}},"provenance":{"component":"workflow","phase":"agent","path":"agent/aw_info.json","index":0}}`,
		`{"type":"model_routing.outcome","data":{"status":"selected","wireModel":"claude-sonnet-5","effectiveEndpoint":"/v1/messages","selectedEndpoint":"/chat/completions","effort":"medium","appliedEffort":"max"},"provenance":{"component":"agent","phase":"agent","path":"agent/awf-routing-outcome.json","index":0}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(sessionDir, "aw_session.jsonl"), []byte(session), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(routingDir, "model-routing.jsonl"), []byte(
		`{"_schema":"model-routing/v0.28.49","stage":"selection","selected_model":"claude-sonnet-5","selected_effort":"medium","endpoint":"/chat/completions"}`+"\n",
	), 0600); err != nil {
		t.Fatal(err)
	}

	summary := analyzeModelRouting(runDir)
	if summary == nil || summary.Endpoint != "/chat/completions" ||
		summary.EffectiveEndpoint != "/v1/messages" || summary.SelectedEndpoint != "/chat/completions" {
		t.Fatalf("unified session endpoints were not applied: %+v", summary)
	}
	attribution := resolveEffectiveModelAttribution(runDir, nil, summary, nil)
	if attribution.Model != "claude-sonnet-5" || attribution.RequestedModel != "agent" ||
		attribution.Effort != "max" || attribution.RoutingStatus != "selected" {
		t.Fatalf("unified session attribution was not preferred: %+v", attribution)
	}
	config := extractEngineConfigWithInferredEngine(runDir, "")
	if config == nil || config.EngineID != "claude" || config.Model != "claude-sonnet-5" ||
		config.RequestedModel != "agent" || config.ModelEffort != "max" {
		t.Fatalf("engine config did not use workflow.info: %+v", config)
	}
	logsSummary := buildModelRoutingLogsSummary([]ProcessedRun{{ModelRouting: summary}})
	if logsSummary == nil || len(logsSummary.Routes) != 1 ||
		logsSummary.Routes[0].EffectiveEndpoint != "/v1/messages" ||
		logsSummary.Routes[0].SelectedEndpoint != "/chat/completions" {
		t.Fatalf("logs routing summary omitted session endpoints: %+v", logsSummary)
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
	before := &AuditComparisonRoute{
		Model: "gpt-5.6-luna", Effort: "medium", Mode: "economy", RouterVersion: "0.1.2",
		EffectiveEndpoint: "/v1/messages", SelectedEndpoint: "/chat/completions",
	}
	after := &AuditComparisonRoute{
		Model: "gpt-5.6-luna", Effort: "high", Mode: "economy", RouterVersion: "0.1.3",
		EffectiveEndpoint: "/responses", SelectedEndpoint: "/chat/completions",
	}
	if sameModelRoutingRoute(before, after) {
		t.Fatal("route changes should be detected even when the model is unchanged")
	}
	if !sameModelRoutingRoute(before, &AuditComparisonRoute{
		Model: "gpt-5.6-luna", Effort: "medium", Mode: "economy", RouterVersion: "0.1.2",
		EffectiveEndpoint: "/v1/messages", SelectedEndpoint: "/chat/completions",
	}) {
		t.Fatal("identical routes should compare as equal")
	}
	if sameModelRoutingRoute(before, &AuditComparisonRoute{
		Model: "gpt-5.6-luna", Effort: "medium", Mode: "economy", RouterVersion: "0.1.2",
		EffectiveEndpoint: "/responses", SelectedEndpoint: "/chat/completions",
	}) {
		t.Fatal("effective endpoint changes should be detected")
	}
	if sameModelRoutingRoute(before, &AuditComparisonRoute{
		Model: "gpt-5.6-luna", Effort: "medium", Mode: "economy", RouterVersion: "0.1.2",
		EffectiveEndpoint: "/v1/messages", SelectedEndpoint: "/responses",
	}) {
		t.Fatal("selected endpoint changes should be detected")
	}
	comparison := buildAuditComparison("success",
		auditComparisonSnapshot{ModelRouting: after},
		&WorkflowRun{DatabaseID: 12},
		&auditComparisonSnapshot{ModelRouting: before},
	)
	if comparison.Classification.Label != "changed" || !comparison.Delta.ModelRouting.Changed {
		t.Fatalf("route-only change should be visible in comparison: %+v", comparison)
	}
	if comparison.Delta.ModelRouting.Before.EffectiveEndpoint != "/v1/messages" ||
		comparison.Delta.ModelRouting.After.EffectiveEndpoint != "/responses" {
		t.Fatalf("route comparison omitted effective endpoints: %+v", comparison.Delta.ModelRouting)
	}
}

func TestBuildModelRoutingLogsSummary(t *testing.T) {
	routing := &ModelRoutingSummary{
		Status: "selected", Labels: ModelRoutingLabels{TaskType: "explain", Scope: "local", TaskComplexity: "trivial"},
		Mode: "economy", SelectedModel: "gpt-5.6-luna", SelectedEffort: "high", RouterVersion: "0.1.3",
		Endpoint: "/chat/completions", EffectiveEndpoint: "/v1/messages", SelectedEndpoint: "/chat/completions",
		RoutedCounts:   map[string]int{"as_selected": 3, "deviated": 1},
		ClassifierCost: ModelRoutingCost{AIC: 0.05}, SelectedModelCost: ModelRoutingCost{AIC: 0.60},
		DeviatedTrafficCost: ModelRoutingCost{AIC: 0.10}, MainAgentCost: ModelRoutingCost{Requests: 2, AIC: 0.12},
		SubagentCosts: []ModelRoutingAgentCost{{
			AgentName: "reader", AgentType: "subagent", InstanceCount: 1, CompletedCount: 1,
			Effort: "low", Models: []string{"claude-haiku-4.5"}, ModelRoutingCost: ModelRoutingCost{Requests: 3, AIC: 0.34},
		}},
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
	if summary.MainAgentCost.Requests != 4 || math.Abs(summary.MainAgentCost.AIC-0.24) > 0.000001 ||
		len(summary.SubagentCosts) != 1 || summary.SubagentCosts[0].Requests != 6 ||
		math.Abs(summary.SubagentCosts[0].AIC-0.68) > 0.000001 || summary.SubagentCosts[0].CompletedCount != 2 {
		t.Fatalf("unexpected per-agent routing costs: main=%+v subagents=%+v", summary.MainAgentCost, summary.SubagentCosts)
	}
}

func TestBuildModelRoutingLogsSummaryOrdersEndpointDistinctRoutes(t *testing.T) {
	route := func(effectiveEndpoint string) ProcessedRun {
		return ProcessedRun{ModelRouting: &ModelRoutingSummary{
			Status: "selected", SelectedModel: "gpt-5.6-luna", SelectedEffort: "medium",
			Endpoint: "/chat/completions", EffectiveEndpoint: effectiveEndpoint, SelectedEndpoint: "/chat/completions",
		}}
	}
	routes := []ProcessedRun{route("/v1/messages"), route("/responses")}
	forward := buildModelRoutingLogsSummary(routes)
	reverse := buildModelRoutingLogsSummary([]ProcessedRun{routes[1], routes[0]})
	if forward == nil || reverse == nil || len(forward.Routes) != 2 || len(reverse.Routes) != 2 {
		t.Fatalf("expected endpoint-distinct routes in both summaries: forward=%+v reverse=%+v", forward, reverse)
	}
	for index := range forward.Routes {
		if forward.Routes[index].EffectiveEndpoint != reverse.Routes[index].EffectiveEndpoint ||
			forward.Routes[index].SelectedEndpoint != reverse.Routes[index].SelectedEndpoint {
			t.Fatalf("route order changed with input order: forward=%+v reverse=%+v", forward.Routes, reverse.Routes)
		}
	}
}

func TestApplyAgentUsageToModelRouting(t *testing.T) {
	summary := &ModelRoutingSummary{}
	applyAgentUsageToModelRouting(summary, []AgentUsageBreakdown{
		{AgentName: "main", AgentType: "main", Requests: 10, AIC: 389.122},
		{
			AgentName: "research", AgentType: "subagent", InstanceCount: 1, CompletedCount: 1,
			Effort: "xhigh", ServedModels: []string{"claude-opus-5"},
			Requests: 45, TokenCoreMetrics: TokenCoreMetrics{InputTokens: 400}, AIC: 494.207,
		},
	})
	if summary.MainAgentCost.Requests != 10 || summary.MainAgentCost.AIC != 389.122 {
		t.Fatalf("unexpected main-agent routing cost: %+v", summary.MainAgentCost)
	}
	if len(summary.SubagentCosts) != 1 || summary.SubagentCosts[0].Requests != 45 ||
		summary.SubagentCosts[0].AIC != 494.207 || summary.SubagentCosts[0].Effort != "xhigh" {
		t.Fatalf("unexpected sub-agent routing cost: %+v", summary.SubagentCosts)
	}
}

func TestAppendRoutingDeviation(t *testing.T) {
	summary := &ModelRoutingSummary{SelectedModel: "gpt-5.6-luna", SelectedEffort: "high"}
	request := modelRoutingRecord{
		RequestedModel: "gpt-5.6-luna", RequestedEffort: "low",
	}
	appendRoutingDeviation(summary, request, "effort")
	appendRoutingDeviation(summary, request, "effort")

	appendRoutingDeviation(summary, modelRoutingRecord{
		RequestedModel: "gpt-5.4", RequestedEffort: request.RequestedEffort,
	}, "effort")
	appendRoutingDeviation(summary, modelRoutingRecord{
		RequestedModel: request.RequestedModel, RequestedEffort: "medium",
	}, "effort")
	summary.SelectedEffort = "medium"
	appendRoutingDeviation(summary, request, "effort")
	summary.SelectedEffort = "high"
	appendRoutingDeviation(summary, request, "endpoint")

	if len(summary.Deviations) != 5 || summary.Deviations[0].Count != 2 {
		t.Fatalf("identical deviations should increment while distinct fields create separate rows: %+v", summary.Deviations)
	}
	for i, deviation := range summary.Deviations[1:] {
		if deviation.Count != 1 {
			t.Errorf("distinct deviation row %d has count %d, want 1", i+1, deviation.Count)
		}
	}

	legacy := &ModelRoutingSummary{Schema: "model-routing/v0.28.35", SelectedModel: "claude-opus-5", SelectedEffort: "max"}
	legacyRequest := modelRoutingRecord{Routed: "deviated", Deviations: []string{"endpoint"}}
	normalizeLegacyEndpointDeviation(legacy, &legacyRequest)
	if legacyRequest.Routed == "deviated" {
		appendRoutingDeviation(legacy, legacyRequest, strings.Join(legacyRequest.Deviations, ","))
	}
	if len(legacy.Deviations) != 0 || !legacy.EndpointOnlyDeviationNormalized {
		t.Fatalf("legacy endpoint-only deviation should normalize without a row: %+v", legacy)
	}
}

func TestAppendModelRoutingSubagentCosts(t *testing.T) {
	empty := &ModelRoutingLogsSummary{}
	appendModelRoutingSubagentCosts(empty, nil)
	if len(empty.SubagentCosts) != 0 {
		t.Fatalf("empty totals should not append sub-agent costs: %+v", empty.SubagentCosts)
	}

	totals := make(map[string]ModelRoutingAgentCost)
	addModelRoutingSubagentTotals(totals, []ModelRoutingAgentCost{
		{
			AgentName: "reader", AgentType: "subagent", Effort: "low", Models: []string{"z-model", "a-model"},
			InstanceCount: 1, ModelRoutingCost: ModelRoutingCost{Requests: 2, AIC: 0.1},
		},
		{
			AgentName: "reader", AgentType: "subagent", Effort: "low", Models: []string{"a-model", "m-model"},
			InstanceCount: 2, ModelRoutingCost: ModelRoutingCost{Requests: 3, AIC: 0.2},
		},
		{
			AgentName: "reader", AgentType: "subagent", Effort: "high", Models: []string{"high-model"},
			InstanceCount: 1, ModelRoutingCost: ModelRoutingCost{Requests: 1, AIC: 0.3},
		},
		{
			AgentName: "alpha", AgentType: "subagent", Effort: "low", Models: []string{"alpha-model"},
			InstanceCount: 1, ModelRoutingCost: ModelRoutingCost{Requests: 1, AIC: 0.4},
		},
	})

	summary := &ModelRoutingLogsSummary{}
	appendModelRoutingSubagentCosts(summary, totals)
	if len(summary.SubagentCosts) != 3 {
		t.Fatalf("same agent and effort should merge, but different efforts should remain separate: %+v", summary.SubagentCosts)
	}
	gotOrder := make([]string, 0, len(summary.SubagentCosts))
	for _, cost := range summary.SubagentCosts {
		gotOrder = append(gotOrder, cost.AgentName+":"+cost.Effort)
	}
	wantOrder := []string{"alpha:low", "reader:high", "reader:low"}
	if !slices.Equal(gotOrder, wantOrder) {
		t.Fatalf("sub-agent costs are not ordered by agent and effort: got %v, want %v", gotOrder, wantOrder)
	}
	readerLow := summary.SubagentCosts[2]
	if !slices.Equal(readerLow.Models, []string{"a-model", "m-model", "z-model"}) ||
		readerLow.InstanceCount != 3 || readerLow.Requests != 5 || math.Abs(readerLow.AIC-0.3) > 0.000001 {
		t.Fatalf("merged sub-agent costs or models are incorrect: %+v", readerLow)
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
