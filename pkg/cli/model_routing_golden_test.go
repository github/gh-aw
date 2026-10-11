package cli

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const modelRoutingGoldenUpdateEnv = "UPDATE_MODEL_ROUTING_GOLDEN"

type modelRoutingGoldenCase struct {
	name                 string
	legacyDifference     string
	unifiedClassifierAIC *float64
	legacyClassifierAIC  *float64
}

var modelRoutingGoldenCases = []modelRoutingGoldenCase{
	{
		name:                 "copilot-routed-gpt-responses",
		legacyDifference:     "the legacy variant attributes routing costs from raw token-usage.jsonl instead of unified firewall.token_usage events",
		unifiedClassifierAIC: new(0.03176),
		legacyClassifierAIC:  new(0.03176),
	},
	{
		name:                 "claude-awf-selected-messages",
		legacyDifference:     "the legacy variant attributes routing costs from raw token-usage.jsonl instead of unified firewall.token_usage events",
		unifiedClassifierAIC: new(0.776),
		legacyClassifierAIC:  new(0.776),
	},
	{
		name:             "pi-claude-two-subagents",
		legacyDifference: "the legacy agent-session.jsonl fallback lacks pi tool execution events needed to correlate proxy requests to agents",
	},
	{
		name:                 "copilot-subagent-failed-and-alias",
		legacyDifference:     "the legacy variant attributes routing costs from raw token-usage.jsonl instead of unified firewall.token_usage events",
		unifiedClassifierAIC: new(0.04802),
		legacyClassifierAIC:  new(0.04802),
	},
	{
		name:                 "claude-awf-steering-notices",
		legacyDifference:     "steering notices are read only from unified firewall.token_usage events, so the legacy variant without usage/aw_session.jsonl lists none",
		unifiedClassifierAIC: new(0.776),
		legacyClassifierAIC:  new(0.776),
	},
	{
		name:                 "legacy-no-session-routing",
		legacyDifference:     "the old session has no routing events; classifier cost is recovered from raw token-usage.jsonl",
		unifiedClassifierAIC: new(0.03908),
		legacyClassifierAIC:  new(0.03908),
	},
}

type modelRoutingGoldenProjection struct {
	ModelRouting       *modelRoutingGoldenRouting    `json:"model_routing,omitempty"`
	FirewallTokenUsage *modelRoutingGoldenTokenUsage `json:"firewall_token_usage,omitempty"`
}

type modelRoutingGoldenRouting struct {
	Status                          string                  `json:"status"`
	SelectedModel                   string                  `json:"selected_model,omitempty"`
	SelectedEffort                  string                  `json:"selected_effort,omitempty"`
	Endpoint                        string                  `json:"endpoint,omitempty"`
	EffectiveEndpoint               string                  `json:"effective_endpoint,omitempty"`
	SelectedEndpoint                string                  `json:"selected_endpoint,omitempty"`
	RoutedCounts                    map[string]int          `json:"routed_counts,omitempty"`
	OutcomeCounts                   map[string]int          `json:"outcome_counts,omitempty"`
	Deviations                      []ModelRoutingDeviation `json:"deviations,omitempty"`
	ClassifierCost                  ModelRoutingCost        `json:"classifier_cost"`
	SelectedModelCost               ModelRoutingCost        `json:"selected_model_cost"`
	DeviatedTrafficCost             ModelRoutingCost        `json:"deviated_traffic_cost"`
	MainAgentCost                   ModelRoutingCost        `json:"main_agent_cost"`
	SubagentCosts                   []ModelRoutingAgentCost `json:"subagent_costs,omitempty"`
	EndpointOnlyDeviationNormalized bool                    `json:"endpoint_only_deviation_normalized,omitempty"`
}

