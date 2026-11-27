package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const legacyNoSessionFixture = "legacy-no-session-routing"

// legacyNoSessionInstance is the expected per-instance result of a sub-agent of
// the legacy-no-session-routing fixture. Its unified subagent.started record
// lacks agentId; the native start with the same data.toolCallId carries it.
type legacyNoSessionInstance struct {
	agentID, toolCallID, definition, displayName, effort string
	requests, input, output, cacheRead, cacheWrite       int
	aic                                                  float64
}

var legacyNoSessionInstances = []legacyNoSessionInstance{
	{
		agentID: "33df47fd-68fd-43e1-bc24-c349e96e241e", toolCallID: "toolu_018v7UfCpbDLjSbGZQVeawNq",
		definition: "research", displayName: "subagent-research", effort: "xhigh",
		requests: 45, input: 5434876, output: 31328, cacheRead: 5183815, aic: 494.2065,
	},
	{
		agentID: "f64e3edf-7263-4e36-a837-bcb92c0604b8", toolCallID: "toolu_01Pt3GzWVC7ek6zmEMGzUVBX",
		definition: "explore", displayName: "ghaw-issues", effort: "low",
		requests: 3, input: 21916, output: 1176, cacheRead: 12696, aic: 9.14105,
	},
	{
		agentID: "aa19566b-0a97-4e6e-bc60-300abecdb799", toolCallID: "toolu_01SmwXa5irbN3GTqMtNT37Hp",
		definition: "explore", displayName: "awf-routing", effort: "low",
		requests: 12, input: 168188, output: 6904, cacheRead: 141157, aic: 40.97735,
	},
}

// Record IDs of the two concurrent explore instances in both session files:
// their starts, configured, selected, model-change, and completed records.
var legacyNoSessionExploreRecordIDs = []string{
	"f5c729f3-19c5-450d-9104-ef688154e1f5", "2caf6377-f188-4cdb-9743-f29401ae64a7",
	"499a3d5c-574d-41f8-8d4a-83da5e458d1d", "92eb22a2-e2ea-4980-b3e3-74f7203dbe56",
	"51d47c9a-b1df-46e9-8226-94452fa791f6", "c1cca7e7-1255-4182-aded-755918082c4b",
	"ba2d5364-a3e1-4736-92f9-6959f21bc9d2", "f351dadf-06ab-4ac5-9f20-1622c7265926",
	"8227f712-f143-466b-a156-108db0c8718e", "e8d1bd8c-10be-46a8-b572-bd1846563bcb",
}

