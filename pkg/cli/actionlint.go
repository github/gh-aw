package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
)

var actionlintLog = logger.New("cli:actionlint")

// actionlintVersion caches the actionlint version to avoid repeated Docker calls
var actionlintVersion string

const actionlintShellMetacharacters = " \t\n'\"`$&;|*?[](){}<>!#"

// actionlintRunOptions configures optional actionlint integrations and ignores.
type actionlintRunOptions struct {
	IncludeShellcheck bool
	IncludePyflakes   bool
	// IgnorePatterns contains regular expressions passed to actionlint via
	// repeated -ignore flags to suppress known false positives.
	IgnorePatterns []string
}

// buildActionlintIntegrationStatus returns a human-readable description of the
// shellcheck/pyflakes integration state for actionlint execution messages.
func buildActionlintIntegrationStatus(includeShellcheck bool, includePyflakes bool) string {
	switch {
	case includeShellcheck && includePyflakes:
		return "with shellcheck/pyflakes"
	case includeShellcheck:
		return "with shellcheck only"
	case includePyflakes:
		return "with pyflakes only"
	default:
		return "without shellcheck/pyflakes"
	}
}

// getActionlintDocsURL returns the documentation URL for a given actionlint error kind
// Error kinds map to documentation anchors at https://github.com/rhysd/actionlint/blob/main/docs/checks.md
func getActionlintDocsURL(kind string) string {
	if kind == "" {
		return "https://github.com/rhysd/actionlint/blob/main/docs/checks.md"
	}

	// Map error kind to documentation anchor
	// Most kinds follow the pattern "check-{kind}" as the anchor
	anchor := kind

	// Special case mappings for kinds that don't follow the standard pattern
	switch kind {
	case "runner-label":
		anchor = "check-runner-labels"
	case "pyflakes":
		anchor = "check-pyflakes-integ"
	case "shellcheck":
		anchor = "check-shellcheck-integ"
	case "expression":
		anchor = "check-syntax-expression"
	case "syntax-check":
		anchor = "check-unexpected-keys"
	default:
		// For other kinds, try the standard "check-{kind}" pattern
		if !strings.HasPrefix(anchor, "check-") {
			anchor = "check-" + anchor
		}
	}

	return "https://github.com/rhysd/actionlint/blob/main/docs/checks.md#" + anchor
}

// actionlintStats tracks aggregate statistics across all actionlint validations
var actionlintStats *ActionlintStats

// ActionlintStats tracks actionlint validation statistics across all files
type ActionlintStats struct {
	TotalWorkflows    int
	TotalErrors       int
	TotalWarnings     int
	IntegrationErrors int // counts tooling/subprocess failures, not lint findings
	ErrorsByKind      map[string]int
}

