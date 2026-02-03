package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/spf13/cobra"
)

var listWorkflowsLog = logger.New("cli:list_workflows")

// WorkflowListItem represents a single workflow for list output
type WorkflowListItem struct {
	Workflow string   `json:"workflow" console:"header:Workflow"`
	EngineID string   `json:"engine_id" console:"header:Engine"`
	Compiled string   `json:"compiled" console:"header:Compiled"`
	Labels   []string `json:"labels,omitempty" console:"header:Labels,omitempty"`
	On       any      `json:"on,omitempty" console:"-"`
}

// NewListCommand creates the list command
func NewListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [pattern]",
		Short: "List agentic workflows in the repository",
		Long: `List all agentic workflows in the repository without checking their status.

Displays a simplified table with workflow name, AI engine, and compilation status.
Unlike 'status', this command does not check GitHub workflow state or time remaining.

The optional pattern argument filters workflows by name (case-insensitive substring match).

Examples:
  ` + string(constants.CLIExtensionPrefix) + ` list                          # List all workflows
  ` + string(constants.CLIExtensionPrefix) + ` list ci-                       # List workflows with 'ci-' in name
  ` + string(constants.CLIExtensionPrefix) + ` list --json                    # Output in JSON format
  ` + string(constants.CLIExtensionPrefix) + ` list --label automation        # List workflows with 'automation' label`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var pattern string
			if len(args) > 0 {
				pattern = args[0]
			}
			verbose, _ := cmd.Flags().GetBool("verbose")
			jsonFlag, _ := cmd.Flags().GetBool("json")
			labelFilter, _ := cmd.Flags().GetString("label")
			return RunListWorkflows(pattern, verbose, jsonFlag, labelFilter)
		},
	}

	addJSONFlag(cmd)
	cmd.Flags().String("label", "", "Filter workflows by label")

	// Register completions for list command
	cmd.ValidArgsFunction = CompleteWorkflowNames

	return cmd
}

// RunListWorkflows lists workflows without checking GitHub status
func RunListWorkflows(pattern string, verbose bool, jsonOutput bool, labelFilter string) error {
	listWorkflowsLog.Printf("Listing workflows: pattern=%s, jsonOutput=%v, labelFilter=%s", pattern, jsonOutput, labelFilter)
	if verbose && !jsonOutput {
		fmt.Fprintf(os.Stderr, "Listing workflow files\n")
		if pattern != "" {
			fmt.Fprintf(os.Stderr, "Filtering by pattern: %s\n", pattern)
		}
	}

	mdFiles, err := getMarkdownWorkflowFiles("")
	if err != nil {
		listWorkflowsLog.Printf("Failed to get markdown workflow files: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
		return nil
	}

	listWorkflowsLog.Printf("Found %d markdown workflow files", len(mdFiles))
	if len(mdFiles) == 0 {
		if jsonOutput {
			// Output empty array for JSON
			output := []WorkflowListItem{}
			jsonBytes, _ := json.MarshalIndent(output, "", "  ")
			fmt.Println(string(jsonBytes))
			return nil
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No workflow files found."))
		return nil
	}

	if verbose && !jsonOutput {
		fmt.Fprintf(os.Stderr, "Found %d markdown workflow files\n", len(mdFiles))
	}

	// Build workflow list
	var workflows []WorkflowListItem

	for _, file := range mdFiles {
		name := extractWorkflowNameFromPath(file)

		// Skip if pattern specified and doesn't match
		if pattern != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(pattern)) {
			continue
		}

		// Extract engine ID from workflow file
		agent := extractEngineIDFromFile(file)

		// Check if compiled (.lock.yml file is in .github/workflows)
		lockFile := stringutil.MarkdownToLockFile(file)
		compiled := "N/A"

		if _, err := os.Stat(lockFile); err == nil {
			// Check if up to date
			mdStat, _ := os.Stat(file)
			lockStat, _ := os.Stat(lockFile)
			if mdStat.ModTime().After(lockStat.ModTime()) {
				compiled = "No"
			} else {
				compiled = "Yes"
			}
		}

		// Extract "on" field and labels from frontmatter
		var onField any
		var labels []string
		if content, err := os.ReadFile(file); err == nil {
			if result, err := parser.ExtractFrontmatterFromContent(string(content)); err == nil {
				if result.Frontmatter != nil {
					onField = result.Frontmatter["on"]
					// Extract labels field if present
					if labelsField, ok := result.Frontmatter["labels"]; ok {
						if labelsArray, ok := labelsField.([]any); ok {
							for _, label := range labelsArray {
								if labelStr, ok := label.(string); ok {
									labels = append(labels, labelStr)
								}
							}
						}
					}
				}
			}
		}

		// Skip if label filter specified and workflow doesn't have the label
		if labelFilter != "" {
			hasLabel := false
			for _, label := range labels {
				if strings.EqualFold(label, labelFilter) {
					hasLabel = true
					break
				}
			}
			if !hasLabel {
				continue
			}
		}

		// Build workflow list item
		workflows = append(workflows, WorkflowListItem{
			Workflow: name,
			EngineID: agent,
			Compiled: compiled,
			Labels:   labels,
			On:       onField,
		})
	}

	// Output results
	if jsonOutput {
		jsonBytes, err := json.MarshalIndent(workflows, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(jsonBytes))
		return nil
	}

	// Print workflow count message for text output
	workflowCount := len(workflows)
	if workflowCount == 1 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Found 1 workflow"))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Found %d workflows", workflowCount)))
	}

	// Render the table using struct-based rendering
	fmt.Fprint(os.Stderr, console.RenderStruct(workflows))

	return nil
}
