package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunOperationalValueReportSupportsWildcardWorkflow(t *testing.T) {
	originalLoadEvaluator := operationalValueReportLoadEvaluator
	originalListRuns := operationalValueReportListRuns
	originalGradeRun := operationalValueReportGradeRun
	t.Cleanup(func() {
		operationalValueReportLoadEvaluator = originalLoadEvaluator
		operationalValueReportListRuns = originalListRuns
		operationalValueReportGradeRun = originalGradeRun
	})

	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
	writeWorkflow := func(name, content string) {
		require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, name), []byte(content), 0o644))
	}
	writeWorkflow("daily-file-diet.md", "---\non: workflow_dispatch\ngraders:\n  operational-value:\n    run: .github/graders/daily-file-diet-operational-value.sh\n---\n# Daily File Diet\n")
	writeWorkflow("weekly-research.md", "---\non: workflow_dispatch\ngraders:\n  operational-value:\n    run: .github/graders/weekly-research-operational-value.sh\n    enabled: false\n---\n# Weekly Research\n")
	writeWorkflow("no-grader.md", "---\non: workflow_dispatch\n---\n# No Grader\n")

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(originalDir)) })

	evaluatorDigest := strings.Repeat("a", 64)
	var requestedWorkflows []string
	operationalValueReportLoadEvaluator = func(_ context.Context, workflowArg, _ string) (*operationalValueReportEvaluator, error) {
		requestedWorkflows = append(requestedWorkflows, workflowArg)
		definitionJSON := json.RawMessage(fmt.Sprintf(`{"schemaVersion":4,"grader":"operational-value","repository":"github/gh-aw","workflowName":%q,"sourcePath":".github/workflows/%s.md","adoption":{"commit":"abc","adoptedAt":"2026-08-01T00:00:00Z"},"operationalValue":"x","evidence":{"opportunity":"file","assignment":"largest","accepted":"Git","repositories":["github/gh-aw"],"collection":"Git API","maturation":"two days","zeroRule":"none","missingRule":"null"},"primaryMetric":{"id":"reduction","formula":"reduction / target","direction":"higher_is_better"},"baseline":{"mode":"attainment-only","value":null,"evidenceCutoff":null,"provenance":[]},"validationExamples":{"sample":{"valid":true}}}`, workflowArg, workflowArg))
		definition, err := parseOperationalValueReportDefinition(definitionJSON)
		if err != nil {
			return nil, err
		}
		return &operationalValueReportEvaluator{
			WorkflowID: workflowArg, EvaluatorRun: ".github/graders/" + workflowArg + "-operational-value.sh",
			EvaluatorDigest: evaluatorDigest, Definition: definition, GraderDirection: "higher-is-better",
		}, nil
	}
	operationalValueReportListRuns = func(context.Context, string, string, string, time.Time, time.Time) ([]operationalValueReportRun, error) {
		return []operationalValueReportRun{{ID: "1", Attempt: 1, CreatedAt: time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)}}, nil
	}
	operationalValueReportGradeRun = func(_ context.Context, evaluator *operationalValueReportEvaluator, run operationalValueReportRun, _ time.Time, _ string) operationalValueReportObservation {
		value := 0.5
		return operationalValueReportObservation{Run: run, Value: &value, Status: "pass", Mature: true, OpportunityKey: "go-file:pkg/example.go", EvaluatorDigest: evaluator.EvaluatorDigest, Source: "evaluator-replay"}
	}

	outputDir := filepath.Join(tmpDir, "report")
	err = RunOperationalValueReport(context.Background(), OperationalValueReportConfig{
		Workflow: "*", RepoOverride: "github/gh-aw", Until: "2026-08-31T00:00:00Z",
		OutputDir: outputDir, CacheDir: filepath.Join(tmpDir, "cache"),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"daily-file-diet"}, requestedWorkflows)
	_, err = os.Stat(filepath.Join(outputDir, "daily-file-diet-operational-value.json"))
	require.NoError(t, err)
}

func TestOperationalValueReportCommandFlags(t *testing.T) {
	cmd := newGradersOperationalValueReportCommand()
	assert.Equal(t, "report <workflow>", cmd.Use)
	for _, flag := range []string{"until", "output", "cache-dir", "concurrency", "refresh", "repo", "json"} {
		assert.NotNil(t, cmd.Flags().Lookup(flag), "expected --%s", flag)
	}
}

