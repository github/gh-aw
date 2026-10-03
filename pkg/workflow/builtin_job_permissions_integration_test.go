package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestBuiltinJobPermissionsAugmentation verifies that user-declared permissions under
// jobs.<built-in>.permissions (e.g. safe_outputs, conclusion) are merged additively into the
// compiled built-in jobs, so scopes such as id-token: write are retained in the lock file.
func TestBuiltinJobPermissionsAugmentation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "builtin-job-permissions-augmentation")
	compiler := NewCompiler()

	workflowContent := `---
on:
  issue_comment:
    types: [created]
engine: copilot
strict: false
permissions:
  contents: read
  id-token: write
safe-outputs:
  add-comment:
jobs:
  safe_outputs:
    permissions:
      id-token: write
      contents: read
      issues: write
  conclusion:
    permissions:
      id-token: write
      contents: read
      issues: write
---
Builtin job permissions augmentation
`

	workflowFile := filepath.Join(tmpDir, "builtin-job-permissions-augmentation.md")
	require.NoError(t, os.WriteFile(workflowFile, []byte(workflowContent), 0644))
	require.NoError(t, compiler.CompileWorkflow(workflowFile))

	lockFile := filepath.Join(tmpDir, "builtin-job-permissions-augmentation.lock.yml")
	lockBytes, err := os.ReadFile(lockFile)
	require.NoError(t, err)

	var lock map[string]any
	require.NoError(t, yaml.Unmarshal(lockBytes, &lock))
	jobs, ok := lock["jobs"].(map[string]any)
	require.True(t, ok)

	for _, jobName := range []string{"safe_outputs", "conclusion"} {
		job, ok := jobs[jobName].(map[string]any)
		require.True(t, ok, "expected %s job in compiled workflow", jobName)
		perms, ok := job["permissions"].(map[string]any)
		require.True(t, ok, "expected %s permissions to be a map", jobName)
		assert.Equal(t, "write", perms["id-token"], "%s should retain id-token: write from jobs.%s.permissions", jobName, jobName)
	}
}

func TestConclusionJobPermissionsDerivedFromSafeOutputsIntegration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		safeOutputsConfig    string
		wantIssues           string
		wantActionsRead      bool
		wantPullRequestsRead bool
	}{
		{
			name: "report-failed-jobs default keeps issues write and actions read",
			safeOutputsConfig: `
  report-failure-as-issue: false
  report-incomplete: false
  noop:
    report-as-issue: false
  threat-detection: false
`,
			wantIssues:      "write",
			wantActionsRead: true,
		},
		{
			name: "report-failure-as-issue upgrades issues read to write",
			safeOutputsConfig: `
  report-failed-jobs: false
  report-failure-as-issue: true
  report-incomplete: false
  threat-detection: false
  create-project:
    target-owner: my-org
`,
			wantIssues:           "write",
			wantActionsRead:      true,
			wantPullRequestsRead: true,
		},
		{
			name: "detection reporting keeps issues write without actions read",
			safeOutputsConfig: `
  report-failed-jobs: false
  report-failure-as-issue: false
  report-incomplete: false
  noop:
    report-as-issue: false
  threat-detection: true
`,
			wantIssues:      "write",
			wantActionsRead: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			perms := compileConclusionPermissionsFromSafeOutputs(t, tc.safeOutputsConfig)

			assert.Equal(t, tc.wantIssues, perms["issues"])

			if tc.wantActionsRead {
				assert.Equal(t, "read", perms["actions"])
			} else {
				assert.NotContains(t, perms, "actions")
			}
			if tc.wantPullRequestsRead {
				assert.Equal(t, "read", perms["pull-requests"])
			} else {
				assert.NotContains(t, perms, "pull-requests")
			}
		})
	}
}

func compileConclusionPermissionsFromSafeOutputs(t *testing.T, safeOutputsConfig string) map[string]any {
	t.Helper()

	tmpDir := testutil.TempDir(t, "conclusion-permissions-integration")
	workflowFile := filepath.Join(tmpDir, "workflow.md")
	lockFile := filepath.Join(tmpDir, "workflow.lock.yml")

	workflowContent := `---
on:
  workflow_dispatch:
engine: copilot
strict: false
permissions:
  contents: read
safe-outputs:
` + strings.Trim(safeOutputsConfig, "\n") + `
---
Conclusion permissions integration fixture
`

	require.NoError(t, os.WriteFile(workflowFile, []byte(workflowContent), 0644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowFile))

	lockBytes, err := os.ReadFile(lockFile)
	require.NoError(t, err)

	var lock map[string]any
	require.NoError(t, yaml.Unmarshal(lockBytes, &lock))

	jobs, ok := lock["jobs"].(map[string]any)
	require.True(t, ok)

	conclusion, ok := jobs["conclusion"].(map[string]any)
	require.True(t, ok, "expected conclusion job in compiled workflow")

	perms, ok := conclusion["permissions"].(map[string]any)
	require.True(t, ok, "expected conclusion permissions to be a map")
	return perms
}
