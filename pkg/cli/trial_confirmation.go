package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var trialConfirmationLog = logger.New("cli:trial_confirmation")

const trialNestedListIndent = "   "

func formatTrialNestedListItem(item string) string {
	return trialNestedListIndent + console.FormatListItemStderr(item)
}

// trialConfirmationOptions holds parameters for showTrialConfirmation.
type trialConfirmationOptions struct {
	parsedSpecs         []*WorkflowSpec
	logicalRepoSlug     string
	cloneRepoSlug       string
	hostRepoSlug        string
	deleteHostRepo      bool
	forceDeleteHostRepo bool
	autoMergePRs        bool
	repeatCount         int
	directTrialMode     bool
	engineOverride      string
}

// showTrialConfirmation displays a confirmation prompt to the user using parsed workflow specs
func showTrialConfirmation(opts trialConfirmationOptions) error {
	trialConfirmationLog.Printf("Showing trial confirmation: workflows=%d, hostRepo=%s, cloneRepo=%s, repeat=%d, directMode=%v", len(opts.parsedSpecs), opts.hostRepoSlug, opts.cloneRepoSlug, opts.repeatCount, opts.directTrialMode)
	renderTrialPlanSections(opts)
	renderTrialExecutionSteps(opts)
	return confirmTrialExecution()
}

func renderTrialPlanSections(opts trialConfirmationOptions) {
	githubHost := getGitHubHost()
	hostRepoSlugURL := fmt.Sprintf("%s/%s", githubHost, opts.hostRepoSlug)

	var sections []string

	// Title box with double border
	titleText := "Trial Execution Plan"
	sections = append(sections, console.RenderTitleBox(titleText, 80)...)

	sections = append(sections, "")

	// Workflow information section
	var workflowInfo strings.Builder
	if len(opts.parsedSpecs) == 1 {
		fmt.Fprintf(&workflowInfo, "Workflow:  %s (from %s)", opts.parsedSpecs[0].WorkflowName, opts.parsedSpecs[0].RepoSlug)
	} else {
		workflowInfo.WriteString("Workflows:")
		for _, spec := range opts.parsedSpecs {
			fmt.Fprintf(&workflowInfo, "\n  • %s (from %s)", spec.WorkflowName, spec.RepoSlug)
		}
	}

	sections = append(sections, console.RenderInfoSection(workflowInfo.String())...)

	sections = append(sections, "")

	// Display target repository info based on mode
	sections = append(sections, console.RenderInfoSection(trialModeDescription(opts))...)
	sections = append(sections, "")

	var hostInfo strings.Builder
	fmt.Fprintf(&hostInfo, "Host Repo:  %s\n", opts.hostRepoSlug)
	fmt.Fprintf(&hostInfo, "            %s", hostRepoSlugURL)
	sections = append(sections, console.RenderInfoSection(hostInfo.String())...)
	sections = append(sections, "")
	sections = append(sections, console.RenderInfoSection(trialConfigurationDescription(opts))...)
	sections = append(sections, "")
	console.RenderComposedSections(sections)
}

func trialModeDescription(opts trialConfirmationOptions) string {
	var modeInfo strings.Builder
	if opts.cloneRepoSlug != "" {
		// Clone-repo mode
		fmt.Fprintf(&modeInfo, "Source:    %s (will be cloned)\n", opts.cloneRepoSlug)
		modeInfo.WriteString("Mode:      Clone repository contents into host repository")
	} else if opts.directTrialMode {
		// Direct trial mode
		fmt.Fprintf(&modeInfo, "Target:    %s (direct)\n", opts.hostRepoSlug)
		modeInfo.WriteString("Mode:      Run workflows directly in repository (no simulation)")
	} else {
		// Logical-repo mode
		fmt.Fprintf(&modeInfo, "Target:    %s (simulated)\n", opts.logicalRepoSlug)
		modeInfo.WriteString("Mode:      Simulate execution against target repository")
	}

	return modeInfo.String()
}

func trialConfigurationDescription(opts trialConfirmationOptions) string {
	// Configuration settings
	var configInfo strings.Builder
	if opts.deleteHostRepo {
		configInfo.WriteString("Cleanup:   Host repository will be deleted after completion")
	} else {
		configInfo.WriteString("Cleanup:   Host repository will be preserved")
	}

	// Display secret usage information (only when engine override is specified)
	if opts.engineOverride != "" {
		configInfo.WriteString("\n")
		fmt.Fprintf(&configInfo, "Secrets:   Will prompt for %s API key if needed (stored as repository secret)", opts.engineOverride)
	}

	// Display repeat count if set
	if opts.repeatCount > 0 {
		fmt.Fprintf(&configInfo, "\nRepeat:    Will run %d times (total executions: %d)", opts.repeatCount, opts.repeatCount+1)
	}

	// Display auto-merge setting if enabled
	if opts.autoMergePRs {
		configInfo.WriteString("\nAuto-merge: Pull requests will be automatically merged")
	}

	return configInfo.String()
}

