//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildLogsData tests the structured data creation for logs
func TestBuildLogsData(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-*")
	workingSetFactor := 2.5

	// Create sample processed runs
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:       12345,
				Number:           1,
				WorkflowName:     "Test Workflow",
				WorkflowPath:     ".github/workflows/test-workflow.yml",
				Status:           "completed",
				Conclusion:       "success",
				Duration:         5 * time.Minute,
				TokenUsage:       1000,
				Turns:            3,
				ErrorCount:       0,
				WarningCount:     1,
				MissingToolCount: 0,
				CreatedAt:        time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
				URL:              "https://github.com/test/repo/actions/runs/12345",
				LogsPath:         filepath.Join(tmpDir, "run-12345"),
				Event:            "push",
				HeadBranch:       "main",
			},
			TaskDomain: &TaskDomainInfo{
				Name:  "triage",
				Label: "Triage",
			},
			BehaviorFingerprint: &BehaviorFingerprint{
				ExecutionStyle:  "directed",
				ToolBreadth:     "narrow",
				ActuationStyle:  "read_only",
				ResourceProfile: "lean",
				DispatchMode:    "standalone",
			},
			AgenticAssessments: []AgenticAssessment{
				{
					Kind:     "overkill_for_agentic",
					Severity: "low",
					Summary:  "Deterministic automation may be a better fit.",
				},
			},
			MissingTools: []MissingToolReport{},
			MCPFailures:  []MCPFailureReport{},
			WorkingSet: &WorkingSetMetrics{
				MeasurementState:      "measured",
				RebuildFactor:         &workingSetFactor,
				CumulativeInputTokens: 2500,
				PeakInputTokens:       1000,
				RebuildExcessTokens:   1500,
				Invocations:           3,
			},
		},
		{
			Run: WorkflowRun{
				DatabaseID:       12346,
				Number:           2,
				WorkflowName:     "Test Workflow",
				WorkflowPath:     ".github/workflows/test-workflow.yml",
				Status:           "completed",
				Conclusion:       "failure",
				Duration:         3 * time.Minute,
				TokenUsage:       500,
				Turns:            2,
				ErrorCount:       1,
				WarningCount:     0,
				MissingToolCount: 1,
				CreatedAt:        time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC),
				URL:              "https://github.com/test/repo/actions/runs/12346",
				LogsPath:         filepath.Join(tmpDir, "run-12346"),
				Event:            "pull_request",
				HeadBranch:       "feature",
			},
			TaskDomain: &TaskDomainInfo{
				Name:  "triage",
				Label: "Triage",
			},
			BehaviorFingerprint: &BehaviorFingerprint{
				ExecutionStyle:  "directed",
				ToolBreadth:     "narrow",
				ActuationStyle:  "read_only",
				ResourceProfile: "lean",
				DispatchMode:    "standalone",
			},
			MissingTools: []MissingToolReport{
				{
					Tool:   "github_search",
					Reason: "Not allowed",
					ReportProvenance: ReportProvenance{
						WorkflowName: "Test Workflow",
						RunID:        12346,
					},
				},
			},
			MCPFailures: []MCPFailureReport{},
		},
	}

	// Build logs data
	logsData := buildLogsData(processedRuns, tmpDir, nil)

	// Verify summary
	if logsData.Summary.TotalRuns != 2 {
		t.Errorf("Expected TotalRuns to be 2, got %d", logsData.Summary.TotalRuns)
	}
	if logsData.Summary.TotalTokens != 1500 {
		t.Errorf("Expected TotalTokens to be 1500, got %d", logsData.Summary.TotalTokens)
	}
	if logsData.Summary.TotalTurns != 5 {
		t.Errorf("Expected TotalTurns to be 5, got %d", logsData.Summary.TotalTurns)
	}
	if logsData.Summary.TotalErrors != 1 {
		t.Errorf("Expected TotalErrors to be 1, got %d", logsData.Summary.TotalErrors)
	}
	if logsData.Summary.TotalWarnings != 1 {
		t.Errorf("Expected TotalWarnings to be 1, got %d", logsData.Summary.TotalWarnings)
	}
	if logsData.Summary.TotalMissingTools != 1 {
		t.Errorf("Expected TotalMissingTools to be 1, got %d", logsData.Summary.TotalMissingTools)
	}
	if logsData.Summary.TotalEpisodes != 2 {
		t.Errorf("Expected TotalEpisodes to be 2, got %d", logsData.Summary.TotalEpisodes)
	}
	if logsData.Summary.HighConfidenceEpisodes != 2 {
		t.Errorf("Expected HighConfidenceEpisodes to be 2, got %d", logsData.Summary.HighConfidenceEpisodes)
	}

	// Verify runs data
	if len(logsData.Runs) != 2 {
		t.Errorf("Expected 2 runs, got %d", len(logsData.Runs))
	}
	if len(logsData.Episodes) != 2 {
		t.Fatalf("Expected 2 episodes, got %d", len(logsData.Episodes))
	}
	if len(logsData.Edges) != 0 {
		t.Fatalf("Expected 0 edges for standalone runs, got %d", len(logsData.Edges))
	}

	// Verify first run
	if logsData.Runs[0].RunID != 12345 {
		t.Errorf("Expected RunID 12345, got %d", logsData.Runs[0].RunID)
	}
	if logsData.Runs[0].TaskDomain == nil || logsData.Runs[0].TaskDomain.Name != "triage" {
		t.Fatalf("Expected first run to include task domain, got %+v", logsData.Runs[0].TaskDomain)
	}
	if logsData.Runs[0].WorkingSet == nil || logsData.Runs[0].WorkingSet.RebuildFactor == nil || *logsData.Runs[0].WorkingSet.RebuildFactor != 2.5 {
		t.Fatalf("Expected first run to include working-set rebuild metrics, got %+v", logsData.Runs[0].WorkingSet)
	}
	if logsData.Runs[0].BehaviorFingerprint == nil || logsData.Runs[0].BehaviorFingerprint.ResourceProfile != "lean" {
		t.Fatalf("Expected first run to include behavior fingerprint, got %+v", logsData.Runs[0].BehaviorFingerprint)
	}
	if len(logsData.Runs[0].AgenticAssessments) != 1 {
		t.Fatalf("Expected first run to include 1 agentic assessment, got %d", len(logsData.Runs[0].AgenticAssessments))
	}
	if logsData.Runs[0].Comparison == nil {
		t.Fatal("Expected first run to include comparison payload")
	}
	if logsData.Runs[0].Comparison.BaselineFound {
		t.Fatal("Expected oldest run to have no baseline in logs comparison")
	}
	if logsData.Runs[1].Comparison == nil || !logsData.Runs[1].Comparison.BaselineFound {
		t.Fatalf("Expected newer run to include a baseline comparison, got %+v", logsData.Runs[1].Comparison)
	}
	if logsData.Runs[1].Comparison.Baseline == nil || logsData.Runs[1].Comparison.Baseline.Selection != "cohort_match" {
		t.Fatalf("Expected newer run to use cohort_match baseline, got %+v", logsData.Runs[1].Comparison.Baseline)
	}
	if logsData.Runs[1].Comparison.Baseline == nil || logsData.Runs[1].Comparison.Baseline.RunID != 12345 {
		t.Fatalf("Expected newer run baseline to point to run 12345, got %+v", logsData.Runs[1].Comparison.Baseline)
	}
	// Duration format from formatDuration is "5.0m", not "5m0s"
	if logsData.Runs[0].Duration == "" {
		t.Errorf("Expected non-empty Duration, got empty string")
	}

	// Verify missing tools summary
	if len(logsData.MissingTools) != 1 {
		t.Errorf("Expected 1 missing tool, got %d", len(logsData.MissingTools))
	}
	if len(logsData.MissingTools) > 0 && logsData.MissingTools[0].Tool != "github_search" {
		t.Errorf("Expected missing tool 'github_search', got '%s'", logsData.MissingTools[0].Tool)
	}
}

