package workflow

import (
	"strconv"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
)

// Keep this topology contract paired with the unset-bootstrap URL check in
// actions/setup/js/claude_harness.test.cjs.
func TestLLMProviderGatewayBaseURLFollowsNetworkTopology(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		provider     LLMProvider
		expectedURL  string
	}{
		{
			name: "default isolated Docker runtime",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf"}},
			},
			provider:    LLMProviderGitHub,
			expectedURL: "http://" + constants.AWFAPIProxyHostname + ":" + strconv.Itoa(constants.CopilotLLMGatewayPort),
		},
		{
			name: "host-access runtime",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf", Runtime: AgentRuntimeDockerSudoIptables}},
			},
			provider:    LLMProviderAnthropic,
			expectedURL: "http://host.docker.internal:" + strconv.Itoa(constants.ClaudeLLMGatewayPort),
		},
		{
			name: "non-isolated runtime",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{Disabled: true}},
			},
			provider:    LLMProviderOpenAI,
			expectedURL: "http://host.docker.internal:" + strconv.Itoa(constants.CodexLLMGatewayPort),
		},
		{
			name: "detection run on default isolated Docker runtime",
			workflowData: &WorkflowData{
				IsDetectionRun: true,
				SandboxConfig:  &SandboxConfig{Agent: &AgentSandboxConfig{Type: SandboxTypeAWF}},
			},
			provider:    LLMProviderGitHub,
			expectedURL: "http://" + constants.AWFAPIProxyHostname + ":" + strconv.Itoa(constants.CopilotLLMGatewayPort),
		},
		{
			name: "detection run on host-access runtime",
			workflowData: &WorkflowData{
				IsDetectionRun: true,
				SandboxConfig:  &SandboxConfig{Agent: &AgentSandboxConfig{Type: SandboxTypeAWF, Runtime: AgentRuntimeDockerSudoIptables}},
			},
			provider:    LLMProviderGitHub,
			expectedURL: "http://host.docker.internal:" + strconv.Itoa(constants.CopilotLLMGatewayPort),
		},
		{
			name: "evals run on default isolated Docker runtime",
			workflowData: &WorkflowData{
				IsEvalsRun:    true,
				SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{Type: SandboxTypeAWF}},
			},
			provider:    LLMProviderGitHub,
			expectedURL: "http://" + constants.AWFAPIProxyHostname + ":" + strconv.Itoa(constants.CopilotLLMGatewayPort),
		},
		{
			name: "evals run on host-access runtime",
			workflowData: &WorkflowData{
				IsEvalsRun:    true,
				SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{Type: SandboxTypeAWF, Runtime: AgentRuntimeDockerSudoIptables}},
			},
			provider:    LLMProviderGitHub,
			expectedURL: "http://host.docker.internal:" + strconv.Itoa(constants.CopilotLLMGatewayPort),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectedURL, llmProviderGatewayBaseURL(tt.provider, tt.workflowData))
		})
	}
}
