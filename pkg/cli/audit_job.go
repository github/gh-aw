package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// auditJobRunOptions holds parameters for auditJobRun.
type auditJobRunOptions struct {
	runID      int64
	jobID      int64
	stepNumber int
	owner      string
	repo       string
	hostname   string
	outputDir  string
	verbose    bool
	jsonOutput bool
}

// auditJobRun performs a targeted audit of a specific job within a workflow run
// If stepNumber > 0, focuses on extracting output for that specific step
func auditJobRun(opts auditJobRunOptions) error {
	opts.hostname = resolveAuditHostname(opts.hostname)
	auditLog.Printf("Starting job-specific audit: runID=%d, jobID=%d, stepNumber=%d, hostname=%s", opts.runID, opts.jobID, opts.stepNumber, opts.hostname)
	if err := os.MkdirAll(opts.outputDir, constants.DirPermSensitive); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	jobLogContent, jobLogPath, err := fetchAuditJobLog(opts)
	if err != nil {
		return err
	}
	if err := extractAuditJobDetails(opts, jobLogContent); err != nil {
		return err
	}
	return renderAuditJobSummary(opts, jobLogPath)
}

func fetchAuditJobLog(opts auditJobRunOptions) (string, string, error) {
	args := []string{"run", "view"}
	if opts.owner != "" && opts.repo != "" {
		args = append(args, "-R", fmt.Sprintf("%s/%s", opts.owner, opts.repo))
	}
	args = append(args, "--job", strconv.FormatInt(opts.jobID, 10), "--log")
	if opts.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Fetching logs for job %d...", opts.jobID)))
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Executing: gh "+strings.Join(args, " ")))
	}
	cmd := workflow.ExecGH(args...)
	workflow.SetGHHostEnv(cmd, opts.hostname)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch job logs: %w\nOutput: %s", err, string(output))
	}
	jobLogPath := filepath.Join(opts.outputDir, fmt.Sprintf("job-%d.log", opts.jobID))
	if err := os.WriteFile(jobLogPath, output, constants.FilePermSensitive); err != nil {
		return "", "", fmt.Errorf("failed to write job log: %w", err)
	}
	if opts.verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Job log saved to "+jobLogPath))
	}
	return string(output), jobLogPath, nil
}

func extractAuditJobDetails(opts auditJobRunOptions, jobLogContent string) error {
	if opts.stepNumber > 0 {
		return extractRequestedStepOutput(opts, jobLogContent)
	}
	return extractFirstFailingStepOutput(opts, jobLogContent)
}

func extractRequestedStepOutput(opts auditJobRunOptions, jobLogContent string) error {
	stepOutput, err := extractStepOutput(jobLogContent, opts.stepNumber)
	if err != nil {
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not extract step %d output: %v", opts.stepNumber, err)))
		}
		return nil
	}
	stepLogPath := filepath.Join(opts.outputDir, fmt.Sprintf("job-%d-step-%d.log", opts.jobID, opts.stepNumber))
	if err := os.WriteFile(stepLogPath, []byte(stepOutput), constants.FilePermSensitive); err != nil {
		return fmt.Errorf("failed to write step log: %w", err)
	}
	if opts.verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Step %d output saved to %s", opts.stepNumber, stepLogPath)))
	}
	return nil
}

func extractFirstFailingStepOutput(opts auditJobRunOptions, jobLogContent string) error {
	failingStepNum, failingStepOutput := findFirstFailingStep(jobLogContent)
	if failingStepNum == 0 {
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No failing steps found in job"))
		}
		return nil
	}
	stepLogPath := filepath.Join(opts.outputDir, fmt.Sprintf("job-%d-step-%d-failed.log", opts.jobID, failingStepNum))
	if err := os.WriteFile(stepLogPath, []byte(failingStepOutput), constants.FilePermSensitive); err != nil {
		return fmt.Errorf("failed to write failing step log: %w", err)
	}
	if opts.verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("First failing step %d output saved to %s", failingStepNum, stepLogPath)))
	}
	return nil
}