type modelRoutingGoldenTokenUsage struct {
	TotalInputTokens       int                         `json:"total_input_tokens"`
	TotalOutputTokens      int                         `json:"total_output_tokens"`
	TotalCacheReadTokens   int                         `json:"total_cache_read_tokens"`
	TotalCacheWriteTokens  int                         `json:"total_cache_write_tokens"`
	TotalRequests          int                         `json:"total_requests"`
	TotalDurationMs        int                         `json:"total_duration_ms"`
	TotalResponseBytes     int                         `json:"total_response_bytes"`
	CacheEfficiency        float64                     `json:"cache_efficiency"`
	TotalAIC               float64                     `json:"total_aic"`
	ByModel                map[string]*ModelTokenUsage `json:"by_model"`
	SubagentModelRequests  []SubagentModelRequest      `json:"subagent_model_requests,omitempty"`
	DeclaredSubagentModels []SubagentModelRequest      `json:"declared_subagent_models,omitempty"`
	SubagentModelActuals   []SubagentModelActual       `json:"subagent_model_actuals,omitempty"`
	AgentUsage             []AgentUsageBreakdown       `json:"agent_usage,omitempty"`
	SteeringNotices        []SteeringNotice            `json:"steering_notices,omitempty"`
	MismatchCount          int                         `json:"mismatch_count,omitempty"`
	Warnings               []string                    `json:"warnings,omitempty"`
}

// Regenerate fixtures with UPDATE_MODEL_ROUTING_GOLDEN=1 go test ./pkg/cli -run 'TestModelRoutingGolden' -count=1.
// Review the resulting expected*.json files by hand; do not use regeneration to mask behavior changes.
func TestModelRoutingGoldenAudit(t *testing.T) {
	type variantResult struct {
		projection modelRoutingGoldenProjection
		json       []byte
		runDir     string
	}
	results := make(map[string]map[string]variantResult, len(modelRoutingGoldenCases))
	update := os.Getenv(modelRoutingGoldenUpdateEnv) == "1"
	fixtureTemp := t.TempDir()

	for _, testCase := range modelRoutingGoldenCases {
		t.Run(testCase.name, func(t *testing.T) {
			results[testCase.name] = make(map[string]variantResult, 2)
			for _, legacy := range []bool{false, true} {
				variant := "downloaded"
				if legacy {
					variant = "legacy"
				}
				t.Run(variant, func(t *testing.T) {
					runDir := filepath.Join(fixtureTemp, testCase.name, variant)
					copyModelRoutingGoldenFixture(t, filepath.Join("testdata", "model_routing_golden", testCase.name), runDir)
					if legacy {
						if err := os.Remove(filepath.Join(runDir, "usage", "aw_session.jsonl")); err != nil && !os.IsNotExist(err) {
							t.Fatalf("remove unified session fixture: %v", err)
						}
					}

					routing := analyzeModelRouting(runDir)
					tokenUsage, err := analyzeTokenUsage(runDir, false)
					if err != nil {
						t.Fatalf("analyze token usage: %v", err)
					}
					projection := projectModelRoutingGoldenAudit(routing, tokenUsage)
					encoded := marshalModelRoutingGolden(t, projection)
					results[testCase.name][variant] = variantResult{projection: projection, json: encoded, runDir: runDir}
					assertModelRoutingGoldenCase(t, testCase, legacy, routing, tokenUsage)
				})
			}

			downloaded := results[testCase.name]["downloaded"]
			legacy := results[testCase.name]["legacy"]
			assertModelRoutingGoldenVariantAgreement(t, downloaded.projection, legacy.projection)
			downloadedPath := filepath.Join("testdata", "model_routing_golden", testCase.name, "expected.json")
			legacyPath := filepath.Join("testdata", "model_routing_golden", testCase.name, "expected.legacy.json")
			if update {
				writeModelRoutingGolden(t, downloadedPath, downloaded.json)
				if bytes.Equal(downloaded.json, legacy.json) {
					if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
						t.Fatalf("remove redundant legacy golden: %v", err)
					}
				} else {
					writeModelRoutingGolden(t, legacyPath, legacy.json)
				}
				return
			}

			assertModelRoutingGoldenFile(t, downloadedPath, downloaded.json)
			expectedLegacyPath := downloadedPath
			if _, err := os.Stat(legacyPath); err == nil {
				expectedLegacyPath = legacyPath
			} else if !os.IsNotExist(err) {
				t.Fatalf("stat legacy golden: %v", err)
			}
			assertModelRoutingGoldenFile(t, expectedLegacyPath, legacy.json)
			if expectedLegacyPath == downloadedPath && !bytes.Equal(downloaded.json, legacy.json) {
				t.Fatalf("downloaded and legacy analyses differ; add %s and explain the expected information loss in the test table", legacyPath)
			}
			if !bytes.Equal(downloaded.json, legacy.json) && testCase.legacyDifference == "" {
				t.Fatalf("downloaded and legacy analyses differ without an explanation in the test table")
			}
		})
	}

	t.Run("logs aggregate", func(t *testing.T) {
		runs := make([]ProcessedRun, 0, len(modelRoutingGoldenCases))
		for _, testCase := range modelRoutingGoldenCases {
			result := results[testCase.name]["downloaded"]
			routing := analyzeModelRouting(result.runDir)
			runs = append(runs, ProcessedRun{
				Run:          WorkflowRun{LogsPath: result.runDir},
				ModelRouting: routing,
			})
		}
		forward := projectModelRoutingGoldenLogs(buildModelRoutingLogsSummary(runs))
		reverseRuns := slices.Clone(runs)
		slices.Reverse(reverseRuns)
		reverse := projectModelRoutingGoldenLogs(buildModelRoutingLogsSummary(reverseRuns))
		forwardJSON := marshalModelRoutingGolden(t, forward)
		reverseJSON := marshalModelRoutingGolden(t, reverse)
		if !bytes.Equal(forwardJSON, reverseJSON) {
			t.Fatalf("logs aggregation depends on run order:\nforward:\n%s\nreverse:\n%s", forwardJSON, reverseJSON)
		}
		logsPath := filepath.Join("testdata", "model_routing_golden", "logs_expected.json")
		if update {
			writeModelRoutingGolden(t, logsPath, forwardJSON)
		} else {
			assertModelRoutingGoldenFile(t, logsPath, forwardJSON)
		}
	})
}