func TestModelRoutingSubagentMissingAgentIDMatchesLegacy(t *testing.T) {
	downloadedDir := modelRoutingGoldenVariantDir(t, legacyNoSessionFixture, false)
	legacyDir := modelRoutingGoldenVariantDir(t, legacyNoSessionFixture, true)
	project := func(runDir string) modelRoutingGoldenProjection {
		t.Helper()
		usage, err := analyzeTokenUsage(runDir, false)
		require.NoError(t, err)
		return projectModelRoutingGoldenAudit(analyzeModelRouting(runDir), usage)
	}
	downloaded, legacy := project(downloadedDir), project(legacyDir)
	require.NotNil(t, downloaded.ModelRouting)
	require.NotNil(t, downloaded.FirewallTokenUsage)

	assert.Equal(t, legacy.ModelRouting.SubagentCosts, downloaded.ModelRouting.SubagentCosts)
	assert.Equal(t, legacy.FirewallTokenUsage.AgentUsage, downloaded.FirewallTokenUsage.AgentUsage)
	assert.Equal(t, legacy.FirewallTokenUsage.SubagentModelRequests, downloaded.FirewallTokenUsage.SubagentModelRequests)
	assert.Equal(t, legacy.FirewallTokenUsage.SubagentModelActuals, downloaded.FirewallTokenUsage.SubagentModelActuals)

	type expectedRow struct {
		displayNames                                   []string
		instances, completed                           int
		effort                                         string
		requests, input, output, cacheRead, cacheWrite int
		aic                                            float64
	}
	expected := map[string]expectedRow{
		"explore":  {[]string{"awf-routing", "ghaw-issues"}, 2, 2, "low", 15, 190104, 8080, 153853, 0, 50.1184},
		"research": {[]string{"subagent-research"}, 1, 1, "xhigh", 45, 5434876, 31328, 5183815, 0, 494.2065},
	}
	costs := modelRoutingSubagentCostsByName(t, downloaded.ModelRouting.SubagentCosts)
	require.Len(t, costs, len(expected))
	agents := make(map[string]AgentUsageBreakdown)
	for _, agent := range downloaded.FirewallTokenUsage.AgentUsage {
		agents[agent.AgentName] = agent
	}
	for name, want := range expected {
		cost := costs[name]
		assert.Equal(t, "subagent", cost.AgentType, name)
		assert.Equal(t, want.displayNames, cost.DisplayNames, name)
		assert.Equal(t, []string{"claude-opus-5"}, cost.Models, name)
		assert.Equal(t, want.instances, cost.InstanceCount, name)
		assert.Equal(t, want.completed, cost.CompletedCount, name)
		assert.Equal(t, want.effort, cost.Effort, name)
		assert.Equal(t, ModelRoutingCost{
			Requests: want.requests, InputTokens: want.input, OutputTokens: want.output,
			CacheReadTokens: want.cacheRead, CacheWriteTokens: want.cacheWrite, AIC: want.aic,
		}, cost.ModelRoutingCost, name)

		agent, ok := agents[name]
		require.True(t, ok, "missing agent_usage row %s", name)
		assert.Equal(t, "subagent", agent.AgentType, name)
		assert.Equal(t, want.displayNames, agent.DisplayNames, name)
		assert.Equal(t, want.instances, agent.InstanceCount, name)
		assert.Equal(t, want.completed, agent.CompletedCount, name)
		assert.Equal(t, want.effort, agent.Effort, name)
		assert.Equal(t, want.requests, agent.Requests, name)
		assert.Equal(t, want.input, agent.InputTokens, name)
		assert.Equal(t, want.output, agent.OutputTokens, name)
		assert.Equal(t, want.cacheRead, agent.CacheReadTokens, name)
		assert.Equal(t, want.cacheWrite, agent.CacheWriteTokens, name)
		assert.InDelta(t, want.aic, agent.AIC, 0.000001, name)
	}
	efforts := make(map[string]string)
	for _, request := range downloaded.FirewallTokenUsage.SubagentModelRequests {
		efforts[request.AgentName] = request.Effort
	}
	assert.Equal(t, map[string]string{"awf-routing": "low", "ghaw-issues": "low", "subagent-research": "xhigh"}, efforts)

	usage := downloaded.FirewallTokenUsage
	assert.Equal(t, 102, usage.TotalRequests)
	assert.Equal(t, 10841, usage.TotalInputTokens)
	assert.Equal(t, 69220, usage.TotalOutputTokens)
	assert.Equal(t, 9534114, usage.TotalCacheReadTokens)
	assert.Equal(t, 446654, usage.TotalCacheWriteTokens)
	assert.InDelta(t, 933.48553, usage.TotalAIC, 0.000001)
}

