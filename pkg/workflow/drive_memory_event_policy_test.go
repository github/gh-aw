//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const driveMemoryAcceptedCondition = "always() && needs.detection.result == 'success' && needs.detection.outputs.detection_success == 'true' && needs.agent.result == 'success'"

func TestCompiledDriveMemoryEventPolicy(t *testing.T) {
	// Drives use the preview service's repository/ref/name namespace, not the
	// Actions cache service's event-based publication policy. In particular,
	// admitted slash commands must retain their validated drive writes.
	for _, trigger := range []string{
		"push",
		"workflow_dispatch",
		"issue_comment",
		"issues",
		"discussion_comment",
		"pull_request",
		"pull_request_target",
		"workflow_call",
		"{push: {}, issue_comment: {}}",
		"{workflow_call: {}, discussion_comment: {}}",
		"{slash_command: {name: remember, events: [issues, issue_comment, pull_request_comment]}}",
	} {
		t.Run(trigger, func(t *testing.T) {
			compiled, content := compileDriveMemoryEventFixture(t, trigger, "true", false, false, false)
			jobs := compiled["jobs"].(map[string]any)
			agent := extractJobSection(content, "agent")
			publisher := jobs[updateDriveMemoryJobName].(map[string]any)
			assert.Equal(t, driveMemoryAcceptedCondition, strings.Join(strings.Fields(publisher["if"].(string)), " "))
			assert.ElementsMatch(t, []any{"agent", "detection", "activation"}, publisher["needs"])
			assert.Equal(t, map[string]any{"contents": "read", "id-token": "write", "drives": "write"}, publisher["permissions"])
			assert.Contains(t, agent, "write: false")
			assert.NotContains(t, agent, "actions/gh-drives-preview/commit@")
			assert.Contains(t, agent, "Upload drive-memory data as artifact")
			update := extractJobSection(content, updateDriveMemoryJobName)
			assert.Contains(t, update, "write: true")
			assert.Contains(t, update, "Check drive-memory for concurrent updates")
			assert.Contains(t, update, "Validate drive-memory before commit")
			assert.Contains(t, update, "actions/gh-drives-preview/commit@")
			assert.NotContains(t, update, "cache-mode:")
		})
	}
}

func TestCompiledDriveMemoryPersistenceModes(t *testing.T) {
	for _, detection := range []string{"true", "false", `"${{ vars.DETECT == 'true' }}"`} {
		for _, ghes := range []bool{false, true} {
			for _, dryRun := range []bool{false, true} {
				for _, restoreOnly := range []bool{false, true} {
					t.Run(fmt.Sprintf("detection=%s/ghes=%t/dry-run=%t/restore-only=%t", detection, ghes, dryRun, restoreOnly), func(t *testing.T) {
						compiled, content := compileDriveMemoryEventFixture(t, "{push: {}, issue_comment: {}}", detection, ghes, dryRun, restoreOnly)
						jobs := compiled["jobs"].(map[string]any)
						agent := extractJobSection(content, "agent")
						if dryRun || restoreOnly {
							assert.Contains(t, agent, "write: false")
							assert.NotContains(t, content, "actions/gh-drives-preview/commit@")
							assert.NotContains(t, jobs, updateDriveMemoryJobName)
						} else if detection == "false" {
							assert.Contains(t, agent, "write: true")
							assert.Contains(t, agent, "actions/gh-drives-preview/commit@")
							assert.NotContains(t, jobs, updateDriveMemoryJobName)
						} else {
							assert.Contains(t, agent, "write: false")
							assert.NotContains(t, agent, "actions/gh-drives-preview/commit@")
							publisher := jobs[updateDriveMemoryJobName].(map[string]any)
							assert.Equal(t, driveMemoryAcceptedCondition, strings.Join(strings.Fields(publisher["if"].(string)), " "))
						}
					})
				}
			}
		}
	}
}

func compileDriveMemoryEventFixture(t *testing.T, trigger, detection string, ghes, dryRun, restoreOnly bool) (map[string]any, string) {
	t.Helper()
	dir := testutil.TempDir(t, "drive-memory-event-policy")
	path := filepath.Join(dir, "drive.md")
	source := fmt.Sprintf(`---
on: %s
strict: false
engine: copilot
permissions:
  contents: read
tools:
  github: false
  drive-memory:
    drive-name: agent-state
    restore-only: %t
    allowed-extensions: [".json"]
safe-outputs:
  create-issue:
  threat-detection: %s
---
Read the saved drive state and persist this admitted run's notes.
`, trigger, restoreOnly, detection)
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	compiler := NewCompiler()
	compiler.gitRoot = dir
	compiler.SetSkipValidation(true)
	compiler.SetGHESCompat(ghes)
	compiler.SetDryRun(dryRun)
	require.NoError(t, compiler.CompileWorkflow(path))
	content, err := os.ReadFile(filepath.Join(dir, "drive.lock.yml"))
	require.NoError(t, err)
	var compiled map[string]any
	require.NoError(t, yaml.Unmarshal(content, &compiled))
	require.NoError(t, compiler.validateGitHubActionsSchemaFromParsed(compiled))
	return compiled, string(content)
}
