package cli

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/goccy/go-yaml"
)

var validationLog = logger.New("cli:run_workflow_validation")

// getLockFilePath converts a markdown workflow path to its compiled lock file path
// Example: "/path/to/workflow.md" -> "/path/to/workflow.lock.yml"
func getLockFilePath(markdownPath string) string {
	// Handle regular workflow files
	return strings.TrimSuffix(markdownPath, ".md") + ".lock.yml"
}

// IsRunnable checks if a workflow can be run (has schedule or workflow_dispatch trigger)
// This function checks the compiled .lock.yml file because that's what GitHub Actions uses.
func IsRunnable(markdownPath string) (bool, error) {
	// Convert markdown path to lock file path
	lockPath := getLockFilePath(markdownPath)
	cleanLockPath := filepath.Clean(lockPath)

	validationLog.Printf("Checking if workflow is runnable: markdown=%s, lock=%s", markdownPath, lockPath)

	// Check if the lock file exists
	if _, err := os.Stat(cleanLockPath); os.IsNotExist(err) {
		validationLog.Printf("Lock file does not exist: %s", cleanLockPath)
		return false, errors.New("workflow has not been compiled yet - run 'gh aw compile' first")
	}

	// Read the lock file - path is sanitized using filepath.Clean() to prevent path traversal attacks.
	// The lockPath is derived from markdownPath which comes from trusted sources (CLI arguments, validated workflow paths).
	contentBytes, err := os.ReadFile(cleanLockPath) // #nosec G304 -- path is sanitized with filepath.Clean() and derived from trusted CLI argument
	if err != nil {
		return false, fmt.Errorf("failed to read lock file: %w", err)
	}

	// Parse the YAML content
	var workflowYAML map[string]any
	if err := yaml.Unmarshal(contentBytes, &workflowYAML); err != nil {
		return false, fmt.Errorf("failed to parse lock file YAML: %w", err)
	}

	// Check if 'on' section is present
	onSection, exists := workflowYAML["on"]
	if !exists {
		validationLog.Printf("No 'on' section found in lock file")
		// If no 'on' section, it's not runnable
		return false, nil
	}

	// Convert to map if possible
	onMap, ok := onSection.(map[string]any)
	if !ok {
		// If 'on' is not a map, check if it's a string/list that might indicate workflow_dispatch
		onStr := fmt.Sprintf("%v", onSection)
		onStrLower := strings.ToLower(onStr)
		hasWorkflowDispatch := strings.Contains(onStrLower, "workflow_dispatch")
		validationLog.Printf("On section is not a map, checking string: hasWorkflowDispatch=%v", hasWorkflowDispatch)
		return hasWorkflowDispatch, nil
	}

	// Check if workflow_dispatch trigger exists
	_, hasWorkflowDispatch := onMap["workflow_dispatch"]
	validationLog.Printf("Workflow runnable check: hasWorkflowDispatch=%v", hasWorkflowDispatch)
	return hasWorkflowDispatch, nil
}

