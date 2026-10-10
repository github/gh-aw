package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/tty"
	"github.com/github/gh-aw/pkg/workflow"
)

var runInteractiveLog = logger.New("cli:run_interactive")

// WorkflowOption represents a workflow that can be run
type WorkflowOption struct {
	Name        string
	Description string
	FilePath    string
	Inputs      map[string]*workflow.InputDefinition
}

// RunWorkflowInteractively runs a workflow in interactive mode
func RunWorkflowInteractively(ctx context.Context, opts RunWorkflowOptions) error {
	runInteractiveLog.Print("Starting interactive workflow run")

	// Check if running in CI environment
	if IsRunningInCI() {
		return errors.New("interactive mode cannot be used in CI environments")
	}

	if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Starting interactive workflow run..."))
	}

	// Step 1: Find workflows with workflow_dispatch trigger
	workflows, err := findRunnableWorkflows(opts.Verbose)
	if err != nil {
		return fmt.Errorf("failed to find runnable workflows: %w", err)
	}

	if len(workflows) == 0 {
		return errors.New("no runnable workflows found. Workflows must have 'workflow_dispatch' trigger")
	}

	// Step 2: Let user select a workflow
	selectedWorkflow, err := selectWorkflow(ctx, workflows)
	if err != nil {
		return fmt.Errorf("workflow selection cancelled or failed: %w", err)
	}

	runInteractiveLog.Printf("Selected workflow: %s", selectedWorkflow.Name)

	// Step 3: Show workflow information
	showWorkflowInfo(selectedWorkflow)

	// Step 4: Collect workflow inputs if needed
	inputValues, err := collectWorkflowInputs(ctx, selectedWorkflow)
	if err != nil {
		return fmt.Errorf("failed to collect workflow inputs: %w", err)
	}

	// Step 5: Confirm execution
	if !confirmExecution(ctx, selectedWorkflow, inputValues) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Workflow execution cancelled"))
		return nil
	}

	// Step 6: Build command string for display
	cmdStr := buildCommandString(selectedWorkflow.Name, inputValues, opts.RepoOverride, opts.RefOverride, opts.AutoMergePRs, opts.Push, opts.EngineOverride, opts.Approve)
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("\nRunning workflow..."))
	fmt.Fprintln(os.Stderr, console.FormatCommandMessage("Equivalent command: "+cmdStr))
	fmt.Fprintln(os.Stderr, "")

	// Step 7: Execute the workflow
	err = RunWorkflowOnGitHub(ctx, selectedWorkflow.Name, RunOptions{
		Enable:         false,
		EngineOverride: opts.EngineOverride,
		RepoOverride:   opts.RepoOverride,
		RefOverride:    opts.RefOverride,
		AutoMergePRs:   opts.AutoMergePRs,
		Push:           opts.Push,
		Inputs:         inputValues,
		Verbose:        opts.Verbose,
		DryRun:         opts.DryRun,
		Approve:        opts.Approve,
	})
	if err != nil {
		return fmt.Errorf("failed to run workflow: %w", err)
	}

	// Show success message with command to run again
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Workflow dispatched successfully!"))
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("To run this workflow again, use:"))
	fmt.Fprintln(os.Stderr, console.FormatCommandMessage(cmdStr))

	return nil
}

// findRunnableWorkflows finds all workflows that support workflow_dispatch
func findRunnableWorkflows(verbose bool) ([]WorkflowOption, error) {
	runInteractiveLog.Print("Finding runnable workflows")

	// Get all markdown workflow files
	workflowsDir := constants.GetWorkflowDir()
	mdFiles, err := getMarkdownWorkflowFiles(workflowsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow files: %w", err)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "Found %d workflow files, checking for workflow_dispatch trigger...\n", len(mdFiles))
	}

	var runnableWorkflows []WorkflowOption

	for _, mdFile := range mdFiles {
		// Check if workflow is runnable
		runnable, err := IsRunnable(mdFile)
		if err != nil {
			runInteractiveLog.Printf("Failed to check if workflow %s is runnable: %v", mdFile, err)
			continue
		}

		if !runnable {
			continue
		}

		// Extract workflow name
		name := normalizeWorkflowID(mdFile)

		// Get workflow inputs
		inputs, err := getWorkflowInputs(mdFile)
		if err != nil {
			runInteractiveLog.Printf("Failed to get inputs for workflow %s: %v", mdFile, err)
			// Continue without inputs
			inputs = nil
		}

		// Build description
		description := buildWorkflowDescription(inputs)

		runnableWorkflows = append(runnableWorkflows, WorkflowOption{
			Name:        name,
			Description: description,
			FilePath:    mdFile,
			Inputs:      inputs,
		})
	}

	runInteractiveLog.Printf("Found %d runnable workflows", len(runnableWorkflows))
	return runnableWorkflows, nil
}

// buildWorkflowDescription creates a description string for a workflow
func buildWorkflowDescription(inputs map[string]*workflow.InputDefinition) string {
	// Always return empty string to avoid showing input counts
	return ""
}

