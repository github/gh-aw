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
	"strings"

	"github.com/github/gh-aw/pkg/constants"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/scanfindings"
)

var poutineLog = logger.New("cli:poutine")

// poutineFinding represents a single finding from poutine JSON output
type poutineFinding struct {
	RuleID string `json:"rule_id"`
	Purl   string `json:"purl"`
	Meta   struct {
		Path    string `json:"path"`
		Line    int    `json:"line"`
		Details string `json:"details"`
	} `json:"meta"`
}

// poutineRule describes a poutine rule definition from the JSON output
type poutineRule struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Level       string `json:"level"` // error, warning, note
}

// poutineRules maps rule identifiers to their definitions
type poutineRules map[string]poutineRule

// poutineOutput represents the complete JSON output from poutine
type poutineOutput struct {
	Findings []poutineFinding `json:"findings"`
	Rules    poutineRules     `json:"rules"`
}

// ensurePoutineConfig creates .poutine.yml to configure allowed runners and
// acknowledged findings if it doesn't exist
func ensurePoutineConfig(gitRoot string) error {
	configPath := filepath.Join(gitRoot, ".poutine.yml")

	// Check if config already exists
	if fileutil.FileExists(configPath) {
		// Config exists, do not update it
		poutineLog.Print(".poutine.yml already exists, skipping creation")
		return nil
	}

	// Create the config file
	configContent := `# Configure poutine security scanner
# See: https://github.com/boostsecurityio/poutine

# Set rule configuration options
rulesConfig:
  pr_runs_on_self_hosted:
    allowed_runners:
      - ubuntu-slim  # GitHub's new built-in runner (not self-hosted)

# Acknowledge findings that do not apply to gh-aw generated workflows.
# poutine has no inline ignore comment mechanism; skips must be declared here.
skip:
  # The generated "activation" job runs helper scripts from
  # "$RUNNER_TEMP/gh-aw/actions/*.sh". Those scripts are extracted from the
  # pinned gh-aw action, not from the repository checkout, so they cannot be
  # controlled by an untrusted contributor. The rule still fires because the
  # workflow declares an untrusted trigger (for example workflow_call).
  - rule: untrusted_checkout_exec
    job: activation
`

	// Write the config file
	if err := os.WriteFile(configPath, []byte(configContent), constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write .poutine.yml: %w", err)
	}

	poutineLog.Printf("Created .poutine.yml at %s", configPath)
	return nil
}

// runPoutineOnDirectory runs the poutine security scanner on a directory containing workflows
func runPoutineOnDirectory(workflowDir string, verbose bool, strict bool) error {
	poutineLog.Printf("Running poutine security scanner on directory: %s", workflowDir)

	// Find git root to get the absolute path for Docker volume mount
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return err
	}

	// Validate gitRoot is an absolute path (security: ensure trusted path from git)
	gitRoot, err = fileutil.ValidateAbsolutePath(gitRoot)
	if err != nil {
		return fmt.Errorf("invalid git root %q: %w", gitRoot, err)
	}

	// Ensure poutine config exists with custom runner configuration
	if err := ensurePoutineConfig(gitRoot); err != nil {
		return fmt.Errorf("failed to ensure poutine config: %w", err)
	}

	cmd, displayArgs, err := buildPoutineCommand(gitRoot)
	if err != nil {
		return err
	}

	// Always show that poutine is running (regular verbosity)
	fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Running poutine security scanner"))

	if verbose {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Run poutine directly: "+shellJoinArgs(displayArgs)))
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the command
	err = cmd.Run()

	// Parse and display output for all files (no filtering)
	totalWarnings, parseErr := parseAndDisplayPoutineOutputForDirectory(stdout.String(), verbose, gitRoot)
	if parseErr != nil {
		poutineLog.Printf("Failed to parse poutine output: %v", parseErr)
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
		// poutine exits with non-zero code when findings are present
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode := exitErr.ExitCode()
			poutineLog.Printf("Poutine exited with code %d (warnings=%d)", exitCode, totalWarnings)
			// Exit code 1 typically indicates findings in the repository
			if exitCode == 1 {
				// In strict mode, any findings in the scan are treated as errors
				if strict && totalWarnings > 0 {
					return fmt.Errorf("strict mode: poutine found %d security warnings/errors - workflows must have no poutine findings in strict mode. Example: rerun after resolving all reported findings", totalWarnings)
				}
				// In non-strict mode, findings are logged but not treated as errors
				return nil
			}
			// Other exit codes are actual errors
			return fmt.Errorf("poutine failed with exit code %d", exitCode)
		}
		// Non-ExitError errors (e.g., command not found)
		return fmt.Errorf("poutine failed: %w", err)
	}

	return nil
}