func renderTrialExecutionSteps(opts trialConfirmationOptions) {
	// Add "Execution Steps" section separator
	executionStepsSections := console.RenderTitleBox("Execution Steps", 80)
	console.RenderComposedSections(executionStepsSections)

	// Check if host repository already exists to update messaging
	hostRepoExists := false
	checkCmd := workflow.ExecGH("repo", "view", opts.hostRepoSlug)
	if err := checkCmd.Run(); err == nil {
		hostRepoExists = true
	}
	trialConfirmationLog.Printf("Host repo check: exists=%v, forceDelete=%v", hostRepoExists, opts.forceDeleteHostRepo)

	// Step 1: Repository creation/reuse
	stepNum := 1
	if hostRepoExists && opts.forceDeleteHostRepo {
		fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Delete and recreate host repository\n"), stepNum)
	} else if hostRepoExists {
		fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Reuse existing host repository\n"), stepNum)
	} else {
		fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Create a private host repository\n"), stepNum)
	}
	stepNum++

	// Step 2: Clone contents (only in clone-repo mode)
	stepNum = renderTrialCloneSteps(opts, hostRepoExists, stepNum)
	stepNum = renderTrialInstallSteps(opts, stepNum)
	stepNum = renderTrialWorkflowExecution(opts, stepNum)
	if opts.deleteHostRepo {
		fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Delete the host repository\n"), stepNum)
	} else {
		fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Preserve the host repository for inspection\n"), stepNum)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"))
	fmt.Fprintln(os.Stderr, "")
}

func renderTrialCloneSteps(opts trialConfirmationOptions, hostRepoExists bool, stepNum int) int {
	if opts.cloneRepoSlug != "" {
		if hostRepoExists && !opts.forceDeleteHostRepo {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Force push contents from %s (overwriting existing content)\n"), stepNum, opts.cloneRepoSlug)
		} else {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Clone contents from %s\n"), stepNum, opts.cloneRepoSlug)
		}
		stepNum++

		// Show that workflows will be disabled
		if len(opts.parsedSpecs) == 1 {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Disable all workflows in cloned repository except %s\n"), stepNum, opts.parsedSpecs[0].WorkflowName)
		} else {
			workflowNames := sliceutil.Map(opts.parsedSpecs, func(spec *WorkflowSpec) string { return spec.WorkflowName })
			fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Disable all workflows in cloned repository except: %s\n"), stepNum, strings.Join(workflowNames, ", "))
		}
		stepNum++
	}
	return stepNum
}

func renderTrialInstallSteps(opts trialConfirmationOptions, stepNum int) int {
	// Step 3/2: Install and compile workflows
	if len(opts.parsedSpecs) == 1 {
		fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Install and compile %s\n"), stepNum, opts.parsedSpecs[0].WorkflowName)
	} else {
		workflowNames := sliceutil.Map(opts.parsedSpecs, func(spec *WorkflowSpec) string { return spec.WorkflowName })
		fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Install and compile: %s\n"), stepNum, strings.Join(workflowNames, ", "))
	}
	stepNum++

	// Step: Configure secrets (only when engine override is specified)
	if opts.engineOverride != "" {
		fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Ensure %s API key secret is configured\n"), stepNum, opts.engineOverride)
		stepNum++
	}
	return stepNum
}

func renderTrialWorkflowExecution(opts trialConfirmationOptions, stepNum int) int {
	// Step 5/4: Execute workflows and auto-merge (repeated if --repeat is used)
	if len(opts.parsedSpecs) == 1 {
		workflowName := opts.parsedSpecs[0].WorkflowName
		if opts.repeatCount > 0 && opts.autoMergePRs {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. For each of %d executions:\n"), stepNum, opts.repeatCount+1)
			fmt.Fprintln(os.Stderr, formatTrialNestedListItem("Execute "+workflowName))
			fmt.Fprintln(os.Stderr, formatTrialNestedListItem("Auto-merge any pull requests created during execution"))
		} else if opts.repeatCount > 0 {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Execute %s %d times\n"), stepNum, workflowName, opts.repeatCount+1)
		} else if opts.autoMergePRs {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Execute %s\n"), stepNum, workflowName)
			stepNum++
			fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Auto-merge any pull requests created during execution\n"), stepNum)
		} else {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Execute %s\n"), stepNum, workflowName)
		}
	} else {
		workflowNames := sliceutil.Map(opts.parsedSpecs, func(spec *WorkflowSpec) string { return spec.WorkflowName })
		workflowList := strings.Join(workflowNames, ", ")

		if opts.repeatCount > 0 && opts.autoMergePRs {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. For each of %d executions:\n"), stepNum, opts.repeatCount+1)
			fmt.Fprintln(os.Stderr, formatTrialNestedListItem("Execute: "+workflowList))
			fmt.Fprintln(os.Stderr, formatTrialNestedListItem("Auto-merge any pull requests created during execution"))
		} else if opts.repeatCount > 0 {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Execute %d times: %s\n"), stepNum, opts.repeatCount+1, workflowList)
		} else if opts.autoMergePRs {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Execute: %s\n"), stepNum, workflowList)
			stepNum++
			fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Auto-merge any pull requests created during execution\n"), stepNum)
		} else {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessageStderr("  %d. Execute: %s\n"), stepNum, workflowList)
		}
	}
	stepNum++
	return stepNum
}

func confirmTrialExecution() error {
	// Ask for confirmation using console helper
	confirmed, err := console.ConfirmAction(
		"Do you want to continue?",
		"Yes, proceed",
		"No, cancel",
	)
	if err != nil {
		return fmt.Errorf("confirmation failed: %w", err)
	}

	if !confirmed {
		trialConfirmationLog.Print("Trial cancelled by user")
		return errors.New("trial cancelled by user")
	}

	trialConfirmationLog.Print("Trial confirmed by user, proceeding")
	return nil
}
