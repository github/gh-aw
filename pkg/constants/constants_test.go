//go:build !integration

package constants

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetWorkflowDir(t *testing.T) {
	t.Parallel()
	expected := filepath.Join(".github", "workflows")
	assert.Equal(t, expected, GetWorkflowDir())
}

func TestGetWorkflowDirEnvOverride(t *testing.T) {
	t.Setenv("GH_AW_WORKFLOWS_DIR", "/tmp/custom-workflows")
	assert.Equal(t, "/tmp/custom-workflows", GetWorkflowDir())
}

func TestGetWorkflowDirEnvEmpty(t *testing.T) {
	t.Setenv("GH_AW_WORKFLOWS_DIR", "")
	expected := filepath.Join(".github", "workflows")
	assert.Equal(t, expected, GetWorkflowDir())
}

func TestDefaultAllowedDomains(t *testing.T) {
	t.Parallel()
	expectedDomains := []string{"localhost", "localhost:*", "127.0.0.1", "127.0.0.1:*"}
	require.NotEmpty(t, DefaultAllowedDomains)
	assert.Equal(t, expectedDomains, DefaultAllowedDomains)
}

func TestSafeWorkflowEvents(t *testing.T) {
	t.Parallel()
	expectedEvents := []string{"workflow_dispatch", "schedule"}
	require.NotEmpty(t, SafeWorkflowEvents)
	assert.Equal(t, expectedEvents, SafeWorkflowEvents)
}

func TestAllowedExpressions(t *testing.T) {
	t.Parallel()
	require.NotEmpty(t, AllowedExpressions)

	for _, expr := range []string{
		"github.event.issue.number",
		"github.event.pull_request.number",
		"github.repository",
		"github.run_id",
		"github.workspace",
	} {
		assert.Contains(t, AllowedExpressions, expr)
	}
}

func TestAgenticEngines(t *testing.T) {
	t.Parallel()
	expectedEngines := []string{"claude", "codex", "copilot", "gemini", "pi"}
	require.NotEmpty(t, AgenticEngines)
	assert.Equal(t, expectedEngines, AgenticEngines)
	assert.Equal(t, "claude", string(ClaudeEngine))
	assert.Equal(t, "codex", string(CodexEngine))
	assert.Equal(t, "copilot", string(CopilotEngine))
	assert.Equal(t, "gemini", string(GeminiEngine))
	assert.Equal(t, CopilotEngine, DefaultEngine)
}

func TestDefaultGitHubTools(t *testing.T) {
	t.Parallel()
	require.NotEmpty(t, DefaultGitHubToolsLocal)
	require.NotEmpty(t, DefaultGitHubToolsRemote)
	require.NotEmpty(t, DefaultReadOnlyGitHubTools)
	assert.Len(t, DefaultGitHubTools, len(DefaultGitHubToolsLocal))
	assert.Len(t, DefaultGitHubToolsLocal, len(DefaultReadOnlyGitHubTools))
	assert.Len(t, DefaultGitHubToolsRemote, len(DefaultReadOnlyGitHubTools))

	requiredTools := []string{
		"get_me",
		"list_issues",
		"pull_request_read",
		"get_file_contents",
		"search_code",
	}

	for name, tools := range map[string][]string{
		"DefaultGitHubToolsLocal":    DefaultGitHubToolsLocal,
		"DefaultGitHubToolsRemote":   DefaultGitHubToolsRemote,
		"DefaultReadOnlyGitHubTools": DefaultReadOnlyGitHubTools,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, tool := range requiredTools {
				assert.Contains(t, tools, tool)
			}
		})
	}
}

func TestDefaultBashTools(t *testing.T) {
	t.Parallel()
	require.NotEmpty(t, DefaultBashTools)
	for _, tool := range []string{"echo", "printf", "ls", "cat", "grep"} {
		assert.Contains(t, DefaultBashTools, tool)
	}
}

func TestPriorityFields(t *testing.T) {
	t.Parallel()
	require.NotEmpty(t, PriorityStepFields)
	require.NotEmpty(t, PriorityJobFields)
	require.NotEmpty(t, PriorityWorkflowFields)
	assert.Equal(t, "name", PriorityStepFields[0])
	assert.Equal(t, "name", PriorityJobFields[0])
	assert.Equal(t, "on", PriorityWorkflowFields[0])
}

func TestConstantValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{"CLIExtensionPrefix", string(CLIExtensionPrefix), "gh aw"},
		{"DefaultMCPRegistryURL", string(DefaultMCPRegistryURL), "https://api.mcp.github.com/v0.1"},
		{"OTELSentryEndpointSecretName", OTELSentryEndpointSecretName, "GH_AW_OTEL_SENTRY_ENDPOINT"},
		{"AWFDefaultCommand", string(AWFDefaultCommand), "awf"},
		{"AWFProxyLogsDir", string(AWFProxyLogsDir), "/tmp/gh-aw/sandbox/firewall/logs"},
		{"AWFAuditDir", string(AWFAuditDir), "/tmp/gh-aw/sandbox/firewall/audit"},
		{"PreAgentAuditFilePath", string(PreAgentAuditFilePath), "/tmp/gh-aw/pre-agent-audit.txt"},
		{"AWFConfigFilePath", string(AWFConfigFilePath), "/tmp/gh-aw/awf-config.json"},
		{"AgentJobName", string(AgentJobName), "agent"},
		{"ActivationJobName", string(ActivationJobName), "activation"},
		{"PreActivationJobName", string(PreActivationJobName), "pre_activation"},
		{"PreActivationHyphenJobName", string(PreActivationHyphenJobName), "pre-activation"},
		{"DetectionJobName", string(DetectionJobName), "detection"},
		{"SafeOutputsJobName", string(SafeOutputsJobName), "safe_outputs"},
		{"SafeOutputsHyphenJobName", string(SafeOutputsHyphenJobName), "safe-outputs"},
		{"UploadAssetsJobName", string(UploadAssetsJobName), "upload_assets"},
		{"UploadCodeScanningJobName", string(UploadCodeScanningJobName), "upload_code_scanning_sarif"},
		{"ConclusionJobName", string(ConclusionJobName), "conclusion"},
		{"UnlockJobName", string(UnlockJobName), "unlock"},
		{"SafeOutputArtifactName", string(SafeOutputArtifactName), "safe-output"},
		{"AgentOutputArtifactName", string(AgentOutputArtifactName), "agent-output"},
		{"SafeOutputItemsArtifactName", string(SafeOutputItemsArtifactName), "safe-outputs-items"},
		{"TemporaryIdMapFilename", string(TemporaryIdMapFilename), "temporary-id-map.json"},
		{"SafeOutputsMCPServerID", string(SafeOutputsMCPServerID), "safeoutputs"},
		{"CheckMembershipStepID", string(CheckMembershipStepID), "check_membership"},
		{"CheckStopTimeStepID", string(CheckStopTimeStepID), "check_stop_time"},
		{"CheckSkipIfMatchStepID", string(CheckSkipIfMatchStepID), "check_skip_if_match"},
		{"CheckSkipIfNoMatchStepID", string(CheckSkipIfNoMatchStepID), "check_skip_if_no_match"},
		{"CheckCommandPositionStepID", string(CheckCommandPositionStepID), "check_command_position"},
		{"CheckCooldownStepID", string(CheckCooldownStepID), "check_cooldown"},
		{"IsTeamMemberOutput", IsTeamMemberOutput, "is_team_member"},
		{"StopTimeOkOutput", StopTimeOkOutput, "stop_time_ok"},
		{"SkipCheckOkOutput", SkipCheckOkOutput, "skip_check_ok"},
		{"SkipNoMatchCheckOkOutput", SkipNoMatchCheckOkOutput, "skip_no_match_check_ok"},
		{"CommandPositionOkOutput", CommandPositionOkOutput, "command_position_ok"},
		{"CooldownOkOutput", CooldownOkOutput, "cooldown_ok"},
		{"ActivatedOutput", ActivatedOutput, "activated"},
		{"DefaultActivationJobRunnerImage", DefaultActivationJobRunnerImage, "ubuntu-slim"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.value)
		})
	}
}

func TestKnownBuiltInJobNamesContainsAllKnownJobs(t *testing.T) {
	t.Parallel()
	for _, jobName := range []string{
		string(AgentJobName),
		string(ActivationJobName),
		string(PreActivationJobName),
		string(PreActivationHyphenJobName),
		string(DetectionJobName),
		string(SafeOutputsJobName),
		string(SafeOutputsHyphenJobName),
		string(UploadAssetsJobName),
		string(UploadCodeScanningJobName),
		string(ConclusionJobName),
		string(UnlockJobName),
	} {
		require.Contains(t, KnownBuiltInJobNames, jobName)
	}
}

func TestModelNameConstants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "test-model", string(ModelName("test-model")))
}