func TestModelRoutingLogsMissingAgentIDPreservesAccounting(t *testing.T) {
	logsRuns := func(caseNames []string) []ProcessedRun {
		t.Helper()
		runs := make([]ProcessedRun, 0, len(caseNames))
		for _, caseName := range caseNames {
			runDir := modelRoutingGoldenVariantDir(t, caseName, false)
			runs = append(runs, ProcessedRun{Run: WorkflowRun{LogsPath: runDir}, ModelRouting: analyzeModelRouting(runDir)})
		}
		return runs
	}
	orderedJSON := func(runs []ProcessedRun) (*ModelRoutingLogsSummary, []byte) {
		t.Helper()
		forward := projectModelRoutingGoldenLogs(buildModelRoutingLogsSummary(runs))
		reverseRuns := slices.Clone(runs)
		slices.Reverse(reverseRuns)
		forwardJSON := marshalModelRoutingGolden(t, forward)
		reverseJSON := marshalModelRoutingGolden(t, projectModelRoutingGoldenLogs(buildModelRoutingLogsSummary(reverseRuns)))
		require.True(t, bytes.Equal(forwardJSON, reverseJSON), "logs aggregation depends on run order:\nforward:\n%s\nreverse:\n%s", forwardJSON, reverseJSON)
		return forward, forwardJSON
	}
	mergedExplore := ModelRoutingAgentCost{
		AgentName: "explore", DisplayNames: []string{"awf-routing", "cost-table-explorer", "ghaw-issues"},
		AgentType: "subagent", InstanceCount: 3, CompletedCount: 3, Effort: "low",
		Models: []string{"claude-opus-5", "gpt-5.6-luna"},
		ModelRoutingCost: ModelRoutingCost{
			Requests: 18, InputTokens: 212352, OutputTokens: 8283, CacheReadTokens: 165857, CacheWriteTokens: 10235, AIC: 50.422823,
		},
	}
	research := ModelRoutingAgentCost{
		AgentName: "research", DisplayNames: []string{"subagent-research"},
		AgentType: "subagent", InstanceCount: 1, CompletedCount: 1, Effort: "xhigh",
		Models: []string{"claude-opus-5"},
		ModelRoutingCost: ModelRoutingCost{
			Requests: 45, InputTokens: 5434876, OutputTokens: 31328, CacheReadTokens: 5183815, AIC: 494.2065,
		},
	}
	findRows := func(summary *ModelRoutingLogsSummary, name string) []ModelRoutingAgentCost {
		var rows []ModelRoutingAgentCost
		for _, row := range summary.SubagentCosts {
			if row.AgentName == name {
				rows = append(rows, row)
			}
		}
		return rows
	}

	t.Run("all fixtures", func(t *testing.T) {
		caseNames := make([]string, 0, len(modelRoutingGoldenCases))
		for _, testCase := range modelRoutingGoldenCases {
			caseNames = append(caseNames, testCase.name)
		}
		summary, encoded := orderedJSON(logsRuns(caseNames))
		assertModelRoutingGoldenFile(t, filepath.Join("testdata", "model_routing_golden", "logs_expected.json"), encoded)

		require.Len(t, summary.SubagentCosts, 10)
		assert.Equal(t, []ModelRoutingAgentCost{mergedExplore}, findRows(summary, "explore"))
		researchRows := findRows(summary, "research")
		require.Len(t, researchRows, 2)
		assert.Contains(t, researchRows, research)
		for _, row := range researchRows {
			if row.Effort != "xhigh" {
				assert.Equal(t, "medium", row.Effort)
				assert.Equal(t, []string{"readme-researcher"}, row.DisplayNames)
				assert.Equal(t, 1, row.InstanceCount)
			}
		}

		assert.Equal(t, 177, summary.TotalRequests)
		assert.Equal(t, 36, summary.DeviatedRequests)
		assert.InDelta(t, 0.20339, summary.DeviatedTrafficShare, 0.0000001)
		assert.InDelta(t, 3.05912, summary.ClassifierAIC, 0.0000001)

		var instances, completed int
		var accounting ModelRoutingCost
		for _, row := range summary.SubagentCosts {
			instances += row.InstanceCount
			completed += row.CompletedCount
			accounting.Requests += row.Requests
			accounting.InputTokens += row.InputTokens
			accounting.OutputTokens += row.OutputTokens
			accounting.CacheReadTokens += row.CacheReadTokens
			accounting.CacheWriteTokens += row.CacheWriteTokens
			accounting.AIC += row.AIC
		}
		assert.Equal(t, 15, instances)
		assert.Equal(t, 12, completed)
		assert.Equal(t, 90, accounting.Requests)
		assert.Equal(t, 6294314, accounting.InputTokens)
		assert.Equal(t, 43473, accounting.OutputTokens)
		assert.Equal(t, 5812528, accounting.CacheReadTokens)
		assert.Equal(t, 88663, accounting.CacheWriteTokens)
		assert.InDelta(t, 590.397857, accounting.AIC, 0.000001)
	})

	t.Run("legacy and built-in sub-agent runs", func(t *testing.T) {
		summary, _ := orderedJSON(logsRuns([]string{legacyNoSessionFixture, "copilot-cli-builtin-subagents"}))
		assert.Equal(t, []ModelRoutingAgentCost{mergedExplore}, findRows(summary, "explore"))
		assert.Contains(t, findRows(summary, "research"), research)
		_, reverse := orderedJSON(logsRuns([]string{"copilot-cli-builtin-subagents", legacyNoSessionFixture}))
		_, forward := orderedJSON(logsRuns([]string{legacyNoSessionFixture, "copilot-cli-builtin-subagents"}))
		assert.True(t, bytes.Equal(forward, reverse), "forward:\n%s\nreverse:\n%s", forward, reverse)
	})
}