// TestRenderLogsJSON tests JSON output rendering
func TestRenderLogsJSON(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-*")

	// Create sample logs data
	logsData := LogsData{
		Summary: LogsSummary{
			TotalRuns:              2,
			TotalDuration:          "8m0s",
			TotalTurns:             5,
			TotalErrors:            1,
			TotalWarnings:          1,
			TotalMissingTools:      1,
			TotalEpisodes:          1,
			HighConfidenceEpisodes: 1,
		},
		Runs: []RunData{
			{
				RunID:        12345,
				Number:       1,
				WorkflowName: "Test Workflow",
				Status:       "completed",
				Conclusion:   "success",
				Duration:     "5m0s",
				TokenUsage:   1000,
				Turns:        3,
				ErrorCount:   0,
				WarningCount: 1,
				CreatedAt:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
				URL:          "https://github.com/test/repo/actions/runs/12345",
				LogsPath:     filepath.Join(tmpDir, "run-12345"),
				Event:        "push",
				Branch:       "main",
				Comparison: &AuditComparisonData{
					BaselineFound: true,
					Baseline: &AuditComparisonBaseline{
						RunID:     12000,
						Selection: "cohort_match",
						MatchedOn: []string{"task_domain", "resource_profile"},
					},
				},
			},
		},
		Episodes: []EpisodeData{
			{
				EpisodeID:       "standalone:12345",
				Kind:            "standalone",
				Confidence:      "high",
				RunIDs:          []int64{12345},
				WorkflowNames:   []string{"Test Workflow"},
				PrimaryWorkflow: "Test Workflow",
				TotalRuns:       1,
				TotalTokens:     1000,
				SuggestedRoute:  "workflow:Test Workflow",
			},
		},
		Edges:        []EpisodeEdge{},
		LogsLocation: tmpDir,
	}

	// Render JSON to buffer
	var buf bytes.Buffer
	err := renderLogsJSONToWriter(&buf, logsData, true)
	if err != nil {
		t.Fatalf("Failed to render JSON: %v", err)
	}
	output := buf.String()

	// Verify it's valid JSON
	var parsedData LogsData
	if err := json.Unmarshal([]byte(output), &parsedData); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nOutput: %s", err, output)
	}

	// Verify key fields
	if parsedData.Summary.TotalRuns != 2 {
		t.Errorf("Expected TotalRuns 2, got %d", parsedData.Summary.TotalRuns)
	}
	if parsedData.Summary.TotalEpisodes != 1 {
		t.Errorf("Expected TotalEpisodes 1, got %d", parsedData.Summary.TotalEpisodes)
	}
	if len(parsedData.Runs) != 1 {
		t.Errorf("Expected 1 run in JSON, got %d", len(parsedData.Runs))
	}
	if parsedData.Runs[0].Comparison == nil || parsedData.Runs[0].Comparison.Baseline == nil || parsedData.Runs[0].Comparison.Baseline.Selection != "cohort_match" {
		t.Fatalf("Expected comparison metadata to survive JSON round-trip, got %+v", parsedData.Runs[0].Comparison)
	}
	if len(parsedData.Episodes) != 1 || parsedData.Episodes[0].PrimaryWorkflow != "Test Workflow" {
		t.Fatalf("Expected episode primary workflow to survive JSON round-trip, got %+v", parsedData.Episodes)
	}
}

func writeTestAwInfo(t *testing.T, runDir string, payload map[string]any) {
	t.Helper()
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("Failed to create run directory %s: %v", runDir, err)
	}
	content, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal aw_info payload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "aw_info.json"), content, 0o644); err != nil {
		t.Fatalf("Failed to write aw_info.json: %v", err)
	}
}

