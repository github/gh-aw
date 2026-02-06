package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/spf13/cobra"
)

var fixLog = logger.New("cli:fix_command")

// FixConfig contains configuration for the fix command
type FixConfig struct {
	WorkflowIDs []string
	Write       bool
	Verbose     bool
	WorkflowDir string // Custom workflow directory
}

// RunFix runs the fix command with the given configuration
func RunFix(config FixConfig) error {
	return runFixCommand(config.WorkflowIDs, config.Write, config.Verbose, config.WorkflowDir)
}

// NewFixCommand creates the fix command
func NewFixCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fix [workflow]...",
		Short: "Apply automatic codemod-style fixes to agentic workflow files",
		Long: `Apply automatic codemod-style fixes to agentic workflow Markdown files.

This command applies a registry of codemods that automatically update deprecated fields
and migrate to new syntax. Codemods preserve formatting and comments as much as possible.

Available codemods:
  • timeout-minutes-migration: Replaces 'timeout_minutes' with 'timeout-minutes'
  • network-firewall-migration: Removes deprecated 'network.firewall' field
  • sandbox-agent-false-removal: Removes 'sandbox.agent: false' (firewall now mandatory)
  • safe-inputs-mode-removal: Removes deprecated 'safe-inputs.mode' field
  • schedule-at-to-around-migration: Converts 'daily at TIME' to 'daily around TIME'
  • delete-schema-file: Deletes deprecated .github/aw/schemas/agentic-workflow.json
  • delete-old-agents: Deletes old .agent.md files moved to .github/aw/
  • delete-old-templates: Removes old template files from pkg/cli/templates/

If no workflows are specified, all Markdown files in .github/workflows will be processed.

The command will:
  1. Scan workflow files for deprecated fields
  2. Apply relevant codemods to fix issues
  3. Report what was changed in each file
  4. Write updated files back to disk (with --write flag)
  5. Update prompt and agent files to latest templates (similar to 'init' command)
  6. Delete deprecated .github/aw/schemas/agentic-workflow.json file if it exists
  7. Delete old template files from pkg/cli/templates/ (with --write flag)
  8. Delete old .agent.md files that have been moved to .github/aw/ (with --write flag)

` + WorkflowIDExplanation + `

Examples:
  ` + string(constants.CLIExtensionPrefix) + ` fix                     # Check all workflows (dry-run)
  ` + string(constants.CLIExtensionPrefix) + ` fix --write             # Fix all workflows
  ` + string(constants.CLIExtensionPrefix) + ` fix my-workflow         # Check specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` fix my-workflow --write # Fix specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` fix --dir custom/workflows # Fix workflows in custom directory
  ` + string(constants.CLIExtensionPrefix) + ` fix --list-codemods     # List available codemods`,
		RunE: func(cmd *cobra.Command, args []string) error {
			listCodemods, _ := cmd.Flags().GetBool("list-codemods")
			write, _ := cmd.Flags().GetBool("write")
			verbose, _ := cmd.Flags().GetBool("verbose")
			dir, _ := cmd.Flags().GetString("dir")

			if listCodemods {
				return listAvailableCodemods()
			}

			return runFixCommand(args, write, verbose, dir)
		},
	}

	cmd.Flags().Bool("write", false, "Write changes to files (default is dry-run)")
	cmd.Flags().Bool("list-codemods", false, "List all available codemods and exit")
	cmd.Flags().StringP("dir", "d", "", "Workflow directory (default: .github/workflows)")

	// Register completions
	cmd.ValidArgsFunction = CompleteWorkflowNames
	RegisterDirFlagCompletion(cmd, "dir")

	return cmd
}

// listAvailableCodemods lists all available codemods
func listAvailableCodemods() error {
	codemods := GetAllCodemods()

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Available Codemods:"))
	fmt.Fprintln(os.Stderr, "")

	for _, codemod := range codemods {
		fmt.Fprintf(os.Stderr, "  %s\n", console.FormatInfoMessage(codemod.Name))
		fmt.Fprintf(os.Stderr, "    ID: %s\n", codemod.ID)
		if codemod.IntroducedIn != "" {
			fmt.Fprintf(os.Stderr, "    Introduced in: %s\n", codemod.IntroducedIn)
		}
		fmt.Fprintf(os.Stderr, "    %s\n", codemod.Description)
		fmt.Fprintln(os.Stderr, "")
	}

	return nil
}