func TestModelRoutingGoldenCaptureMatchesDownload(t *testing.T) {
	fullDir := os.Getenv("MODEL_ROUTING_GOLDEN_FULL_DIR")
	fixtureDir := os.Getenv("MODEL_ROUTING_GOLDEN_FIXTURE_DIR")
	if fullDir == "" || fixtureDir == "" {
		t.Skip("capture validation requires full download and fixture directories")
	}

	analyze := func(runDir string) []byte {
		t.Helper()
		routing := analyzeModelRouting(runDir)
		usage, err := analyzeTokenUsage(runDir, false)
		if err != nil {
			t.Fatalf("analyze token usage in %s: %v", runDir, err)
		}
		return marshalModelRoutingGolden(t, projectModelRoutingGoldenAudit(routing, usage))
	}
	full := analyze(fullDir)
	fixture := analyze(fixtureDir)
	if !bytes.Equal(full, fixture) {
		t.Fatalf("fixture analysis differs from full download:\nfull:\n%s\nfixture:\n%s", full, fixture)
	}
}

func copyModelRoutingGoldenFixture(t *testing.T, source, destination string) {
	t.Helper()
	if err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	}); err != nil {
		t.Fatalf("copy golden fixture: %v", err)
	}
}

func projectModelRoutingGoldenAudit(routing *ModelRoutingSummary, usage *TokenUsageSummary) modelRoutingGoldenProjection {
	projection := modelRoutingGoldenProjection{}
	if routing != nil {
		projected := &modelRoutingGoldenRouting{
			Status: routing.Status, SelectedModel: routing.SelectedModel, SelectedEffort: routing.SelectedEffort,
			Endpoint: routing.Endpoint, EffectiveEndpoint: routing.EffectiveEndpoint, SelectedEndpoint: routing.SelectedEndpoint,
			RoutedCounts: routing.RoutedCounts, OutcomeCounts: routing.OutcomeCounts,
			Deviations: slices.Clone(routing.Deviations), ClassifierCost: roundedModelRoutingCost(routing.ClassifierCost),
			SelectedModelCost:               roundedModelRoutingCost(routing.SelectedModelCost),
			DeviatedTrafficCost:             roundedModelRoutingCost(routing.DeviatedTrafficCost),
			MainAgentCost:                   roundedModelRoutingCost(routing.MainAgentCost),
			SubagentCosts:                   slices.Clone(routing.SubagentCosts),
			EndpointOnlyDeviationNormalized: routing.EndpointOnlyDeviationNormalized,
		}
		slices.SortFunc(projected.Deviations, func(left, right ModelRoutingDeviation) int {
			return strings.Compare(
				strings.Join([]string{left.RequestedModel, left.RequestedEffort, left.SelectedModel, left.SelectedEffort, left.Deviation}, "\x00"),
				strings.Join([]string{right.RequestedModel, right.RequestedEffort, right.SelectedModel, right.SelectedEffort, right.Deviation}, "\x00"),
			)
		})
		for i := range projected.SubagentCosts {
			projected.SubagentCosts[i].ModelRoutingCost = roundedModelRoutingCost(projected.SubagentCosts[i].ModelRoutingCost)
			projected.SubagentCosts[i].Models = slices.Clone(projected.SubagentCosts[i].Models)
			slices.Sort(projected.SubagentCosts[i].Models)
		}
		slices.SortFunc(projected.SubagentCosts, func(left, right ModelRoutingAgentCost) int {
			if order := strings.Compare(left.AgentName, right.AgentName); order != 0 {
				return order
			}
			return strings.Compare(left.Effort, right.Effort)
		})
		projection.ModelRouting = projected
	}
	if usage != nil {
		projected := &modelRoutingGoldenTokenUsage{
			TotalInputTokens: usage.TotalInputTokens, TotalOutputTokens: usage.TotalOutputTokens,
			TotalCacheReadTokens: usage.TotalCacheReadTokens, TotalCacheWriteTokens: usage.TotalCacheWriteTokens,
			TotalRequests: usage.TotalRequests, TotalDurationMs: usage.TotalDurationMs,
			TotalResponseBytes: usage.TotalResponseBytes, CacheEfficiency: roundedModelRoutingCredit(usage.CacheEfficiency),
			TotalAIC: roundedModelRoutingCredit(usage.TotalAIC), ByModel: make(map[string]*ModelTokenUsage, len(usage.ByModel)),
			SubagentModelRequests:  slices.Clone(usage.SubagentModelRequests),
			DeclaredSubagentModels: slices.Clone(usage.DeclaredSubagentModels),
			SubagentModelActuals:   slices.Clone(usage.SubagentModelActuals), AgentUsage: slices.Clone(usage.AgentUsage),
			SteeringNotices: slices.Clone(usage.SteeringNotices),
			MismatchCount:   usage.MismatchCount, Warnings: slices.Clone(usage.Warnings),
		}
		for model, modelUsage := range usage.ByModel {
			if modelUsage == nil {
				projected.ByModel[model] = nil
				continue
			}
			copy := *modelUsage
			copy.AIC = roundedModelRoutingCredit(copy.AIC)
			projected.ByModel[model] = &copy
		}
		for i := range projected.SubagentModelRequests {
			projected.SubagentModelRequests[i].ServedModels = slices.Clone(projected.SubagentModelRequests[i].ServedModels)
			slices.Sort(projected.SubagentModelRequests[i].ServedModels)
		}
		for i := range projected.DeclaredSubagentModels {
			projected.DeclaredSubagentModels[i].ServedModels = slices.Clone(projected.DeclaredSubagentModels[i].ServedModels)
			slices.Sort(projected.DeclaredSubagentModels[i].ServedModels)
		}
		for i := range projected.SubagentModelActuals {
			projected.SubagentModelActuals[i].AIC = roundedModelRoutingCredit(projected.SubagentModelActuals[i].AIC)
			projected.SubagentModelActuals[i].ServedModels = slices.Clone(projected.SubagentModelActuals[i].ServedModels)
			slices.Sort(projected.SubagentModelActuals[i].ServedModels)
		}
		for i := range projected.AgentUsage {
			projected.AgentUsage[i].AIC = roundedModelRoutingCredit(projected.AgentUsage[i].AIC)
			projected.AgentUsage[i].RequestedModels = slices.Clone(projected.AgentUsage[i].RequestedModels)
			projected.AgentUsage[i].ResolvedModels = slices.Clone(projected.AgentUsage[i].ResolvedModels)
			projected.AgentUsage[i].ServedModels = slices.Clone(projected.AgentUsage[i].ServedModels)
			slices.Sort(projected.AgentUsage[i].RequestedModels)
			slices.Sort(projected.AgentUsage[i].ResolvedModels)
			slices.Sort(projected.AgentUsage[i].ServedModels)
			projected.AgentUsage[i].Models = slices.Clone(projected.AgentUsage[i].Models)
			for j := range projected.AgentUsage[i].Models {
				projected.AgentUsage[i].Models[j].AIC = roundedModelRoutingCredit(projected.AgentUsage[i].Models[j].AIC)
			}
			slices.SortFunc(projected.AgentUsage[i].Models, func(left, right AgentModelUsage) int {
				return strings.Compare(left.Model, right.Model)
			})
		}
		slices.SortFunc(projected.SubagentModelRequests, func(left, right SubagentModelRequest) int {
			return strings.Compare(left.AgentName, right.AgentName)
		})
		slices.SortFunc(projected.DeclaredSubagentModels, func(left, right SubagentModelRequest) int {
			return strings.Compare(left.AgentName, right.AgentName)
		})
		slices.SortFunc(projected.SubagentModelActuals, func(left, right SubagentModelActual) int {
			if order := strings.Compare(left.Model, right.Model); order != 0 {
				return order
			}
			return strings.Compare(left.ResolvedModel, right.ResolvedModel)
		})
		slices.SortFunc(projected.AgentUsage, func(left, right AgentUsageBreakdown) int {
			if order := strings.Compare(left.AgentName, right.AgentName); order != 0 {
				return order
			}
			return strings.Compare(left.Effort, right.Effort)
		})
		slices.Sort(projected.Warnings)
		projection.FirewallTokenUsage = projected
	}
	return projection
}

