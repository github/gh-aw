//go:build !integration

package constants_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/constants"
)

func TestSpec_Constants_FormattingAndEngines(t *testing.T) {
	assert.Equal(t, "gh aw", constants.CLIExtensionPrefix.String(), "CLIExtensionPrefix should match the documented CLI prefix")
	assert.Equal(t, constants.ExpressionBreakThreshold, constants.LineLength(100), "ExpressionBreakThreshold should match the documented value")
	assert.Equal(t, constants.MaxExpressionLineLength, constants.LineLength(120), "MaxExpressionLineLength should match the documented value")
	tests := []struct {
		name string
		got  constants.EngineName
		want string
	}{
		{name: "default", got: constants.DefaultEngine, want: "copilot"},
		{name: "copilot", got: constants.CopilotEngine, want: "copilot"},
		{name: "claude", got: constants.ClaudeEngine, want: "claude"},
		{name: "codex", got: constants.CodexEngine, want: "codex"},
		{name: "gemini", got: constants.GeminiEngine, want: "gemini"},
		{name: "pi", got: constants.PiEngine, want: "pi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.got), "engine identifier should match the README")
		})
	}
}

func TestSpec_Constants_FeatureFlags(t *testing.T) {
	tests := []struct {
		name string
		got  constants.FeatureFlag
		want string
	}{
		{name: "mcp scripts", got: constants.MCPScriptsFeatureFlag, want: "mcp-scripts"},
		{name: "mcp gateway", got: constants.MCPGatewayFeatureFlag, want: "mcp-gateway"},
		{name: "disable xpia prompt", got: constants.DisableXPIAPromptFeatureFlag, want: "disable-xpia-prompt"},
		{name: "difc proxy", got: constants.DIFCProxyFeatureFlag, want: "difc-proxy"},
		{name: "diagnostic logs", got: constants.AwfDiagnosticLogsFeatureFlag, want: "awf-diagnostic-logs"},
		{name: "byok copilot", got: constants.ByokCopilotFeatureFlag, want: "byok-copilot"},
		{name: "integrity reactions", got: constants.IntegrityReactionsFeatureFlag, want: "integrity-reactions"},
		{name: "group concurrency queue", got: constants.GroupConcurrencyQueueFeatureFlag, want: "group-concurrency-queue"},
		{name: "disable sandbox agent", got: constants.DangerouslyDisableSandboxAgentFeatureFlag, want: "dangerously-disable-sandbox-agent"},
		{name: "gh-aw detection", got: constants.GHAWDetectionFeatureFlag, want: "gh-aw-detection"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.got), "feature flag should match the documented value")
		})
	}
}

func TestSpec_Constants_JobAndServerIdentifiers(t *testing.T) {
	jobs := []struct {
		name string
		got  constants.JobName
		want string
	}{
		{name: "agent", got: constants.AgentJobName, want: "agent"},
		{name: "activation", got: constants.ActivationJobName, want: "activation"},
		{name: "pre activation", got: constants.PreActivationJobName, want: "pre_activation"},
		{name: "pre activation compatibility", got: constants.PreActivationHyphenJobName, want: "pre-activation"},
		{name: "detection", got: constants.DetectionJobName, want: "detection"},
		{name: "evals", got: constants.EvalsJobName, want: "evals"},
		{name: "safe outputs", got: constants.SafeOutputsJobName, want: "safe_outputs"},
		{name: "safe outputs compatibility", got: constants.SafeOutputsHyphenJobName, want: "safe-outputs"},
		{name: "upload assets", got: constants.UploadAssetsJobName, want: "upload_assets"},
		{name: "upload code scanning", got: constants.UploadCodeScanningJobName, want: "upload_code_scanning_sarif"},
		{name: "upload coverage", got: constants.UploadCodeCoverageJobName, want: "upload_code_coverage"},
		{name: "conclusion", got: constants.ConclusionJobName, want: "conclusion"},
		{name: "unlock", got: constants.UnlockJobName, want: "unlock"},
	}
	for _, tt := range jobs {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.got.String(), "job name should match the documented value")
		})
	}
	servers := []struct {
		name string
		got  constants.MCPServerID
		want string
	}{
		{name: "github", got: constants.GitHubMCPServerID, want: "github"},
		{name: "safe outputs", got: constants.SafeOutputsMCPServerID, want: "safeoutputs"},
		{name: "mcp scripts", got: constants.MCPScriptsMCPServerID, want: "mcpscripts"},
		{name: "agentic workflows", got: constants.AgenticWorkflowsMCPServerID, want: "agenticworkflows"},
	}
	for _, tt := range servers {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.got.String(), "MCP server ID should match the documented value")
		})
	}
}

func TestSpec_PublicAPI_GetEngineOption(t *testing.T) {
	tests := []struct {
		name   string
		engine string
		found  bool
	}{
		{name: "documented copilot engine", engine: "copilot", found: true},
		{name: "unknown engine", engine: "not-a-documented-engine", found: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			option := constants.GetEngineOption(tt.engine)
			if tt.found {
				require.NotNil(t, option, "configured engine should have an option")
				assert.NotEmpty(t, option.Label, "documented example reads the option label")
				assert.NotEmpty(t, option.SecretName, "documented example reads the option secret name")
				return
			}
			assert.Nil(t, option, "unconfigured engine should return nil")
		})
	}
}

func TestSpec_PublicAPI_GetAllEngineSecretNames(t *testing.T) {
	secrets := constants.GetAllEngineSecretNames()
	require.NotEmpty(t, secrets, "the union of configured secret names should not be empty")
	seen := make(map[string]struct{}, len(secrets))
	for _, secret := range secrets {
		_, duplicate := seen[secret]
		assert.False(t, duplicate, "GetAllEngineSecretNames should deduplicate %q", secret)
		seen[secret] = struct{}{}
	}
}

func TestSpec_PublicAPI_GetWorkflowDir(t *testing.T) {
	t.Setenv("GH_AW_WORKFLOWS_DIR", "custom/workflows")
	assert.Equal(t, "custom/workflows", constants.GetWorkflowDir(), "GetWorkflowDir should honor the environment at call time")
	t.Setenv("GH_AW_WORKFLOWS_DIR", "other/workflows")
	assert.Equal(t, "other/workflows", constants.GetWorkflowDir(), "GetWorkflowDir should read the current environment on every call")
}

// SPEC_AMBIGUITY: The README does not exhaustively identify which semantic
// types implement IsValid, so only String methods used by README examples are
// exercised above.