// runFixCommand runs the fix command on specified or all workflows
func runFixCommand(workflowIDs []string, write bool, verbose bool, workflowDir string) error {
	fixLog.Printf("Running fix command: workflowIDs=%v, write=%v, verbose=%v, workflowDir=%s", workflowIDs, write, verbose, workflowDir)

	// Set up workflow directory (using default if not specified)
	if workflowDir == "" {
		workflowDir = ".github/workflows"
		fixLog.Printf("Using default workflow directory: %s", workflowDir)
	} else {
		workflowDir = filepath.Clean(workflowDir)
		fixLog.Printf("Using custom workflow directory: %s", workflowDir)
	}

	// Get workflow files to process
	var files []string
	var err error

	if len(workflowIDs) > 0 {
		// Process specific workflows
		for _, workflowID := range workflowIDs {
			file, err := resolveWorkflowFileInDir(workflowID, verbose, workflowDir)
			if err != nil {
				return err
			}
			files = append(files, file)
		}
	} else {
		// Process all workflows in the workflow directory
		files, err = getMarkdownWorkflowFiles(workflowDir)
		if err != nil {
			return err
		}
	}

	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No workflow files found."))
		return nil
	}

	// Load all codemods
	codemods := GetAllCodemods()
	fixLog.Printf("Loaded %d codemods", len(codemods))

	// Process each file
	var totalFixed int
	var totalFiles int
	var workflowsNeedingFixes []workflowFixInfo

	for _, file := range files {
		fixLog.Printf("Processing file: %s", file)

		fixed, appliedFixes, err := processWorkflowFileWithInfo(file, codemods, write, verbose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", console.FormatErrorMessage(fmt.Sprintf("Error processing %s: %v", filepath.Base(file), err)))
			continue
		}

		totalFiles++
		if fixed {
			totalFixed++
			if !write {
				workflowsNeedingFixes = append(workflowsNeedingFixes, workflowFixInfo{
					File:  filepath.Base(file),
					Fixes: appliedFixes,
				})
			}
		}
	}

	// Update prompt and agent files (similar to init command)
	// This ensures the latest templates are always used
	fixLog.Print("Updating prompt and agent files")

	// Update dispatcher agent
	if err := ensureAgenticWorkflowsDispatcher(verbose, false); err != nil {
		fixLog.Printf("Failed to update dispatcher agent: %v", err)
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatWarningMessage(fmt.Sprintf("Warning: Failed to update dispatcher agent: %v", err)))
	}

	// Delete old template files from pkg/cli/templates/ (only with --write)
	if write {
		fixLog.Print("Cleaning up old template files")
		if err := deleteOldTemplateFiles(verbose); err != nil {
			fixLog.Printf("Failed to delete old template files: %v", err)
			fmt.Fprintf(os.Stderr, "%s\n", console.FormatWarningMessage(fmt.Sprintf("Warning: Failed to delete old template files: %v", err)))
		}
	}

	// Delete old agent files if write flag is set
	if write {
		fixLog.Print("Deleting old agent files")
		if err := deleteOldAgentFiles(verbose); err != nil {
			fixLog.Printf("Failed to delete old agent files: %v", err)
			fmt.Fprintf(os.Stderr, "%s\n", console.FormatWarningMessage(fmt.Sprintf("Warning: Failed to delete old agent files: %v", err)))
		}
	}

	// Delete deprecated schema file if it exists
	schemaPath := filepath.Join(".github", "aw", "schemas", "agentic-workflow.json")
	if _, err := os.Stat(schemaPath); err == nil {
		fixLog.Printf("Found deprecated schema file at %s", schemaPath)
		if write {
			if err := os.Remove(schemaPath); err != nil {
				fixLog.Printf("Failed to delete schema file: %v", err)
				fmt.Fprintf(os.Stderr, "%s\n", console.FormatWarningMessage(fmt.Sprintf("Warning: Failed to delete deprecated schema file: %v", err)))
			} else {
				fixLog.Print("Deleted deprecated schema file")
				if verbose {
					fmt.Fprintf(os.Stderr, "%s\n", console.FormatSuccessMessage("Deleted deprecated .github/aw/schemas/agentic-workflow.json"))
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Would delete deprecated .github/aw/schemas/agentic-workflow.json"))
		}
	}

	// Print summary
	fmt.Fprintln(os.Stderr, "")
	if write {
		if totalFixed > 0 {
			fmt.Fprintf(os.Stderr, "%s\n", console.FormatSuccessMessage(fmt.Sprintf("✓ Fixed %d of %d workflow files", totalFixed, totalFiles)))
		} else {
			fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("✓ No fixes needed"))
		}
	} else {
		if totalFixed > 0 {
			fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage(fmt.Sprintf("Would fix %d of %d workflow files", totalFixed, totalFiles)))
			fmt.Fprintln(os.Stderr, "")

			// Output as agent prompt
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("To fix these issues, run:"))
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "  gh aw fix --write")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Or fix them individually:"))
			fmt.Fprintln(os.Stderr, "")
			for _, wf := range workflowsNeedingFixes {
				fmt.Fprintf(os.Stderr, "  gh aw fix %s --write\n", strings.TrimSuffix(wf.File, ".md"))
			}
		} else {
			fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("✓ No fixes needed"))
		}
	}

	return nil
}

