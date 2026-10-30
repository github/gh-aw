package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	workflowlint "github.com/github/gh-aw/pkg/workflow/lint"
	"github.com/spf13/cobra"
)

var lintCommandLog = logger.New("cli:lint_command")

var defaultGhAwActionlintIgnorePatterns = []string{
	// gh-aw extends GitHub Actions permissions with copilot-requests.
	`unknown permission scope "copilot-requests"`,
	// GitHub is rolling out an additional permissions scope before actionlint support.
	`unknown permission scope "vulnerability-alerts"`,
	// gh-aw's upload-code-coverage safe output requires code-quality before actionlint support.
	`unknown permission scope "code-quality"`,
	// GitHub is rolling out the drives permission scope before actionlint support.
	`unknown permission scope "drives"`,
	// gh-aw exposes additional job.workflow_* context properties.
	`property "workflow_(repository|sha|ref|file_path)" is not defined in object type`,
	// GitHub is rolling out queue under concurrency before actionlint support.
	`unexpected key "queue" for "concurrency" section`,
	// Generated workflows use GitHub's workflow/job cache policies before actionlint support.
	`^unexpected key "cache-mode" for "workflow" section(\.|$)`,
	`^unexpected key "cache-mode" for "job" section(\.|$)`,
	// gh-aw injects additional activation context properties in generated workflows.
	`property "(activation|activated)" is not defined in object type`,
	// gh-aw injects additional artifact context properties in generated workflows.
	`property "artifact_prefix" is not defined in object type`,
}

// NewLintCommand creates the lint command.
func NewLintCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lint [workflow-file-or-directory]...",
		Short: "Lint AW Markdown or Actions YAML workflow ASTs",
		Long: `Lint .md, .yml, and .yaml workflows using built-in Actions/AW AST rules.

Rules consume parsed ASTs only. This command never recompiles workflows.
AW Markdown is checked as authored; imports and generated execution require an Actions AST.
Directories prefer an existing .lock.yml over its paired .md and skip plain Markdown without frontmatter.
Actionlint runs only on YAML files. This command does not run zizmor or poutine.
By default, shellcheck and pyflakes integrations are disabled for generated run scripts.
All registered AST rules run by default. Use --rules to select AST rules and skip external actionlint.`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` lint                                  # Lint workflows in the default directory
  ` + string(constants.CLIExtensionPrefix) + ` lint .github/workflows/foo.lock.yml    # Lint a specific lock file
  ` + string(constants.CLIExtensionPrefix) + ` lint .github/workflows/foo.md           # Lint the authored AW AST without compiling
  ` + string(constants.CLIExtensionPrefix) + ` lint .github/workflows/ci.yml           # Lint a native Actions AST
  ` + string(constants.CLIExtensionPrefix) + ` lint --dir custom-workflows/            # Lint workflows in a custom directory
  ` + string(constants.CLIExtensionPrefix) + ` lint --shellcheck --pyflakes            # Enable actionlint script integrations
  ` + string(constants.CLIExtensionPrefix) + ` lint --rules secret-leak --json         # Run the built-in taint checker
  ` + string(constants.CLIExtensionPrefix) + ` lint --list-rules                       # List built-in AST rules`,
		RunE: runLintCommand,
	}

	cmd.Flags().StringP("dir", "d", "", "Workflow directory (default: $GH_AW_WORKFLOWS_DIR or .github/workflows)")
	cmd.Flags().Bool("shellcheck", false, "Enable shellcheck integration in actionlint")
	cmd.Flags().Bool("pyflakes", false, "Enable pyflakes integration in actionlint")
	cmd.Flags().StringSlice("rules", nil, "Select built-in AST rules and skip actionlint (default: all AST rules plus actionlint; empty selects all)")
	cmd.Flags().Bool("list-rules", false, "List available built-in AST lint rules")
	cmd.Flags().Bool("json", false, "Emit structured diagnostics (requires --rules)")
	cmd.Flags().Bool("debug", false, "Include secret-leak analysis coverage warnings for investigation")
	registerJavaScriptLintFlags(cmd)
	// The root routes help to stderr. Structured lint reports belong on stdout.
	cmd.SetOut(os.Stdout)

	RegisterDirFlagCompletion(cmd, "dir")

	return cmd
}