func TestBuildLogsDataAggregatesDispatchEpisode(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-episode-*")
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:   2001,
				WorkflowName: "orchestrator",
				WorkflowPath: ".github/workflows/orchestrator.yml",
				Status:       "completed",
				Conclusion:   "success",
				Duration:     2 * time.Minute,
				TokenUsage:   300,
				CreatedAt:    time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC),
				StartedAt:    time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC),
				UpdatedAt:    time.Date(2024, 2, 1, 12, 2, 0, 0, time.UTC),
				LogsPath:     filepath.Join(tmpDir, "run-2001"),
			},
			AgenticAssessments: []AgenticAssessment{{Kind: "resource_heavy_for_domain", Severity: "medium"}},
		},
		{
			Run: WorkflowRun{
				DatabaseID:       2002,
				WorkflowName:     "worker",
				WorkflowPath:     ".github/workflows/worker.yml",
				Status:           "completed",
				Conclusion:       "success",
				Duration:         4 * time.Minute,
				TokenUsage:       700,
				MissingToolCount: 1,
				CreatedAt:        time.Date(2024, 2, 1, 12, 3, 0, 0, time.UTC),
				StartedAt:        time.Date(2024, 2, 1, 12, 3, 0, 0, time.UTC),
				UpdatedAt:        time.Date(2024, 2, 1, 12, 7, 0, 0, time.UTC),
				LogsPath:         filepath.Join(tmpDir, "run-2002"),
			},
			AwContext: &AwContext{
				Repo:           "github/gh-aw",
				RunID:          "2001",
				WorkflowID:     "github/gh-aw/.github/workflows/orchestrator.yml@refs/heads/main",
				WorkflowCallID: "2001-1",
				EventType:      "workflow_dispatch",
			},
			BehaviorFingerprint: &BehaviorFingerprint{ActuationStyle: "selective_write"},
			AgenticAssessments:  []AgenticAssessment{{Kind: "resource_heavy_for_domain", Severity: "high"}},
			MCPFailures:         []MCPFailureReport{{ServerName: "github", Status: "failed"}},
		},
	}

	logsData := buildLogsData(processedRuns, tmpDir, nil)

	if logsData.Summary.TotalEpisodes != 1 {
		t.Fatalf("Expected 1 episode, got %d", logsData.Summary.TotalEpisodes)
	}
	if logsData.Summary.HighConfidenceEpisodes != 1 {
		t.Fatalf("Expected 1 high-confidence episode, got %d", logsData.Summary.HighConfidenceEpisodes)
	}
	if len(logsData.Edges) != 1 {
		t.Fatalf("Expected 1 edge, got %d", len(logsData.Edges))
	}
	edge := logsData.Edges[0]
	if edge.SourceRunID != 2001 || edge.TargetRunID != 2002 {
		t.Fatalf("Expected edge 2001->2002, got %d->%d", edge.SourceRunID, edge.TargetRunID)
	}
	if edge.EdgeType != "dispatch_workflow" {
		t.Fatalf("Expected dispatch_workflow edge, got %s", edge.EdgeType)
	}
	episode := logsData.Episodes[0]
	if episode.Kind != "dispatch_workflow" {
		t.Fatalf("Expected dispatch_workflow episode, got %s", episode.Kind)
	}
	if episode.TotalRuns != 2 {
		t.Fatalf("Expected episode TotalRuns 2, got %d", episode.TotalRuns)
	}
	if episode.TotalTokens != 1000 {
		t.Fatalf("Expected episode TotalTokens 1000, got %d", episode.TotalTokens)
	}
	if episode.MCPFailureCount != 1 {
		t.Fatalf("Expected episode MCPFailureCount 1, got %d", episode.MCPFailureCount)
	}
	if episode.WriteCapableNodeCount != 1 {
		t.Fatalf("Expected episode WriteCapableNodeCount 1, got %d", episode.WriteCapableNodeCount)
	}
	if !episode.EscalationEligible {
		t.Fatalf("Expected dispatch episode to be escalation-eligible, got %+v", episode)
	}
	if episode.EscalationReason != "repeated_resource_heavy_for_domain" {
		t.Fatalf("Expected repeated_risky_runs escalation reason, got %s", episode.EscalationReason)
	}
	if episode.PrimaryWorkflow != "orchestrator" {
		t.Fatalf("Expected primary workflow orchestrator, got %s", episode.PrimaryWorkflow)
	}
	if episode.SuggestedRoute != "workflow:orchestrator" {
		t.Fatalf("Expected suggested route workflow:orchestrator, got %s", episode.SuggestedRoute)
	}
}

