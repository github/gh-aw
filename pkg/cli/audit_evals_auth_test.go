package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvalsAuthUnsupportedFinding(t *testing.T) {
	for _, location := range []string{"", "evals", "abcd1234-evals", "usage/evals", "abcd1234-usage/evals"} {
		t.Run(location, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, location)
			require.NoError(t, os.MkdirAll(dir, 0o755))
			marker := map[string]string{
				"status": "not_evaluated", "reason_code": "auth_unsupported", "reason": "sensitive provider body",
				"run_id": "123", "run_attempt": "2",
			}
			data, err := json.Marshal(marker)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "evals_status.json"), data, 0o600))
			run := WorkflowRun{LogsPath: root, DatabaseID: 123, Attempt: 2, Conclusion: "success"}
			findings := generateEvalsAuthFindings(run)
			require.Len(t, findings, 1)
			assert.Equal(t, AuditFindingEvalsAuthUnsupported, findings[0].Code)
			assert.Contains(t, findings[0].Description, "auth_unsupported")
			assert.Contains(t, findings[0].Description, "personal access token")
			assert.NotContains(t, findings[0].Description, "sensitive")
			allFindings := generateFindings(ProcessedRun{Run: run}, MetricsData{}, nil)
			assert.Contains(t, allFindings, AuditFinding{
				Code: findings[0].Code, Category: findings[0].Category, Severity: findings[0].Severity,
				Title: findings[0].Title, Description: findings[0].Description, Impact: findings[0].Impact,
				PlaybookURL: auditFindingPlaybookURL(findings[0].Code),
			})
			assert.NotEmpty(t, findEvalsNotEvaluatedStatusFile(root))
			assert.False(t, runHasEvals(root, false))
			run.Attempt = 1
			assert.Empty(t, generateEvalsAuthFindings(run))
			run.Attempt = 2
			run.DatabaseID = 456
			assert.Empty(t, generateEvalsAuthFindings(run))
		})
	}
}

func TestEvalsAuthUnsupportedIgnoresOtherMarkers(t *testing.T) {
	for _, marker := range []string{
		`not-json`,
		`{"status":"not_evaluated","reason_code":"judge_failed","run_id":"123","run_attempt":"2"}`,
		`{"status":"evaluated","reason_code":"auth_unsupported","run_id":"123","run_attempt":"2"}`,
		`{"status":"not_evaluated","reason_code":"auth_unsupported"}`,
	} {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "evals_status.json"), []byte(marker), 0o600))
		assert.Empty(t, generateEvalsAuthFindings(WorkflowRun{LogsPath: root, DatabaseID: 123, Attempt: 2}))
	}
}