func selectWorkflowLintRules(cmd *cobra.Command) (*workflowlint.Runner, error) {
	runner := workflowlint.Default()
	listRules, _ := cmd.Flags().GetBool("list-rules")
	if listRules {
		for _, rule := range runner.Rules() {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n", rule.ID, rule.Severity, rule.Targets, rule.Description, rule.ThreatRuleID); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	ruleIDs, _ := cmd.Flags().GetStringSlice("rules")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput && !cmd.Flags().Changed("rules") {
		return nil, errors.New("--json requires --rules for built-in AST linting")
	}
	return runner.Select(ruleIDs...)
}

func runLintCommand(cmd *cobra.Command, args []string) (resultErr error) {
	selected, err := selectWorkflowLintRules(cmd)
	if err != nil || selected == nil {
		return err
	}
	workflowDir, _ := cmd.Flags().GetString("dir")
	includeShellcheck, _ := cmd.Flags().GetBool("shellcheck")
	includePyflakes, _ := cmd.Flags().GetBool("pyflakes")
	if cmd.Flags().Changed("rules") && (includeShellcheck || includePyflakes) {
		return errors.New("--shellcheck and --pyflakes require actionlint; omit --rules to use them")
	}
	verbose, _ := cmd.Flags().GetBool("verbose")
	lintCommandLog.Printf("Executing lint: dir=%s, shellcheck=%v, pyflakes=%v, args=%d", workflowDir, includeShellcheck, includePyflakes, len(args))
	lockFiles, err := resolveLockFilesForLint(args, workflowDir)
	if err != nil {
		return err
	}
	lintCommandLog.Printf("Resolved %d workflow file(s) for linting", len(lockFiles))
	jsonOutput, _ := cmd.Flags().GetBool("json")
	debug, _ := cmd.Flags().GetBool("debug")
	javascript, closeJavaScript, err := javascriptLintOptions(cmd)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closeJavaScript()) }()
	workflowLintErr := runWorkflowLint(cmd.Context(), lockFiles, selected, cmd.OutOrStdout(), jsonOutput, workflowLintOptions{
		JavaScript: javascript, Debug: debug,
	})
	if cmd.Flags().Changed("rules") {
		return workflowLintErr
	}
	actionFiles := slices.DeleteFunc(slices.Clone(lockFiles), func(file string) bool {
		return strings.HasSuffix(file, ".md")
	})
	if len(actionFiles) == 0 {
		return workflowLintErr
	}
	initActionlintStats()
	defer displayActionlintSummary()
	actionlintErr := runActionlintOnFilesWithOptions(cmd.Context(), actionFiles, verbose, true, actionlintRunOptions{
		IncludeShellcheck: includeShellcheck,
		IncludePyflakes:   includePyflakes,
		IgnorePatterns:    defaultGhAwActionlintIgnorePatterns,
	})
	return errors.Join(workflowLintErr, actionlintErr)
}

func resolveLockFilesForLint(inputs []string, workflowDir string) ([]string, error) {
	if workflowDir == "" {
		workflowDir = constants.GetWorkflowDir()
	}
	candidates := inputs
	if len(candidates) == 0 {
		candidates = []string{workflowDir}
	}

	var lockFiles []string
	for _, candidate := range candidates {
		lockFilesFromCandidate, err := expandLintCandidate(candidate)
		if err != nil {
			return nil, err
		}
		lockFiles = append(lockFiles, lockFilesFromCandidate...)
	}

	if len(lockFiles) == 0 {
		return nil, errors.New("no .md, .yml, or .yaml workflow files found to lint")
	}

	slices.Sort(lockFiles)
	lockFiles = slices.Compact(lockFiles)

	return lockFiles, nil
}

func expandLintCandidate(candidate string) ([]string, error) {
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path %q: %w", candidate, err)
	}

	info, err := os.Stat(absCandidate)
	if err != nil {
		return nil, fmt.Errorf("failed to access %q: %w", candidate, err)
	}

	if info.IsDir() {
		return expandLintDirectory(absCandidate)
	}

	switch filepath.Ext(absCandidate) {
	case ".md", ".yml", ".yaml":
		return []string{absCandidate}, nil
	default:
		return nil, fmt.Errorf("path %q is not a .md, .yml, or .yaml workflow file or directory", candidate)
	}
}
