//go:build !integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestCursorSharedEngineGatewayRouting(t *testing.T) {
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

	data := &WorkflowData{
		Name:  "cursor-test",
		Model: "cursor/auto",
		EngineConfig: &EngineConfig{
			ID: "cursor",
		},
		Tools: map[string]any{},
		NetworkPermissions: &NetworkPermissions{
			Allowed:           []string{},
			ExplicitlyDefined: true,
			Firewall:          &FirewallConfig{Enabled: true},
		},
	}
	steps := engine.GetExecutionSteps(data, "/tmp/cursor.log")
	require.NotEmpty(t, steps)
	execution := strings.Join(steps[len(steps)-1], "\n")
	assert.Contains(t, execution, `\"openai\":{\"host\":\"api2.cursor.sh\"}`)
	assert.Contains(t, execution, "--exclude-env OPENAI_API_KEY")
	assert.Contains(t, execution, "OPENAI_API_KEY: ${{ secrets.CURSOR_API_KEY }}")
	assert.Contains(t, execution, "CURSOR_API_KEY: awf-proxy")
	assert.NotContains(t, execution, "CURSOR_API_KEY: ${{ secrets.CURSOR_API_KEY }}")
	assert.Contains(t, execution, "AWF_REFLECT_ENABLED: 1")
	assert.NotContains(t, engine.allowedDomains(data), "api2.cursor.sh")
	assert.NotContains(t, engine.allowedDomains(data), "agentn.global.api5.cursor.sh")
	assert.Nil(t, data.EngineConfig.Env, "definition defaults must not mutate the caller")

	t.Run("workflow overrides retain precedence", func(t *testing.T) {
		data.EngineConfig.Env = map[string]string{
			"OPENAI_BASE_URL": "https://cursor-proxy.example",
		}
		steps := engine.GetExecutionSteps(data, "/tmp/cursor.log")
		execution := strings.Join(steps[len(steps)-1], "\n")
		assert.Contains(t, execution, `\"openai\":{\"host\":\"cursor-proxy.example\"}`)
		assert.Contains(t, execution, "--exclude-env OPENAI_API_KEY")
		assert.Contains(t, execution, "CURSOR_API_KEY: awf-proxy")
		assert.Equal(t, map[string]string{"OPENAI_BASE_URL": "https://cursor-proxy.example"}, data.EngineConfig.Env)
	})
}