func projectModelRoutingGoldenLogs(summary *ModelRoutingLogsSummary) *ModelRoutingLogsSummary {
	if summary == nil {
		return nil
	}
	projected := *summary
	projected.ClassifierAIC = roundedModelRoutingCredit(projected.ClassifierAIC)
	projected.MainAgentCost = roundedModelRoutingCost(projected.MainAgentCost)
	projected.DeviatedTrafficShare = roundedModelRoutingCredit(projected.DeviatedTrafficShare)
	projected.Routes = slices.Clone(projected.Routes)
	for i := range projected.Routes {
		projected.Routes[i].TotalAIC = roundedModelRoutingCredit(projected.Routes[i].TotalAIC)
		projected.Routes[i].AverageAIC = roundedModelRoutingCredit(projected.Routes[i].AverageAIC)
	}
	projected.SubagentCosts = slices.Clone(projected.SubagentCosts)
	for i := range projected.SubagentCosts {
		projected.SubagentCosts[i].ModelRoutingCost = roundedModelRoutingCost(projected.SubagentCosts[i].ModelRoutingCost)
		projected.SubagentCosts[i].Models = slices.Clone(projected.SubagentCosts[i].Models)
		slices.Sort(projected.SubagentCosts[i].Models)
	}
	return &projected
}