// selectWorkflow displays an interactive list for workflow selection with fuzzy search
func selectWorkflow(ctx context.Context, workflows []WorkflowOption) (*WorkflowOption, error) {
	runInteractiveLog.Printf("Displaying workflow selection: %d workflows", len(workflows))

	// Check if we're in a TTY environment
	if !tty.IsStderrTerminal() {
		return selectWorkflowNonInteractive(workflows)
	}

	// Build select options
	options := sliceutil.Map(workflows, func(wf WorkflowOption) huh.Option[string] { return huh.NewOption(wf.Name, wf.Name) })

	var selected string
	form := console.NewSelectForm(
		huh.NewSelect[string]().
			Title("Select a workflow to run").
			Description("↑/↓ to navigate, / to search, Enter to select").
			Options(options...).
			Filtering(true).
			Height(15).
			Value(&selected),
	)

	if err := form.RunWithContext(ctx); err != nil {
		return nil, fmt.Errorf("workflow selection cancelled or failed: %w", err)
	}

	// Find the selected workflow
	for i := range workflows {
		if workflows[i].Name == selected {
			return &workflows[i], nil
		}
	}

	return nil, fmt.Errorf("selected workflow not found: %s", selected)
}

// selectWorkflowNonInteractive provides a fallback for non-TTY environments
func selectWorkflowNonInteractive(workflows []WorkflowOption) (*WorkflowOption, error) {
	runInteractiveLog.Printf("Non-TTY detected, showing text list: %d workflows", len(workflows))

	fmt.Fprintf(os.Stderr, "\nSelect a workflow to run:\n\n")
	for i, wf := range workflows {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, wf.Name)
	}
	fmt.Fprintf(os.Stderr, "\nSelect (1-%d): ", len(workflows))

	var choice int
	_, err := fmt.Scanf("%d", &choice)
	if err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	if choice < 1 || choice > len(workflows) {
		return nil, fmt.Errorf("selection out of range (must be 1-%d)", len(workflows))
	}

	selectedWorkflow := &workflows[choice-1]
	runInteractiveLog.Printf("Selected workflow from text list: %s", selectedWorkflow.Name)
	return selectedWorkflow, nil
}

// showWorkflowInfo displays information about the selected workflow
func showWorkflowInfo(wf *WorkflowOption) {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Workflow: "+wf.Name))

	if len(wf.Inputs) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("\nWorkflow Inputs:"))
		for name, input := range wf.Inputs {
			required := ""
			if input.Required {
				required = " (required)"
			}
			desc := ""
			if input.Description != "" {
				desc = " - " + input.Description
			}
			defaultVal := ""
			if input.Default != "" {
				defaultVal = fmt.Sprintf(" [default: %s]", input.Default)
			}
			fmt.Fprintf(os.Stderr, "  • %s%s%s%s\n", name, required, desc, defaultVal)
		}
	}
	fmt.Fprintln(os.Stderr, "")
}

// collectWorkflowInputs collects input values from the user
func collectWorkflowInputs(ctx context.Context, wf *WorkflowOption) ([]string, error) {
	if len(wf.Inputs) == 0 {
		return nil, nil
	}

	runInteractiveLog.Printf("Collecting %d workflow inputs", len(wf.Inputs))
	return collectInputsWithMap(ctx, wf.Inputs)
}

// collectInputsWithMap collects inputs using a map to properly capture values
func collectInputsWithMap(ctx context.Context, inputs map[string]*workflow.InputDefinition) ([]string, error) {
	// Create a map to store string values for the form
	inputValues := make(map[string]string)
	// Create a map to track the string pointers we'll pass to huh
	inputPtrs := make(map[string]*string)
	var formGroups []*huh.Group

	// Create input fields for each workflow input
	for name, input := range inputs {
		inputName := name
		inputDef := input

		// Initialize with default value (convert any to string)
		defaultStr := ""
		if inputDef.Default != nil {
			defaultStr = fmt.Sprintf("%v", inputDef.Default)
		}
		inputValues[inputName] = defaultStr

		// Create a string variable for this input that huh can update
		valueStr := defaultStr
		inputPtrs[inputName] = &valueStr

		// Create input field that updates the string variable
		field := huh.NewInput().
			Title(fmt.Sprintf("Enter value for '%s'", inputName)).
			Value(inputPtrs[inputName])

		if inputDef.Description != "" {
			field = field.Description(inputDef.Description)
		}

		if inputDef.Required {
			field = field.Validate(func(s string) error {
				if s == "" {
					return errors.New("this input is required")
				}
				return nil
			})
		}

		group := huh.NewGroup(field)
		formGroups = append(formGroups, group)
	}

	// Show the form
	form := console.NewForm(formGroups...)
	if err := form.RunWithContext(ctx); err != nil {
		return nil, fmt.Errorf("input collection cancelled: %w", err)
	}

	// Collect the final values from the pointers
	var result []string
	for name, valuePtr := range inputPtrs {
		value := *valuePtr
		if value != "" {
			result = append(result, fmt.Sprintf("%s=%s", name, value))
		}
	}

	runInteractiveLog.Printf("Collected %d input values", len(result))
	return result, nil
}