func TestModelRoutingMissingAgentIDLeavesOtherGoldensUnchanged(t *testing.T) {
	for _, testCase := range modelRoutingGoldenCases {
		if testCase.name == legacyNoSessionFixture {
			continue
		}
		for _, legacy := range []bool{false, true} {
			t.Run(testCase.name+"/"+map[bool]string{false: "downloaded", true: "legacy"}[legacy], func(t *testing.T) {
				runDir := modelRoutingGoldenVariantDir(t, testCase.name, legacy)
				if !legacy {
					// Their subagent.started records, where present, carry agentId, so
					// no instance needs the native identity bridge.
					path, err := findUnifiedSessionPath(runDir)
					require.NoError(t, err)
					require.NotEmpty(t, path)
					models, err := parseSessionSubagentFile(path, true)
					require.NoError(t, err)
					if models != nil {
						assert.False(t, models.hasStartsWithoutAgentID())
					}
				}
				routing := analyzeModelRouting(runDir)
				usage, err := analyzeTokenUsage(runDir, false)
				require.NoError(t, err)
				actual := marshalModelRoutingGolden(t, projectModelRoutingGoldenAudit(routing, usage))
				expectedPath := filepath.Join("testdata", "model_routing_golden", testCase.name, "expected.json")
				if legacyPath := filepath.Join("testdata", "model_routing_golden", testCase.name, "expected.legacy.json"); legacy {
					if _, err := os.Stat(legacyPath); err == nil {
						expectedPath = legacyPath
					}
				}
				assertModelRoutingGoldenFile(t, expectedPath, actual)
			})
		}
	}
}

