//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func awfConfigAgentTimeout(t *testing.T, jsonStr string) (int, bool) {
	t.Helper()
	var parsed struct {
		Container *struct {
			AgentTimeout *int `json:"agentTimeout"`
		} `json:"container"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed))
	if parsed.Container == nil || parsed.Container.AgentTimeout == nil {
		return 0, false
	}
	return *parsed.Container.AgentTimeout, true
}

func TestBuildAWFConfigJSON_AgentTimeoutDockerRuntime(t *testing.T) {
	supported := string(constants.AWFAgentTimeoutSteeringMinVersion)

	tests := []struct {
		name           string
		timeoutMinutes string
		version        string
		wantTimeout    int
		wantPresent    bool
	}{
		{name: "literal timeout on supported AWF", timeoutMinutes: "timeout-minutes: 45", version: supported, wantTimeout: 45, wantPresent: true},
		{name: "literal timeout on latest AWF", timeoutMinutes: "timeout-minutes: 45", version: "latest", wantTimeout: 45, wantPresent: true},
		{name: "omitted timeout uses the agent step default", timeoutMinutes: "", version: supported, wantTimeout: 20, wantPresent: true},
		{name: "expression timeout is omitted", timeoutMinutes: "timeout-minutes: ${{ inputs.timeout }}", version: supported},
		{name: "runtime-variable default timeout is omitted", timeoutMinutes: "timeout-minutes: " + compilerenv.BuildTimeoutMinutesExpression(compilerenv.DefaultTimeoutMinutes, 20), version: supported},
		{name: "unsupported AWF version", timeoutMinutes: "timeout-minutes: 45", version: "v0.28.50"},
		{name: "default AWF version", timeoutMinutes: "timeout-minutes: 45", version: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(compilerenv.DefaultTimeoutMinutes, "")
			jsonStr, err := BuildAWFConfigJSON(AWFCommandConfig{
				EngineName:     "copilot",
				AllowedDomains: "github.com",
				WorkflowData: &WorkflowData{
					EngineConfig:   &EngineConfig{ID: "copilot"},
					TimeoutMinutes: tt.timeoutMinutes,
					NetworkPermissions: &NetworkPermissions{
						Firewall: &FirewallConfig{Enabled: true, Version: tt.version},
					},
				},
			})
			require.NoError(t, err)
			got, present := awfConfigAgentTimeout(t, jsonStr)
			assert.Equal(t, tt.wantPresent, present, "agentTimeout presence: %s", jsonStr)
			assert.Equal(t, tt.wantTimeout, got)
		})
	}
}

func TestResolveAWFAgentTimeoutMinutesNeverBelowStepTimeout(t *testing.T) {
	t.Setenv(compilerenv.DefaultTimeoutMinutes, "")
	firewall := &FirewallConfig{Enabled: true, Version: string(constants.AWFAgentTimeoutSteeringMinVersion)}

	t.Run("raised to the literal step timeout", func(t *testing.T) {
		timeout := TemplatableInt32("60")
		data := &WorkflowData{
			TimeoutMinutes:    "timeout-minutes: 30",
			ParsedFrontmatter: &FrontmatterConfig{TimeoutMinutes: &timeout},
		}
		assert.Equal(t, 60, resolveAWFAgentTimeoutMinutes(data, firewall))
	})

	t.Run("omitted when the step timeout is an expression", func(t *testing.T) {
		timeout := TemplatableInt32("${{ inputs.timeout }}")
		data := &WorkflowData{
			TimeoutMinutes:    "timeout-minutes: 30",
			ParsedFrontmatter: &FrontmatterConfig{TimeoutMinutes: &timeout},
		}
		assert.Zero(t, resolveAWFAgentTimeoutMinutes(data, firewall))
	})
}

func TestBuildAWFConfigJSON_AgentTimeoutThreatDetection(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		jobs        map[string]any
		wantTimeout int
		wantPresent bool
	}{
		{name: "default detection job timeout", version: string(constants.AWFAgentTimeoutSteeringMinVersion), wantTimeout: 10, wantPresent: true},
		{
			name:        "configured detection job timeout",
			version:     string(constants.AWFAgentTimeoutSteeringMinVersion),
			jobs:        map[string]any{string(constants.DetectionJobName): map[string]any{"timeout-minutes": 15}},
			wantTimeout: 15,
			wantPresent: true,
		},
		{name: "unsupported AWF version", version: "v0.28.50"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &WorkflowData{
				AI:             "copilot",
				TimeoutMinutes: "timeout-minutes: 45",
				Jobs:           tt.jobs,
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true, Version: tt.version},
				},
			}
			detection := buildThreatDetectionWorkflowData(source, "copilot")
			jsonStr, err := BuildAWFConfigJSON(AWFCommandConfig{
				EngineName:     "copilot",
				AllowedDomains: "github.com",
				WorkflowData:   detection,
			})
			require.NoError(t, err)
			got, present := awfConfigAgentTimeout(t, jsonStr)
			assert.Equal(t, tt.wantPresent, present, "agentTimeout presence: %s", jsonStr)
			assert.Equal(t, tt.wantTimeout, got)
		})
	}
}

func TestCompileWorkflow_AgentTimeoutDockerRuntime(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    []string
	}{
		{name: "supported AWF version", version: string(constants.AWFAgentTimeoutSteeringMinVersion), want: []string{"45", "10"}},
		{name: "unsupported AWF version", version: "v0.28.50"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowsDir := t.TempDir()
			markdown := `---
on:
  workflow_dispatch:
engine: copilot
strict: false
timeout-minutes: 45
sandbox:
  agent:
    id: awf
    version: ` + tt.version + `
safe-outputs:
  create-issue:
---

# Test agentTimeout
`
			testFile := filepath.Join(workflowsDir, "test-agent-timeout.md")
			require.NoError(t, os.WriteFile(testFile, []byte(markdown), 0o644))
			require.NoError(t, NewCompiler().CompileWorkflow(testFile))

			lockContent, err := os.ReadFile(filepath.Join(workflowsDir, "test-agent-timeout.lock.yml"))
			require.NoError(t, err)
			matches := regexp.MustCompile(`\\"agentTimeout\\":(\d+)`).FindAllStringSubmatch(string(lockContent), -1)
			got := make([]string, 0, len(matches))
			for _, match := range matches {
				got = append(got, match[1])
			}
			if len(tt.want) == 0 {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tt.want, got, "agent and detection AWF configs should carry their own step timeouts")
		})
	}
}