// workflowFixInfo tracks workflow files that need fixes
type workflowFixInfo struct {
	File  string
	Fixes []string
}

// processWorkflowFile processes a single workflow file with all codemods
func processWorkflowFile(filePath string, codemods []Codemod, write bool, verbose bool) (bool, error) {
	fixed, _, err := processWorkflowFileWithInfo(filePath, codemods, write, verbose)
	return fixed, err
}

// processWorkflowFileWithInfo processes a single workflow file and returns detailed fix information
func processWorkflowFileWithInfo(filePath string, codemods []Codemod, write bool, verbose bool) (bool, []string, error) {
	fixLog.Printf("Processing workflow file: %s", filePath)

	// Read the file
	content, err := os.ReadFile(filePath)
	if err != nil {
		return false, nil, fmt.Errorf("failed to read file: %w", err)
	}

	originalContent := string(content)
	currentContent := originalContent

	// Track what was applied
	var appliedCodemods []string
	var hasChanges bool

	// Apply each codemod
	for _, codemod := range codemods {
		fixLog.Printf("Attempting codemod: %s", codemod.ID)

		// Re-parse frontmatter for each codemod to get fresh state
		currentResult, err := parser.ExtractFrontmatterFromContent(currentContent)
		if err != nil {
			fixLog.Printf("Failed to parse frontmatter for codemod %s: %v", codemod.ID, err)
			continue
		}

		newContent, applied, err := codemod.Apply(currentContent, currentResult.Frontmatter)
		if err != nil {
			fixLog.Printf("Codemod %s failed: %v", codemod.ID, err)
			return false, nil, fmt.Errorf("codemod %s failed: %w", codemod.ID, err)
		}

		if applied {
			currentContent = newContent
			appliedCodemods = append(appliedCodemods, codemod.Name)
			hasChanges = true
			fixLog.Printf("Applied codemod: %s", codemod.ID)
		}
	}

	// If no changes, report and return
	if !hasChanges {
		if verbose {
			fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage(fmt.Sprintf("  %s - no fixes needed", filepath.Base(filePath))))
		}
		return false, nil, nil
	}

	// Report changes
	fileName := filepath.Base(filePath)
	if write {
		// Write the file with owner-only read/write permissions (0600) for security best practices
		if err := os.WriteFile(filePath, []byte(currentContent), 0600); err != nil {
			return false, nil, fmt.Errorf("failed to write file: %w", err)
		}

		fmt.Fprintf(os.Stderr, "%s\n", console.FormatSuccessMessage(fmt.Sprintf("✓ %s", fileName)))
		for _, codemodName := range appliedCodemods {
			fmt.Fprintf(os.Stderr, "    • %s\n", codemodName)
		}
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatWarningMessage(fmt.Sprintf("⚠ %s", fileName)))
		for _, codemodName := range appliedCodemods {
			fmt.Fprintf(os.Stderr, "    • %s\n", codemodName)
		}
	}

	return true, appliedCodemods, nil
}
