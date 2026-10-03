//go:build !integration

package workflow

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func loadOpenCodeSample(t *testing.T) *EngineDefinition {
	t.Helper()
	content, err := os.ReadFile("../../.github/workflows/shared/opencode.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(content), "---", 3)
	require.Len(t, parts, 3)
	var frontmatter struct {
		Engine EngineDefinition `yaml:"engine"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &frontmatter))
	require.NotNil(t, frontmatter.Engine.Behaviors)
	return &frontmatter.Engine
}

func TestOpenCodeSampleInstallationAndCapabilities(t *testing.T) {
	def := loadOpenCodeSample(t)
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	assert.True(t, engine.GetCapabilities().MCP)
	assert.True(t, engine.GetCapabilities().ToolsAllowlist, "configured MCP tools must not be discarded")
	assert.True(t, engine.GetCapabilities().MaxTurns)
	assert.False(t, engine.GetCapabilities().Plugins, "native npm plugins are not Agent Plugins")
	assert.Contains(t, engine.GetAgentManifestFiles(), "opencode.json")
	assert.Contains(t, engine.GetAgentManifestFiles(), "opencode.jsonc")
	assert.Contains(t, engine.GetAgentManifestPathPrefixes(), ".opencode/")

	data := &WorkflowData{
		Name:         "OpenCode",
		Model:        "copilot/auto",
		EngineConfig: &EngineConfig{Version: def.Version},
	}
	steps := strings.Join(flattenSteps(engine.GetInstallationSteps(data)), "\n")
	assert.Contains(t, steps, "opencode-ai@"+def.Version)
	assert.Contains(t, steps, "NPM_CONFIG_MIN_RELEASE_AGE: '3'")
	assert.Contains(t, steps, "opencode --version")
	assert.NotContains(t, steps, "--ignore-scripts")
	assert.Contains(t, engine.GetMCPConfigAdapterFilename(), "opencode_mcp_config_adapter.cjs")
	assert.Contains(t, engine.GetLogParserScriptSource(), `require("./parse_opencode_log.cjs").parseOpenCodeLog`)
}

func TestOpenCodeSampleVersionOutsideCooldown(t *testing.T) {
	def := loadOpenCodeSample(t)
	require.Equal(t, "1.18.33", def.Version, "update release publication evidence with the pin")
	publishedAt, err := time.Parse(time.RFC3339, "2026-09-28T04:22:46Z")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(publishedAt), 72*time.Hour)
}

func TestOpenCodeSampleProviderExecution(t *testing.T) {
	def := loadOpenCodeSample(t)
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	for _, test := range []struct {
		model    string
		provider string
		secret   string
	}{
		{"copilot/auto", "github", "COPILOT_GITHUB_TOKEN"},
		{"anthropic/claude-sonnet-4-5", "anthropic", "ANTHROPIC_API_KEY"},
		{"openai/gpt-5", "openai", "OPENAI_API_KEY"},
		{"codex/gpt-5", "openai", "CODEX_API_KEY"},
	} {
		t.Run(test.provider+"-"+test.model, func(t *testing.T) {
			data := &WorkflowData{
				Name:         "OpenCode",
				Model:        test.model,
				EngineConfig: &EngineConfig{Version: def.Version},
			}
			steps := engine.GetExecutionSteps(data, "/tmp/gh-aw/agent-stdio.log")
			require.NotEmpty(t, steps)
			execution := strings.Join(steps[len(steps)-1], "\n")
			assert.Contains(t, execution, "opencode_harness.cjs")
			assert.Contains(t, execution, "OPENCODE_MODEL: "+test.model)
			assert.Contains(t, execution, "GH_AW_LLM_PROVIDER: "+test.provider)
			assert.Contains(t, execution, "AWF_REFLECT_ENABLED: 1")
			assert.Contains(t, execution, "GH_AW_MAX_TURNS:")
			assert.Contains(t, execution, "--format json")
			assert.Contains(t, execution, "--exclude-env "+test.secret)
			assert.NotContains(t, execution, `"$(cat /tmp/gh-aw/aw-prompts/prompt.txt)"`)
			assert.NotContains(t, def.Behaviors.HarnessScript, "172.30.0.30")
		})
	}
}
