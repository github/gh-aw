//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClaudeDetectionPreservesProviderAndContextPolicy(t *testing.T) {
	config := &EngineConfig{
		ID: "claude", LLMProvider: LLMProviderGitHub, Bare: true, PermissionMode: "dontAsk",
		Auth:     &EngineAuthConfig{Type: "github-oidc", Provider: "anthropic"},
		MaxTurns: "10", MaxAICredits: 50, Agent: "main-only", Cwd: "/main-only",
	}
	data := &WorkflowData{
		AI: "claude", EngineConfig: config,
		SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
	}
	step := strings.Join(NewCompiler().buildDetectionEngineExecutionStep(data), "")
	assert.Contains(t, step, "GH_AW_LLM_PROVIDER: github")
	assert.Contains(t, step, "--bare")
	assert.Contains(t, step, "--permission-mode dontAsk")
	external := resolveExternalDetectorEngineConfig(data, "claude")
	assert.Equal(t, config.Auth, external.Auth)
	assert.True(t, external.Bare)
	assert.Equal(t, LLMProviderGitHub, external.LLMProvider)
	assert.Empty(t, external.MaxTurns)
	assert.Zero(t, external.MaxAICredits)
	assert.Empty(t, external.Agent)
	assert.Empty(t, external.Cwd)
	assert.Equal(t, "main-only", config.Agent)
	assert.Equal(t, int64(50), config.MaxAICredits)
}

func TestDetectionDoesNotEnableMainCopilotSDK(t *testing.T) {
	data := &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot", CopilotSDK: true}}
	config := resolveExternalDetectorEngineConfig(data, "copilot")
	assert.False(t, config.CopilotSDK)
	assert.True(t, data.EngineConfig.CopilotSDK)
}
