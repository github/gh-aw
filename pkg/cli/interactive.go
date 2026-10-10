package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"charm.land/huh/v2"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/envutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/tty"
	"github.com/github/gh-aw/pkg/workflow"
)

var interactiveLog = logger.New("cli:interactive")

// commonWorkflowNames contains common workflow name patterns for autocomplete suggestions
var commonWorkflowNames = []string{
	"issue-triage",
	"pr-review-helper",
	"code-quality-check",
	"security-scan",
	"daily-report",
	"weekly-summary",
	"release-notes",
	"bug-reporter",
	"dependency-update",
	"documentation-check",
}

// InteractiveWorkflowBuilder collects user input to build an agentic workflow
type InteractiveWorkflowBuilder struct {
	ctx           context.Context
	nonTTYScanner *bufio.Scanner
	WorkflowName  string
	Trigger       string
	Engine        string
	Tools         []string
	SafeOutputs   []string
	Intent        string
	NetworkAccess string
	CustomDomains []string
}

func (b *InteractiveWorkflowBuilder) ensureNonTTYScanner(r io.Reader) *bufio.Scanner {
	if b.nonTTYScanner == nil {
		b.nonTTYScanner = bufio.NewScanner(r)
	}
	return b.nonTTYScanner
}

// CreateWorkflowInteractively prompts the user to build a workflow interactively
func CreateWorkflowInteractively(ctx context.Context, workflowName string, verbose bool, force bool) error {
	interactiveLog.Printf("Starting interactive workflow creation: workflowName=%s, force=%v", workflowName, force)

	// Assert this function is not running in automated unit tests or CI.
	// GO_TEST_MODE intentionally uses GetBoolFromEnv so common boolean spellings
	// are treated consistently across test and automation environments, while
	// IsRunningInCI centralizes the broader CI environment detection logic.
	if envutil.GetBoolFromEnv("GO_TEST_MODE", false, interactiveLog) || IsRunningInCI() {
		return errors.New("interactive workflow creation cannot be used in automated tests or CI environments")
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Starting interactive workflow creation..."))
	}

	builder := &InteractiveWorkflowBuilder{
		ctx:          ctx,
		WorkflowName: workflowName,
	}

	// If using default workflow name, prompt for a better one
	if workflowName == "my-workflow" {
		if err := builder.promptForWorkflowName(); err != nil {
			return fmt.Errorf("failed to get workflow name: %w", err)
		}
	}

	// Run through the interactive prompts organized by groups
	if err := builder.promptForConfiguration(); err != nil {
		return fmt.Errorf("failed to get workflow configuration: %w", err)
	}

	// Generate the workflow
	if err := builder.generateWorkflow(force); err != nil {
		return fmt.Errorf("failed to generate workflow: %w", err)
	}

	// Compile the workflow
	if err := builder.compileWorkflow(ctx, verbose); err != nil {
		return fmt.Errorf("failed to compile workflow: %w", err)
	}

	return nil
}

// promptForWorkflowName asks the user for a workflow name
func (b *InteractiveWorkflowBuilder) promptForWorkflowName() error {
	if !tty.IsStderrTerminal() {
		return b.promptForWorkflowNameFrom(os.Stdin)
	}

	form := console.NewInputForm(
		huh.NewInput().
			Title("What should we call this workflow?").
			Description("Enter a descriptive name for your workflow (e.g., 'issue-triage', 'code-review-helper')").
			Suggestions(commonWorkflowNames).
			Value(&b.WorkflowName).
			Validate(ValidateWorkflowName),
	)

	return form.RunWithContext(b.ctx)
}

// promptForWorkflowNameFrom is the non-TTY fallback for promptForWorkflowName.
// It reads the workflow name from r as plain text. Separated from promptForWorkflowName
// so it can be exercised in unit tests without a real TTY.
func (b *InteractiveWorkflowBuilder) promptForWorkflowNameFrom(r io.Reader) error {
	interactiveLog.Print("Non-TTY detected, using text prompt for workflow name")
	fmt.Fprintf(os.Stderr, "\nWorkflow name (e.g. issue-triage, code-review-helper): ")

	scanner := b.ensureNonTTYScanner(r)
	if scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if err := ValidateWorkflowName(name); err != nil {
			return fmt.Errorf("invalid workflow name %q: %w", name, err)
		}
		b.WorkflowName = name
		return nil
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read workflow name: %w", err)
	}
	return errors.New("no workflow name provided")
}