func TestSessionSubagentMissingAgentIDKeepsConcurrentInstancesSeparate(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "model_routing_golden", legacyNoSessionFixture)
	unified := legacyNoSessionExploreExtract(t, filepath.Join(fixtureDir, "usage", "aw_session.jsonl"))
	native := legacyNoSessionExploreExtract(t, filepath.Join(fixtureDir, "agent-session.jsonl"))
	explore := legacyNoSessionInstances[1:]

	t.Run("linked instances", func(t *testing.T) {
		for _, reverseMetrics := range []bool{false, true} {
			unifiedRecords := unified
			if reverseMetrics {
				unifiedRecords = reorderAgentMetricKeys(t, unified)
			}
			runDir := writeSubagentIdentityRun(t, unifiedRecords, native)
			rows := legacyNoSessionInstanceRows(t, runDir)
			for _, want := range explore {
				row, ok := rows[want.displayName]
				require.True(t, ok, "missing %s row in %+v", want.displayName, rows)
				assert.Equal(t, 1, row.InstanceCount, want.displayName)
				assert.Equal(t, 1, row.CompletedCount, want.displayName)
				assert.Equal(t, want.effort, row.Effort, want.displayName)
				assert.Equal(t, want.requests, row.Requests, want.displayName)
				assert.Equal(t, want.input, row.InputTokens, want.displayName)
				assert.Equal(t, want.output, row.OutputTokens, want.displayName)
				assert.Equal(t, want.cacheRead, row.CacheReadTokens, want.displayName)
				assert.Equal(t, want.cacheWrite, row.CacheWriteTokens, want.displayName)
				assert.InDelta(t, want.aic, row.AIC, 0.000001, want.displayName)
			}
			models, err := parseSessionSubagentFile(filepath.Join(runDir, "usage", "aw_session.jsonl"), true)
			require.NoError(t, err)
			models.reconcileSubagentIdentities(readNativeSubagentSession(runDir))
			for _, want := range explore {
				assert.Contains(t, models.agentUsage, want.agentID, want.displayName)
				assert.NotContains(t, models.agentUsage, want.toolCallID, want.displayName)
				assert.NotContains(t, models.agents, want.toolCallID, want.displayName)
			}
			merged := legacyNoSessionDefinitionRow(t, runDir, "explore")
			assert.Equal(t, 2, merged.InstanceCount)
			assert.Equal(t, 2, merged.CompletedCount)
			assert.Equal(t, "low", merged.Effort)
			assert.Equal(t, 15, merged.Requests)
			assert.Equal(t, []string{"awf-routing", "ghaw-issues"}, merged.DisplayNames)
			assert.InDelta(t, 50.1184, merged.AIC, 0.000001)
		}
	})

	t.Run("unmatched instance keeps its previous output", func(t *testing.T) {
		awf := explore[1]
		baseline := legacyNoSessionInstanceRows(t, writeSubagentIdentityRun(t, unified, nil))
		// Remove the native start that carries the awf-routing toolCallId ->
		// agentId bridge; its configured and completed records keep agentId.
		withoutBridge := slices.DeleteFunc(slices.Clone(native), func(record string) bool {
			return strings.Contains(record, `"type":"subagent.started"`) && strings.Contains(record, awf.toolCallID)
		})
		require.Len(t, withoutBridge, len(native)-1)
		rows := legacyNoSessionInstanceRows(t, writeSubagentIdentityRun(t, unified, withoutBridge))
		assert.Equal(t, baseline[awf.displayName], rows[awf.displayName])
		assert.Equal(t, 2, rows[awf.displayName].InstanceCount, "the lifecycle and metrics halves stay separate")
		assert.Empty(t, rows[awf.displayName].Effort)
		ghaw := rows[explore[0].displayName]
		assert.Equal(t, 1, ghaw.InstanceCount)
		assert.Equal(t, "low", ghaw.Effort)
		assert.Equal(t, explore[0].requests, ghaw.Requests)

		// Without the native trace, nothing is linked.
		assert.Equal(t, 2, baseline[explore[0].displayName].InstanceCount)
		assert.Empty(t, baseline[explore[0].displayName].Effort)
	})

	t.Run("ambiguous evidence keeps its previous output", func(t *testing.T) {
		baseline := legacyNoSessionInstanceRows(t, writeSubagentIdentityRun(t, unified, nil))
		// Both explore tool calls appear on more than one native start: the
		// bridge is no longer unique, so neither instance is linked.
		duplicated := slices.Clone(native)
		for _, record := range native {
			if strings.Contains(record, `"type":"subagent.started"`) {
				duplicated = append(duplicated, record)
			}
		}
		rows := legacyNoSessionInstanceRows(t, writeSubagentIdentityRun(t, unified, duplicated))
		assert.Equal(t, baseline, rows)
	})
}

// legacyNoSessionExploreExtract returns the session records needed to analyze
// the concurrent explore instances, in their original order and shape.
func legacyNoSessionExploreExtract(t *testing.T, path string) []string {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	var records []string
	for scanner.Scan() {
		line := scanner.Text()
		var record struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		switch {
		case record.Type == "session.format", record.Type == "session.start", record.Type == "session.init",
			record.Type == "session.shutdown", record.Type == "session.result",
			slices.Contains(legacyNoSessionExploreRecordIDs, record.ID):
			records = append(records, line)
		}
	}
	require.NoError(t, scanner.Err())
	return records
}