// actionlintError represents a single error from actionlint JSON output
type actionlintError struct {
	Message   string `json:"message"`
	Filepath  string `json:"filepath"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	Kind      string `json:"kind"`
	Snippet   string `json:"snippet"`
	EndColumn int    `json:"end_column"`
}

// initActionlintStats initializes the global actionlint statistics tracker
func initActionlintStats() {
	actionlintStats = &ActionlintStats{
		ErrorsByKind: make(map[string]int),
	}
}

// displayActionlintSummary displays aggregate statistics for all actionlint validations
func displayActionlintSummary() {
	if actionlintStats == nil || actionlintStats.TotalWorkflows == 0 {
		return
	}

	// Create visual separator
	separator := strings.Repeat("━", 60)

	fmt.Fprintf(os.Stderr, "\n%s\n", separator)
	fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Actionlint Summary"))
	fmt.Fprintf(os.Stderr, "%s\n\n", separator)

	// Show total workflows checked
	fmt.Fprintf(os.Stderr, "%s\n",
		console.FormatSuccessMessage(fmt.Sprintf("Checked %d workflow(s)", actionlintStats.TotalWorkflows)))

	// Show total issues found
	totalIssues := actionlintStats.TotalErrors + actionlintStats.TotalWarnings
	if totalIssues > 0 {
		issueText := fmt.Sprintf("Found %d issue(s)", totalIssues)
		if actionlintStats.TotalErrors > 0 && actionlintStats.TotalWarnings > 0 {
			issueText += fmt.Sprintf(" (%d error(s), %d warning(s))", actionlintStats.TotalErrors, actionlintStats.TotalWarnings)
		} else if actionlintStats.TotalErrors > 0 {
			issueText += fmt.Sprintf(" (%d error(s))", actionlintStats.TotalErrors)
		} else if actionlintStats.TotalWarnings > 0 {
			issueText += fmt.Sprintf(" (%d warning(s))", actionlintStats.TotalWarnings)
		}
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatWarningMessage(issueText))

		// Break down by error kind if we have multiple kinds
		if len(actionlintStats.ErrorsByKind) > 0 {
			fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatInfoMessage("Issues by type:"))
			for kind, count := range actionlintStats.ErrorsByKind {
				fmt.Fprintf(os.Stderr, "  • %s: %d\n", kind, count)
			}
		}
	} else if actionlintStats.IntegrationErrors > 0 {
		// Integration failures occurred but no lint issues were parsed.
		// Explicitly distinguish this from a clean run so users are not misled.
		msg := fmt.Sprintf("No lint issues found, but %d actionlint invocation(s) failed. "+
			"This likely indicates a tooling or integration error, not a workflow problem.",
			actionlintStats.IntegrationErrors)
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatWarningMessage(msg))
	} else {
		fmt.Fprintf(os.Stderr, "%s\n",
			console.FormatSuccessMessage("No issues found"))
	}

	// Report any integration failures alongside lint findings
	if totalIssues > 0 && actionlintStats.IntegrationErrors > 0 {
		msg := fmt.Sprintf("%d actionlint invocation(s) also failed with tooling errors (not workflow validation failures)",
			actionlintStats.IntegrationErrors)
		fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatWarningMessage(msg))
	}

	fmt.Fprintf(os.Stderr, "\n%s\n", separator)
}

// getActionlintVersion fetches and caches the actionlint version from Docker.
// The provided context allows caller-driven cancellation.
func getActionlintVersion(ctx context.Context) (string, error) {
	// Return cached version if already fetched
	if actionlintVersion != "" {
		return actionlintVersion, nil
	}

	actionlintLog.Print("Fetching actionlint version from Docker")

	// Run docker command to get version with a 30 second timeout
	versionCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(
		versionCtx,
		"docker",
		"run",
		"--rm",
		ActionlintImage,
		"--version",
	)

	output, err := cmd.Output()
	if err != nil {
		actionlintLog.Printf("Failed to get actionlint version: %v", err)
		return "", fmt.Errorf("failed to get actionlint version: %w", err)
	}

	// Parse version from output (format: "1.7.9\ninstalled by...\nbuilt with...")
	// We only want the first line which contains the version number
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 {
		return "", errors.New("no version output from actionlint")
	}
	version := strings.TrimSpace(lines[0])
	actionlintVersion = version
	actionlintLog.Printf("Cached actionlint version: %s", version)

	return version, nil
}

// runActionlintOnFiles runs the actionlint linter on one or more .lock.yml files using Docker.
// The provided context allows caller-driven cancellation.
func runActionlintOnFiles(ctx context.Context, lockFiles []string, verbose bool, strict bool) error {
	return runActionlintOnFilesWithOptions(ctx, lockFiles, verbose, strict, actionlintRunOptions{
		IncludeShellcheck: true,
		IncludePyflakes:   true,
		IgnorePatterns:    defaultGhAwActionlintIgnorePatterns,
	})
}

type actionlintCommandResult struct {
	stdout          string
	stderr          string
	err             error
	timeoutDuration time.Duration
	ctxErr          error
}

func runActionlintOnFilesWithOptions(ctx context.Context, lockFiles []string, verbose bool, strict bool, options actionlintRunOptions) error {
	if len(lockFiles) == 0 {
		return nil
	}
	actionlintLog.Printf("Running actionlint on %d file(s): %v (verbose=%t, strict=%t)", len(lockFiles), lockFiles, verbose, strict)
	fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage(fmt.Sprintf("Running actionlint on %d file(s)", len(lockFiles))))
	maybePrintActionlintVersion(ctx)

	gitRoot, relPaths, err := resolveActionlintPaths(lockFiles)
	if err != nil {
		return err
	}

	runResult := runActionlintCommand(ctx, gitRoot, lockFiles, relPaths, verbose, options)
	if err := actionlintContextError(runResult, lockFiles); err != nil {
		return err
	}

	// Parse and reformat the output only when actionlint completed validation and
	// returned either success or lint findings.
	totalErrors := 0
	var errorsByKind map[string]int
	var parseErr error
	shouldParseOutput := actionlintShouldParseOutput(runResult.err)
	if shouldParseOutput {
		totalErrors, errorsByKind, parseErr = parseAndDisplayActionlintOutput(runResult.stdout, verbose)
		if parseErr != nil {
			actionlintLog.Printf("Failed to parse actionlint output: %v", parseErr)
			// Track this as an integration error: output was produced but could not be parsed.
			if actionlintStats != nil {
				actionlintStats.IntegrationErrors++
			}
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
				"actionlint output could not be parsed — this is a tooling error, not a workflow validation failure: "+parseErr.Error()))
			// Fall back to showing raw output.
			if runResult.stdout != "" {
				fmt.Fprint(os.Stderr, runResult.stdout)
			}
			if runResult.stderr != "" {
				fmt.Fprint(os.Stderr, runResult.stderr)
			}
		} else if actionlintStats != nil {
			// Track error statistics.
			actionlintStats.TotalErrors += totalErrors
			for kind, count := range errorsByKind {
				actionlintStats.ErrorsByKind[kind] += count
			}
		}
	}

	if shouldParseOutput && actionlintStats != nil {
		actionlintStats.TotalWorkflows += len(lockFiles)
	}
	return handleActionlintExecutionError(runResult.err, strict, lockFiles, totalErrors, parseErr, runResult.stdout)
}

func maybePrintActionlintVersion(ctx context.Context) {
	if actionlintVersion != "" {
		return
	}
	version, err := getActionlintVersion(ctx)
	if err != nil {
		// Log error but continue - version display is not critical
		actionlintLog.Printf("Could not fetch actionlint version: %v", err)
		return
	}
	fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Using actionlint "+version))
}

func resolveActionlintPaths(lockFiles []string) (string, []string, error) {
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return "", nil, err
	}
	relPaths := make([]string, 0, len(lockFiles))
	for _, lockFile := range lockFiles {
		relPath, err := filepath.Rel(gitRoot, lockFile)
		if err != nil {
			return "", nil, fmt.Errorf("failed to get relative path for %s: %w", lockFile, err)
		}
		relPaths = append(relPaths, relPath)
	}
	return gitRoot, relPaths, nil
}

func runActionlintCommand(ctx context.Context, gitRoot string, lockFiles, relPaths []string, verbose bool, options actionlintRunOptions) actionlintCommandResult {
	timeoutDuration := time.Duration(max(5, len(lockFiles))) * time.Minute
	runCtx, cancel := context.WithTimeout(ctx, timeoutDuration)
	defer cancel()

	verboseHint := ""
	if verbose {
		verboseHint = buildActionlintDockerCommand(gitRoot, relPaths, options)
	}
	printActionlintRunMessage(lockFiles, relPaths, verboseHint, options)

	cmd := exec.CommandContext(runCtx, "docker", buildActionlintDockerArgs(gitRoot, relPaths, options)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	return actionlintCommandResult{
		stdout:          stdout.String(),
		stderr:          stderr.String(),
		err:             err,
		timeoutDuration: timeoutDuration,
		ctxErr:          runCtx.Err(),
	}
}

func buildActionlintDockerArgs(gitRoot string, relPaths []string, options actionlintRunOptions) []string {
	dockerArgs := []string{
		"run",
		"--rm",
		"-v", gitRoot + ":/workdir",
		"-w", "/workdir",
		ActionlintImage,
		"-format", "{{json .}}",
	}
	if !options.IncludeShellcheck {
		dockerArgs = append(dockerArgs, "-shellcheck=")
	}
	if !options.IncludePyflakes {
		dockerArgs = append(dockerArgs, "-pyflakes=")
	}
	for _, ignorePattern := range options.IgnorePatterns {
		dockerArgs = append(dockerArgs, "-ignore", ignorePattern)
	}
	return append(dockerArgs, relPaths...)
}

func buildActionlintDockerCommand(gitRoot string, relPaths []string, options actionlintRunOptions) string {
	args := buildActionlintDockerArgs(gitRoot, relPaths, options)
	formattedArgs := append([]string(nil), args...)
	for i, arg := range formattedArgs {
		formattedArgs[i] = actionlintShellQuoteArg(arg)
	}
	return "docker " + strings.Join(formattedArgs, " ")
}

func actionlintShellQuoteArg(arg string) string {
	if arg == "" || strings.ContainsAny(arg, actionlintShellMetacharacters) {
		return strconv.Quote(arg)
	}
	return arg
}

func printActionlintRunMessage(lockFiles, relPaths []string, verboseHint string, options actionlintRunOptions) {
	integrationStatus := buildActionlintIntegrationStatus(options.IncludeShellcheck, options.IncludePyflakes)
	if len(lockFiles) == 1 {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Running actionlint ("+integrationStatus+") on "+relPaths[0]))
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage(fmt.Sprintf("Running actionlint (%s) on %d files", integrationStatus, len(lockFiles))))
	}
	if verboseHint == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Run actionlint directly: "+verboseHint))
}

func actionlintShouldParseOutput(err error) bool {
	if err == nil {
		return true
	}
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1
}

func actionlintContextError(result actionlintCommandResult, lockFiles []string) error {
	if errors.Is(result.ctxErr, context.DeadlineExceeded) {
		if actionlintStats != nil {
			actionlintStats.IntegrationErrors++
		}
		return fmt.Errorf("actionlint timed out after %d minutes on %s - this may indicate a Docker or network issue",
			int(result.timeoutDuration.Minutes()), actionlintFileDescription(lockFiles))
	}
	if errors.Is(result.ctxErr, context.Canceled) {
		return errors.New("actionlint was canceled before completion (for example by Ctrl+C or caller cancellation)")
	}
	return nil
}

func handleActionlintExecutionError(err error, strict bool, lockFiles []string, totalErrors int, parseErr error, stdout string) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		if actionlintStats != nil {
			actionlintStats.IntegrationErrors++
		}
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
			"actionlint could not be invoked — this is a tooling error, not a workflow validation failure: "+err.Error()))
		return fmt.Errorf("actionlint failed: %w", err)
	}

	exitCode := exitErr.ExitCode()
	actionlintLog.Printf("Actionlint exited with code %d, found %d errors", exitCode, totalErrors)
	if exitCode == 1 {
		return handleActionlintFindings(strict, lockFiles, totalErrors, parseErr, stdout)
	}

	fileDescription := actionlintFileDescription(lockFiles)
	if actionlintStats != nil {
		actionlintStats.IntegrationErrors++
	}
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
		fmt.Sprintf("actionlint failed with exit code %d on %s — this is a tooling error, not a workflow validation failure", exitCode, fileDescription)))
	return fmt.Errorf("actionlint failed with exit code %d on %s", exitCode, fileDescription)
}

func handleActionlintFindings(strict bool, lockFiles []string, totalErrors int, parseErr error, stdout string) error {
	if strict {
		fileDescription := actionlintFileDescription(lockFiles)
		if parseErr != nil {
			return fmt.Errorf("strict mode: actionlint exited with errors on %s but output could not be parsed — this is likely a tooling or integration error", fileDescription)
		}
		return fmt.Errorf("strict mode: actionlint found %d errors in %s - workflows must have no actionlint errors in strict mode", totalErrors, fileDescription)
	}

	// In non-strict mode, fail only on high-severity errors.
	highSeverityCount := countHighSeverityErrors(stdout)
	if highSeverityCount > 0 {
		fileDescription := actionlintFileDescription(lockFiles)
		return fmt.Errorf("actionlint found %d high severity error(s) in %s", highSeverityCount, fileDescription)
	}

	if parseErr != nil {
		actionlintLog.Printf("actionlint findings could not be parsed in non-strict mode: %v", parseErr)
	}
	return nil
}

func actionlintFileDescription(lockFiles []string) string {
	if len(lockFiles) == 1 {
		return filepath.Base(lockFiles[0])
	}
	return "workflows"
}

// parseAndDisplayActionlintOutput parses actionlint JSON output and displays it in the desired format
// Returns the total number of errors found and a breakdown by kind
func parseAndDisplayActionlintOutput(stdout string, verbose bool) (int, map[string]int, error) {
	// Skip if no output
	if stdout == "" || strings.TrimSpace(stdout) == "" {
		actionlintLog.Print("No actionlint output to parse")
		return 0, make(map[string]int), nil
	}

	// Parse JSON errors from stdout - actionlint outputs a single JSON array
	var errors []actionlintError
	if err := json.Unmarshal([]byte(stdout), &errors); err != nil {
		return 0, nil, fmt.Errorf("failed to parse actionlint JSON output: %w", err)
	}

	totalErrors := len(errors)
	actionlintLog.Printf("Parsed %d actionlint errors from output", totalErrors)

	// Track errors by kind
	errorsByKind := make(map[string]int)
	for _, err := range errors {
		if err.Kind != "" {
			errorsByKind[err.Kind]++
		}
		fmt.Fprint(os.Stderr, console.FormatError(buildActionlintCompilerError(err)))
	}

	return totalErrors, errorsByKind, nil
}

// countHighSeverityErrors counts actionlint errors that are high severity.
// Non-shellcheck errors are always considered high severity.
// Shellcheck errors are high severity only if their severity level is "error" or "warning".
// Shellcheck "info" and "style" level findings are considered low severity.
func countHighSeverityErrors(stdout string) int {
	if stdout == "" || strings.TrimSpace(stdout) == "" {
		return 0
	}
	var errors []actionlintError
	if err := json.Unmarshal([]byte(stdout), &errors); err != nil {
		return 0
	}
	count := 0
	for _, e := range errors {
		if isHighSeverityActionlintError(e) {
			count++
		}
	}
	return count
}

// isHighSeverityActionlintError determines whether an actionlint error is high severity.
// Shellcheck findings with "info" or "style" severity are low severity.
// All other errors (including non-shellcheck errors) are high severity.
func isHighSeverityActionlintError(e actionlintError) bool {
	if e.Kind != "shellcheck" {
		return true
	}
	// Shellcheck messages from actionlint have the format:
	// "shellcheck reported issue in this script: SC<code>:<severity>:<line>:<col>: <message>"
	severity := extractShellcheckSeverity(e.Message)
	switch severity {
	case "info", "style":
		return false
	default:
		return true
	}
}

// extractShellcheckSeverity extracts the severity from a shellcheck actionlint message.
// Expected format: "shellcheck reported issue in this script: SC<code>:<severity>:<line>:<col>: <message>"
func extractShellcheckSeverity(message string) string {
	// Find the "SC" code prefix
	scIdx := strings.Index(message, "SC")
	if scIdx < 0 {
		return ""
	}
	// After "SC<digits>:" we expect the severity
	rest := message[scIdx:]
	parts := strings.SplitN(rest, ":", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func buildActionlintCompilerError(err actionlintError) console.CompilerError {
	return console.CompilerError{
		Position: console.ErrorPosition{
			File:   err.Filepath,
			Line:   err.Line,
			Column: err.Column,
		},
		Type:    actionlintErrorType(err.Kind),
		Message: actionlintErrorMessage(err),
		Context: actionlintErrorContext(err.Snippet),
	}
}

func actionlintErrorType(kind string) string {
	if strings.Contains(strings.ToLower(kind), "warning") {
		return "warning"
	}
	return "error"
}

func actionlintErrorMessage(err actionlintError) string {
	if err.Kind == "" {
		return err.Message
	}
	return fmt.Sprintf("[%s] %s\n\n  📖 %s", err.Kind, err.Message, getActionlintDocsURL(err.Kind))
}

func actionlintErrorContext(snippet string) []string {
	if snippet == "" {
		return nil
	}
	lines := strings.Split(snippet, "\n")
	return []string{lines[0]}
}