// getWorkflowInputs extracts workflow_dispatch inputs from the compiled lock file
// This function checks the .lock.yml file because that's what GitHub Actions uses.
func getWorkflowInputs(markdownPath string) (map[string]*workflow.InputDefinition, error) {
	// Convert markdown path to lock file path
	lockPath := getLockFilePath(markdownPath)
	cleanLockPath := filepath.Clean(lockPath)

	validationLog.Printf("Extracting workflow inputs from lock file: %s", lockPath)

	// Check if the lock file exists
	if _, err := os.Stat(cleanLockPath); os.IsNotExist(err) {
		validationLog.Printf("Lock file does not exist: %s", cleanLockPath)
		return nil, errors.New("workflow has not been compiled yet - run 'gh aw compile' first")
	}

	// Read the lock file - path is sanitized using filepath.Clean() to prevent path traversal attacks.
	// The lockPath is derived from markdownPath which comes from trusted sources (CLI arguments, validated workflow paths).
	contentBytes, err := os.ReadFile(cleanLockPath) // #nosec G304 -- path is sanitized with filepath.Clean() and derived from trusted CLI argument
	if err != nil {
		return nil, fmt.Errorf("failed to read lock file: %w", err)
	}

	// Parse the YAML content
	var workflowYAML map[string]any
	if err := yaml.Unmarshal(contentBytes, &workflowYAML); err != nil {
		return nil, fmt.Errorf("failed to parse lock file YAML: %w", err)
	}

	// Check if 'on' section is present
	onSection, exists := workflowYAML["on"]
	if !exists {
		return nil, nil
	}

	// Convert to map if possible
	onMap, ok := onSection.(map[string]any)
	if !ok {
		return nil, nil
	}

	// Get workflow_dispatch section
	workflowDispatch, exists := onMap["workflow_dispatch"]
	if !exists {
		return nil, nil
	}

	// Convert to map
	workflowDispatchMap, ok := workflowDispatch.(map[string]any)
	if !ok {
		// workflow_dispatch might be null/empty
		return nil, nil
	}

	// Get inputs section
	inputsSection, exists := workflowDispatchMap["inputs"]
	if !exists {
		return nil, nil
	}

	// Convert to map
	inputsMap, ok := inputsSection.(map[string]any)
	if !ok {
		return nil, nil
	}

	// Parse input definitions
	parsed := workflow.ParseInputDefinitions(inputsMap)

	// Remove aw_context from the returned inputs - it is an internal input managed by the
	// agentic workflow system and should never be surfaced to users for prompting or display.
	delete(parsed, workflow.AwContextInputName)

	return parsed, nil
}

// validateWorkflowInputs validates that required inputs are provided and checks for typos.
//
// This validation function is co-located with the run command implementation because:
//   - It's specific to the workflow run operation
//   - It's only called during workflow dispatch
//   - It provides immediate feedback before triggering the workflow
//
// The function validates:
//   - All required inputs are provided
//   - Provided input names match defined inputs (typo detection)
//   - Suggestions for misspelled input names
//
// This follows the principle that domain-specific validation belongs in domain files.
func validateWorkflowInputs(markdownPath string, providedInputs []string) error {
	// Extract workflow inputs
	workflowInputs, err := getWorkflowInputs(markdownPath)
	if err != nil {
		// Don't fail validation if we can't extract inputs
		validationLog.Printf("Failed to extract workflow inputs: %v", err)
		return nil
	}

	// If no inputs are defined, no validation needed
	if len(workflowInputs) == 0 {
		return nil
	}

	// Parse provided inputs into a map
	providedInputsMap := make(map[string]string)
	for _, input := range providedInputs {
		parts := strings.SplitN(input, "=", 2)
		if len(parts) == 2 {
			providedInputsMap[parts[0]] = parts[1]
		}
	}

	// Check for required inputs that are missing (ignore inputs with a default value)
	var missingInputs []string
	for inputName, inputDef := range workflowInputs {
		if inputDef.Required && inputDef.Default == nil {
			if _, exists := providedInputsMap[inputName]; !exists {
				missingInputs = append(missingInputs, inputName)
			}
		}
	}

	// Check for typos in provided input names
	var typos []string
	var suggestions []string
	validInputNames := slices.Collect(maps.Keys(workflowInputs))

	for providedName := range providedInputsMap {
		// Check if this is a valid input name
		if _, exists := workflowInputs[providedName]; !exists {
			// Find closest matches
			matches := parser.FindClosestMatches(providedName, validInputNames, 3)
			if len(matches) > 0 {
				typos = append(typos, providedName)
				suggestions = append(suggestions, fmt.Sprintf("'%s' -> did you mean '%s'?", providedName, strings.Join(matches, "', '")))
			} else {
				typos = append(typos, providedName)
				suggestions = append(suggestions, fmt.Sprintf("'%s' is not a valid input name", providedName))
			}
		}
	}

	// Build error message if there are validation errors
	if len(missingInputs) > 0 || len(typos) > 0 {
		var errorParts []string

		if len(missingInputs) > 0 {
			errorParts = append(errorParts, "Missing required input(s): "+strings.Join(missingInputs, ", "))
		}

		if len(typos) > 0 {
			errorParts = append(errorParts, "Invalid input name(s):\n  "+strings.Join(suggestions, "\n  "))
		}

		// Add helpful information about valid inputs
		if len(workflowInputs) > 0 {
			var inputDescriptions []string
			sortedNames := sliceutil.SortedKeys(workflowInputs)
			for _, name := range sortedNames {
				def := workflowInputs[name]
				required := ""
				if def.Required && def.Default == nil {
					required = " (required)"
				}
				desc := ""
				if def.Description != "" {
					desc = ": " + def.Description
				}
				defaultStr := ""
				if def.Default != nil {
					defaultStr = fmt.Sprintf(" [default: %s]", def.GetDefaultAsString())
				}
				inputDescriptions = append(inputDescriptions, fmt.Sprintf("  %s%s%s%s", name, required, desc, defaultStr))
			}

			// Derive the workflow name for the syntax hint
			workflowName := strings.TrimSuffix(filepath.Base(markdownPath), ".md")
			var syntaxExamples []string
			for _, name := range sortedNames {
				def := workflowInputs[name]
				if def.Required && def.Default == nil {
					syntaxExamples = append(syntaxExamples, fmt.Sprintf("  gh aw run %s -F %s=<value>", workflowName, name))
				}
			}

			validInputsMsg := "\nValid inputs:\n" + strings.Join(inputDescriptions, "\n")
			if len(syntaxExamples) > 0 {
				validInputsMsg += "\n\nTo set required inputs, use:\n" + strings.Join(syntaxExamples, "\n")
			}
			errorParts = append(errorParts, validInputsMsg)
		}

		return workflow.NewValidationError(
			"on.workflow_dispatch.inputs",
			strings.Join(providedInputs, ", "),
			strings.Join(errorParts, "\n\n"),
			fmt.Sprintf("Define and provide valid workflow_dispatch inputs.\n\nExample workflow frontmatter:\n\non:\n  workflow_dispatch:\n    inputs:\n      issue_url:\n        description: \"Issue URL\"\n        required: true\n        type: string\n\nExample command:\n  gh aw run %s -F issue_url=https://github.com/org/repo/issues/123", strings.TrimSuffix(filepath.Base(markdownPath), ".md")),
		)
	}

	return nil
}