// reorderAgentMetricKeys rewrites each agentMetrics snapshot with its keys in
// reverse order, leaving every other field intact.
func reorderAgentMetricKeys(t *testing.T, records []string) []string {
	t.Helper()
	reordered := slices.Clone(records)
	for i, record := range reordered {
		var event map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(record), &event))
		var data map[string]json.RawMessage
		if json.Unmarshal(event["data"], &data) != nil || data["agentMetrics"] == nil {
			continue
		}
		var metrics map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(data["agentMetrics"], &metrics))
		keys := make([]string, 0, len(metrics))
		for key := range metrics {
			keys = append(keys, key)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(keys)))
		var buffer bytes.Buffer
		buffer.WriteByte('{')
		for j, key := range keys {
			if j > 0 {
				buffer.WriteByte(',')
			}
			encodedKey, err := json.Marshal(key)
			require.NoError(t, err)
			buffer.Write(encodedKey)
			buffer.WriteByte(':')
			buffer.Write(metrics[key])
		}
		buffer.WriteByte('}')
		data["agentMetrics"] = buffer.Bytes()
		encodedData, err := json.Marshal(data)
		require.NoError(t, err)
		event["data"] = encodedData
		encoded, err := json.Marshal(event)
		require.NoError(t, err)
		reordered[i] = string(encoded)
	}
	return reordered
}

func writeSubagentIdentityRun(t *testing.T, unified, native []string) string {
	t.Helper()
	runDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "usage"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "usage", "aw_session.jsonl"), []byte(strings.Join(unified, "\n")+"\n"), 0o600))
	if native != nil {
		require.NoError(t, os.WriteFile(filepath.Join(runDir, "agent-session.jsonl"), []byte(strings.Join(native, "\n")+"\n"), 0o600))
	}
	return runDir
}

// legacyNoSessionInstanceRows returns the sub-agent rows before definition-level
// merging, keyed by display name.
func legacyNoSessionInstanceRows(t *testing.T, runDir string) map[string]AgentUsageBreakdown {
	t.Helper()
	_, _, agents, found, err := readSessionSubagentModelsDetailed(runDir)
	require.NoError(t, err)
	require.True(t, found)
	rows := make(map[string]AgentUsageBreakdown)
	for _, agent := range agents {
		if agent.AgentType == "subagent" {
			_, duplicate := rows[agent.AgentName]
			require.False(t, duplicate, "duplicate instance row %s", agent.AgentName)
			agent.requestUsages = nil
			rows[agent.AgentName] = agent
		}
	}
	return rows
}

func legacyNoSessionDefinitionRow(t *testing.T, runDir, definition string) AgentUsageBreakdown {
	t.Helper()
	_, _, agents, _, err := readSessionSubagentModelsDetailed(runDir)
	require.NoError(t, err)
	for _, row := range mergeAgentUsageByDefinition(agents) {
		if row.AgentName == definition {
			return row
		}
	}
	require.Failf(t, "missing definition row", "%s in %+v", definition, agents)
	return AgentUsageBreakdown{}
}