func renderAuditJobSummary(opts auditJobRunOptions, jobLogPath string) error {
	if opts.jsonOutput {
		return nil
	}
	absOutputDir := opts.outputDir
	if abs, err := filepath.Abs(opts.outputDir); err == nil {
		absOutputDir = abs
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Job audit complete. Logs saved to "+absOutputDir))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("\nDownloaded files:"))
	fmt.Fprintf(os.Stderr, "  - %s (full job log)\n", jobLogPath)
	if opts.stepNumber > 0 {
		renderRequestedStepSummary(opts)
		return nil
	}
	return renderFailingStepSummary(opts)
}

func renderRequestedStepSummary(opts auditJobRunOptions) {
	stepLogPath := filepath.Join(opts.outputDir, fmt.Sprintf("job-%d-step-%d.log", opts.jobID, opts.stepNumber))
	if fileutil.FileExists(stepLogPath) {
		fmt.Fprintf(os.Stderr, "  - %s (step %d output)\n", stepLogPath, opts.stepNumber)
	}
}

func renderFailingStepSummary(opts auditJobRunOptions) error {
	failingStepPath := filepath.Join(opts.outputDir, fmt.Sprintf("job-%d-step-*-failed.log", opts.jobID))
	matches, err := filepath.Glob(failingStepPath)
	if err != nil {
		return fmt.Errorf("could not search for failing step logs: %w", err)
	}
	for _, match := range matches {
		fmt.Fprintf(os.Stderr, "  - %s (first failing step)\n", match)
	}
	return nil
}

// extractStepOutput extracts the output of a specific step from job logs
func extractStepOutput(jobLog string, stepNumber int) (string, error) {
	auditLog.Printf("Extracting output for step %d from job logs (%d bytes)", stepNumber, len(jobLog))
	lines := strings.Split(jobLog, "\n")
	var stepOutput []string
	inStep := false
	stepPattern := "##[group]Run " // GitHub Actions step marker
	stepEndPattern := "##[endgroup]"
	currentStep := 0

	for _, line := range lines {
		// Detect step boundaries
		if strings.Contains(line, stepPattern) || strings.HasPrefix(line, fmt.Sprintf("##[group]Step %d:", stepNumber)) {
			currentStep++
			if currentStep == stepNumber {
				inStep = true
			}
		} else if strings.Contains(line, stepEndPattern) {
			if inStep {
				break // End of target step
			}
		}

		if inStep {
			stepOutput = append(stepOutput, line)
		}
	}

	if len(stepOutput) == 0 {
		auditLog.Printf("Step %d not found in job logs (scanned %d lines)", stepNumber, len(lines))
		return "", fmt.Errorf("step %d not found in job logs", stepNumber)
	}

	auditLog.Printf("Extracted %d lines for step %d", len(stepOutput), stepNumber)
	return strings.Join(stepOutput, "\n"), nil
}

// findFirstFailingStep finds the first step that failed in the job logs
func findFirstFailingStep(jobLog string) (int, string) {
	auditLog.Printf("Searching for first failing step in job logs (%d bytes)", len(jobLog))
	lines := strings.Split(jobLog, "\n")
	var stepOutput []string
	inStep := false
	currentStep := 0
	foundFailure := false

	for _, line := range lines {
		// Detect step start
		if strings.Contains(line, "##[group]") {
			if inStep && foundFailure {
				break // We found a complete failing step
			}
			inStep = true
			currentStep++
			stepOutput = []string{line}
			foundFailure = false
		} else if inStep {
			stepOutput = append(stepOutput, line)

			// Detect failure indicators
			if strings.Contains(line, "##[error]") ||
				strings.Contains(line, "Error:") ||
				strings.Contains(line, "FAILED") ||
				strings.Contains(line, "exit code") && !strings.Contains(line, "exit code 0") {
				foundFailure = true
			}
		}
	}

	if foundFailure && len(stepOutput) > 0 {
		auditLog.Printf("Found failing step %d with %d lines of output", currentStep, len(stepOutput))
		return currentStep, strings.Join(stepOutput, "\n")
	}

	auditLog.Print("No failing step found in job logs")
	return 0, ""
}