// promptForConfiguration organizes all prompts into logical groups with titles and descriptions
func (b *InteractiveWorkflowBuilder) promptForConfiguration() error {
	if !tty.IsStderrTerminal() {
		return b.promptForConfigurationFrom(os.Stdin)
	}

	// Prepare trigger options
	triggerOptions := []huh.Option[string]{
		huh.NewOption("Manual trigger (workflow_dispatch)", "workflow_dispatch"),
		huh.NewOption("Issue opened or reopened", "issues"),
		huh.NewOption("Pull request opened or synchronized", "pull_request"),
		huh.NewOption("Push to main branch", "push"),
		huh.NewOption("Issue comment created", "issue_comment"),
		huh.NewOption("Schedule (daily, scattered execution time)", "schedule_daily"),
		huh.NewOption("Schedule (weekly on Monday, scattered execution time)", "schedule_weekly"),
		huh.NewOption("Command trigger (/bot-name)", "command"),
	}

	// Prepare engine options
	engineOptions := []huh.Option[string]{
		huh.NewOption("copilot - GitHub Copilot CLI", "copilot"),
		huh.NewOption("claude - Anthropic Claude Code coding agent", "claude"),
		huh.NewOption("codex - OpenAI Codex engine", "codex"),
		huh.NewOption("gemini - Google Gemini CLI", "gemini"),
	}

	// Prepare tool options
	toolOptions := []huh.Option[string]{
		huh.NewOption("github - GitHub API tools (issues, PRs, comments, repos)", "github"),
		huh.NewOption("edit - File editing tools", "edit"),
		huh.NewOption("bash - Shell command tools", "bash"),
		huh.NewOption("web-fetch - Web content fetching tools", "web-fetch"),
		huh.NewOption("web-search - Web search tools", "web-search"),
		huh.NewOption("playwright - Browser automation tools", "playwright"),
	}

	// Prepare safe output options programmatically from safe_outputs_tools.json
	outputOptions := buildSafeOutputOptions()

	// Pre-detect network access based on repo contents
	detectedNetworks := detectNetworkFromRepo()
	interactiveLog.Printf("Pre-detected networks from repo: %v", detectedNetworks)

	// Prepare network options
	networkOptions := []huh.Option[string]{
		huh.NewOption("defaults - Basic infrastructure only", "defaults"),
		huh.NewOption("ecosystem - Common development ecosystems (Python, Node.js, Go, etc.)", "ecosystem"),
	}
	if len(detectedNetworks) > 0 {
		// Build a custom option that reflects what was auto-detected
		label := "detected - Auto-detected ecosystems: " + strings.Join(detectedNetworks, ", ")
		networkOptions = append([]huh.Option[string]{huh.NewOption(label, strings.Join(append([]string{"defaults"}, detectedNetworks...), ","))}, networkOptions...)
	}

	// Set default network access
	b.NetworkAccess = "defaults"
	if len(detectedNetworks) > 0 {
		b.NetworkAccess = strings.Join(append([]string{"defaults"}, detectedNetworks...), ",")
	}

	// Variables to hold multi-select results
	var selectedTools []string
	var selectedOutputs []string

	// Create form with organized groups
	form := console.NewForm(
		// Group 1: Basic Configuration
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("When should this workflow run?").
				Description("Choose the GitHub event that triggers this workflow").
				Options(triggerOptions...).
				Height(8).
				Value(&b.Trigger),
			huh.NewSelect[string]().
				Title("Which AI engine should process this workflow?").
				Description("The AI engine interprets instructions and executes tasks using available tools").
				Options(engineOptions...).
				Value(&b.Engine),
		).
			Title("Basic Configuration").
			Description("Let's start with the fundamentals of your workflow"),

		// Group 2: Capabilities
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Which tools should the AI have access to?").
				Description("Tools enable the AI to interact with code, APIs, and external systems").
				Options(toolOptions...).
				Height(8).
				Value(&selectedTools),
			huh.NewMultiSelect[string]().
				Title("What outputs should the AI be able to create?").
				Description("Safe outputs allow the AI to create GitHub resources after human approval").
				Options(outputOptions...).
				Height(10).
				Value(&selectedOutputs),
		).
			Title("Capabilities").
			Description("Select the tools and outputs your workflow needs"),

		// Group 3: Network & Security
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("What network access does the workflow need?").
				Description("Network access controls which external domains the workflow can reach").
				Options(networkOptions...).
				Value(&b.NetworkAccess),
		).
			Title("Network & Security").
			Description("Configure network access and security settings"),

		// Group 4: Instructions
		huh.NewGroup(
			huh.NewText().
				Title("Describe what this workflow should do:").
				Description("Provide clear, detailed instructions for the AI to follow when executing this workflow").
				Value(&b.Intent).
				Validate(ValidateWorkflowIntent),
		).
			Title("Instructions").
			Description("Describe what you want this workflow to accomplish"),
	)

	if err := form.RunWithContext(b.ctx); err != nil {
		return err
	}

	// Store the multi-select results
	b.Tools = selectedTools
	b.SafeOutputs = selectedOutputs

	interactiveLog.Printf("User configuration selected: trigger=%s, engine=%s, tools=%v, safe_outputs=%v", b.Trigger, b.Engine, selectedTools, selectedOutputs)

	return nil
}