func TestSessionSubagentMissingAgentIDRefusesUnsafeLinks(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "model_routing_golden", legacyNoSessionFixture)
	unified := legacyNoSessionExploreExtract(t, filepath.Join(fixtureDir, "usage", "aw_session.jsonl"))
	native := legacyNoSessionExploreExtract(t, filepath.Join(fixtureDir, "agent-session.jsonl"))
	ghaw, awf := legacyNoSessionInstances[1], legacyNoSessionInstances[2]
	isRecord := func(eventType, toolCallID string) func(map[string]any) bool {
		return func(event map[string]any) bool {
			data, _ := event["data"].(map[string]any)
			return event["type"] == eventType && (toolCallID == "" || data["toolCallId"] == toolCallID)
		}
	}
	hasID := func(id string) func(map[string]any) bool {
		return func(event map[string]any) bool { return event["id"] == id }
	}
	editMetric := func(agentID string, edit func(metric map[string]any)) func(map[string]any) {
		return func(event map[string]any) {
			metrics := event["data"].(map[string]any)["agentMetrics"].(map[string]any)
			metric, _ := metrics[agentID].(map[string]any)
			if metric == nil {
				metric = make(map[string]any)
				metrics[agentID] = metric
			}
			edit(metric)
		}
	}
	withoutUnifiedNames := editSessionRecords(t, unified, isRecord("subagent.started", ghaw.toolCallID), func(event map[string]any) {
		data := event["data"].(map[string]any)
		delete(data, "agentName")
		delete(data, "agentDisplayName")
	})
	// ghaw-issues' configured and completed records carry its agentId in the
	// native trace; mixed-ID unified sessions can carry it too.
	withMixedIDs := func(records []string, matches func(map[string]any) bool) []string {
		return editSessionRecords(t, records, matches, func(event map[string]any) { event["agentId"] = ghaw.agentID })
	}
	metricsFirst := func(records []string) []string {
		reordered := slices.Clone(records)
		index := slices.IndexFunc(reordered, func(record string) bool { return strings.Contains(record, `"agentMetrics"`) })
		require.GreaterOrEqual(t, index, 0)
		shutdown := reordered[index]
		reordered = slices.Delete(reordered, index, index+1)
		completed := slices.IndexFunc(reordered, func(record string) bool {
			return strings.Contains(record, `"subagent.completed"`) || strings.Contains(record, `"subagent.failed"`)
		})
		require.GreaterOrEqual(t, completed, 0)
		return slices.Insert(reordered, completed, shutdown)
	}

	t.Run("missing unified names still link consistent evidence", func(t *testing.T) {
		runDir := writeSubagentIdentityRun(t, withoutUnifiedNames, native)
		models := reconciledSubagentModels(t, runDir)
		assert.NotContains(t, models.agentUsage, ghaw.toolCallID)
		row := legacyNoSessionInstanceRows(t, runDir)[ghaw.displayName]
		assert.Equal(t, 1, row.InstanceCount)
		assert.Equal(t, 1, row.CompletedCount)
		assert.Equal(t, ghaw.effort, row.Effort)
		assert.Equal(t, ghaw.requests, row.Requests)
	})

	refused := map[string][]string{
		"missing unified names, conflicting metrics definition": editSessionRecords(t, withoutUnifiedNames, isRecord("session.shutdown", ""),
			editMetric(ghaw.agentID, func(metric map[string]any) { metric["agentName"] = "research" })),
		"missing unified names, conflicting metrics display name": editSessionRecords(t, withoutUnifiedNames, isRecord("session.shutdown", ""),
			editMetric(ghaw.agentID, func(metric map[string]any) { metric["agentDisplayName"] = "subagent-research" })),
		"lifecycle half with credits only": editSessionRecords(t, unified, isRecord("session.shutdown", ""),
			editMetric(ghaw.toolCallID, func(metric map[string]any) {
				metric["agentName"], metric["agentDisplayName"], metric["totalNanoAiu"] = "explore", ghaw.displayName, 1250000000
			})),
		"lifecycle half with API duration only": editSessionRecords(t, unified, isRecord("session.shutdown", ""),
			editMetric(ghaw.toolCallID, func(metric map[string]any) {
				metric["agentName"], metric["agentDisplayName"], metric["totalApiDurationMs"] = "explore", ghaw.displayName, 4321
			})),
		"mixed IDs, configured by agentId":        withMixedIDs(unified, hasID("c1cca7e7-1255-4182-aded-755918082c4b")),
		"mixed IDs, completed by agentId":         withMixedIDs(unified, isRecord("subagent.completed", ghaw.toolCallID)),
		"mixed IDs, metrics before completion":    metricsFirst(withMixedIDs(unified, isRecord("subagent.completed", ghaw.toolCallID))),
		"mixed IDs, metrics before failure":       metricsFirst(withMixedIDs(editSessionRecords(t, unified, isRecord("subagent.completed", ghaw.toolCallID), func(event map[string]any) { event["type"] = "subagent.failed" }), isRecord("subagent.failed", ghaw.toolCallID))),
		"mixed IDs, configured and metrics first": metricsFirst(withMixedIDs(unified, hasID("c1cca7e7-1255-4182-aded-755918082c4b"))),
	}
	for name, records := range refused {
		t.Run(name+" keeps its previous output", func(t *testing.T) {
			baseline := subagentInstanceRowList(t, writeSubagentIdentityRun(t, records, nil))
			runDir := writeSubagentIdentityRun(t, records, native)
			models := reconciledSubagentModels(t, runDir)
			assert.Contains(t, models.agentUsage, ghaw.toolCallID, "ghaw-issues must not be linked")
			assert.NotContains(t, models.agentUsage, awf.toolCallID, "awf-routing is still linked")
			rows := subagentInstanceRowList(t, runDir)
			isAWF := func(row AgentUsageBreakdown) bool { return row.AgentName == awf.displayName }
			assert.ElementsMatch(t, slices.DeleteFunc(slices.Clone(baseline), isAWF), slices.DeleteFunc(slices.Clone(rows), isAWF))
			awfIndex := slices.IndexFunc(rows, isAWF)
			require.GreaterOrEqual(t, awfIndex, 0)
			assert.Equal(t, 1, rows[awfIndex].InstanceCount)
			assert.Equal(t, awf.effort, rows[awfIndex].Effort)

			var baselineAIC, aic float64
			var baselineDuration, duration int
			for _, row := range baseline {
				baselineAIC += row.AIC
				baselineDuration += row.TotalApiDurationMs
			}
			for _, row := range rows {
				aic += row.AIC
				duration += row.TotalApiDurationMs
			}
			assert.InDelta(t, baselineAIC, aic, 0.000001, "no credits are dropped")
			assert.Equal(t, baselineDuration, duration, "no API duration is dropped")
		})
	}
}

