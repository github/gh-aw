package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/scanfindings"
)

var runnerGuardLog = logger.New("cli:runner_guard")

// runnerGuardFinding represents a single finding from runner-guard JSON output
type runnerGuardFinding struct {
	RuleID      string `json:"rule_id"`
	Name        string `json:"name"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
	Remediation string `json:"remediation"`
	File        string `json:"file"`
	JobID       string `json:"job_id"`
	Line        int    `json:"line"`
}

// runnerGuardOutput represents the complete JSON output from runner-guard
type runnerGuardOutput struct {
	Findings []runnerGuardFinding `json:"findings"`
	Score    int                  `json:"score,omitempty"`
	Grade    string               `json:"grade,omitempty"`
}

func buildRunnerGuardContainerScanPath(scanPath string) (string, error) {
	if scanPath == "" {
		return "./", nil
	}
	cleanPath := filepath.Clean(scanPath)
	if !filepath.IsLocal(cleanPath) {
		return "", fmt.Errorf("runner-guard scan path must stay local to the repository. Expected a relative path inside the repository. Example: .github/workflows. Got: %q", scanPath)
	}
	if containsControlCharacters(cleanPath) {
		return "", fmt.Errorf("runner-guard scan path contains invalid control characters. Expected a plain relative path. Example: .github/workflows. Got: %q", scanPath)
	}
	return "./" + filepath.ToSlash(cleanPath), nil
}

// runRunnerGuardOnDirectory runs the runner-guard taint analysis scanner on a directory
// containing workflows using the Docker image.
func runRunnerGuardOnDirectory(workflowDir string, verbose bool, strict bool) error {
	runnerGuardLog.Printf("Running runner-guard taint analysis on directory: %s", workflowDir)

	// Find git root to get the absolute path for Docker volume mount
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return err
	}

	gitRoot, err = fileutil.ValidateAbsolutePath(gitRoot)
	if err != nil {
		return fmt.Errorf("git root %q is not a valid absolute path; runner-guard requires an absolute repository root. Example: run gh aw from inside a git checkout: %w", gitRoot, err)
	}

	// Determine the scan path: use workflowDir relative to gitRoot when possible,
	// so the scan is scoped to the compiled workflows directory.
	scanPath := "."
	if workflowDir != "" {
		absWorkflowDir, err := filepath.Abs(workflowDir)
		if err != nil {
			return fmt.Errorf("workflow directory %q could not be resolved to an absolute path; expected an existing directory. Example: .github/workflows: %w", workflowDir, err)
		}
		if err := fileutil.ValidatePathWithinBase(gitRoot, absWorkflowDir); err != nil {
			return fmt.Errorf("workflow directory %q must stay within git root %q; expected a directory inside the repository. Example: .github/workflows: %w", workflowDir, gitRoot, err)
		}
		relDir, relErr := filepath.Rel(gitRoot, absWorkflowDir)
		if relErr != nil {
			return fmt.Errorf("workflow directory %q could not be expressed relative to git root %q; expected a directory inside the repository. Example: .github/workflows: %w", workflowDir, gitRoot, relErr)
		}
		if !filepath.IsLocal(relDir) {
			return fmt.Errorf("workflow directory %q resolved to non-local relative path %q", workflowDir, relDir)
		}
		scanPath = filepath.Clean(relDir)
	}

	// Prefix with "./" and convert host separators to forward slashes for the Linux container.
	// This prevents option injection: without the prefix a workflowDir such as "--help" would
	// produce a scanPath beginning with "-", which runner-guard could interpret as a flag.
	containerScanPath, err := buildRunnerGuardContainerScanPath(scanPath)
	if err != nil {
		return fmt.Errorf("runner-guard scan path is invalid; expected a relative path inside the repository. Example: .github/workflows: %w", err)
	}

	// Build the Docker command
	// docker run --rm -v "$gitRoot:/workdir" -w /workdir ghcr.io/vigilant-llc/runner-guard:latest scan <path> --format json
	dockerPath, err := fileutil.ResolveExecutablePath("docker")
	if err != nil {
		return fmt.Errorf("docker command not found: %w", err)
	}
	volumeMount, err := buildDockerVolumeMount(gitRoot, "/workdir")
	if err != nil {
		return fmt.Errorf("docker mount path for git root %q is invalid; expected an absolute host path. Example: /home/user/repo: %w", gitRoot, err)
	}
	runnerGuardImageRef, err := validateDockerImageRef(RunnerGuardImage)
	if err != nil {
		return fmt.Errorf("runner-guard scanner image reference %q is invalid; expected a registry reference. Example: ghcr.io/owner/image:tag: %w", RunnerGuardImage, err)
	}
	// #nosec G204 -- gitRoot is validated as an absolute path above (from git rev-parse, a trusted
	// source). containerScanPath is derived from filepath.Rel(gitRoot, workflowDir), cleaned with
	// filepath.Clean, validated to not escape the repository root (no ".." prefix), and prefixed
	// with "./" to prevent option injection. dockerPath is resolved from the allowlisted executable
	// name "docker" via fileutil.ResolveExecutablePath. runnerGuardImageRef is the validated result
	// of validateDockerImageRef above. exec.Command passes args directly to the OS (no shell).
	dockerArgs := runnerGuardDockerArgs(runnerGuardImageRef, volumeMount, containerScanPath)
	// #nosec G204 -- see the trust-boundary rationale above.
	cmd := exec.Command(dockerPath, dockerArgs...)

	// Always show that runner-guard is running (regular verbosity)
	fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Running runner-guard taint analysis scanner"))

	// In verbose mode, also show the command that users can run directly
	if verbose {
		dockerCmd := shellJoinArgs(append([]string{"docker"}, dockerArgs...))
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Run runner-guard directly: "+dockerCmd))
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the command
	err = cmd.Run()

	// Parse and display output
	totalFindings, parseErr := parseAndDisplayRunnerGuardOutput(stdout.String(), verbose, gitRoot)
	if parseErr != nil {
		runnerGuardLog.Printf("Failed to parse runner-guard output: %v", parseErr)
		// Fall back to showing raw output
		if stdout.Len() > 0 {
			fmt.Fprint(os.Stderr, stdout.String())
		}

		if stderr.Len() > 0 {
			fmt.Fprint(os.Stderr, stderr.String())
		}
	}

	// Check if the error is due to findings or actual failure
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode := exitErr.ExitCode()
			runnerGuardLog.Printf("runner-guard exited with code %d (findings=%d)", exitCode, totalFindings)
			// Exit code 1 typically indicates findings in the repository
			if exitCode == 1 {
				if strict {
					if parseErr != nil {
						// JSON parsing failed but exit code confirms findings exist
						return fmt.Errorf("strict mode: runner-guard exited with code 1 (findings present) and output could not be parsed: %w", parseErr)
					}
					if totalFindings > 0 {
						return fmt.Errorf("strict mode: runner-guard found %d security findings - workflows must have no runner-guard findings in strict mode. Example: rerun after resolving all reported findings", totalFindings)
					}
					// Exit code 1 with no remaining findings means every reported finding was
					// a known false positive that was filtered out, so the scan passes.
					return nil
				}
				// In non-strict mode, findings are logged but not treated as errors
				return nil
			}
			// Other exit codes are actual errors
			return fmt.Errorf("runner-guard failed with exit code %d; expected 0 (clean) or 1 (findings reported). Example: rerun with gh aw --verbose to see the scanner output", exitCode)
		}
		// Non-ExitError errors (e.g., command not found)
		return fmt.Errorf("runner-guard failed to start; a working docker installation is required. Example: run docker info to check the daemon: %w", err)
	}

	return nil
}

func runnerGuardDockerArgs(imageRef, volumeMount, containerScanPath string) []string {
	return []string{
		"run",
		"--rm",
		"-v", volumeMount,
		"-w", "/workdir",
		imageRef,
		"scan",
		containerScanPath,
		"--format", "json",
	}
}

// parseAndDisplayRunnerGuardOutput parses runner-guard JSON output and displays findings.
// Returns the total number of findings found.
func parseAndDisplayRunnerGuardOutput(stdout string, verbose bool, gitRoot string) (int, error) {
	if stdout == "" {
		return 0, nil // No output means no findings
	}

	trimmed := strings.TrimSpace(stdout)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		if trimmed != "" {
			return 0, fmt.Errorf("unexpected runner-guard output format (expected JSON object or array). Example: {\"findings\":[]}. Got: %s", trimmed)
		}
		return 0, nil
	}

	var output runnerGuardOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		return 0, fmt.Errorf("runner-guard JSON output could not be parsed; expected a JSON object. Example: {\"findings\":[]}: %w", err)
	}

	totalFindings := len(output.Findings)
	if totalFindings == 0 {
		return 0, nil
	}

	// Drop RGS-004 findings for jobs that are gated behind gh-aw's activation job chain.
	// runner-guard evaluates jobs in isolation and does not follow needs: edges.
	output.Findings = filterRunnerGuardFindings(output.Findings, gitRoot)

	// Drop RGS-005 findings for compiler-generated jobs that perform narrowly scoped
	// lifecycle and safe-output writes. The main agent job remains read-only.
	output.Findings = filterGeneratedSafeOutputPermissionFindings(output.Findings, gitRoot)

	// Drop findings that carry an inline runner-guard suppression comment near the reported
	// location in the compiled workflow.
	output.Findings = filterRunnerGuardIgnoredFindings(output.Findings, gitRoot)

	// Drop RGS-012 findings for Copilot allow-tool declarations that only document local curl
	// permissions. The declarations are not executable curl calls and cannot exfiltrate secrets.
	output.Findings = filterCopilotLocalAllowToolFindings(output.Findings, gitRoot)

	totalFindings = len(output.Findings)
	if totalFindings == 0 {
		return 0, nil
	}

	// Display score/grade if present
	if output.Score > 0 || output.Grade != "" {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage(
			fmt.Sprintf("Runner-Guard Score: %d/100 (Grade: %s)", output.Score, output.Grade),
		))
	}

	// Group findings by file for better readability
	findingsByFile := make(map[string][]runnerGuardFinding)
	for _, finding := range output.Findings {
		findingsByFile[finding.File] = append(findingsByFile[finding.File], finding)
	}

	// Display findings for each file
	for filePath, findings := range findingsByFile {
		// Validate and sanitize file path to prevent path traversal
		cleanPath := filepath.Clean(filePath)

		absPath := cleanPath
		if !filepath.IsAbs(cleanPath) {
			absPath = filepath.Join(gitRoot, cleanPath)
		}

		absGitRoot, err := filepath.Abs(gitRoot)
		if err != nil {
			runnerGuardLog.Printf("Failed to get absolute path for git root: %v", err)
			continue
		}

		absPath, err = filepath.Abs(absPath)
		if err != nil {
			runnerGuardLog.Printf("Failed to get absolute path for %s: %v", filePath, err)
			continue
		}

		// Check if the resolved path is within gitRoot to prevent path traversal
		relPath, err := filepath.Rel(absGitRoot, absPath)
		if err != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
			runnerGuardLog.Printf("Skipping file outside git root: %s", filePath)
			continue
		}

		// Read file content for context display
		// #nosec G304 -- absPath is validated through: 1) filepath.Clean() normalization,
		// 2) absolute path resolution, and 3) filepath.Rel() check ensuring it's within gitRoot.
		// Path traversal attacks are prevented by the boundary validation above.
		fileContent, err := os.ReadFile(absPath)
		var fileLines []string
		if err == nil {
			fileLines = strings.Split(string(fileContent), "\n")
		}

		scanfindings.Render(os.Stderr, runnerGuardFindingsToShared(findings, fileLines))
	}

	return totalFindings, nil
}

// runnerGuardFindingsToShared maps runner-guard's native findings onto the shared
// finding representation used by every scanner integration.
func runnerGuardFindingsToShared(findings []runnerGuardFinding, fileLines []string) []scanfindings.Finding {
	shared := make([]scanfindings.Finding, 0, len(findings))
	for _, finding := range findings {
		lineNum := finding.Line
		if lineNum == 0 {
			lineNum = 1
		}

		message := scanfindings.FormatMessage(finding.Severity, finding.RuleID, finding.Name)
		if finding.Description != "" {
			message = fmt.Sprintf("%s - %s", message, finding.Description)
		}

		shared = append(shared, scanfindings.Finding{
			RuleID:   finding.RuleID,
			Severity: scanfindings.ParseSeverity(finding.Severity),
			Message:  message,
			File:     finding.File,
			Line:     lineNum,
			Column:   1,
			Context:  scanfindings.ContextLines(fileLines, lineNum),
		})
	}
	return shared
}