func TestBuildLogsDataJoinsWorkflowCallEpisode(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-workflow-call-*")
	parentDir := filepath.Join(tmpDir, "run-3001")
	childOneDir := filepath.Join(tmpDir, "run-3002")
	childTwoDir := filepath.Join(tmpDir, "run-3003")

	sharedInfo := map[string]any{
		"engine_id":   "copilot",
		"repository":  "github/gh-aw",
		"ref":         "refs/heads/main",
		"sha":         "abc123",
		"actor":       "monalisa",
		"run_attempt": "1",
	}
	writeTestAwInfo(t, parentDir, sharedInfo)
	writeTestAwInfo(t, childOneDir, map[string]any{
		"engine_id":   "copilot",
		"repository":  "github/gh-aw",
		"ref":         "refs/heads/main",
		"sha":         "abc123",
		"actor":       "monalisa",
		"run_attempt": "1",
		"event_name":  "workflow_call",
		"target_repo": "github/platform-workflows",
	})
	writeTestAwInfo(t, childTwoDir, map[string]any{
		"engine_id":   "copilot",
		"repository":  "github/gh-aw",
		"ref":         "refs/heads/main",
		"sha":         "abc123",
		"actor":       "monalisa",
		"run_attempt": "1",
		"event_name":  "workflow_call",
		"target_repo": "github/platform-workflows",
	})

	processedRuns := []ProcessedRun{
		{Run: WorkflowRun{DatabaseID: 3001, WorkflowName: "orchestrator", WorkflowPath: ".github/workflows/orchestrator.yml", Status: "completed", Conclusion: "success", Event: "push", HeadBranch: "main", HeadSha: "abc123", CreatedAt: time.Date(2024, 2, 2, 10, 0, 0, 0, time.UTC), StartedAt: time.Date(2024, 2, 2, 10, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2024, 2, 2, 10, 2, 0, 0, time.UTC), LogsPath: parentDir}},
		{Run: WorkflowRun{DatabaseID: 3002, WorkflowName: "worker-a", WorkflowPath: ".github/workflows/worker-a.yml", Status: "completed", Conclusion: "success", Event: "workflow_call", HeadBranch: "main", HeadSha: "abc123", CreatedAt: time.Date(2024, 2, 2, 10, 3, 0, 0, time.UTC), StartedAt: time.Date(2024, 2, 2, 10, 3, 0, 0, time.UTC), UpdatedAt: time.Date(2024, 2, 2, 10, 5, 0, 0, time.UTC), LogsPath: childOneDir}},
		{Run: WorkflowRun{DatabaseID: 3003, WorkflowName: "worker-b", WorkflowPath: ".github/workflows/worker-b.yml", Status: "completed", Conclusion: "success", Event: "workflow_call", HeadBranch: "main", HeadSha: "abc123", CreatedAt: time.Date(2024, 2, 2, 10, 6, 0, 0, time.UTC), StartedAt: time.Date(2024, 2, 2, 10, 6, 0, 0, time.UTC), UpdatedAt: time.Date(2024, 2, 2, 10, 8, 0, 0, time.UTC), LogsPath: childTwoDir}},
	}

	logsData := buildLogsData(processedRuns, tmpDir, nil)

	if logsData.Summary.TotalEpisodes != 1 {
		t.Fatalf("Expected 1 workflow_call episode, got %d", logsData.Summary.TotalEpisodes)
	}
	if len(logsData.Edges) != 2 {
		t.Fatalf("Expected 2 workflow_call edges, got %d", len(logsData.Edges))
	}
	for _, edge := range logsData.Edges {
		if edge.EdgeType != "workflow_call" {
			t.Fatalf("Expected workflow_call edge, got %+v", edge)
		}
	}
	episode := logsData.Episodes[0]
	if episode.Kind != "workflow_call" {
		t.Fatalf("Expected workflow_call episode kind, got %s", episode.Kind)
	}
	if episode.TotalRuns != 3 {
		t.Fatalf("Expected 3 runs in workflow_call episode, got %d", episode.TotalRuns)
	}
	if episode.Confidence != "high" {
		t.Fatalf("Expected high-confidence workflow_call episode, got %s", episode.Confidence)
	}
}

func TestBuildLogsDataDoesNotCoalesceWorkflowCallEpisodesWithoutRepositoryAndRef(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-workflow-call-low-info-*")
	firstDir := filepath.Join(tmpDir, "run-5001")
	secondDir := filepath.Join(tmpDir, "run-5002")

	writeTestAwInfo(t, firstDir, map[string]any{
		"engine_id":   "copilot",
		"sha":         "abc123",
		"actor":       "monalisa",
		"run_attempt": "1",
		"event_name":  "workflow_call",
	})
	writeTestAwInfo(t, secondDir, map[string]any{
		"engine_id":   "copilot",
		"sha":         "abc123",
		"actor":       "monalisa",
		"run_attempt": "1",
		"event_name":  "workflow_call",
	})

	processedRuns := []ProcessedRun{
		{Run: WorkflowRun{DatabaseID: 5001, WorkflowName: "worker-a", WorkflowPath: ".github/workflows/worker-a.yml", Status: "completed", Conclusion: "success", Event: "workflow_call", HeadBranch: "main", HeadSha: "abc123", CreatedAt: time.Date(2024, 2, 4, 10, 0, 0, 0, time.UTC), StartedAt: time.Date(2024, 2, 4, 10, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2024, 2, 4, 10, 2, 0, 0, time.UTC), LogsPath: firstDir}},
		{Run: WorkflowRun{DatabaseID: 5002, WorkflowName: "worker-b", WorkflowPath: ".github/workflows/worker-b.yml", Status: "completed", Conclusion: "success", Event: "workflow_call", HeadBranch: "main", HeadSha: "abc123", CreatedAt: time.Date(2024, 2, 4, 10, 3, 0, 0, time.UTC), StartedAt: time.Date(2024, 2, 4, 10, 3, 0, 0, time.UTC), UpdatedAt: time.Date(2024, 2, 4, 10, 5, 0, 0, time.UTC), LogsPath: secondDir}},
	}

	logsData := buildLogsData(processedRuns, tmpDir, nil)

	if logsData.Summary.TotalEpisodes != 2 {
		t.Fatalf("Expected 2 separate workflow_call episodes, got %d", logsData.Summary.TotalEpisodes)
	}
	if len(logsData.Edges) != 0 {
		t.Fatalf("Expected no workflow_call edges for low-information runs, got %d", len(logsData.Edges))
	}
	for _, episode := range logsData.Episodes {
		if episode.Kind != "workflow_call" {
			t.Fatalf("Expected workflow_call episode kind, got %s", episode.Kind)
		}
		if episode.Confidence != "low" {
			t.Fatalf("Expected low-confidence workflow_call episode, got %s", episode.Confidence)
		}
		if episode.TotalRuns != 1 {
			t.Fatalf("Expected isolated workflow_call episode, got %d runs", episode.TotalRuns)
		}
	}
}