// promptForConfigurationFrom is the non-TTY fallback for promptForConfiguration.
// It prints numbered option lists for single-select fields and accepts comma-separated
// values for multi-select fields (tools and safe-outputs). Separated from
// promptForConfiguration so it can be exercised in unit tests without a real TTY.
func (b *InteractiveWorkflowBuilder) promptForConfigurationFrom(r io.Reader) error {
	interactiveLog.Print("Non-TTY detected, using text prompts for configuration")
	scanner := b.ensureNonTTYScanner(r)

	// --- Trigger (single-select) ---
	triggerOptions := []struct{ label, value string }{
		{"Manual trigger (workflow_dispatch)", "workflow_dispatch"},
		{"Issue opened or reopened", "issues"},
		{"Pull request opened or synchronized", "pull_request"},
		{"Push to main branch", "push"},
		{"Issue comment created", "issue_comment"},
		{"Schedule (daily, scattered execution time)", "schedule_daily"},
		{"Schedule (weekly on Monday, scattered execution time)", "schedule_weekly"},
		{"Command trigger (/bot-name)", "command"},
	}
	trigger, err := promptNonInteractiveSelect(scanner, "When should this workflow run?", triggerOptions)
	if err != nil {
		return fmt.Errorf("failed to select trigger: %w", err)
	}
	b.Trigger = trigger

	// --- Engine (single-select) ---
	engineOptions := []struct{ label, value string }{
		{"copilot - GitHub Copilot CLI", "copilot"},
		{"claude - Anthropic Claude Code coding agent", "claude"},
		{"codex - OpenAI Codex engine", "codex"},
		{"gemini - Google Gemini CLI", "gemini"},
	}
	engine, err := promptNonInteractiveSelect(scanner, "Which AI engine should process this workflow?", engineOptions)
	if err != nil {
		return fmt.Errorf("failed to select engine: %w", err)
	}
	b.Engine = engine

	// --- Tools (multi-select) ---
	toolOptions := []struct{ label, value string }{
		{"github - GitHub API tools (issues, PRs, comments, repos)", "github"},
		{"edit - File editing tools", "edit"},
		{"bash - Shell command tools", "bash"},
		{"web-fetch - Web content fetching tools", "web-fetch"},
		{"web-search - Web search tools", "web-search"},
		{"playwright - Browser automation tools", "playwright"},
	}
	tools, err := promptNonInteractiveMultiSelect(scanner, "Which tools should the AI have access to? (comma-separated values or numbers, or leave blank for none)", toolOptions)
	if err != nil {
		return fmt.Errorf("failed to select tools: %w", err)
	}
	b.Tools = tools

	// --- Safe outputs (multi-select) ---
	outputOptions := buildSafeOutputOptions()
	safeOutputItems := make([]struct{ label, value string }, len(outputOptions))
	for i, opt := range outputOptions {
		safeOutputItems[i] = struct{ label, value string }{opt.Key, opt.Value}
	}
	safeOutputs, err := promptNonInteractiveMultiSelect(scanner, "What outputs should the AI be able to create? (comma-separated values or numbers, or leave blank for none)", safeOutputItems)
	if err != nil {
		return fmt.Errorf("failed to select safe outputs: %w", err)
	}
	b.SafeOutputs = safeOutputs

	// --- Network access (single-select) ---
	detectedNetworks := detectNetworkFromRepo()
	networkItems := []struct{ label, value string }{
		{"defaults - Basic infrastructure only", "defaults"},
		{"ecosystem - Common development ecosystems (Python, Node.js, Go, etc.)", "ecosystem"},
	}
	if len(detectedNetworks) > 0 {
		label := "detected - Auto-detected ecosystems: " + strings.Join(detectedNetworks, ", ")
		value := strings.Join(append([]string{"defaults"}, detectedNetworks...), ",")
		networkItems = append([]struct{ label, value string }{{label, value}}, networkItems...)
	}
	network, err := promptNonInteractiveSelect(scanner, "What network access does the workflow need?", networkItems)
	if err != nil {
		return fmt.Errorf("failed to select network access: %w", err)
	}
	b.NetworkAccess = network

	// --- Intent / instructions (free text) ---
	fmt.Fprintf(os.Stderr, "\nDescribe what this workflow should do (enter text, then press Enter):\n> ")
	if scanner.Scan() {
		b.Intent = strings.TrimSpace(scanner.Text())
	} else if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read workflow intent: %w", err)
	}
	if err := ValidateWorkflowIntent(b.Intent); err != nil {
		return fmt.Errorf("invalid workflow intent: %w", err)
	}

	interactiveLog.Printf("Non-TTY configuration selected: trigger=%s, engine=%s, tools=%v, safe_outputs=%v", b.Trigger, b.Engine, b.Tools, b.SafeOutputs)
	return nil
}

