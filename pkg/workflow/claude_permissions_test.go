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
	assert.Contains(t, step, "--disallowed-tools AskUserQuestion,Bash,WebFetch,WebSearch,Edit,Write,NotebookEdit")
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
	assert.NotContains(t, claudeDisabledTools(data, allowed), "Edit")
}

func TestClaudePermissionModeSchema(t *testing.T) {
	for _, mode := range []string{"dontAsk", "default", "acceptEdits", "auto", "plan", "bypassPermissions"} {
		t.Run(mode, func(t *testing.T) {
			_, err := NewCompiler(WithSkipValidation(true)).ParseWorkflowString("---\non: workflow_dispatch\nengine:\n  id: claude\n  permission-mode: "+mode+"\n---\nTask", "workflow.md")
			assert.NoError(t, err)
		})
	}
}