func TestNumericConstants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		value    LineLength
		minValue LineLength
	}{
		{"MaxExpressionLineLength", MaxExpressionLineLength, 1},
		{"ExpressionBreakThreshold", ExpressionBreakThreshold, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.GreaterOrEqual(t, tt.value, tt.minValue)
		})
	}
}

func TestPolicyConstants(t *testing.T) {
	t.Parallel()
	assert.EqualValues(t, 1000, DefaultMaxAICredits)
	assert.EqualValues(t, 400, DefaultDetectionMaxAICredits)
	assert.Equal(t, "5000", DefaultMaxDailyAICredits)
	assert.Equal(t, 500, DefaultMaxRuns)
	assert.Equal(t, 5, DefaultMaxTurnCacheMisses)
	assert.Greater(t, DefaultMaxAICredits, DefaultDetectionMaxAICredits)
}

func TestTimeoutConstants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		value      time.Duration
		minValue   time.Duration
		checkExact bool
		exactValue time.Duration
	}{
		{"DefaultAgenticWorkflowTimeout", DefaultAgenticWorkflowTimeout, time.Minute, false, 0},
		{"DefaultToolTimeout", DefaultToolTimeout, time.Second, false, 0},
		{"DefaultMCPStartupTimeout", DefaultMCPStartupTimeout, time.Second, false, 0},
		{"DefaultHTTPClientTimeout", DefaultHTTPClientTimeout, time.Second, true, 30 * time.Second},
		{"MCPSessionTimeoutMin", MCPSessionTimeoutMin, time.Minute, true, 5 * time.Minute},
		{"MCPToolTimeoutMin", MCPToolTimeoutMin, time.Second, true, 10 * time.Second},
		{"MCPToolTimeoutMax", MCPToolTimeoutMax, time.Second, true, 600 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.GreaterOrEqual(t, tt.value, tt.minValue)
			if tt.checkExact {
				assert.Equal(t, tt.exactValue, tt.value)
			}
		})
	}

	assert.Less(t, MCPToolTimeoutMin, MCPToolTimeoutMax)
	assert.Less(t, MCPSessionTimeoutMin, MCPToolTimeoutMax)
}

func TestFeatureFlagConstants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		value    FeatureFlag
		expected string
	}{
		{"MCPScriptsFeatureFlag", MCPScriptsFeatureFlag, "mcp-scripts"},
		{"MCPGatewayFeatureFlag", MCPGatewayFeatureFlag, "mcp-gateway"},
		{"DisableXPIAPromptFeatureFlag", DisableXPIAPromptFeatureFlag, "disable-xpia-prompt"},
		{"DIFCProxyFeatureFlag", DIFCProxyFeatureFlag, "difc-proxy"},
		{"AwfDiagnosticLogsFeatureFlag", AwfDiagnosticLogsFeatureFlag, "awf-diagnostic-logs"},
		{"GroupConcurrencyQueueFeatureFlag", GroupConcurrencyQueueFeatureFlag, "group-concurrency-queue"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, string(tt.value))
		})
	}
}

func TestFeatureFlagType(t *testing.T) {
	t.Parallel()
	var flag FeatureFlag = "test-flag"
	assert.Equal(t, "test-flag", string(flag))
	assert.Equal(t, MCPScriptsFeatureFlag, FeatureFlag("mcp-scripts"))
}

func TestSemanticTypeAliases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		convert  func() string
		expected string
	}{
		{"URL", func() string { var testURL URL = "https://example.com"; return string(testURL) }, "https://example.com"},
		{"DefaultMCPRegistryURL", func() string { return string(DefaultMCPRegistryURL) }, "https://api.mcp.github.com/v0.1"},
		{"ModelName", func() string { var testModel ModelName = "test-model"; return string(testModel) }, "test-model"},
		{"JobName", func() string { var testJob JobName = "test-job"; return string(testJob) }, "test-job"},
		{"AgentJobName", func() string { return string(AgentJobName) }, "agent"},
		{"ActivationJobName", func() string { return string(ActivationJobName) }, "activation"},
		{"PreActivationJobName", func() string { return string(PreActivationJobName) }, "pre_activation"},
		{"DetectionJobName", func() string { return string(DetectionJobName) }, "detection"},
		{"StepID", func() string { var testStep StepID = "test-step"; return string(testStep) }, "test-step"},
		{"CheckMembershipStepID", func() string { return string(CheckMembershipStepID) }, "check_membership"},
		{"CheckStopTimeStepID", func() string { return string(CheckStopTimeStepID) }, "check_stop_time"},
		{"CheckSkipIfMatchStepID", func() string { return string(CheckSkipIfMatchStepID) }, "check_skip_if_match"},
		{"CheckCommandPositionStepID", func() string { return string(CheckCommandPositionStepID) }, "check_command_position"},
		{"CommandPrefix", func() string { var prefix CommandPrefix = "test-prefix"; return string(prefix) }, "test-prefix"},
		{"CLIExtensionPrefix", func() string { return string(CLIExtensionPrefix) }, "gh aw"},
		{"WorkflowID", func() string { var workflow WorkflowID = "ci-doctor"; return string(workflow) }, "ci-doctor"},
		{"EngineName", func() string { var engine EngineName = "copilot"; return string(engine) }, "copilot"},
		{"CopilotEngine", func() string { return string(CopilotEngine) }, "copilot"},
		{"ClaudeEngine", func() string { return string(ClaudeEngine) }, "claude"},
		{"CodexEngine", func() string { return string(CodexEngine) }, "codex"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.convert())
		})
	}
}