func TestBuildLogsDataAttachesWorkflowRunEpisode(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-workflow-run-*")
	parentDir := filepath.Join(tmpDir, "run-4001")
	childDir := filepath.Join(tmpDir, "run-4002")

	writeTestAwInfo(t, parentDir, map[string]any{
		"engine_id":   "copilot",
		"repository":  "github/gh-aw",
		"ref":         "refs/heads/main",
		"sha":         "def456",
		"actor":       "hubot",
		"run_attempt": "1",
	})
	writeTestAwInfo(t, childDir, map[string]any{
		"engine_id":   "copilot",
		"repository":  "github/gh-aw",
		"ref":         "refs/heads/main",
		"sha":         "def456",
		"actor":       "hubot",
		"run_attempt": "1",
		"event_name":  "workflow_run",
	})

	processedRuns := []ProcessedRun{
		{Run: WorkflowRun{DatabaseID: 4001, WorkflowName: "ci", WorkflowPath: ".github/workflows/ci.yml", Status: "completed", Conclusion: "success", Event: "push", HeadBranch: "main", HeadSha: "def456", CreatedAt: time.Date(2024, 2, 3, 9, 0, 0, 0, time.UTC), StartedAt: time.Date(2024, 2, 3, 9, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2024, 2, 3, 9, 4, 0, 0, time.UTC), LogsPath: parentDir}},
		{Run: WorkflowRun{DatabaseID: 4002, WorkflowName: "observability-followup", WorkflowPath: ".github/workflows/followup.yml", Status: "completed", Conclusion: "success", Event: "workflow_run", HeadBranch: "main", HeadSha: "def456", CreatedAt: time.Date(2024, 2, 3, 9, 10, 0, 0, time.UTC), StartedAt: time.Date(2024, 2, 3, 9, 10, 0, 0, time.UTC), UpdatedAt: time.Date(2024, 2, 3, 9, 13, 0, 0, time.UTC), LogsPath: childDir}},
	}

	logsData := buildLogsData(processedRuns, tmpDir, nil)

	if len(logsData.Edges) != 1 {
		t.Fatalf("Expected 1 workflow_run edge, got %d", len(logsData.Edges))
	}
	edge := logsData.Edges[0]
	if edge.EdgeType != "workflow_run" {
		t.Fatalf("Expected workflow_run edge, got %+v", edge)
	}
	if edge.Confidence != "medium" {
		t.Fatalf("Expected medium-confidence workflow_run edge, got %+v", edge)
	}
	if logsData.Summary.TotalEpisodes != 1 {
		t.Fatalf("Expected 1 merged workflow_run episode, got %d", logsData.Summary.TotalEpisodes)
	}
}

// TestBuildMissingToolsSummary tests missing tools aggregation
func TestBuildMissingToolsSummary(t *testing.T) {
	t.Parallel()
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				WorkflowName: "Workflow A",
				DatabaseID:   1,
			},
			MissingTools: []MissingToolReport{
				{
					Tool:   "github_search",
					Reason: "Not allowed",
					ReportProvenance: ReportProvenance{
						WorkflowName: "Workflow A",
						RunID:        1,
					},
				},
			},
		},
		{
			Run: WorkflowRun{
				WorkflowName: "Workflow B",
				DatabaseID:   2,
			},
			MissingTools: []MissingToolReport{
				{
					Tool:   "github_search",
					Reason: "Permission denied",
					ReportProvenance: ReportProvenance{
						WorkflowName: "Workflow B",
						RunID:        2,
					},
				},
				{
					Tool:   "web_fetch",
					Reason: "Not configured",
					ReportProvenance: ReportProvenance{
						WorkflowName: "Workflow B",
						RunID:        2,
					},
				},
			},
		},
	}

	summary := buildMissingToolsSummary(processedRuns)

	// Should have 2 unique tools
	if len(summary) != 2 {
		t.Errorf("Expected 2 unique tools, got %d", len(summary))
	}

	// github_search should have count 2 and be first (sorted by count desc)
	if summary[0].Tool != "github_search" {
		t.Errorf("Expected first tool to be 'github_search', got '%s'", summary[0].Tool)
	}
	if summary[0].Count != 2 {
		t.Errorf("Expected github_search count 2, got %d", summary[0].Count)
	}
	if len(summary[0].Workflows) != 2 {
		t.Errorf("Expected github_search in 2 workflows, got %d", len(summary[0].Workflows))
	}

	// web_fetch should have count 1
	if summary[1].Tool != "web_fetch" {
		t.Errorf("Expected second tool to be 'web_fetch', got '%s'", summary[1].Tool)
	}
	if summary[1].Count != 1 {
		t.Errorf("Expected web_fetch count 1, got %d", summary[1].Count)
	}
}

// TestBuildLogsDataWithContinuation tests continuation field in logs data
func TestBuildLogsDataWithContinuation(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-*")

	// Create sample processed runs
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:   12345,
				Number:       1,
				WorkflowName: "Test Workflow",
				Status:       "completed",
				Conclusion:   "success",
				CreatedAt:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
				URL:          "https://github.com/test/repo/actions/runs/12345",
				LogsPath:     filepath.Join(tmpDir, "run-12345"),
			},
		},
		{
			Run: WorkflowRun{
				DatabaseID:   12344,
				Number:       2,
				WorkflowName: "Test Workflow",
				Status:       "completed",
				Conclusion:   "success",
				CreatedAt:    time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC),
				URL:          "https://github.com/test/repo/actions/runs/12344",
				LogsPath:     filepath.Join(tmpDir, "run-12344"),
			},
		},
	}

	// Create continuation data (simulating timeout scenario)
	continuation := &ContinuationData{
		Message:      "Timeout reached. Use these parameters to continue fetching more logs.",
		WorkflowName: "Test Workflow",
		Count:        100,
		StartDate:    "2024-01-01",
		EndDate:      "2024-12-31",
		Engine:       "copilot",
		Branch:       "main",
		AfterRunID:   0,
		BeforeRunID:  12344, // Continue from the oldest run
		Timeout:      50,
	}

	// Build logs data with continuation
	logsData := buildLogsData(processedRuns, tmpDir, continuation)

	// Verify continuation field is present
	if logsData.Continuation == nil {
		t.Fatal("Expected continuation field to be present, got nil")
	}

	// Verify continuation data
	if logsData.Continuation.Message != "Timeout reached. Use these parameters to continue fetching more logs." {
		t.Errorf("Expected continuation message, got '%s'", logsData.Continuation.Message)
	}
	if logsData.Continuation.WorkflowName != "Test Workflow" {
		t.Errorf("Expected WorkflowName 'Test Workflow', got '%s'", logsData.Continuation.WorkflowName)
	}
	if logsData.Continuation.BeforeRunID != 12344 {
		t.Errorf("Expected BeforeRunID 12344, got %d", logsData.Continuation.BeforeRunID)
	}
	if logsData.Continuation.Count != 100 {
		t.Errorf("Expected Count 100, got %d", logsData.Continuation.Count)
	}
	if logsData.Continuation.Engine != "copilot" {
		t.Errorf("Expected Engine 'copilot', got '%s'", logsData.Continuation.Engine)
	}

	// Test JSON serialization of continuation
	jsonOutput, err := json.MarshalIndent(logsData, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal logs data to JSON: %v", err)
	}

	// Verify continuation is in JSON
	var parsedData LogsData
	if err := json.Unmarshal(jsonOutput, &parsedData); err != nil {
		t.Fatalf("Failed to unmarshal logs data from JSON: %v", err)
	}

	if parsedData.Continuation == nil {
		t.Fatal("Expected continuation field in unmarshaled JSON, got nil")
	}
	if parsedData.Continuation.BeforeRunID != 12344 {
		t.Errorf("Expected BeforeRunID 12344 in unmarshaled JSON, got %d", parsedData.Continuation.BeforeRunID)
	}
}

