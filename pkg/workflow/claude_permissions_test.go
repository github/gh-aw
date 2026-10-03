//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClaudeRestrictivePermissions(t *testing.T) {
	data := &WorkflowData{
		Tools:       map[string]any{"bash": false, "edit": false, "web-fetch": false, "web-search": false},
		SafeOutputs: &SafeOutputsConfig{NoOp: &NoOpConfig{}},
	}
	step := strings.Join(NewClaudeEngine().GetExecutionSteps(data, "/tmp/log")[0], "\n")
	assert.Contains(t, step, "--permission-mode dontAsk")
	assert.Contains(t, step, "--disallowed-tools AskUserQuestion,Bash,WebFetch,WebSearch,Edit,Write,MultiEdit,NotebookEdit")
	assert.NotContains(t, step, "# - Write\n")
	assert.NotContains(t, step, "# - Bash\n")
	assert.Contains(t, step, "mcp__safeoutputs")
}

func TestClaudeScopedMemoryPermissions(t *testing.T) {
	data := &WorkflowData{
		Tools:             map[string]any{"edit": false, "cache-memory": true},
		CacheMemoryConfig: &CacheMemoryConfig{Caches: []CacheMemoryEntry{{ID: ""}}},
	}

	allowed := NewClaudeEngine().computeAllowedClaudeToolsString(data.Tools, nil, data.CacheMemoryConfig, nil, nil, nil)
	assert.Contains(t, allowed, "Edit(//tmp/gh-aw/cache-memory/**)")
	assert.NotContains(t, allowed, "Write(")
	assert.NotContains(t, allowed, "MultiEdit(")
	assert.Equal(t, []string{"AskUserQuestion", "WebFetch", "WebSearch", "Write", "MultiEdit", "NotebookEdit"}, claudeDisabledTools(data, allowed))
	assert.NotContains(t, claudeDisabledTools(data, allowed), "Edit")
	for _, tool := range []string{"Write", "MultiEdit", "NotebookEdit"} {
		assert.Contains(t, claudeDisabledTools(data, allowed), tool)
	}
}

func TestClaudeEditorGrantsAreIndependent(t *testing.T) {
	for _, tt := range []struct {
		name    string
		allowed string
		denied  []string
	}{
		{name: "no editors", denied: []string{"Edit", "Write", "MultiEdit", "NotebookEdit"}},
		{name: "scoped edit only", allowed: "Edit(//tmp/memory/**)", denied: []string{"Write", "MultiEdit", "NotebookEdit"}},
		{name: "write only", allowed: "Write", denied: []string{"Edit", "MultiEdit", "NotebookEdit"}},
		{name: "all explicit editors", allowed: "Edit,Write,MultiEdit,NotebookEdit"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			denied := claudeDisabledTools(&WorkflowData{}, tt.allowed)
			for _, tool := range []string{"Edit", "Write", "MultiEdit", "NotebookEdit"} {
				if strings.Contains(","+tt.allowed+",", ","+tool+",") || tool == "Edit" && strings.Contains(tt.allowed, "Edit(") {
					assert.NotContains(t, denied, tool)
				} else {
					assert.Contains(t, denied, tool)
				}
			}
			for _, tool := range tt.denied {
				assert.Contains(t, denied, tool)
			}
		})
	}
}

func TestClaudeMemoryOnlyWorkflowDeniesOtherEditorsWithPermissionOverrides(t *testing.T) {
	for _, mode := range []string{"dontAsk", "acceptEdits", "bypassPermissions"} {
		t.Run(mode, func(t *testing.T) {
			data := &WorkflowData{
				Tools:             map[string]any{"edit": false, "cache-memory": true},
				CacheMemoryConfig: &CacheMemoryConfig{Caches: []CacheMemoryEntry{{ID: "default"}}},
				EngineConfig:      &EngineConfig{PermissionMode: mode},
			}
			step := strings.Join(NewClaudeEngine().GetExecutionSteps(data, "/tmp/log")[0], "\n")
			assert.Contains(t, step, "Edit(//tmp/gh-aw/cache-memory/**)")
			assert.Contains(t, step, "--disallowed-tools AskUserQuestion,WebFetch,WebSearch,Write,MultiEdit,NotebookEdit")
			assert.Contains(t, step, "--permission-mode "+mode)
			assert.Contains(t, step, "GH_AW_CLAUDE_DISABLE_REPO_EDITS: true")
		})
	}
}

func TestClaudePermissionModeSchema(t *testing.T) {
	for _, mode := range []string{"dontAsk", "default", "acceptEdits", "auto", "plan", "bypassPermissions"} {
		t.Run(mode, func(t *testing.T) {
			_, err := NewCompiler(WithSkipValidation(true)).ParseWorkflowString("---\non: workflow_dispatch\nengine:\n  id: claude\n  permission-mode: "+mode+"\n---\nTask", "workflow.md")
			assert.NoError(t, err)
		})
	}
}
