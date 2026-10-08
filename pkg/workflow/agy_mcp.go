package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func (e *AgyEngine) RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) error {
	return renderDefaultJSONMCPConfig(yaml, tools, mcpTools, workflowData, constants.ShellMcpServersJsonPath)
}

func (e *AgyEngine) GetMCPConfigAdapterFilename() string {
	return "convert_gateway_config_agy.cjs"
}

func (e *AgyEngine) GetMCPConfigAdapterWriteStep() GitHubActionStep {
	return nil
}