func TestRunOperationalValueReportWritesCompleteArtifacts(t *testing.T) {
	originalLoadEvaluator := operationalValueReportLoadEvaluator
	originalListRuns := operationalValueReportListRuns
	originalGradeRun := operationalValueReportGradeRun
	t.Cleanup(func() {
		operationalValueReportLoadEvaluator = originalLoadEvaluator
		operationalValueReportListRuns = originalListRuns
		operationalValueReportGradeRun = originalGradeRun
	})

	evaluatorDigest := strings.Repeat("a", 64)
	definitionJSON := json.RawMessage(`{"schemaVersion":4,"grader":"operational-value","repository":"github/gh-aw","workflowName":"Daily File Diet","sourcePath":".github/workflows/daily-file-diet.md","adoption":{"commit":"abc","adoptedAt":"2026-08-01T00:00:00Z"},"operationalValue":"Improve the assigned file.","evidence":{"opportunity":"file","assignment":"largest","accepted":"Git","repositories":["github/gh-aw"],"collection":"Git API","maturation":"two days","zeroRule":"none","missingRule":"null"},"primaryMetric":{"id":"reduction","formula":"reduction / target","direction":"higher_is_better"},"baseline":{"mode":"attainment-only","value":null,"evidenceCutoff":null,"provenance":[]},"validationExamples":{"sample":{"valid":true}}}`)
	definition, err := parseOperationalValueReportDefinition(definitionJSON)
	require.NoError(t, err)
	operationalValueReportLoadEvaluator = func(context.Context, string, string) (*operationalValueReportEvaluator, error) {
		return &operationalValueReportEvaluator{
			WorkflowID: "daily-file-diet", EvaluatorRun: ".github/graders/daily-file-diet-operational-value.sh",
			EvaluatorDigest: evaluatorDigest, Definition: definition, GraderDirection: "higher-is-better",
		}, nil
	}
	operationalValueReportListRuns = func(_ context.Context, repository, hostname, workflowFile string, _, _ time.Time) ([]operationalValueReportRun, error) {
		assert.Equal(t, "github/gh-aw", repository)
		assert.Equal(t, "github.com", hostname)
		assert.Equal(t, "daily-file-diet.lock.yml", workflowFile)
		return []operationalValueReportRun{{ID: "42", Attempt: 1, CreatedAt: time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)}}, nil
	}
	operationalValueReportGradeRun = func(_ context.Context, evaluator *operationalValueReportEvaluator, run operationalValueReportRun, _ time.Time, _ string) operationalValueReportObservation {
		value := 0.75
		return operationalValueReportObservation{Run: run, Value: &value, Status: "pass", Mature: true, OpportunityKey: "go-file:pkg/example.go", EvaluatorDigest: evaluator.EvaluatorDigest, Source: "evaluator-replay"}
	}

	outputDir := filepath.Join(t.TempDir(), "report")
	err = RunOperationalValueReport(context.Background(), OperationalValueReportConfig{
		Workflow: "daily-file-diet", RepoOverride: "github/gh-aw", Until: "2026-08-31T00:00:00Z",
		OutputDir: outputDir, CacheDir: filepath.Join(t.TempDir(), "cache"),
	})
	require.NoError(t, err)
	for _, extension := range []string{"json", "svg", "md"} {
		_, err := os.Stat(filepath.Join(outputDir, "daily-file-diet-operational-value."+extension))
		require.NoError(t, err)
	}
	reportData, err := os.ReadFile(filepath.Join(outputDir, "daily-file-diet-operational-value.json"))
	require.NoError(t, err)
	var report operationalValueReport
	require.NoError(t, json.Unmarshal(reportData, &report))
	require.Len(t, report.Observations, 1)
	assert.Equal(t, evaluatorDigest, report.Evaluator.SHA256)
	assert.Equal(t, ".github/graders/daily-file-diet-operational-value.sh", report.Evaluator.Path)
	assert.Equal(t, "2026-08-31T00:00:00Z", report.Window.EndAt)
	assert.NotEqual(t, report.Window.EndAt, report.GeneratedAt)
}
