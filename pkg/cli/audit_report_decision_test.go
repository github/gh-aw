//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const decisionGateTestReceipt = `{
  "version":1,"decision":"skip","status":"evaluated","configured_alias":"decision",
  "resolved_model":"test-model","provider":"test-provider","duration_ms":42,
  "fail_open":false,"question_count":2,"evaluated_count":1,"unevaluated_count":1,
  "deterministic_eligible":true,"activation_allowed":false,"usage_complete":true,
  "usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12},
  "unevaluated_questions":[{"index":1,"reason":"short_circuit"}],
  "questions":[{"index":0,"decision":"skip","status":"evaluated","cause":"model_skip",
    "reason":"Sensitive customer summary and personal information","configured_alias":"decision","resolved_model":"test-model",
    "provider":"test-provider","duration_ms":42,"fail_open":false,"evaluated":true,
    "usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}},
    {"index":1,"decision":"run","status":"unevaluated","cause":"short_circuit",
     "reason":"short_circuit","duration_ms":0,"evaluated":false,"fail_open":false,"usage":null}],
  "prompt":"not part of the public receipt"
}`

func TestExtractDecisionGateData(t *testing.T) {
	t.Parallel()
	for _, directory := range []string{"", "decision-gate", "abc123-decision-gate"} {
		t.Run(directory, func(t *testing.T) {
			t.Parallel()
			runDir := t.TempDir()
			artifactDir := filepath.Join(runDir, directory)
			require.NoError(t, os.MkdirAll(artifactDir, 0700))
			require.NoError(t, os.WriteFile(filepath.Join(artifactDir, "decision-gate.json"), []byte(decisionGateTestReceipt), 0600))

			data := extractDecisionGateData(runDir)
			require.NotNil(t, data)
			assert.Equal(t, "skip", data.Decision)
			assert.Equal(t, "decision", data.ConfiguredAlias)
			assert.Equal(t, "test-model", data.ResolvedModel)
			assert.Equal(t, "test-provider", data.Provider)
			assert.EqualValues(t, 42, data.DurationMS)
			assert.True(t, data.DeterministicEligible)
			assert.False(t, data.ActivationAllowed)
			assert.Equal(t, 1, data.UnevaluatedCount)
			require.Len(t, data.UnevaluatedQuestions, 1)
			assert.Equal(t, 1, data.UnevaluatedQuestions[0].Index)
			assert.Equal(t, "short_circuit", data.UnevaluatedQuestions[0].Reason)
			assert.Equal(t, 2, data.QuestionCount)
			assert.Equal(t, 1, data.EvaluatedCount)
			assert.True(t, data.UsageComplete)
			require.NotNil(t, data.Usage)
			assert.EqualValues(t, 12, data.Usage.TotalTokens)
			require.Len(t, data.Questions, 2)
			assert.Equal(t, 0, data.Questions[0].Index)
			assert.True(t, data.Questions[0].Evaluated)
			assert.Empty(t, data.Questions[0].Reason)
			require.NotNil(t, data.Questions[0].Usage)
			assert.EqualValues(t, 12, data.Questions[0].Usage.TotalTokens)
			assert.False(t, data.Questions[1].Evaluated)
			assert.Empty(t, data.Questions[1].Reason)
			assert.Equal(t, "short_circuit", data.Questions[1].Cause)
			assert.Nil(t, data.Questions[1].Usage)
			encoded, err := json.Marshal(AuditData{DecisionGate: data})
			require.NoError(t, err)
			assert.Contains(t, string(encoded), `"decision_gate":`)
			assert.NotContains(t, string(encoded), "not part of the public receipt")
			assert.NotContains(t, string(encoded), "Sensitive customer summary")
		})
	}
}

func TestExtractDecisionGateDataOptionalAndUnsafe(t *testing.T) {
	t.Parallel()
	assert.Nil(t, extractDecisionGateData(""))
	assert.Nil(t, extractDecisionGateData(t.TempDir()))
	for _, raw := range []string{
		`{"version":1,"decision":"run","status": "sensitive trailing`,
		`{"version":2,"decision":"run","status":"evaluated"}`,
		`{"version":1,"decision":"run","status":"evaluated","questions":[{},{},{},{},{},{},{},{},{}]}`,
		`{"version":1,"decision":"run","status":"evaluated","unevaluated_questions":[{},{},{},{},{},{},{},{},{}]}`,
		`null`,
		decisionGateTestReceipt + strings.Repeat(" ", maxDecisionGateArtifactBytes),
	} {
		t.Run(raw[:min(len(raw), 30)], func(t *testing.T) {
			t.Parallel()
			runDir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(runDir, "decision-gate.json"), []byte(raw), 0600))
			assert.Nil(t, extractDecisionGateData(runDir))
		})
	}
}

