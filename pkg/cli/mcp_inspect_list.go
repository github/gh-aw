package cli

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
)

// filterOutSafeOutputs removes safe-outputs MCP servers from the list since they are
// handled by the workflow compiler and not actual MCP servers that can be inspected
func filterOutSafeOutputs(configs []parser.MCPServerConfig) []parser.MCPServerConfig {
	var filteredConfigs []parser.MCPServerConfig
	for _, config := range configs {
		if config.Name != constants.SafeOutputsMCPServerID {
			filteredConfigs = append(filteredConfigs, config)
		}
	}
	return filteredConfigs
}

// listWorkflowsWithMCP shows available workflow files that contain MCP configurations
func listWorkflowsWithMCP(workflowsDir string, verbose bool) error {
	// Scan workflows for MCP configurations
	results, err := ScanWorkflowsForMCP(workflowsDir, "", verbose)
	if err != nil {
		if os.IsNotExist(err) {
			errMsg := "no .github/workflows directory found"
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
			return fmt.Errorf("no .github/workflows directory found")
		}
		return err
	}

	// Filter out safe-outputs MCP servers for inspection
	var workflowsWithMCP []string
	for _, result := range results {
		filteredConfigs := filterOutSafeOutputs(result.MCPConfigs)
		if len(filteredConfigs) > 0 {
			workflowsWithMCP = append(workflowsWithMCP, result.FileName)
		}
	}

	if len(workflowsWithMCP) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No workflows with MCP servers found"))
		return nil
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Workflows with MCP servers:"))
	for _, workflow := range workflowsWithMCP {
		fmt.Fprintf(os.Stderr, "  • %s\n", workflow)
	}
	fmt.Fprintf(os.Stderr, "\nRun 'gh aw mcp inspect <workflow-name>' to inspect MCP servers in a specific workflow.\n")

	return nil
}
