package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadOperationalValueReportEvaluatorResolvesRelativeWorkflowPath(t *testing.T) {
	workingDirectory, err := os.Getwd()
	require.NoError(t, err)
	t.Chdir(filepath.Join(workingDirectory, "..", ".."))

	evaluator, err := loadOperationalValueReportEvaluator(context.Background(), "daily-file-diet", "https://github.com")
	require.NoError(t, err)
	t.Cleanup(func() {
		if evaluator.cleanup != nil {
			evaluator.cleanup()
		}
	})
	assert.Equal(t, ".github/workflows/daily-file-diet.md", evaluator.Definition.SourcePath)
	assert.Equal(t, ".github/graders/daily-file-diet-operational-value.sh", evaluator.EvaluatorRun)
	assert.NotEqual(t, evaluator.EvaluatorRun, evaluator.EvaluatorPath)
}

func TestParseOperationalValueReportDefinition(t *testing.T) {
	definition, err := parseOperationalValueReportDefinition([]byte(`{
		"schemaVersion":4,
		"grader":"operational-value",
		"repository":"github/gh-aw",
		"workflowName":"Daily File Diet",
		"sourcePath":".github/workflows/daily-file-diet.md",
		"adoption":{"commit":"abc123","adoptedAt":"2025-11-15T13:36:21Z"},
		"operationalValue":"Decompose the assigned oversized file.",
		"evidence":{"opportunity":"An oversized file","assignment":"largest file","accepted":"Git evidence","repositories":["github/gh-aw"],"collection":"GitHub API","maturation":"48 hours","zeroRule":"none","missingRule":"null"},
		"primaryMetric":{"id":"decomposition","formula":"reduction / target","direction":"higher_is_better"},
		"diagnosticMetrics":[{"id":"health","name":"Repository health","formula":"healthy / total","direction":"higher_is_better","aggregation":"latest"}],
		"baseline":{"mode":"baseline-comparable","value":0.25,"evidenceCutoff":"2025-11-15T13:27:11Z","provenance":[{"repository":"github/gh-aw","kind":"commit","ref":"abc123"}]},
		"validationExamples":{"sample":{"valid":true}}
	}`))
	require.NoError(t, err)
	assert.Equal(t, "github/gh-aw", definition.Repository)
	require.Len(t, definition.DiagnosticMetrics, 1)
	assert.Equal(t, "health", definition.DiagnosticMetrics[0].ID)
	require.NotNil(t, definition.Baseline.Value)
	assert.InDelta(t, 0.25, *definition.Baseline.Value, 0.000001)
}

func TestParseOperationalValueReportDefinitionRejectsInvalidDiagnosticMetric(t *testing.T) {
	_, err := parseOperationalValueReportDefinition([]byte(`{
		"schemaVersion":4,
		"grader":"operational-value",
		"repository":"github/gh-aw",
		"workflowName":"Daily File Diet",
		"sourcePath":".github/workflows/daily-file-diet.md",
		"adoption":{"commit":"abc123","adoptedAt":"2025-11-15T13:36:21Z"},
		"operationalValue":"Decompose the assigned oversized file.",
		"evidence":{"opportunity":"An oversized file","assignment":"largest file","accepted":"Git evidence","repositories":["github/gh-aw"],"collection":"GitHub API","maturation":"48 hours","zeroRule":"none","missingRule":"null"},
		"primaryMetric":{"id":"decomposition","formula":"reduction / target","direction":"higher_is_better"},
		"diagnosticMetrics":[{"id":"decomposition","name":"Duplicate","formula":"x","direction":"higher_is_better","aggregation":"latest"}],
		"baseline":{"mode":"attainment-only","value":null,"evidenceCutoff":null,"provenance":[]},
		"validationExamples":{"sample":{"valid":true}}
	}`))
	require.ErrorContains(t, err, "duplicated")
}

func TestParseOperationalValueReportDefinitionRejectsIncompleteContract(t *testing.T) {
	_, err := parseOperationalValueReportDefinition([]byte(`{
		"schemaVersion":4,
		"grader":"operational-value",
		"baseline":{"mode":"attainment-only","value":null}
	}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repository, workflowName, and sourcePath")
}

func TestParseOperationalValueReportDefinitionRejectsMissingEvidenceContractFields(t *testing.T) {
	_, err := parseOperationalValueReportDefinition([]byte(`{
		"schemaVersion":4,
		"grader":"operational-value",
		"repository":"github/gh-aw",
		"workflowName":"Daily File Diet",
		"sourcePath":".github/workflows/daily-file-diet.md",
		"adoption":{"commit":"abc123","adoptedAt":"2025-11-15T13:36:21Z"},
		"operationalValue":"Decompose the assigned oversized file.",
		"evidence":{"opportunity":"x","accepted":"y","repositories":["github/gh-aw"]},
		"primaryMetric":{"id":"decomposition","formula":"reduction / target","direction":"higher_is_better"},
		"baseline":{"mode":"attainment-only","value":null,"evidenceCutoff":null,"provenance":[]},
		"validationExamples":{"sample":{"valid":true}}
	}`))
	require.ErrorContains(t, err, "complete evidence contract fields")
}

func TestParseOperationalValueReportDefinitionRejectsComparableBaselineWithoutCutoff(t *testing.T) {
	_, err := parseOperationalValueReportDefinition([]byte(`{
		"schemaVersion":4,
		"grader":"operational-value",
		"repository":"github/gh-aw",
		"workflowName":"Daily File Diet",
		"sourcePath":".github/workflows/daily-file-diet.md",
		"adoption":{"commit":"abc123","adoptedAt":"2025-11-15T13:36:21Z"},
		"operationalValue":"Decompose the assigned oversized file.",
		"evidence":{"opportunity":"An oversized file","assignment":"largest file","accepted":"Git evidence","repositories":["github/gh-aw"],"collection":"GitHub API","maturation":"48 hours","zeroRule":"none","missingRule":"null"},
		"primaryMetric":{"id":"decomposition","formula":"reduction / target","direction":"higher_is_better"},
		"baseline":{"mode":"baseline-comparable","value":0.25,"evidenceCutoff":null,"provenance":[{"repository":"github/gh-aw","kind":"commit","ref":"abc123"}]},
		"validationExamples":{"sample":{"valid":true}}
	}`))
	require.ErrorContains(t, err, "baseline.evidenceCutoff")
}
