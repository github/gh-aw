package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var domainsCommandLog = logger.New("cli:domains_command")

// WorkflowDomainsSummary represents a workflow's domain configuration for list output
type WorkflowDomainsSummary struct {
	Workflow string `json:"workflow" console:"header:Workflow"`
	Engine   string `json:"engine" console:"header:Engine"`
	Allowed  int    `json:"allowed" console:"header:Allowed"`
	Blocked  int    `json:"blocked" console:"header:Blocked"`
}

// WorkflowDomainsDetail represents the detailed domain configuration for a single workflow
type WorkflowDomainsDetail struct {
	Workflow       string   `json:"workflow"`
	Engine         string   `json:"engine"`
	AllowedDomains []string `json:"allowed_domains"`
	BlockedDomains []string `json:"blocked_domains"`
}

// DomainItem represents a single domain entry for tabular display
type DomainItem struct {
	Domain    string `json:"domain" console:"header:Domain"`
	Ecosystem string `json:"ecosystem" console:"header:Ecosystem"`
	Status    string `json:"status" console:"header:Status"`
}

// NewDomainsCommand creates the domains command
func NewDomainsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domains [workflow]",
		Short: "List network domains configured in agentic workflows",
		Long: `List network domains configured in agentic workflows.

When no workflow is specified, lists all workflows with a summary of their allowed
and blocked domain counts.

When a workflow ID or file is specified, lists all effective allowed and blocked
domains for that workflow, including domains expanded from named domain sets
(e.g., "node", "python", "github", "copilot").

The workflow argument can be:
- A workflow ID (basename without .md extension, e.g., "weekly-research")
- A file path (e.g., "weekly-research.md" or ".github/workflows/weekly-research.md")`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` domains                      # List all workflows with domain counts
  ` + string(constants.CLIExtensionPrefix) + ` domains weekly-research       # List domains for weekly-research workflow
  ` + string(constants.CLIExtensionPrefix) + ` domains --json                # Output summary in JSON format
  ` + string(constants.CLIExtensionPrefix) + ` domains weekly-research --json # Output workflow domains in JSON format`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonFlag, _ := cmd.Flags().GetBool("json")

			if len(args) == 1 {
				return RunWorkflowDomains(args[0], jsonFlag)
			}
			return RunListDomains(jsonFlag)
		},
	}

	addJSONFlag(cmd)
	cmd.ValidArgsFunction = CompleteWorkflowNames

	return cmd
}

// RunListDomains lists all workflows with their domain configuration summary
func RunListDomains(jsonOutput bool) error {
	domainsCommandLog.Printf("Listing domains for all workflows: jsonOutput=%v", jsonOutput)

	workflowsDir := getWorkflowsDir()
	mdFiles, err := getMarkdownWorkflowFiles(workflowsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
		return nil
	}

	if len(mdFiles) == 0 {
		if jsonOutput {
			fmt.Fprintln(os.Stdout, "[]")
			return nil
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No workflow files found."))
		return nil
	}

	var summaries []WorkflowDomainsSummary

	for _, file := range mdFiles {
		name := extractWorkflowNameFromPath(file)
		engineID, network, tools, runtimes := extractWorkflowDomainConfig(file)

		allowedDomains := computeAllowedDomains(constants.EngineName(engineID), network, tools, runtimes)
		blockedDomains := workflow.GetBlockedDomains(network)

		summaries = append(summaries, WorkflowDomainsSummary{
			Workflow: name,
			Engine:   engineID,
			Allowed:  len(allowedDomains),
			Blocked:  len(blockedDomains),
		})
	}

	if jsonOutput {
		jsonBytes, err := marshalIndentJSONOrWrap(summaries, "domain summaries")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(jsonBytes))
		return nil
	}

	if len(summaries) == 1 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Found 1 workflow"))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Found %d workflows", len(summaries))))
	}
	fmt.Fprint(os.Stderr, console.RenderStruct(summaries))

	return nil
}