// promptNonInteractiveSelect prints a numbered list and reads a single selection.
// The user may enter a number (1-based index) or the option value directly.
func promptNonInteractiveSelect(scanner *bufio.Scanner, title string, options []struct{ label, value string }) (string, error) {
	fmt.Fprintf(os.Stderr, "\n%s\n", title)
	for i, opt := range options {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, opt.label)
	}
	fmt.Fprintf(os.Stderr, "Select (1-%d): ", len(options))

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("failed to read input: %w", err)
		}
		return "", errors.New("no input provided")
	}
	input := strings.TrimSpace(scanner.Text())

	// Accept a numeric index
	if idx, err := strconv.Atoi(input); err == nil {
		if idx < 1 || idx > len(options) {
			return "", fmt.Errorf("selection out of range (must be 1-%d)", len(options))
		}
		return options[idx-1].value, nil
	}

	// Accept the value directly
	for _, opt := range options {
		if opt.value == input {
			return opt.value, nil
		}
	}
	return "", fmt.Errorf("invalid selection %q", input)
}

// promptNonInteractiveMultiSelect prints a numbered list and reads comma-separated selections.
// Each token may be a 1-based index or an option value. An empty input selects nothing.
func promptNonInteractiveMultiSelect(scanner *bufio.Scanner, title string, options []struct{ label, value string }) ([]string, error) {
	fmt.Fprintf(os.Stderr, "\n%s\n", title)
	for i, opt := range options {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, opt.label)
	}
	fmt.Fprintf(os.Stderr, "Enter comma-separated numbers or values (leave blank for none): ")

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("failed to read input: %w", err)
		}
		// EOF / empty → no selections
		return nil, nil
	}
	input := strings.TrimSpace(scanner.Text())
	if input == "" {
		return nil, nil
	}

	// Build a lookup map for value-based selection
	valueSet := make(map[string]string, len(options))
	for _, opt := range options {
		valueSet[opt.value] = opt.value
	}

	tokens := strings.Split(input, ",")
	seen := make(map[string]struct{}, len(tokens))
	var selected []string
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}

		// Try numeric index
		if idx, err := strconv.Atoi(tok); err == nil {
			if idx < 1 || idx > len(options) {
				return nil, fmt.Errorf("selection %d out of range (must be 1-%d)", idx, len(options))
			}
			val := options[idx-1].value
			if _, dup := seen[val]; !dup {
				seen[val] = struct{}{}
				selected = append(selected, val)
			}
			continue
		}

		// Try value directly
		if val, ok := valueSet[tok]; ok {
			if _, dup := seen[val]; !dup {
				seen[val] = struct{}{}
				selected = append(selected, val)
			}
			continue
		}

		return nil, fmt.Errorf("unknown option %q", tok)
	}
	return selected, nil
}

