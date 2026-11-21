package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/sliceutil"
)

func validateHTTPMCPAzureOIDCGateway(data *WorkflowData) error {
	if data == nil {
		return nil
	}
	for _, name := range sliceutil.SortedKeys(data.Tools) {
		tool, ok := data.Tools[name].(map[string]any)
		if !ok {
			continue
		}
		if hasMCP, mcpType := hasMCPConfig(tool); !hasMCP || mcpType != "http" {
			continue
		}
		config, err := getMCPConfig(tool, name)
		if err != nil {
			return err
		}
		if config.Type != "http" || config.Auth == nil || config.Auth.Provider != "azure" {
			continue
		}
		// No released MCPG version supports Azure federation yet. Do not invent a
		// minimum version or send Azure configuration to the default gateway.
		if data.SandboxConfig == nil || data.SandboxConfig.MCP == nil ||
			data.SandboxConfig.MCP.Container == "" ||
			data.SandboxConfig.MCP.Container == constants.DefaultMCPGatewayContainer ||
			data.SandboxConfig.MCP.Version == "" {
			return fmt.Errorf("mcp-servers.%s.auth.provider: azure requires an unreleased MCP gateway feature (github/gh-aw-mcpg#16956); set sandbox.mcp.container and sandbox.mcp.version to an explicit custom build containing that change", name)
		}
	}
	return nil
}
