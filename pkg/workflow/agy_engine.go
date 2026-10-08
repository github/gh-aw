package workflow

import (
	"slices"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var agyLog = logger.New("workflow:agy_engine")

type AgyEngine struct {
	BaseEngine
}

var _ CodingAgentEngine = (*AgyEngine)(nil)
var _ HarnessRunner = (*AgyEngine)(nil)
var _ MCPConfigAdapterProvider = (*AgyEngine)(nil)

func NewAgyEngine() *AgyEngine {
	return &AgyEngine{
		BaseEngine: BaseEngine{
			id:           string(constants.AgyEngine),
			displayName:  "Google Antigravity CLI",
			description:  "Experimental native Agy CLI with Gemini API-key authentication",
			experimental: true,
			capabilities: EngineCapabilities{
				MCP: true,
			},
			dedicatedLLMGatewayPort: constants.GeminiLLMGatewayPort,
		},
	}
}

func (e *AgyEngine) GetModelEnvVarName() string {
	return "GH_AW_AGY_MODEL"
}

func (e *AgyEngine) GetHarnessScriptName() string {
	return "agy_harness.cjs"
}

func (e *AgyEngine) GetLogParserScriptId() string {
	return "parse_agy_log"
}

func (e *AgyEngine) GetAgentManifestFiles() []string {
	return []string{"AGENTS.md", "GEMINI.md"}
}

func (e *AgyEngine) GetAgentManifestPathPrefixes() []string {
	return []string{".agents/", ".gemini/"}
}

func (e *AgyEngine) GetSupportedEnvVarKeys() []string {
	return []string{constants.GeminiAPIKey}
}

func (e *AgyEngine) GetRequiredSecretNames(workflowData *WorkflowData) []string {
	if workflowData == nil {
		return []string{constants.GeminiAPIKey}
	}
	secrets := append([]string{constants.GeminiAPIKey}, collectCommonMCPSecrets(workflowData)...)
	parsedTools, tools := extractToolsConfig(workflowData)
	if hasGitHubTool(parsedTools) {
		secrets = append(secrets, "GITHUB_MCP_SERVER_TOKEN")
	}
	for name := range collectHTTPMCPHeaderSecrets(tools) {
		secrets = append(secrets, name)
	}
	slices.Sort(secrets)
	return slices.Compact(secrets)
}

func (e *AgyEngine) GetSecretValidationStep(workflowData *WorkflowData) GitHubActionStep {
	return BuildEngineSecretValidationStep(workflowData, EngineSecretValidationConfig{
		SecretNames: []string{constants.GeminiAPIKey},
		EngineName:  e.GetDisplayName(),
	})
}

func (e *AgyEngine) GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
	agyLog.Print("Generating Agy runtime installation steps")
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Version == "" {
		workflowData.EngineConfig.Version = string(constants.DefaultAgyVersion)
	}
	return BuildNpmEngineInstallStepsWithAWF([]GitHubActionStep{GenerateNodeJsSetupStep()}, workflowData)
}