func roundedModelRoutingCost(cost ModelRoutingCost) ModelRoutingCost {
	cost.AIC = roundedModelRoutingCredit(cost.AIC)
	return cost
}

func roundedModelRoutingCredit(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	return math.Round(value*1_000_000) / 1_000_000
}

func marshalModelRoutingGolden(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden projection: %v", err)
	}
	return append(encoded, '\n')
}

func writeModelRoutingGolden(t *testing.T, path string, contents []byte) {
	t.Helper()
	if existing, err := os.ReadFile(path); err == nil {
		var existingJSON, updatedJSON bytes.Buffer
		if json.Compact(&existingJSON, existing) == nil &&
			json.Compact(&updatedJSON, contents) == nil &&
			bytes.Equal(existingJSON.Bytes(), updatedJSON.Bytes()) {
			return
		}
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write golden file %s: %v", path, err)
	}
}

func assertModelRoutingGoldenFile(t *testing.T, path string, actual []byte) {
	t.Helper()
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file %s (regenerate with UPDATE_MODEL_ROUTING_GOLDEN=1): %v", path, err)
	}
	var expectedJSON, actualJSON bytes.Buffer
	if err := json.Compact(&expectedJSON, expected); err != nil {
		t.Fatalf("parse golden file %s: %v", path, err)
	}
	if err := json.Compact(&actualJSON, actual); err != nil {
		t.Fatalf("parse actual golden projection for %s: %v", path, err)
	}
	if !bytes.Equal(expectedJSON.Bytes(), actualJSON.Bytes()) {
		t.Errorf("golden mismatch for %s; regenerate with UPDATE_MODEL_ROUTING_GOLDEN=1 and review the diff\nexpected:\n%s\nactual:\n%s",
			path, expected, actual)
	}
}

