//go:build !integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnifiedSessionAgentArtifactPaths(t *testing.T) {
	for _, id := range []string{"claude", "copilot", "codex", "gemini", "pi"} {
		t.Run(id, func(t *testing.T) {
			compiler := NewCompiler()
			engine, err := compiler.getAgenticEngine(id)
			require.NoError(t, err)
			data := &WorkflowData{AI: id}
			paths := compiler.collectArtifactPaths(data, engine, constants.AgentStdioLogPath, nil)
			assert.Contains(t, paths, "/tmp/gh-aw/agent-session.jsonl")
			assert.Contains(t, paths, agentExecutionExitCodePath)
		})
	}
}