// buildPoutineCommand uses a compatible local binary when available, falling
// back to the pinned Docker image otherwise.
func buildPoutineCommand(gitRoot string) (*exec.Cmd, []string, error) {
	args := []string{"analyze_local", ".", "--format", "json", "--quiet"}
	if localPath := localScannerPath(context.Background(), "poutine"); localPath != "" {
		cmd := exec.Command(localPath, args...)
		cmd.Dir = gitRoot
		return cmd, append([]string{localPath}, args...), nil
	}

	volumeMount, err := buildDockerVolumeMount(gitRoot, "/workdir")
	if err != nil {
		return nil, nil, fmt.Errorf("invalid docker mount path: %w", err)
	}
	poutineImageRef, err := validateDockerImageRef(PoutineImage)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid poutine scanner image reference %q: %w", PoutineImage, err)
	}
	dockerPath, err := fileutil.ResolveExecutablePath("docker")
	if err != nil {
		return nil, nil, fmt.Errorf("docker command not found: %w", err)
	}
	dockerArgs := []string{
		"run",
		"--rm",
		"-v", volumeMount,
		"-w", "/workdir",
		poutineImageRef,
	}
	dockerArgs = append(dockerArgs, args...)
	// #nosec G204 -- gitRoot comes from git rev-parse, the mount and image
	// are validated above, and exec.Command does not invoke a shell.
	cmd := exec.Command(dockerPath, dockerArgs...)
	cmd.Dir = gitRoot
	return cmd, append([]string{dockerPath}, dockerArgs...), nil
}

// runPoutineOnFile runs the poutine security scanner on a single .lock.yml file using Docker
// This is a wrapper that filters the directory scan results to a single file for backward compatibility
func runPoutineOnFile(lockFile string, verbose bool, strict bool) error {
	poutineLog.Printf("Running poutine security scanner: file=%s, strict=%v", lockFile, strict)

	// Find git root to get the absolute path for Docker volume mount
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return err
	}

	// Validate gitRoot is an absolute path (security: ensure trusted path from git)
	gitRoot, err = fileutil.ValidateAbsolutePath(gitRoot)
	if err != nil {
		return fmt.Errorf("invalid git root %q: %w", gitRoot, err)
	}

	// Ensure poutine config exists with custom runner configuration
	if err := ensurePoutineConfig(gitRoot); err != nil {
		return fmt.Errorf("failed to ensure poutine config: %w", err)
	}

	// Get the relative path from git root
	relPath, err := filepath.Rel(gitRoot, lockFile)
	if err != nil {
		return fmt.Errorf("failed to get relative path: %w", err)
	}

	cmd, displayArgs, err := buildPoutineCommand(gitRoot)
	if err != nil {
		return err
	}

	// Always show that poutine is running (regular verbosity)
	fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Running poutine security scanner"))

	// In verbose mode, also show the command that users can run directly
	if verbose {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Run poutine directly: "+shellJoinArgs(displayArgs)))
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the command
	err = cmd.Run()

	// Parse and reformat the output, get total warning count
	totalWarnings, parseErr := parseAndDisplayPoutineOutput(stdout.String(), relPath, verbose)
	if parseErr != nil {
		poutineLog.Printf("Failed to parse poutine output: %v", parseErr)
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
		// poutine exits with non-zero code when findings are present
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode := exitErr.ExitCode()
			poutineLog.Printf("Poutine exited with code %d (warnings=%d)", exitCode, totalWarnings)
			// Exit code 1 typically indicates findings in the repository
			// In non-strict mode, we allow this even if we don't have findings
			// specific to the current file (poutine scans the whole directory)
			if exitCode == 1 {
				// In strict mode, any findings in the scan are treated as errors
				if strict && totalWarnings > 0 {
					return fmt.Errorf("strict mode: poutine found %d security warnings/errors in %s - workflows must have no poutine findings in strict mode. Example: rerun after resolving all reported findings in %s", totalWarnings, filepath.Base(lockFile), filepath.Base(lockFile))
				}
				// In non-strict mode, findings are logged but not treated as errors
				return nil
			}
			// Other exit codes are actual errors
			return fmt.Errorf("poutine failed with exit code %d on %s", exitCode, filepath.Base(lockFile))
		}
		// Non-ExitError errors (e.g., command not found)
		return fmt.Errorf("poutine failed on %s: %w", filepath.Base(lockFile), err)
	}

	return nil
}

// parseAndDisplayPoutineOutput parses poutine JSON output and displays it in the desired format
// Returns the total number of warnings found for the specific file
func parseAndDisplayPoutineOutput(stdout, targetFile string, verbose bool) (int, error) {
	// Parse JSON output from stdout
	var output poutineOutput
	if stdout == "" {
		return 0, nil // No output means no findings
	}

	trimmed := strings.TrimSpace(stdout)
	if !strings.HasPrefix(trimmed, "{") {
		// Non-JSON output, likely an error
		if trimmed != "" {
			return 0, fmt.Errorf("unexpected poutine output format (expected JSON object). Example: {\"findings\":[]}. Got: %s", trimmed)
		}
		return 0, nil
	}

	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		return 0, fmt.Errorf("failed to parse poutine JSON output: %w", err)
	}

	// Filter findings to only those relevant to the target file
	var relevantFindings []poutineFinding
	for _, finding := range output.Findings {
		if finding.Meta.Path == targetFile {
			relevantFindings = append(relevantFindings, finding)
		}
	}

	totalWarnings := len(relevantFindings)

	// Skip files with 0 warnings
	if totalWarnings == 0 {
		return 0, nil
	}

	// Read file content for context display
	fileContent, err := os.ReadFile(targetFile)
	var fileLines []string
	if err == nil {
		fileLines = strings.Split(string(fileContent), "\n")
	}

	// Display detailed findings using the shared finding representation
	scanfindings.Render(os.Stderr, poutineFindingsToShared(relevantFindings, output.Rules, targetFile, fileLines))

	return totalWarnings, nil
}