func TestExtractDecisionGateDataFailOpen(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	raw := `{"version":1,"decision":"run","status":"fail_open","cause":"model_unavailable",
		"duration_ms":0,"fail_open":true,"unevaluated_count":2,"questions":[],
		"deterministic_eligible":true,"activation_allowed":true}`
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "decision-gate.json"), []byte(raw), 0600))
	data := extractDecisionGateData(runDir)
	require.NotNil(t, data)
	assert.Equal(t, "run", data.Decision)
	assert.Equal(t, "fail_open", data.Status)
	assert.Equal(t, "model_unavailable", data.Cause)
	assert.True(t, data.FailOpen)
	assert.True(t, data.DeterministicEligible)
	assert.True(t, data.ActivationAllowed)
	assert.Equal(t, 2, data.UnevaluatedCount)
}

func TestExtractDecisionGateDataIneligible(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	raw := `{"version":1,"decision":"skip","status":"ineligible","duration_ms":0,
		"deterministic_eligible":false,"activation_allowed":false,"unevaluated_count":2,
		"questions":[{"index":0,"decision":"run","status":"unevaluated","reason":"ineligible","duration_ms":0},
			{"index":1,"decision":"run","status":"unevaluated","reason":"ineligible","duration_ms":0}]}`
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "decision-gate.json"), []byte(raw), 0600))
	data := extractDecisionGateData(runDir)
	require.NotNil(t, data)
	assert.Equal(t, "ineligible", data.Status)
	assert.False(t, data.DeterministicEligible)
	assert.False(t, data.ActivationAllowed)
	assert.Equal(t, 2, data.UnevaluatedCount)
	require.Len(t, data.Questions, 2)
	assert.Equal(t, "unevaluated", data.Questions[1].Status)
	assert.Empty(t, data.Questions[1].Reason)
}

func TestExtractDecisionGateDataRejectsSymlinks(t *testing.T) {
	t.Parallel()
	for _, linkDirectory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[linkDirectory], func(t *testing.T) {
			t.Parallel()
			runDir := t.TempDir()
			targetDir := t.TempDir()
			target := filepath.Join(targetDir, "decision-gate.json")
			require.NoError(t, os.WriteFile(target, []byte(decisionGateTestReceipt), 0600))
			if linkDirectory {
				require.NoError(t, os.Symlink(targetDir, filepath.Join(runDir, "decision-gate")))
			} else {
				require.NoError(t, os.Symlink(target, filepath.Join(runDir, "decision-gate.json")))
			}
			assert.Nil(t, extractDecisionGateData(runDir))
		})
	}
}

func TestAuditDecisionGateAbsentOmitsJSON(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(AuditData{})
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), `"decision_gate"`)
}

func TestAuditDecisionGateIncludesSkippedAgentReceipt(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "decision-gate.json"), []byte(decisionGateTestReceipt), 0600))
	data := assembleAuditData(auditDataInputs{
		processedRun: ProcessedRun{Run: WorkflowRun{LogsPath: runDir, Conclusion: "skipped"}},
	})
	require.NotNil(t, data.DecisionGate)
	assert.Equal(t, "skip", data.DecisionGate.Decision)
	assert.Equal(t, "test-model", data.DecisionGate.Questions[0].ResolvedModel)
}
