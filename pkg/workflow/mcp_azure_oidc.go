package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/semverutil"
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
		if data.SandboxConfig != nil && data.SandboxConfig.MCP != nil {
			gateway := data.SandboxConfig.MCP
			if gateway.Container != "" && gateway.Container != constants.DefaultMCPGatewayContainer {
				if !isFullSemanticVersion(gateway.Version) {
					return fmt.Errorf("mcp-servers.%s.auth.provider: azure requires an explicit sandbox.mcp.version with a full semantic version for a custom gateway build supporting Azure federation", name)
				}
				continue
			}
		}
		version := effectiveMCPGatewayVersion(data)
		minVersion := string(constants.MCPGAzureOIDCMinVersion)
		if !isFullSemanticVersion(version) || semverutil.Compare(version, minVersion) < 0 {
			return fmt.Errorf("mcp-servers.%s.auth.provider: azure requires MCP gateway %s or newer; effective version is %s", name, minVersion, version)
		}
	}
	return nil
}