// confirmExecution asks the user to confirm workflow execution
func confirmExecution(ctx context.Context, wf *WorkflowOption, inputs []string) bool {
	runInteractiveLog.Print("Requesting execution confirmation")

	var confirm bool
	message := fmt.Sprintf("Run workflow '%s'?", wf.Name)

	if len(inputs) > 0 {
		message = fmt.Sprintf("Run workflow '%s' with %d input(s)?", wf.Name, len(inputs))
	}

	form := console.NewConfirmForm(
		huh.NewConfirm().
			Title(message).
			Affirmative("Yes, run it").
			Negative("No, cancel").
			Value(&confirm),
	)

	if err := form.RunWithContext(ctx); err != nil {
		if console.IsCancelled(err) {
			runInteractiveLog.Print("User aborted confirmation")
		} else {
			runInteractiveLog.Printf("Confirmation failed: %v", err)
		}
		return false
	}

	runInteractiveLog.Printf("User confirmed: %v", confirm)
	return confirm
}

// RunWorkflowOptions holds parameters for RunSpecificWorkflowInteractively.
type RunWorkflowOptions struct {
	WorkflowName       string
	Verbose            bool
	EngineOverride     string
	RepoOverride       string
	RefOverride        string
	AutoMergePRs       bool
	Push               bool
	DryRun             bool
	Approve            bool
	requiredInputsOnly bool
}

// RunSpecificWorkflowInteractively runs a specific workflow in interactive mode
// This is similar to RunWorkflowInteractively but skips the workflow selection step
// since the workflow name is already known. It will still collect inputs if the workflow has them.
func RunSpecificWorkflowInteractively(ctx context.Context, opts RunWorkflowOptions) error {
	runInteractiveLog.Printf("Running specific workflow interactively: %s", opts.WorkflowName)

	// Find the workflow file
	workflowsDir := constants.GetWorkflowDir()
	mdFile := filepath.Join(workflowsDir, opts.WorkflowName+".md")

	// Check if file exists
	if _, err := os.Stat(mdFile); os.IsNotExist(err) {
		return fmt.Errorf("workflow file not found: %s", mdFile)
	}

	// Get workflow inputs
	inputs, err := getWorkflowInputs(mdFile)
	if err != nil {
		runInteractiveLog.Printf("Failed to get inputs for workflow %s: %v", opts.WorkflowName, err)
		// Continue without inputs - they might not be required
		inputs = nil
	}
	if opts.requiredInputsOnly {
		inputs = requiredWorkflowInputs(inputs)
	}

	// Create the workflow option used to collect required inputs.
	wf := &WorkflowOption{
		Name:     opts.WorkflowName,
		FilePath: mdFile,
		Inputs:   inputs,
	}

	// Collect workflow inputs if needed
	inputValues, err := collectWorkflowInputs(ctx, wf)
	if err != nil {
		return fmt.Errorf("failed to collect workflow inputs: %w", err)
	}

	// Confirm execution (skip if no inputs were collected - user already confirmed they want to run)
	if len(inputValues) > 0 && !confirmExecution(ctx, wf, inputValues) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Workflow execution cancelled"))
		return nil
	}

	// Execute the workflow
	err = RunWorkflowOnGitHub(ctx, opts.WorkflowName, RunOptions{
		Enable:            false,
		EngineOverride:    opts.EngineOverride,
		RepoOverride:      opts.RepoOverride,
		RefOverride:       opts.RefOverride,
		AutoMergePRs:      opts.AutoMergePRs,
		Push:              opts.Push,
		WaitForCompletion: true,
		Inputs:            inputValues,
		Verbose:           opts.Verbose,
		DryRun:            opts.DryRun,
		Approve:           opts.Approve,
	})
	if err != nil {
		return fmt.Errorf("failed to run workflow: %w", err)
	}

	return nil
}

func requiredWorkflowInputs(inputs map[string]*workflow.InputDefinition) map[string]*workflow.InputDefinition {
	requiredInputs := make(map[string]*workflow.InputDefinition)
	for name, definition := range inputs {
		if definition != nil && definition.Required {
			requiredInputs[name] = definition
		}
	}
	return requiredInputs
}

// buildCommandString builds the equivalent command string for display
func buildCommandString(workflowName string, inputs []string, repoOverride, refOverride string, autoMergePRs, push bool, engineOverride string, approve bool) string {
	parts := []string{string(constants.CLIExtensionPrefix), "run", workflowName}

	// Add inputs
	for _, input := range inputs {
		parts = append(parts, "-F", input)
	}

	// Add flags
	if repoOverride != "" {
		parts = append(parts, "--repo", repoOverride)
	}
	if refOverride != "" {
		parts = append(parts, "--ref", refOverride)
	}
	if autoMergePRs {
		parts = append(parts, "--auto-merge-prs")
	}
	if push {
		parts = append(parts, "--push")
	}
	if engineOverride != "" {
		parts = append(parts, "--engine", engineOverride)
	}
	if approve {
		parts = append(parts, "--approve")
	}

	return strings.Join(parts, " ")
}
