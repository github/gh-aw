package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var mcpWorkflowScannerLog = logger.New("cli:mcp_workflow_scanner")

// WorkflowMCPMetadata contains metadata about MCP servers in a workflow
type WorkflowMCPMetadata struct {
	FilePath    string
	FileName    string
	BaseName    string
	MCPConfigs  []parser.MCPServerConfig
	Frontmatter map[string]any
}

// ScanWorkflowsForMCP scans workflow files for MCP configurations
// Returns metadata for workflows that contain MCP servers
func ScanWorkflowsForMCP(workflowsDir string, serverFilter string, verbose bool) ([]WorkflowMCPMetadata, error) {
	mcpWorkflowScannerLog.Printf("Scanning workflows for MCP configurations: dir=%s, filter=%s", workflowsDir, serverFilter)
	// Check if the workflows directory exists
	if _, err := os.Stat(workflowsDir); os.IsNotExist(err) {
		mcpWorkflowScannerLog.Printf("Workflows directory not found: %s", workflowsDir)
		return nil, fmt.Errorf("workflows directory not found: %s", workflowsDir)
	}

	// Find all .md files in the workflows directory
	files, err := filepath.Glob(filepath.Join(workflowsDir, "*.md"))
	if err != nil {
		mcpWorkflowScannerLog.Printf("Failed to search for workflow files: %v", err)
		return nil, fmt.Errorf("failed to search for workflow files: %w", err)
	}

	// Filter out README.md files
	files = filterWorkflowFiles(files)

	mcpWorkflowScannerLog.Printf("Found %d workflow files to scan", len(files))
	var results []WorkflowMCPMetadata

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping %s: %v", filepath.Base(file), err)))
			}
			continue
		}

		frontmatterData, err := parser.ExtractFrontmatterFromContent(string(content))
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping %s: %v", filepath.Base(file), err)))
			}
			continue
		}

		mcpConfigs, err := parser.ExtractMCPConfigurations(frontmatterData.Frontmatter, serverFilter)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Error extracting MCP from %s: %v", filepath.Base(file), err)))
			}
			continue
		}

		if len(mcpConfigs) > 0 {
			baseName := normalizeWorkflowID(file)
			mcpWorkflowScannerLog.Printf("Found MCP configuration in %s: %d servers", filepath.Base(file), len(mcpConfigs))
			results = append(results, WorkflowMCPMetadata{
				FilePath:    file,
				FileName:    filepath.Base(file),
				BaseName:    baseName,
				MCPConfigs:  mcpConfigs,
				Frontmatter: frontmatterData.Frontmatter,
			})
		}
	}

	mcpWorkflowScannerLog.Printf("Scan completed: found %d workflows with MCP configurations", len(results))
	return results, nil
}