// validateRemoteWorkflow checks if a workflow exists in a remote repository and can be triggered.
//
// This validation function is co-located with the run command implementation because:
//   - It's specific to remote workflow execution
//   - It's only called when running workflows in remote repositories
//   - It provides early validation before attempting workflow dispatch
//
// The function validates:
//   - The specified repository exists and is accessible
//   - The workflow file exists in the repository
//   - The workflow can be triggered via GitHub Actions API
//
// This follows the principle that domain-specific validation belongs in domain files.
func validateRemoteWorkflow(workflowName string, repoOverride string, verbose bool) error {
	if repoOverride == "" {
		return errors.New("repository must be specified for remote workflow validation")
	}

	// Normalize workflow ID to handle both "workflow-name" and ".github/workflows/workflow-name.md" formats
	normalizedID := normalizeWorkflowID(workflowName)

	// Add .lock.yml extension
	lockFileName := normalizedID + ".lock.yml"

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatProgressMessageStderr(fmt.Sprintf("Checking if workflow '%s' exists in repository '%s'...", lockFileName, repoOverride)))
	}

	workflows, err := fetchGitHubWorkflows(context.Background(), repoOverride, verbose)
	if err != nil {
		return fmt.Errorf("failed to list workflows in repository '%s': %w", repoOverride, err)
	}

	// Look for the workflow by checking if the lock file path exists
	for _, wf := range workflows {
		if filepath.Base(wf.Path) == lockFileName {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatProgressMessageStderr(fmt.Sprintf("Found workflow '%s' in repository (path: %s, state: %s)",
					wf.Name, wf.Path, wf.State)))
			}
			return nil
		}
	}

	suggestions := []string{
		"Check if the workflow has been pushed to the remote repository",
		"Verify the workflow file exists in the repository's .github/workflows directory",
		fmt.Sprintf("Run '%s status' to see available workflows", string(constants.CLIExtensionPrefix)),
	}
	return errors.New(console.FormatErrorWithSuggestions(
		fmt.Sprintf("workflow '%s' not found in repository '%s'", lockFileName, repoOverride),
		suggestions,
	))
}
