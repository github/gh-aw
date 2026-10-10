package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var mcpInspectListLog = logger.New("cli:mcp_inspect_list")

// filterOutSafeOutputs removes safe-outputs MCP servers from the list since they are
// handled by the workflow compiler and not actual MCP servers that can be inspected
func filterOutSafeOutputs(configs []parser.RegistryMCPServerConfig) []parser.RegistryMCPServerConfig {
	var filteredConfigs []parser.RegistryMCPServerConfig
	for _, config := range configs {
		if config.Name != constants.SafeOutputsMCPServerID.String() {
			filteredConfigs = append(filteredConfigs, config)
		}
	}
	return filteredConfigs
}

// listWorkflowsWithMCP shows available workflow files that contain MCP configurations
func listWorkflowsWithMCP(workflowsDir string, verbose bool) error {
	mcpInspectListLog.Printf("Listing workflows with MCP servers: dir=%s, verbose=%v", workflowsDir, verbose)

	// Scan workflows for MCP configurations
	results, err := ScanWorkflowsForMCP(workflowsDir, "", verbose)
	if err != nil {
		if os.IsNotExist(err) {
			errMsg := "no .github/workflows directory found"
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
			return errors.New("no .github/workflows directory found")
		}
		return err
	}

	mcpInspectListLog.Printf("Scanned %d workflows for MCP configurations", len(results))

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

	mcpInspectListLog.Printf("Found %d workflows with MCP servers", len(workflowsWithMCP))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No workflow specified; showing MCP summary list (same behavior as 'gh aw mcp list')."))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Workflows with MCP servers:"))
	for _, workflow := range workflowsWithMCP {
		fmt.Fprintf(os.Stderr, "  • %s\n", workflow)
	}
	fmt.Fprintf(os.Stderr, "\nRun 'gh aw mcp inspect <workflow-name>' to inspect MCP servers in a specific workflow.\n")

	return nil
}
