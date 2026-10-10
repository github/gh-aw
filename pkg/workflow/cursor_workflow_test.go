//go:build !integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/actions/setup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestCursorSampleNativeSessionRuntime(t *testing.T) {
	content, err := os.ReadFile("../../.github/workflows/shared/cursor.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(content), "---", 3)
	require.Len(t, parts, 3)
	var frontmatter struct {
		Engine EngineDefinition `yaml:"engine"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &frontmatter))
	engine, err := NewBehaviorDefinedEngine(&frontmatter.Engine)
	require.NoError(t, err)
	data := &WorkflowData{Name: "Cursor", Model: "cursor/auto", EngineConfig: &EngineConfig{}}
	steps := strings.Join(flattenSteps(engine.GetExecutionSteps(data, "/tmp/gh-aw/agent-stdio.log")), "\n")
	assert.Contains(t, steps, "--output-format stream-json")
	assert.NotContains(t, steps, "--stream-partial-output")
	assert.Contains(t, steps, `stdio: ["ignore", 2, 2]`)
	assert.Contains(t, engine.GetLogParserScriptSource(), `require("./parse_cursor_log.cjs").parseCursorLog`)
	assert.Equal(t, "cursor_log_parser", engine.GetLogParserScriptId())
	embedded, err := setup.SessionParserSources.ReadFile("js/parse_cursor_log.cjs")
	require.NoError(t, err)
	assert.Contains(t, string(embedded), "function parseCursorLog(")
}
