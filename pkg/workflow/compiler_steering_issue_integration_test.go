//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestCompileSteeringIssue(t *testing.T) {
	tmpDir := testutil.TempDir(t, "steering-issue")
	workflowPath := filepath.Join(tmpDir, "steering.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(`---
on: workflow_dispatch
engine: copilot
strict: false
permissions:
  issues: read
safe-outputs:
  steer: true
  create-pull-request:
    branch-prefix: "signed/"
---

# Steering test

Create a change and open a pull request.
`), 0o644))

	compiler := NewCompiler(WithVersion("dev"))
	compiler.SetActionMode(ActionModeDev)
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	compiled, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)
	yaml := string(compiled)

	activation := extractJobSection(yaml, "activation")
	agent := extractJobSection(yaml, "agent")
	safeOutputs := extractJobSection(yaml, "safe_outputs")
	conclusion := extractJobSection(yaml, "conclusion")

	assert.Contains(t, activation, "id: create-steering-issue")
	assert.Contains(t, activation, `GH_AW_WORKFLOW_ID: "steering"`)
	assert.Contains(t, activation, "GH_AW_ENGINE_ID: ${{ steps.generate_aw_info.outputs.engine_id }}")
	assert.Contains(t, activation, "GH_AW_ENGINE_MODEL: ${{ "+modelRoutingInactiveInActivation+" && steps.generate_aw_info.outputs.model || '' }}")
	assert.Contains(t, activation, "issues: write")
	assert.NotContains(t, activation, "contents: write")
	assert.NotContains(t, activation, "pull-requests: write")
	assert.NotContains(t, activation, "checks: write")
	assert.NotContains(t, agent, "gh-aw/pre-created")
	assert.NotContains(t, safeOutputs, "pre_created_pull_request")
	assert.Contains(t, conclusion, "complete_steering_issue.cjs")
	assert.Contains(t, conclusion, "GH_AW_STEERING_ISSUE_NUMBER")
	failureStepStart := strings.Index(conclusion, "name: Handle agent failure")
	require.NotEqual(t, -1, failureStepStart)
	failureStepEnd := strings.Index(conclusion[failureStepStart:], "name: Report failed jobs")
	require.NotEqual(t, -1, failureStepEnd)
	failureStep := conclusion[failureStepStart : failureStepStart+failureStepEnd]
	assert.Contains(t, failureStep, "GH_AW_FAILURE_ISSUE_NUMBER: ${{ needs.activation.outputs.steering_issue_number }}")
}

func TestCompileRejectsLegacyPreCreatePullRequest(t *testing.T) {
	tmpDir := testutil.TempDir(t, "legacy-pre-create-pull-request")
	workflowPath := filepath.Join(tmpDir, "pre-create.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(`---
on: workflow_dispatch
engine: copilot
strict: false
safe-outputs:
  create-pull-request:
    pre-create: true
---

# Legacy pre-create test
`), 0o644))

	compiler := NewCompiler(WithVersion("dev"))
	compiler.SetActionMode(ActionModeDev)
	err := compiler.CompileWorkflow(workflowPath)
	require.ErrorContains(t, err, "pre-create")
}

func TestCompileRejectsCreatePullRequestSteer(t *testing.T) {
	tmpDir := testutil.TempDir(t, "nested-steer")
	workflowPath := filepath.Join(tmpDir, "nested-steer.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(`---
on: workflow_dispatch
engine: copilot
strict: false
safe-outputs:
  create-pull-request:
    steer: true
---

# Nested steering test
`), 0o644))

	compiler := NewCompiler(WithVersion("dev"))
	compiler.SetActionMode(ActionModeDev)
	err := compiler.CompileWorkflow(workflowPath)
	require.ErrorContains(t, err, "steer")
}