// TestBuildLogsDataWithCountLimitContinuation tests that count-limit continuation
// (issued when a date-range fetch fills the requested count) is correctly serialised.
// This mirrors the scenario fixed in issue #42994 where daily audits only received
// partial run sets because the count cap was hit mid-range with no pagination signal.
func TestBuildLogsDataWithCountLimitContinuation(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-*")

	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:   20000,
				WorkflowName: "api-consumption-report",
				LogsPath:     filepath.Join(tmpDir, "run-20000"),
			},
		},
		{
			Run: WorkflowRun{
				DatabaseID:   19999,
				WorkflowName: "api-consumption-report",
				LogsPath:     filepath.Join(tmpDir, "run-19999"),
			},
		},
	}

	// Simulate what DownloadWorkflowLogs emits when fetchAllInRange is true and the
	// count limit is reached before all runs in the date window are fetched.
	// DownloadWorkflowLogs receives resolved absolute dates (the CLI resolves relative
	// shorthands like "-1d" before calling it), so use a concrete date here.
	continuation := &ContinuationData{
		Message:     "Count limit reached. Use these parameters to continue fetching more logs from the same date range.",
		StartDate:   "2026-06-01",
		Count:       100,
		BeforeRunID: 19999, // oldest processed run
		Timeout:     3,
	}

	logsData := buildLogsData(processedRuns, tmpDir, continuation)

	if logsData.Continuation == nil {
		t.Fatal("Expected continuation field, got nil")
	}
	if logsData.Continuation.BeforeRunID != 19999 {
		t.Errorf("Expected BeforeRunID 19999, got %d", logsData.Continuation.BeforeRunID)
	}
	if logsData.Continuation.StartDate != "2026-06-01" {
		t.Errorf("Expected StartDate '2026-06-01', got %q", logsData.Continuation.StartDate)
	}

	// Verify round-trip JSON serialisation preserves continuation.
	raw, err := json.Marshal(logsData)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var parsed LogsData
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if parsed.Continuation == nil {
		t.Fatal("Expected continuation in round-tripped JSON, got nil")
	}
	if parsed.Continuation.BeforeRunID != 19999 {
		t.Errorf("Round-trip BeforeRunID: got %d, want 19999", parsed.Continuation.BeforeRunID)
	}
}

// TestBuildLogsDataWithoutContinuation tests that continuation is omitted when nil
func TestBuildLogsDataWithoutContinuation(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-*")

	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:   12345,
				WorkflowName: "Test Workflow",
				LogsPath:     filepath.Join(tmpDir, "run-12345"),
			},
		},
	}

	// Build logs data without continuation
	logsData := buildLogsData(processedRuns, tmpDir, nil)

	// Verify continuation field is nil
	if logsData.Continuation != nil {
		t.Errorf("Expected continuation field to be nil, got %+v", logsData.Continuation)
	}

	// Test JSON serialization
	jsonOutput, err := json.Marshal(logsData)
	if err != nil {
		t.Fatalf("Failed to marshal logs data to JSON: %v", err)
	}

	// Verify continuation is omitted from JSON (due to omitempty tag)
	var parsedMap map[string]any
	if err := json.Unmarshal(jsonOutput, &parsedMap); err != nil {
		t.Fatalf("Failed to unmarshal logs data to map: %v", err)
	}

	if _, exists := parsedMap["continuation"]; exists {
		t.Error("Expected continuation field to be omitted from JSON when nil")
	}
}

// TestBuildMCPFailuresSummary tests MCP failures aggregation
func TestBuildMCPFailuresSummary(t *testing.T) {
	t.Parallel()
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				WorkflowName: "Workflow A",
				DatabaseID:   1,
			},
			MCPFailures: []MCPFailureReport{
				{
					ServerName: "playwright",
					Status:     "failed",
					ReportProvenance: ReportProvenance{
						WorkflowName: "Workflow A",
						RunID:        1,
					},
				},
			},
		},
		{
			Run: WorkflowRun{
				WorkflowName: "Workflow B",
				DatabaseID:   2,
			},
			MCPFailures: []MCPFailureReport{
				{
					ServerName: "playwright",
					Status:     "failed",
					ReportProvenance: ReportProvenance{
						WorkflowName: "Workflow B",
						RunID:        2,
					},
				},
			},
		},
	}

	summary := buildMCPFailuresSummary(processedRuns)

	// Should have 1 unique server
	if len(summary) != 1 {
		t.Errorf("Expected 1 unique server, got %d", len(summary))
	}

	// playwright should have count 2
	if summary[0].ServerName != "playwright" {
		t.Errorf("Expected server 'playwright', got '%s'", summary[0].ServerName)
	}
	if summary[0].Count != 2 {
		t.Errorf("Expected playwright count 2, got %d", summary[0].Count)
	}
	if len(summary[0].Workflows) != 2 {
		t.Errorf("Expected playwright in 2 workflows, got %d", len(summary[0].Workflows))
	}
}