// generateWorkflow creates the markdown workflow file based on user selections
func (b *InteractiveWorkflowBuilder) generateWorkflow(force bool) error {
	interactiveLog.Printf("Generating workflow file: name=%s, engine=%s, trigger=%s", b.WorkflowName, b.Engine, b.Trigger)

	// Get current working directory for .github/workflows
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Create .github/workflows directory if it doesn't exist
	githubWorkflowsDir := filepath.Join(workingDir, constants.GetWorkflowDir())
	if err := os.MkdirAll(githubWorkflowsDir, constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create .github/workflows directory: %w", err)
	}

	// Construct the destination file path
	destFile := filepath.Join(githubWorkflowsDir, b.WorkflowName+".md")

	// Check if destination file already exists
	if _, err := os.Stat(destFile); err == nil && !force {
		var overwrite bool
		confirmForm := console.NewConfirmForm(
			huh.NewConfirm().
				Title(fmt.Sprintf("Workflow file '%s' already exists. Overwrite?", filepath.Base(destFile))).
				Affirmative("Yes, overwrite").
				Negative("No, cancel").
				Value(&overwrite),
		)

		if err := confirmForm.Run(); err != nil {
			return fmt.Errorf("confirmation failed: %w", err)
		}

		if !overwrite {
			return errors.New("workflow creation cancelled")
		}
	}

	// Generate workflow content
	content := b.generateWorkflowContent()

	// Write the workflow to file with owner-only read/write permissions (0600) for security best practices
	if err := os.WriteFile(destFile, []byte(content), constants.FilePermSensitive); err != nil {
		return fmt.Errorf("failed to write workflow file '%s': %w", destFile, err)
	}

	interactiveLog.Printf("Workflow file created successfully: %s", destFile)
	fmt.Fprintf(os.Stderr, "Created new workflow: %s\n", destFile)
	return nil
}

// generateWorkflowContent creates the workflow markdown content
func (b *InteractiveWorkflowBuilder) generateWorkflowContent() string {
	interactiveLog.Printf("Generating workflow content: trigger=%s, engine=%s, tools=%v, safe_outputs=%v", b.Trigger, b.Engine, b.Tools, b.SafeOutputs)
	var content strings.Builder

	// Write frontmatter
	content.WriteString("---\n")

	// Add trigger configuration
	content.WriteString(b.generateTriggerConfig())

	// Add permissions
	content.WriteString(b.generatePermissionsConfig())

	// Add engine configuration
	fmt.Fprintf(&content, "engine: %s\n", b.Engine)

	// Add network configuration
	content.WriteString(b.generateNetworkConfig())

	// Add tools configuration
	if len(b.Tools) > 0 {
		content.WriteString(b.generateToolsConfig())
	}

	// Add safe outputs configuration
	if len(b.SafeOutputs) > 0 {
		content.WriteString(b.generateSafeOutputsConfig())
	}

	content.WriteString("---\n\n")

	// Add workflow title and content
	fmt.Fprintf(&content, "# %s\n\n", b.WorkflowName)

	if b.Intent != "" {
		fmt.Fprintf(&content, "%s\n\n", b.Intent)
	}

	// Add TODO sections for customization
	content.WriteString("<!--\n")
	content.WriteString("## TODO: Customize this workflow\n\n")
	content.WriteString("The workflow has been generated based on your selections. Consider adding:\n\n")
	content.WriteString("- [ ] More specific instructions for the AI\n")
	content.WriteString("- [ ] Error handling requirements\n")
	content.WriteString("- [ ] Output format specifications\n")
	content.WriteString("- [ ] Integration with other workflows\n")
	content.WriteString("- [ ] Testing and validation steps\n\n")

	content.WriteString("## Configuration Summary\n\n")
	fmt.Fprintf(&content, "- **Trigger**: %s\n", b.describeTrigger())
	fmt.Fprintf(&content, "- **AI Engine**: %s\n", b.Engine)

	if len(b.Tools) > 0 {
		fmt.Fprintf(&content, "- **Tools**: %s\n", strings.Join(b.Tools, ", "))
	}

	if len(b.SafeOutputs) > 0 {
		fmt.Fprintf(&content, "- **Safe Outputs**: %s\n", strings.Join(b.SafeOutputs, ", "))
	}

	fmt.Fprintf(&content, "- **Network Access**: %s\n", b.NetworkAccess)

	content.WriteString("\n## Next Steps\n\n")
	content.WriteString("1. Review and customize the workflow content above\n")
	content.WriteString("2. Remove TODO sections when ready\n")
	fmt.Fprintf(&content, "3. Run `%s compile` to generate the GitHub Actions workflow\n", string(constants.CLIExtensionPrefix))
	content.WriteString("4. Test the workflow with a manual trigger or appropriate event\n")
	content.WriteString("-->\n")

	return content.String()
}