// parseAndDisplayPoutineOutputForDirectory parses poutine JSON output and displays all findings
// Returns the total number of warnings found across all files
func parseAndDisplayPoutineOutputForDirectory(stdout string, verbose bool, gitRoot string) (int, error) {
	// Parse JSON output from stdout
	var output poutineOutput
	if stdout == "" {
		return 0, nil // No output means no findings
	}

	trimmed := strings.TrimSpace(stdout)
	if !strings.HasPrefix(trimmed, "{") {
		// Non-JSON output, likely an error
		if trimmed != "" {
			return 0, fmt.Errorf("unexpected poutine output format (expected JSON object). Example: {\"findings\":[]}. Got: %s", trimmed)
		}
		return 0, nil
	}

	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		return 0, fmt.Errorf("failed to parse poutine JSON output: %w", err)
	}

	// Display all findings (no filtering by file)
	totalWarnings := len(output.Findings)

	// Skip if no warnings
	if totalWarnings == 0 {
		return 0, nil
	}

	// Group findings by file for better readability
	findingsByFile := make(map[string][]poutineFinding)
	for _, finding := range output.Findings {
		findingsByFile[finding.Meta.Path] = append(findingsByFile[finding.Meta.Path], finding)
	}

	// Display findings for each file
	for filePath, findings := range findingsByFile {
		// Validate and sanitize file path to prevent path traversal
		cleanPath := filepath.Clean(filePath)

		// Convert to absolute path if relative
		absPath := cleanPath
		if !filepath.IsAbs(cleanPath) {
			absPath = filepath.Join(gitRoot, cleanPath)
		}

		// Ensure the file is within gitRoot to prevent path traversal
		absGitRoot, err := filepath.Abs(gitRoot)
		if err != nil {
			poutineLog.Printf("Failed to get absolute path for git root: %v", err)
			continue
		}

		absPath, err = filepath.Abs(absPath)
		if err != nil {
			poutineLog.Printf("Failed to get absolute path for %s: %v", filePath, err)
			continue
		}

		// Check if the resolved path is within gitRoot
		relPath, err := filepath.Rel(absGitRoot, absPath)
		if err != nil || strings.HasPrefix(relPath, "..") {
			poutineLog.Printf("Skipping file outside git root: %s", filePath)
			continue
		}

		// Read file content for context display
		// #nosec G304 -- absPath is validated through: 1) filepath.Clean() normalization,
		// 2) absolute path resolution, and 3) filepath.Rel() check ensuring it's within gitRoot
		// (lines 414-441). Path traversal attacks are prevented by the boundary validation.
		fileContent, err := os.ReadFile(absPath)
		var fileLines []string
		if err == nil {
			fileLines = strings.Split(string(fileContent), "\n")
		}

		// Display detailed findings using the shared finding representation
		scanfindings.Render(os.Stderr, poutineFindingsToShared(findings, output.Rules, filePath, fileLines))
	}

	return totalWarnings, nil
}

// poutineFindingsToShared maps poutine's native findings onto the shared finding
// representation used by every scanner integration. Rule metadata supplies the
// severity level and title when available.
func poutineFindingsToShared(findings []poutineFinding, rules poutineRules, filePath string, fileLines []string) []scanfindings.Finding {
	shared := make([]scanfindings.Finding, 0, len(findings))
	for _, finding := range findings {
		ruleInfo := rules[finding.RuleID]

		severityLabel := ruleInfo.Level
		if severityLabel == "" {
			severityLabel = "warning" // Default to warning if not specified
		}

		title := ruleInfo.Title
		if title == "" {
			title = finding.RuleID
		}

		// Get line number (poutine uses 1-based indexing)
		lineNum := finding.Meta.Line
		if lineNum == 0 {
			lineNum = 1 // Default to line 1 if not specified
		}

		message := scanfindings.FormatMessage(severityLabel, finding.RuleID, title)
		if finding.Meta.Details != "" {
			message = fmt.Sprintf("%s - %s", message, finding.Meta.Details)
		}

		shared = append(shared, scanfindings.Finding{
			RuleID:   finding.RuleID,
			Severity: scanfindings.ParseSeverity(severityLabel),
			Message:  message,
			File:     firstNonEmpty(finding.Meta.Path, filePath),
			Line:     lineNum,
			Column:   1, // poutine doesn't provide column info
			Context:  scanfindings.ContextLines(fileLines, lineNum),
		})
	}
	return shared
}