// subagentInstanceRowList returns the sub-agent rows before definition-level
// merging; unlike legacyNoSessionInstanceRows it allows repeated names.
func subagentInstanceRowList(t *testing.T, runDir string) []AgentUsageBreakdown {
	t.Helper()
	_, _, agents, found, err := readSessionSubagentModelsDetailed(runDir)
	require.NoError(t, err)
	require.True(t, found)
	rows := make([]AgentUsageBreakdown, 0, len(agents))
	for _, agent := range agents {
		if agent.AgentType == "subagent" {
			agent.requestUsages = nil
			rows = append(rows, agent)
		}
	}
	return rows
}

func reconciledSubagentModels(t *testing.T, runDir string) *subagentSessionModels {
	t.Helper()
	models, err := parseSessionSubagentFile(filepath.Join(runDir, "usage", "aw_session.jsonl"), true)
	require.NoError(t, err)
	require.NotNil(t, models)
	models.reconcileSubagentIdentities(readNativeSubagentSession(runDir))
	return models
}

// editSessionRecords applies edit to each record that matches; at least one
// record must match.
func editSessionRecords(t *testing.T, records []string, matches func(map[string]any) bool, edit func(map[string]any)) []string {
	t.Helper()
	edited := slices.Clone(records)
	count := 0
	for i, record := range edited {
		decoder := json.NewDecoder(strings.NewReader(record))
		decoder.UseNumber()
		var event map[string]any
		require.NoError(t, decoder.Decode(&event))
		if !matches(event) {
			continue
		}
		edit(event)
		encoded, err := json.Marshal(event)
		require.NoError(t, err)
		edited[i] = string(encoded)
		count++
	}
	require.Positive(t, count, "no session record matched")
	return edited
}