func TestTypeSafetyBetweenSemanticTypes(t *testing.T) {
	t.Parallel()
	job1 := AgentJobName
	job2 := ActivationJobName
	assert.NotEqual(t, job1, job2)

	step1 := CheckMembershipStepID
	step2 := CheckStopTimeStepID
	assert.NotEqual(t, step1, step2)

	assert.Equal(t, "agent", string(job1))
	assert.Equal(t, "check_membership", string(step1))
}

func TestHelperMethods(t *testing.T) {
	t.Parallel()
	type semanticValue interface {
		String() string
		IsValid() bool
	}

	tests := []struct {
		name     string
		value    semanticValue
		empty    semanticValue
		expected string
	}{
		{"Version", Version("1.0.0"), Version(""), "1.0.0"},
		{"JobName", JobName("agent"), JobName(""), "agent"},
		{"StepID", StepID("check_membership"), StepID(""), "check_membership"},
		{"CommandPrefix", CommandPrefix("gh aw"), CommandPrefix(""), "gh aw"},
		{"WorkflowID", WorkflowID("ci-doctor"), WorkflowID(""), "ci-doctor"},
		{"ArtifactName", ArtifactName("agent-output"), ArtifactName(""), "agent-output"},
		{"Filename", Filename("agent_output.json"), Filename(""), "agent_output.json"},
		{"FilePath", FilePath("/tmp/file"), FilePath(""), "/tmp/file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.value.String())
			assert.True(t, tt.value.IsValid())
			assert.False(t, tt.empty.IsValid())
		})
	}
}

func TestGetAllEngineSecretNames(t *testing.T) {
	t.Parallel()
	secrets := GetAllEngineSecretNames()
	require.NotEmpty(t, secrets)

	for _, secret := range []string{
		"COPILOT_GITHUB_TOKEN",
		"ANTHROPIC_API_KEY",
		"OPENAI_API_KEY",
		"CODEX_API_KEY",
		"GH_AW_GITHUB_TOKEN",
	} {
		assert.Contains(t, secrets, secret)
	}

	seen := make(map[string]bool)
	for _, secret := range secrets {
		assert.False(t, seen[secret], "GetAllEngineSecretNames() returned duplicate secret: %q", secret)
		seen[secret] = true
	}
}

func TestGetEngineOption_AllBuiltInEngines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		engine       string
		label        string
		secret       string
		alternatives []string
	}{
		{string(CopilotEngine), "GitHub Copilot", CopilotGitHubToken, nil},
		{string(ClaudeEngine), "Claude", AnthropicAPIKey, []string{}},
		{string(CodexEngine), "Codex", OpenAIAPIKey, []string{CodexAPIKey}},
		{string(GeminiEngine), "Gemini", GeminiAPIKey, nil},
		{string(PiEngine), "Pi", CopilotGitHubToken, []string{AnthropicAPIKey, OpenAIAPIKey, CodexAPIKey}},
	}

	for _, tt := range tests {
		t.Run(tt.engine, func(t *testing.T) {
			t.Parallel()
			opt := GetEngineOption(tt.engine)
			require.NotNil(t, opt)
			assert.Equal(t, tt.engine, opt.Value)
			assert.Equal(t, tt.label, opt.Label)
			assert.Equal(t, tt.secret, opt.SecretName)
			assert.Equal(t, tt.alternatives, opt.AlternativeSecrets)
		})
	}

	assert.Nil(t, GetEngineOption("unknown-engine-xyz"))
}