func assertModelRoutingGoldenVariantAgreement(t *testing.T, downloaded, legacy modelRoutingGoldenProjection) {
	t.Helper()
	if (downloaded.ModelRouting == nil) != (legacy.ModelRouting == nil) {
		t.Fatalf("legacy fallback changed whether routing was available: downloaded=%+v legacy=%+v", downloaded.ModelRouting, legacy.ModelRouting)
	}
	if downloaded.ModelRouting != nil {
		type routingIdentity struct {
			Status            string
			SelectedModel     string
			SelectedEffort    string
			Endpoint          string
			EffectiveEndpoint string
			SelectedEndpoint  string
			RoutedCounts      map[string]int
			OutcomeCounts     map[string]int
			Deviations        []ModelRoutingDeviation
		}
		identity := func(routing *modelRoutingGoldenRouting) routingIdentity {
			return routingIdentity{
				Status: routing.Status, SelectedModel: routing.SelectedModel, SelectedEffort: routing.SelectedEffort,
				Endpoint: routing.Endpoint, EffectiveEndpoint: routing.EffectiveEndpoint, SelectedEndpoint: routing.SelectedEndpoint,
				RoutedCounts: routing.RoutedCounts, OutcomeCounts: routing.OutcomeCounts, Deviations: routing.Deviations,
			}
		}
		if !reflect.DeepEqual(identity(downloaded.ModelRouting), identity(legacy.ModelRouting)) {
			t.Fatalf("raw routing facts should agree across variants: downloaded=%+v legacy=%+v", identity(downloaded.ModelRouting), identity(legacy.ModelRouting))
		}
	}
	downloadedUsage, legacyUsage := downloaded.FirewallTokenUsage, legacy.FirewallTokenUsage
	if (downloadedUsage == nil) != (legacyUsage == nil) {
		t.Fatalf("legacy fallback changed whether token usage was available: downloaded=%+v legacy=%+v", downloadedUsage, legacyUsage)
	}
	if downloadedUsage != nil {
		if downloadedUsage.TotalInputTokens != legacyUsage.TotalInputTokens ||
			downloadedUsage.TotalOutputTokens != legacyUsage.TotalOutputTokens ||
			downloadedUsage.TotalCacheReadTokens != legacyUsage.TotalCacheReadTokens ||
			downloadedUsage.TotalCacheWriteTokens != legacyUsage.TotalCacheWriteTokens ||
			downloadedUsage.TotalRequests != legacyUsage.TotalRequests ||
			downloadedUsage.TotalDurationMs != legacyUsage.TotalDurationMs ||
			downloadedUsage.TotalResponseBytes != legacyUsage.TotalResponseBytes ||
			downloadedUsage.TotalAIC != legacyUsage.TotalAIC ||
			!reflect.DeepEqual(downloadedUsage.ByModel, legacyUsage.ByModel) {
			t.Fatalf("raw proxy token totals should agree across variants: downloaded=%+v legacy=%+v", downloadedUsage, legacyUsage)
		}
	}
}

