package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var completionsLog = logger.New("cli:completions")

// getWorkflowDescription extracts the description field from a workflow's frontmatter
// Returns empty string if the description is not found or if there's an error reading the file
func getWorkflowDescription(filePath string) string {
	// Validate the path for security
	cleanPath, err := fileutil.ValidateAbsolutePath(filePath)
	if err != nil {
		completionsLog.Printf("Invalid workflow file path: %v", err)
		return ""
	}

	content, err := os.ReadFile(cleanPath)
	if err != nil {
		completionsLog.Printf("Failed to read workflow file %s: %v", cleanPath, err)
		return ""
	}

	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		completionsLog.Printf("Failed to parse frontmatter from %s: %v", filePath, err)
		return ""
	}

	if result.Frontmatter == nil {
		return ""
	}

	// Look for description field
	if desc, ok := result.Frontmatter["description"]; ok {
		if descStr, ok := desc.(string); ok {
			// Truncate to 60 characters for better display
			if len(descStr) > 60 {
				return descStr[:57] + "..."
			}
			return descStr
		}
	}

	// Fallback to name field if description not found
	if name, ok := result.Frontmatter["name"]; ok {
		if nameStr, ok := name.(string); ok {
			if len(nameStr) > 60 {
				return nameStr[:57] + "..."
			}
			return nameStr
		}
	}

	return ""
}

// ValidEngineNames returns the list of valid AI engine names for shell completion
func ValidEngineNames() []string {
	registry := workflow.GetGlobalEngineRegistry()
	return registry.GetSupportedEngines()
}

// CompleteWorkflowNames provides shell completion for workflow names
// It returns workflow IDs (basenames without .md extension) from .github/workflows/
// with tab-separated descriptions for Cobra v1.9.0+ CompletionWithDesc support
func CompleteWorkflowNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	completionsLog.Printf("Completing workflow names with prefix: %s", toComplete)

	mdFiles, err := getMarkdownWorkflowFiles("")
	if err != nil {
		completionsLog.Printf("Failed to get workflow files: %v", err)
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var workflows []string
	for _, file := range mdFiles {
		base := filepath.Base(file)
		name := normalizeWorkflowID(base)
		// Filter by prefix if toComplete is provided
		if toComplete == "" || strings.HasPrefix(name, toComplete) {
			desc := getWorkflowDescription(file)
			if desc != "" {
				// Format: "completion\tdescription" for shell completion with descriptions
				workflows = append(workflows, name+"\t"+desc)
			} else {
				// No description available, just add the workflow name
				workflows = append(workflows, name)
			}
		}
	}

	completionsLog.Printf("Found %d matching workflows", len(workflows))
	return workflows, cobra.ShellCompDirectiveNoFileComp
}

// CompleteEngineNames provides shell completion for engine names (--engine flag)
func CompleteEngineNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	completionsLog.Printf("Completing engine names with prefix: %s", toComplete)

	engines := ValidEngineNames()

	var filtered []string
	for _, engine := range engines {
		if toComplete == "" || strings.HasPrefix(engine, toComplete) {
			filtered = append(filtered, engine)
		}
	}

	completionsLog.Printf("Found %d matching engines", len(filtered))
	return filtered, cobra.ShellCompDirectiveNoFileComp
}

// CompleteMCPServerNames provides shell completion for MCP server names
// If a workflow is specified, it returns the MCP servers defined in that workflow
func CompleteMCPServerNames(workflowFile string) func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		completionsLog.Printf("Completing MCP server names for workflow: %s, prefix: %s", workflowFile, toComplete)

		if workflowFile == "" {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		// Resolve the workflow path
		workflowPath, err := ResolveWorkflowPath(workflowFile)
		if err != nil {
			completionsLog.Printf("Failed to resolve workflow path: %v", err)
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		// Load MCP configs from the workflow
		// The second parameter is the server filter - empty string means no filtering
		_, mcpConfigs, err := loadWorkflowMCPConfigs(workflowPath, "" /* serverFilter */)
		if err != nil {
			completionsLog.Printf("Failed to load MCP configs: %v", err)
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		var servers []string
		for _, config := range mcpConfigs {
			if toComplete == "" || strings.HasPrefix(config.Name, toComplete) {
				servers = append(servers, config.Name)
			}
		}

		completionsLog.Printf("Found %d matching MCP servers", len(servers))
		return servers, cobra.ShellCompDirectiveNoFileComp
	}
}

// CompleteDirectories provides shell completion for directory paths
func CompleteDirectories(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	completionsLog.Printf("Completing directories with prefix: %s", toComplete)
	return nil, cobra.ShellCompDirectiveFilterDirs
}

// RegisterEngineFlagCompletion registers completion for the --engine flag on a command
func RegisterEngineFlagCompletion(cmd *cobra.Command) {
	_ = cmd.RegisterFlagCompletionFunc("engine", CompleteEngineNames)
}

// RegisterDirFlagCompletion registers completion for directory-type flags on a command
func RegisterDirFlagCompletion(cmd *cobra.Command, flagName string) {
	_ = cmd.RegisterFlagCompletionFunc(flagName, CompleteDirectories)
}