// Helper methods for generating configuration sections

func (b *InteractiveWorkflowBuilder) generateTriggerConfig() string {
	interactiveLog.Printf("Generating trigger config: trigger=%s", b.Trigger)
	switch b.Trigger {
	case "workflow_dispatch":
		return "on:\n  workflow_dispatch:\n"
	case "issues":
		return "on:\n  issues:\n    types: [opened, reopened]\n"
	case "pull_request":
		return "on:\n  pull_request:\n    types: [opened, synchronize]\n"
	case "push":
		return "on:\n  push:\n    branches: [main]\n"
	case "issue_comment":
		return "on:\n  issue_comment:\n    types: [created]\n"
	case "schedule_daily":
		return "on:\n  schedule: daily\n"
	case "schedule_weekly":
		return "on:\n  schedule: weekly on monday\n"
	case "command":
		return "on:\n  command:\n    name: bot-name  # TODO: Replace with your bot name\n"
	default:
		return "on:\n  workflow_dispatch:\n"
	}
}

func (b *InteractiveWorkflowBuilder) generatePermissionsConfig() string {
	// Compute read permissions needed by the AI agent for data access.
	// Write permissions are NEVER set here — they are always handled automatically
	// by the safe-outputs job via workflow.ComputePermissionsForSafeOutputs().
	perms := workflow.NewPermissions()
	perms.Set(workflow.PermissionContents, workflow.PermissionRead)

	if slices.Contains(b.Tools, "github") {
		// Default toolsets: context, repos, issues, pull_requests
		// repos → contents: read (already set)
		// issues → issues: read
		// pull_requests → pull-requests: read
		perms.Set(workflow.PermissionIssues, workflow.PermissionRead)
		perms.Set(workflow.PermissionPullRequests, workflow.PermissionRead)
	}

	// Include read permissions needed by the safe-outputs job (e.g. contents: read
	// is already present; actions: read for autofix scanning alerts).
	// Write permissions from ComputePermissionsForSafeOutputs are handled by the
	// safe-outputs job automatically and must not appear in the main workflow block.
	safeOutputsPerms := workflow.ComputePermissionsForSafeOutputs(workflow.SafeOutputsConfigFromKeys(b.SafeOutputs))
	for _, scope := range workflow.GetAllPermissionScopes() {
		if level, exists := safeOutputsPerms.Get(scope); exists && level == workflow.PermissionRead {
			perms.Set(scope, workflow.PermissionRead)
		}
	}

	return perms.RenderToYAML() + "\n"
}

func (b *InteractiveWorkflowBuilder) generateNetworkConfig() string {
	interactiveLog.Printf("Generating network config: network=%s", b.NetworkAccess)
	switch b.NetworkAccess {
	case "ecosystem":
		return "network:\n  allowed:\n    - defaults\n    - python\n    - node\n    - go\n    - java\n"
	default:
		// Handle comma-separated networks (e.g. "defaults,node,python")
		parts := strings.Split(b.NetworkAccess, ",")
		if len(parts) == 1 {
			return fmt.Sprintf("network: %s\n", parts[0])
		}
		var cfg strings.Builder
		cfg.WriteString("network:\n  allowed:\n")
		for _, p := range parts {
			fmt.Fprintf(&cfg, "    - %s\n", strings.TrimSpace(p))
		}
		return cfg.String()
	}
}

func (b *InteractiveWorkflowBuilder) generateToolsConfig() string {
	if len(b.Tools) == 0 {
		return ""
	}

	var config strings.Builder
	config.WriteString("tools:\n")

	// Add standard tools
	for _, tool := range b.Tools {
		switch tool {
		case "github":
			// Use default toolsets (context, repos, issues, pull_requests)
			// which matches the DefaultGitHubToolsets constant.
			config.WriteString("  github:\n    toolsets: [default]\n")
		case "bash":
			config.WriteString("  bash:\n")
		default:
			fmt.Fprintf(&config, "  %s:\n", tool)
		}
	}

	return config.String()
}