func assertModelRoutingGoldenCase(t *testing.T, testCase modelRoutingGoldenCase, legacy bool, routing *ModelRoutingSummary, usage *TokenUsageSummary) {
	t.Helper()
	expectedClassifierAIC := testCase.unifiedClassifierAIC
	if legacy && testCase.legacyClassifierAIC != nil {
		expectedClassifierAIC = testCase.legacyClassifierAIC
	}
	if expectedClassifierAIC != nil &&
		(routing == nil || routing.ClassifierCost.Requests != 1 || routing.ClassifierCost.AIC != *expectedClassifierAIC) {
		t.Fatalf("unexpected classifier routing cost for legacy=%t: %+v", legacy, routing)
	}
	switch testCase.name {
	case "copilot-routed-gpt-responses":
		if !legacy && routing.SelectedModelCost.AIC == 0 {
			t.Fatal("routed GPT selection should have non-zero selected-model cost")
		}
	case "claude-awf-selected-messages":
		if legacy {
			return
		}
		if routing == nil || routing.Status != "selected" || routing.Endpoint != "/chat/completions" ||
			routing.EffectiveEndpoint != "/v1/messages" || routing.SelectedEndpoint != "/chat/completions" ||
			len(routing.Deviations) != 0 {
			t.Fatalf("unexpected Claude AWF endpoint routing: %+v", routing)
		}
	case "claude-awf-steering-notices":
		var want []SteeringNotice
		if !legacy {
			want = []SteeringNotice{
				{Type: "ai_credit", Threshold: 80, RequestID: "d00ff6a1-276b-4edd-819e-1b29e4fddb6b", Phase: "agent"},
				{Type: "timeout", Threshold: 90, RequestID: "6509695f-5e67-4f95-9380-af82072a78fb", Phase: "agent"},
				{Type: "token", Threshold: 95, RequestID: "b82dc73a-a50a-4d64-91e7-84f9cf437442", Phase: "detection"},
			}
		}
		if usage == nil || !reflect.DeepEqual(usage.SteeringNotices, want) {
			t.Fatalf("unexpected steering notices for legacy=%t: %+v", legacy, usage)
		}
	case "pi-claude-two-subagents":
		if usage == nil || len(usage.AgentUsage) == 0 {
			t.Fatal("pi sub-agent fixture did not produce per-agent usage rows")
		}
		if legacy {
			return
		}
		total := 0.0
		for _, agent := range usage.AgentUsage {
			total += agent.AIC
		}
		if math.Abs(total-usage.TotalAIC) > 0.000001 {
			t.Fatalf("main and sub-agent costs %.6f do not equal proxy total %.6f", total, usage.TotalAIC)
		}
		if usage.TotalAIC != 0 && len(usage.Warnings) > 0 {
			for _, warning := range usage.Warnings {
				if strings.Contains(strings.ToLower(warning), "reconcil") {
					t.Fatalf("unexpected sub-agent reconciliation warning: %s", warning)
				}
			}
		}
	case "copilot-subagent-failed-and-alias":
		if usage == nil {
			t.Fatal("expected token usage summary")
		}
		var failedAgent, smallAlias bool
		for _, agent := range usage.SubagentModelRequests {
			if agent.ReasonCode == "SUBAGENT_FAILED" {
				failedAgent = agent.EffectiveModel == "" && agent.FailedCount > 0
			}
			if agent.RequestedModel == "small" {
				smallAlias = agent.ResolvedModel == "gpt-5.4-mini" &&
					slices.Contains(agent.ServedModels, "gpt-5.4-mini-2026-03-17")
			}
		}
		if !failedAgent || !smallAlias {
			t.Fatalf("failed-agent or alias attribution missing: %+v", usage.SubagentModelRequests)
		}
	case "legacy-no-session-routing":
		if routing == nil || routing.Status != "selected" || len(routing.SubagentCosts) == 0 {
			t.Fatalf("legacy routing or agentMetrics-derived sub-agent rows missing: %+v", routing)
		}
	}
}

func TestModelRoutingGoldenActivationAwInfoFallback(t *testing.T) {
	runDir := filepath.Join(t.TempDir(), "legacy-no-session-routing")
	copyModelRoutingGoldenFixture(t, filepath.Join("testdata", "model_routing_golden", "legacy-no-session-routing"), runDir)
	if err := os.Remove(filepath.Join(runDir, "aw_info.json")); err != nil {
		t.Fatal(err)
	}
	activationDir := filepath.Join(runDir, "activation")
	if err := os.MkdirAll(activationDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(activationDir, "aw_info.json"), []byte(
		`{"model_routing":{"status":"selected","endpoint":"/activation/messages","selected_endpoint":"/chat/completions"}}`,
	), 0o600); err != nil {
		t.Fatal(err)
	}
	routing := analyzeModelRouting(runDir)
	if routing == nil || routing.EffectiveEndpoint != "/activation/messages" || routing.SelectedEndpoint != "/chat/completions" {
		t.Fatalf("activation aw_info fallback was not applied: %+v", routing)
	}
}

func TestModelRoutingGoldenProjectionPrecision(t *testing.T) {
	got := roundedModelRoutingCredit(1.23456789)
	if got != 1.234568 {
		t.Fatalf("credits should round to six decimal places: got %v", got)
	}
	if !math.IsNaN(roundedModelRoutingCredit(math.NaN())) {
		t.Fatal("NaN should remain NaN during projection")
	}
}