// TestBuildLogsDataRepositoryAndOrganizationFields verifies that repository and organization
// fields are populated on both RunData and EpisodeData from aw_info.json.
func TestBuildLogsDataRepositoryAndOrganizationFields(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-repo-org-*")
	runDir := filepath.Join(tmpDir, "run-9001")

	writeTestAwInfo(t, runDir, map[string]any{
		"engine_id":  "copilot",
		"repository": "myorg/myrepo",
		"ref":        "refs/heads/main",
		"sha":        "abc123",
		"actor":      "monalisa",
	})

	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:   9001,
				WorkflowName: "test-workflow",
				WorkflowPath: ".github/workflows/test-workflow.yml",
				Status:       "completed",
				Conclusion:   "success",
				CreatedAt:    time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC),
				StartedAt:    time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC),
				UpdatedAt:    time.Date(2024, 3, 1, 10, 5, 0, 0, time.UTC),
				LogsPath:     runDir,
			},
		},
	}

	logsData := buildLogsData(processedRuns, tmpDir, nil)

	// Verify RunData fields
	if len(logsData.Runs) != 1 {
		t.Fatalf("Expected 1 run, got %d", len(logsData.Runs))
	}
	run := logsData.Runs[0]
	if run.Repository != "myorg/myrepo" {
		t.Errorf("Expected run.Repository 'myorg/myrepo', got %q", run.Repository)
	}
	if run.Organization != "myorg" {
		t.Errorf("Expected run.Organization 'myorg', got %q", run.Organization)
	}

	// Verify EpisodeData fields
	if len(logsData.Episodes) != 1 {
		t.Fatalf("Expected 1 episode, got %d", len(logsData.Episodes))
	}
	episode := logsData.Episodes[0]
	if episode.Repository != "myorg/myrepo" {
		t.Errorf("Expected episode.Repository 'myorg/myrepo', got %q", episode.Repository)
	}
	if episode.Organization != "myorg" {
		t.Errorf("Expected episode.Organization 'myorg', got %q", episode.Organization)
	}
}

// TestBuildLogsDataOrganizationEmptyWhenNoRepository verifies that organization is empty
// when no repository is set (e.g., older aw_info.json without repository field).
func TestBuildLogsDataOrganizationEmptyWhenNoRepository(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-no-repo-*")

	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:   9002,
				WorkflowName: "test-workflow",
				Status:       "completed",
				Conclusion:   "success",
				CreatedAt:    time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC),
			},
		},
	}

	logsData := buildLogsData(processedRuns, tmpDir, nil)

	if len(logsData.Runs) != 1 {
		t.Fatalf("Expected 1 run, got %d", len(logsData.Runs))
	}
	run := logsData.Runs[0]
	if run.Repository != "" {
		t.Errorf("Expected empty run.Repository, got %q", run.Repository)
	}
	if run.Organization != "" {
		t.Errorf("Expected empty run.Organization, got %q", run.Organization)
	}

	if len(logsData.Episodes) != 1 {
		t.Fatalf("Expected 1 episode, got %d", len(logsData.Episodes))
	}
	episode := logsData.Episodes[0]
	if episode.Repository != "" {
		t.Errorf("Expected empty episode.Repository, got %q", episode.Repository)
	}
	if episode.Organization != "" {
		t.Errorf("Expected empty episode.Organization, got %q", episode.Organization)
	}
}

func TestBuildLogsDataUsesGitHubRunMetadataWithoutAwInfo(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	logsData := buildLogsData([]ProcessedRun{{
		Run: WorkflowRun{
			DatabaseID:   9003,
			WorkflowName: "daily-report",
			WorkflowPath: ".github/workflows/daily-report.lock.yml",
			Status:       "completed",
			Conclusion:   "failure",
			Repository:   "myorg/myrepo",
			Actor:        "octocat",
			Attempt:      2,
			HeadSha:      "abc123",
			Event:        "schedule",
			LogsPath:     runDir,
		},
	}}, runDir, nil)

	require.Len(t, logsData.Runs, 1)
	run := logsData.Runs[0]
	assert.Equal(t, "myorg/myrepo", run.Repository)
	assert.Equal(t, "myorg", run.Organization)
	assert.Equal(t, "octocat", run.Actor)
	assert.Equal(t, "2", run.RunAttempt)
	assert.Equal(t, "abc123", run.SHA)
	assert.Equal(t, "schedule", run.EventName)
}

func TestApplyGitHubMetadataToRunDataPreservesExistingValues(t *testing.T) {
	t.Parallel()
	runData := RunData{
		Repository: "myorg/myrepo",
		SHA:        "abc123",
		Actor:      "octocat",
		EventName:  "schedule",
	}

	applyGitHubMetadataToRunData(&runData, WorkflowRun{})

	assert.Equal(t, "myorg/myrepo", runData.Repository)
	assert.Equal(t, "abc123", runData.SHA)
	assert.Equal(t, "octocat", runData.Actor)
	assert.Equal(t, "schedule", runData.EventName)
}

// TestInferWorkflowPathFromDisplayName verifies that display names are correctly
// slugified into conventional lock-file paths.
func TestInferWorkflowPathFromDisplayName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		displayName string
		want        string
	}{
		{
			name:        "simple display name with spaces",
			displayName: "Auto-Triage Issues",
			want:        ".github/workflows/auto-triage-issues.lock.yml",
		},
		{
			name:        "display name with mixed case and spaces",
			displayName: "CI Failure Doctor",
			want:        ".github/workflows/ci-failure-doctor.lock.yml",
		},
		{
			name:        "already kebab-case slug",
			displayName: "weekly-research",
			want:        ".github/workflows/weekly-research.lock.yml",
		},
		{
			name:        "display name with slash",
			displayName: "CI/CD Pipeline",
			want:        ".github/workflows/ci-cd-pipeline.lock.yml",
		},
		{
			name:        "display name with colon",
			displayName: "Deploy: Production",
			want:        ".github/workflows/deploy-production.lock.yml",
		},
		{
			name:        "empty display name",
			displayName: "",
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferWorkflowPathFromDisplayName(tt.displayName)
			if got != tt.want {
				t.Errorf("inferWorkflowPathFromDisplayName(%q) = %q, want %q", tt.displayName, got, tt.want)
			}
		})
	}
}