// RunWorkflowDomains lists all effective domains for a specific workflow
func RunWorkflowDomains(workflowArg string, jsonOutput bool) error {
	domainsCommandLog.Printf("Listing domains for workflow: %s, jsonOutput=%v", workflowArg, jsonOutput)

	workflowPath, err := ResolveWorkflowPath(workflowArg)
	if err != nil {
		return err
	}

	engineID, network, tools, runtimes := extractWorkflowDomainConfig(workflowPath)
	name := extractWorkflowNameFromPath(workflowPath)

	allowedDomains := computeAllowedDomains(constants.EngineName(engineID), network, tools, runtimes)
	blockedDomains := workflow.GetBlockedDomains(network)

	if jsonOutput {
		detail := WorkflowDomainsDetail{
			Workflow:       name,
			Engine:         engineID,
			AllowedDomains: allowedDomains,
			BlockedDomains: blockedDomains,
		}
		jsonBytes, err := marshalIndentJSONOrWrap(detail, "domain details")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(jsonBytes))
		return nil
	}

	// Console output: show domain items grouped by allowed/blocked
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(
		fmt.Sprintf("Network domains for %s (engine: %s)", name, engineID),
	))

	items := buildDomainItems(allowedDomains, blockedDomains)

	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No domains configured."))
		return nil
	}

	// Build table rows
	headers := []string{"Domain", "Ecosystem", "Status"}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{item.Domain, item.Ecosystem, item.Status})
	}

	tableConfig := console.TableConfig{
		Title:   "Domains for " + name,
		Headers: headers,
		Rows:    rows,
	}
	fmt.Fprint(os.Stderr, console.RenderTable(tableConfig))

	fmt.Fprintf(os.Stderr, "\n%d allowed, %d blocked\n", len(allowedDomains), len(blockedDomains))

	return nil
}

// extractWorkflowDomainConfig reads a workflow file and returns its engine ID,
// network permissions, tools, and runtimes configuration.
func extractWorkflowDomainConfig(filePath string) (engineID string, network *workflow.NetworkPermissions, tools map[string]any, runtimes map[string]any) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		domainsCommandLog.Printf("Failed to read workflow file %s: %v", filePath, err)
		return "copilot", nil, nil, nil
	}

	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil || result.Frontmatter == nil {
		domainsCommandLog.Printf("Failed to parse frontmatter from %s: %v", filePath, err)
		return "copilot", nil, nil, nil
	}

	// Reuse the existing engine ID extraction helper which handles both string and object formats
	engineID = extractEngineIDFromFrontmatter(result.Frontmatter)

	// Parse structured frontmatter config to get NetworkPermissions and runtimes
	config, err := workflow.ParseFrontmatterConfig(result.Frontmatter)
	if err != nil {
		domainsCommandLog.Printf("Failed to parse frontmatter config from %s: %v", filePath, err)
		return engineID, nil, nil, nil
	}

	// Extract tools map from raw frontmatter (tools is kept as map[string]any)
	var toolsMap map[string]any
	if toolsRaw, ok := result.Frontmatter["tools"]; ok {
		toolsMap, _ = toolsRaw.(map[string]any)
	}

	return engineID, config.Network, toolsMap, config.Runtimes
}

// computeAllowedDomains returns the effective allowed domains for an engine + network config.
// It mirrors the logic used during workflow compilation.
func computeAllowedDomains(engine constants.EngineName, network *workflow.NetworkPermissions, tools map[string]any, runtimes map[string]any) []string {
	combined := workflow.GetAllowedDomainsForEngine(engine, network, tools, runtimes)
	if combined == "" {
		return []string{}
	}
	// GetAllowedDomainsForEngine returns a comma-separated string; split it
	parts := strings.Split(combined, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// buildDomainItems creates a list of DomainItem from allowed and blocked domain slices
func buildDomainItems(allowedDomains, blockedDomains []string) []DomainItem {
	var items []DomainItem
	for _, d := range allowedDomains {
		ecosystem := workflow.GetDomainEcosystem(d)
		items = append(items, DomainItem{
			Domain:    d,
			Ecosystem: ecosystem,
			Status:    "✓ Allowed",
		})
	}
	for _, d := range blockedDomains {
		ecosystem := workflow.GetDomainEcosystem(d)
		items = append(items, DomainItem{
			Domain:    d,
			Ecosystem: ecosystem,
			Status:    "✗ Blocked",
		})
	}
	return items
}
