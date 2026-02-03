package cli

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/parser"
)

// loadWorkflowMCPConfigs loads a workflow file and extracts MCP configurations.
// This is a shared helper used by multiple MCP commands to avoid code duplication.
//
// Parameters:
//   - workflowPath: absolute path to the workflow file
//   - serverFilter: optional server name to filter by (empty string for no filter)
//
// Returns:
//   - *parser.FrontmatterResult: parsed workflow data containing frontmatter and content
//   - []parser.MCPServerConfig: list of MCP server configurations
//   - error: any error that occurred during loading or parsing
func loadWorkflowMCPConfigs(workflowPath string, serverFilter string) (*parser.FrontmatterResult, []parser.MCPServerConfig, error) {
	// Read the workflow file
	content, err := os.ReadFile(workflowPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read workflow file: %w", err)
	}

	// Parse the frontmatter
	workflowData, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse workflow file: %w", err)
	}

	// Extract MCP configurations
	mcpConfigs, err := parser.ExtractMCPConfigurations(workflowData.Frontmatter, serverFilter)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to extract MCP configurations: %w", err)
	}

	return workflowData, mcpConfigs, nil
}