// TestBuildLogsDataInfersWorkflowPathFromAwInfo verifies that buildLogsData falls back
// to inferring workflow_path from aw_info.json when the GitHub API returned an empty path.
func TestBuildLogsDataInfersWorkflowPathFromAwInfo(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-infer-workflow-path-*")

	// Create a run with an empty WorkflowPath (simulating what the GitHub API returns
	// for scheduled/agentic workflow runs).
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   99901,
			WorkflowName: "Auto-Triage Issues",
			WorkflowPath: "", // empty — the bug scenario
			Status:       "completed",
			Conclusion:   "success",
			LogsPath:     tmpDir,
		},
	}

	// Write an aw_info.json that contains the workflow display name.
	awInfoPath := filepath.Join(tmpDir, "aw_info.json")
	awInfoData := map[string]any{
		"engine_id":     "copilot",
		"engine_name":   "GitHub Copilot CLI",
		"workflow_name": "Auto-Triage Issues",
	}
	awInfoBytes, err := json.Marshal(awInfoData)
	if err != nil {
		t.Fatalf("Failed to marshal aw_info: %v", err)
	}
	if err := os.WriteFile(awInfoPath, awInfoBytes, 0644); err != nil {
		t.Fatalf("Failed to write aw_info.json: %v", err)
	}

	logsData := buildLogsData([]ProcessedRun{processedRun}, tmpDir, nil)

	if len(logsData.Runs) != 1 {
		t.Fatalf("Expected 1 run, got %d", len(logsData.Runs))
	}

	got := logsData.Runs[0].WorkflowPath
	want := ".github/workflows/auto-triage-issues.lock.yml"
	if got != want {
		t.Errorf("WorkflowPath = %q, want %q", got, want)
	}
}

// TestBuildLogsDataPreservesExplicitWorkflowPath verifies that an explicit WorkflowPath
// set by the GitHub API is never overwritten by the inference fallback.
func TestBuildLogsDataPreservesExplicitWorkflowPath(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-preserve-workflow-path-*")

	explicitPath := ".github/workflows/custom-path.lock.yml"
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   99902,
			WorkflowName: "Auto-Triage Issues",
			WorkflowPath: explicitPath,
			Status:       "completed",
			Conclusion:   "success",
			LogsPath:     tmpDir,
		},
	}

	// Write an aw_info.json with a different workflow_name to ensure it does not
	// overwrite the explicit path that came from the GitHub API.
	awInfoPath := filepath.Join(tmpDir, "aw_info.json")
	awInfoData := map[string]any{
		"engine_id":     "copilot",
		"workflow_name": "Different Name",
	}
	awInfoBytes, err := json.Marshal(awInfoData)
	if err != nil {
		t.Fatalf("Failed to marshal aw_info.json content: %v", err)
	}
	if err := os.WriteFile(awInfoPath, awInfoBytes, 0644); err != nil {
		t.Fatalf("Failed to write aw_info.json: %v", err)
	}

	logsData := buildLogsData([]ProcessedRun{processedRun}, tmpDir, nil)

	if len(logsData.Runs) != 1 {
		t.Fatalf("Expected 1 run, got %d", len(logsData.Runs))
	}

	got := logsData.Runs[0].WorkflowPath
	if got != explicitPath {
		t.Errorf("WorkflowPath = %q, want %q (explicit path must not be overwritten)", got, explicitPath)
	}
}

// TestCompactLogsDataEpisodesEmptySliceNotNull verifies that compactLogsData produces
// empty slices (not nil) for Episodes and Edges when all episodes are standalone.
// nil slices marshal to JSON null, which breaks agent Python code that calls
// len(d.get('episodes', [])) — the default [] is only used when the key is absent,
// but null is a present key with a null value, causing a TypeError.
func TestCompactLogsDataEpisodesEmptySliceNotNull(t *testing.T) {
	t.Parallel()
	data := LogsData{
		Episodes: []EpisodeData{
			{
				EpisodeID: "standalone:1",
				Kind:      "standalone",
				TotalRuns: 1,
			},
		},
		Edges: []EpisodeEdge{},
	}

	compact := compactLogsData(data)

	// After compaction with only standalone episodes, Episodes and Edges must be
	// non-nil empty slices so JSON marshaling emits [] rather than null.
	if compact.Episodes == nil {
		t.Error("compactLogsData: Episodes must be an empty slice (not nil) when all episodes are standalone")
	}
	if compact.Edges == nil {
		t.Error("compactLogsData: Edges must be an empty slice (not nil) when all episodes are standalone")
	}
	if len(compact.Episodes) != 0 {
		t.Errorf("compactLogsData: expected 0 episodes, got %d", len(compact.Episodes))
	}

	// Marshal to JSON and confirm episodes/edges appear as [] not null.
	b, err := json.Marshal(compact)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	jsonStr := string(b)
	if !strings.Contains(jsonStr, `"episodes":[]`) {
		t.Errorf("expected JSON to contain \"episodes\":[], got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"edges":[]`) {
		t.Errorf("expected JSON to contain \"edges\":[], got: %s", jsonStr)
	}
}

func TestRenderLogsJSONIncludesMCPToolCalls(t *testing.T) {
	t.Parallel()

	logsData := buildLogsData([]ProcessedRun{{
		Run: WorkflowRun{DatabaseID: 12345, WorkflowName: "Test Workflow"},
		MCPToolUsage: &MCPToolUsageData{ToolCalls: []MCPToolCall{{
			ToolCallID: "call-1",
			Timestamp:  "2026-09-09T00:00:00Z",
			ServerName: "github",
			ToolName:   "issue_read",
			InputSize:  100,
			OutputSize: 200,
			Duration:   "25ms",
			Status:     "success",
		}}},
	}}, t.TempDir(), nil)

	var output bytes.Buffer
	require.NoError(t, renderLogsJSONToWriter(&output, logsData, false))

	var rendered LogsData
	require.NoError(t, json.Unmarshal(output.Bytes(), &rendered))
	require.NotNil(t, rendered.MCPToolUsage)
	require.Len(t, rendered.MCPToolUsage.ToolCalls, 1)
	assert.Equal(t, "github", rendered.MCPToolUsage.ToolCalls[0].ServerName)
	assert.Equal(t, "issue_read", rendered.MCPToolUsage.ToolCalls[0].ToolName)
}