func (b *InteractiveWorkflowBuilder) generateSafeOutputsConfig() string {
	if len(b.SafeOutputs) == 0 {
		return ""
	}

	var config strings.Builder
	config.WriteString("safe-outputs:\n")

	for _, output := range b.SafeOutputs {
		fmt.Fprintf(&config, "  %s:\n", output)
	}

	return config.String()
}

func (b *InteractiveWorkflowBuilder) describeTrigger() string {
	switch b.Trigger {
	case "workflow_dispatch":
		return "Manual trigger"
	case "issues":
		return "Issue opened or reopened"
	case "pull_request":
		return "Pull request opened or synchronized"
	case "push":
		return "Push to main branch"
	case "issue_comment":
		return "Issue comment created"
	case "schedule_daily":
		return "Daily schedule (fuzzy, scattered time)"
	case "schedule_weekly":
		return "Weekly schedule (Monday, fuzzy scattered time)"
	case "command":
		return "Command trigger (/bot-name)"
	case "custom":
		return "Custom trigger (TODO: configure)"
	default:
		return "Unknown trigger"
	}
}

// compileWorkflow automatically compiles the generated workflow
func (b *InteractiveWorkflowBuilder) compileWorkflow(ctx context.Context, verbose bool) error {
	interactiveLog.Printf("Starting workflow compilation: name=%s, verbose=%v", b.WorkflowName, verbose)

	// Create spinner for compilation progress
	spinner := console.NewSpinner("Compiling your workflow...")
	spinner.Start()

	// Use the existing compile functionality
	config := CompileConfig{
		MarkdownFiles:        []string{b.WorkflowName},
		Verbose:              verbose,
		EngineOverride:       "",
		Validate:             true,
		Watch:                false,
		WorkflowDir:          "",
		NoEmit:               false,
		Purge:                false,
		TrialMode:            false,
		TrialLogicalRepoSlug: "",
	}

	_, err := CompileWorkflows(ctx, config)

	if err != nil {
		spinner.Stop()
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(fmt.Sprintf("Compilation failed: %v", err)))
		return err
	}

	// Stop spinner with success message
	spinner.StopWithMessage("✓ Workflow compiled successfully!")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("You can now find your compiled workflow at .github/workflows/%s.lock.yml", b.WorkflowName)))

	return nil
}

// buildSafeOutputOptions loads safe output tool options from safe_outputs_tools.json
// and returns them as huh form options.
func buildSafeOutputOptions() []huh.Option[string] {
	tools := workflow.GetSafeOutputToolOptions()

	options := make([]huh.Option[string], 0, len(tools))
	for _, t := range tools {
		// Truncate long descriptions so option labels remain readable
		desc := t.Description
		if idx := strings.IndexByte(desc, '.'); idx > 0 && idx < 120 {
			desc = desc[:idx]
		} else if len(desc) > 120 {
			desc = desc[:120] + "…"
		}
		label := fmt.Sprintf("%s - %s", t.Key, desc)
		options = append(options, huh.NewOption(label, t.Key))
	}
	return options
}

// repoLanguageMarkers maps well-known ecosystem indicator files to their network bucket name.
var repoLanguageMarkers = []struct {
	file   string
	bucket string
}{
	{"go.mod", "go"},
	{"package.json", "node"},
	{"requirements.txt", "python"},
	{"pyproject.toml", "python"},
	{"Pipfile", "python"},
	{"Gemfile", "ruby"},
	{"Cargo.toml", "rust"},
	{"pom.xml", "java"},
	{"build.gradle", "java"},
	{"*.csproj", "dotnet"},
	{"*.fsproj", "dotnet"},
}

// detectNetworkFromRepo scans the current working directory for ecosystem indicator
// files and returns a deduplicated, sorted list of network bucket names to add.
func detectNetworkFromRepo() []string {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}

	seen := map[string]struct {
	}{}
	for _, m := range repoLanguageMarkers {
		var found bool
		if strings.ContainsAny(m.file, "*?[") {
			// Glob pattern
			matches, err := filepath.Glob(filepath.Join(cwd, m.file))
			found = err == nil && len(matches) > 0
		} else {
			_, err := os.Stat(filepath.Join(cwd, m.file))
			found = err == nil
		}
		if found && !setutil.Contains(seen, m.bucket) {
			seen[m.bucket] = struct {
			}{}
		}
	}

	buckets := sliceutil.SortedKeys(seen)
	return buckets
}
