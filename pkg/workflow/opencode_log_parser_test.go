//go:build !integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestOpenCodeLogParserBehavior(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/shared/opencode.md")
	require.NoError(t, err)
	frontmatter := strings.SplitN(string(source), "---", 3)
	require.Len(t, frontmatter, 3)
	var wrapper struct {
		Engine EngineDefinition `yaml:"engine"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(frontmatter[1]), &wrapper))
	engine, err := NewBehaviorDefinedEngine(&wrapper.Engine)
	require.NoError(t, err)
	assert.Equal(t, "opencode_log_parser", engine.GetLogParserScriptId())
	assert.Contains(t, wrapper.Engine.Behaviors.Execution.Args, "--format")
	assert.Contains(t, wrapper.Engine.Behaviors.Execution.Args, "json")
	assert.Equal(t, `function parseLog(logContent) {
  return require("./parse_opencode_log.cjs").parseOpenCodeLog(logContent);
}
`, wrapper.Engine.Behaviors.LogParser)
	steps := engine.GetExecutionSteps(&WorkflowData{Name: "OpenCode test"}, "/tmp/agent.log")
	require.GreaterOrEqual(t, len(steps), 2)
	var rendered []string
	for _, step := range steps {
		rendered = append(rendered, strings.Join(step, "\n"))
	}
	parserStep := strings.Join(rendered, "\n")
	assert.Contains(t, parserStep, "if: always()")
	assert.Contains(t, parserStep, "createEngineLogParser")
	assert.Contains(t, parserStep, "parseFunction: parseLog")
	assert.Contains(t, parserStep, `require("./parse_opencode_log.cjs")`)
	execution := strings.Join(steps[len(steps)-1], "\n")
	assert.Contains(t, execution, "opencode run --format json --print-logs --log-level DEBUG")
}
