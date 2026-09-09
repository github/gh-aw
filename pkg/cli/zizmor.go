package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/scanfindings"
	"github.com/github/gh-aw/pkg/setutil"
)

var zizmorLog = logger.New("cli:zizmor")

// zizmorFinding represents a single finding from zizmor JSON output
type zizmorFinding struct {
	Ident          string `json:"ident"`
	Desc           string `json:"desc"`
	URL            string `json:"url"`
	Determinations struct {
		Severity string `json:"severity"`
	} `json:"determinations"`
	Locations []struct {
		Symbolic struct {
			Key struct {
				Local struct {
					GivenPath    string `json:"given_path"`
					VerbatimPath string `json:"verbatim_path"`
				} `json:"Local"`
			} `json:"key"`
			Annotation string `json:"annotation"`
		} `json:"symbolic"`
		Concrete struct {
			Location struct {
				StartPoint struct {
					Row    int `json:"row"`
					Column int `json:"column"`
				} `json:"start_point"`
			} `json:"location"`
		} `json:"concrete"`
	} `json:"locations"`
}

// buildZizmorContainerScanPath converts a repository-relative lock file path into a
// container path safe to pass to zizmor. The "./" prefix confines the argument to the
// mounted working directory and prevents a path such as "-x" from being parsed as a flag.
func buildZizmorContainerScanPath(scanPath string) (string, error) {
	if scanPath == "" {
		return "", errors.New("zizmor scan path cannot be empty. Expected a relative path inside the repository. Example: .github/workflows/example.lock.yml")
	}
	cleanPath := filepath.Clean(scanPath)
	if !filepath.IsLocal(cleanPath) {
		return "", fmt.Errorf("zizmor scan path must stay local to the repository. Expected a relative path inside the repository. Example: .github/workflows/example.lock.yml. Got: %q", scanPath)
	}
	if containsControlCharacters(cleanPath) {
		return "", fmt.Errorf("zizmor scan path contains invalid control characters. Expected a plain relative path. Example: .github/workflows/example.lock.yml. Got: %q", scanPath)
	}
	return "./" + filepath.ToSlash(cleanPath), nil
}

// zizmorScanPaths converts lock file paths into repository-relative paths (for display)
// and container-safe scan paths (for the docker argv). Each candidate is validated with
// fileutil.ValidatePathWithinBase, which resolves symlinks before comparison, so a
// lock-file symlink that resolves outside the git root is rejected instead of silently
// producing a container path that escapes the mounted checkout.
func zizmorScanPaths(gitRoot string, lockFiles []string) (relPaths []string, containerPaths []string, err error) {
	for _, lockFile := range lockFiles {
		if err := fileutil.ValidatePathWithinBase(gitRoot, lockFile); err != nil {
			return nil, nil, fmt.Errorf("zizmor lock file %q is invalid; expected a path inside the repository at %q: %w", lockFile, gitRoot, err)
		}
		relPath, relErr := filepath.Rel(gitRoot, lockFile)
		if relErr != nil {
			return nil, nil, fmt.Errorf("failed to get relative path for %s: %w", lockFile, relErr)
		}
		containerPath, pathErr := buildZizmorContainerScanPath(relPath)
		if pathErr != nil {
			return nil, nil, fmt.Errorf("zizmor scan path for %s is invalid; expected a relative path inside the repository. Example: .github/workflows/example.lock.yml: %w", lockFile, pathErr)
		}
		relPaths = append(relPaths, relPath)
		containerPaths = append(containerPaths, containerPath)
	}
	return relPaths, containerPaths, nil
}

// zizmorDockerArgs builds the `docker run` arguments used to scan the given container paths.
func zizmorDockerArgs(imageRef, volumeMount string, containerPaths []string) []string {
	args := []string{
		"run",
		"--rm",
		"-v", volumeMount,
		"-w", "/workdir",
		imageRef,
		"--persona", "auditor",
		"--format", "json",
	}
	return append(args, containerPaths...)
}

// fatalFindingError wraps an error that must fail compilation regardless of strict
// mode (e.g. high/critical severity scanner findings). handleBatchToolError checks
// for this type and refuses to suppress it even when strict is false.
type fatalFindingError struct {
	err error
}

func (e *fatalFindingError) Error() string { return e.err.Error() }
func (e *fatalFindingError) Unwrap() error { return e.err }

// interpretZizmorRunError maps the docker/zizmor exit status onto a gh-aw error.
// zizmor uses exit codes to indicate findings:
//
//	0 = no findings, 10-13 = findings at different severity levels,
//	14 = findings with mixed severities, other codes = actual errors.
func interpretZizmorRunError(runErr error, totalWarnings, highSeverityCount int, fileDescription string, strict bool) error {
	if runErr == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		// Non-ExitError errors (e.g., command not found)
		return fmt.Errorf("zizmor failed: %w", runErr)
	}
	exitCode := exitErr.ExitCode()
	zizmorLog.Printf("Zizmor exited with code %d (warnings=%d, high=%d)", exitCode, totalWarnings, highSeverityCount)
	if exitCode < 10 || exitCode > 14 {
		// Other exit codes are actual errors
		return fmt.Errorf("zizmor failed with exit code %d on %s", exitCode, fileDescription)
	}
	// High/critical severity findings always fail, regardless of strict mode. Wrap in
	// fatalFindingError so handleBatchToolError does not suppress it in non-strict mode.
	if highSeverityCount > 0 {
		return &fatalFindingError{err: fmt.Errorf("zizmor found %d high/critical severity finding(s) in %s", highSeverityCount, fileDescription)}
	}
	// In strict mode, all findings are treated as errors
	if strict {
		return fmt.Errorf("strict mode: zizmor found %d security warnings/errors in %s - workflows must have no zizmor findings in strict mode", totalWarnings, fileDescription)
	}
	// In non-strict mode, non-high findings are logged but not treated as errors
	return nil
}

// buildZizmorCommand assembles the validated `docker run` invocation for the zizmor
// scanner along with the repository-relative paths used for progress reporting.
func buildZizmorCommand(lockFiles []string) (cmd *exec.Cmd, relPaths []string, dockerArgs []string, err error) {
	// Find git root to get the absolute path for Docker volume mount
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to find git root: %w", err)
	}

	// Validate gitRoot is an absolute path before use in Docker volume mount
	gitRoot, err = fileutil.ValidateAbsolutePath(gitRoot)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("git root %q is not a valid absolute path; zizmor requires an absolute repository root. Example: run gh aw from inside a git checkout: %w", gitRoot, err)
	}

	relPaths, containerPaths, err := zizmorScanPaths(gitRoot, lockFiles)
	if err != nil {
		return nil, nil, nil, err
	}

	// Build the Docker command with JSON output for easier parsing
	// docker run --rm -v "$(pwd)":/workdir -w /workdir <ZizmorImage> --persona auditor --format json <file1> <file2> ...
	dockerPath, err := fileutil.ResolveExecutablePath("docker")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("docker command not found: %w", err)
	}
	volumeMount, err := buildDockerVolumeMount(gitRoot, "/workdir")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("docker mount path for git root %q is invalid; expected an absolute host path. Example: /home/user/repo: %w", gitRoot, err)
	}
	zizmorImageRef, err := validateDockerImageRef(ZizmorImage)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("zizmor scanner image reference %q is invalid; expected a registry reference. Example: ghcr.io/owner/image:tag: %w", ZizmorImage, err)
	}
	for _, containerPath := range containerPaths {
		if err := validateExecArgument(containerPath); err != nil {
			return nil, nil, nil, fmt.Errorf("invalid zizmor scan path argument %q: %w", containerPath, err)
		}
	}
	dockerArgs = zizmorDockerArgs(zizmorImageRef, volumeMount, containerPaths)

	// #nosec G204 -- dockerPath is resolved from the allowlisted executable name "docker" via
	// fileutil.ResolveExecutablePath. gitRoot comes from git rev-parse (trusted) and is validated
	// as an absolute path before being turned into a docker -v mount. zizmorImageRef is the pinned
	// image constant validated by validateDockerImageRef. containerPaths are derived from
	// filepath.Rel(gitRoot, lockFile), confined to the repository root, and prefixed with "./" to
	// prevent option injection. exec.Command passes args directly to the OS (no shell).
	return exec.Command(dockerPath, dockerArgs...), relPaths, dockerArgs, nil
}

// runZizmorOnFiles runs the zizmor security scanner on one or more .lock.yml files using Docker
func runZizmorOnFiles(lockFiles []string, verbose bool, strict bool) error {
	if len(lockFiles) == 0 {
		return nil
	}

	zizmorLog.Printf("Running zizmor security scanner on %d file(s): %v (verbose=%t, strict=%t)", len(lockFiles), lockFiles, verbose, strict)

	cmd, relPaths, dockerArgs, err := buildZizmorCommand(lockFiles)
	if err != nil {
		return err
	}

	// Always show that zizmor is running (regular verbosity)
	if len(lockFiles) == 1 {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Running zizmor security scanner on "+relPaths[0]))
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage(fmt.Sprintf("Running zizmor security scanner on %d files", len(lockFiles))))
	}

	// In verbose mode, also show the command that users can run directly
	if verbose {
		dockerCmd := shellJoinArgs(append([]string{"docker"}, dockerArgs...))
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Run zizmor directly: "+dockerCmd))
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the command
	runErr := cmd.Run()

	// Parse and reformat the output, get total warning count and high severity count
	totalWarnings, highSeverityCount, parseErr := parseAndDisplayZizmorOutput(stdout.String(), stderr.String(), verbose)
	if parseErr != nil {
		zizmorLog.Printf("Failed to parse zizmor output: %v", parseErr)
		// Fall back to showing raw output
		if stdout.Len() > 0 {
			fmt.Fprint(os.Stderr, stdout.String())
		}
		if stderr.Len() > 0 {
			fmt.Fprint(os.Stderr, stderr.String())
		}
	}

	fileDescription := "workflows"
	if len(lockFiles) == 1 {
		fileDescription = filepath.Base(lockFiles[0])
	}

	return interpretZizmorRunError(runErr, totalWarnings, highSeverityCount, fileDescription, strict)
}

// runZizmorOnFile runs the zizmor security scanner on a single .lock.yml file using Docker
// This is a wrapper around runZizmorOnFiles for backward compatibility
func runZizmorOnFile(lockFile string, verbose bool, strict bool) error {
	zizmorLog.Printf("Running zizmor security scanner: file=%s, strict=%v", lockFile, strict)
	return runZizmorOnFiles([]string{lockFile}, verbose, strict)
}

// parseAndDisplayZizmorOutput parses zizmor JSON output and displays it in the desired format
// Returns the total number of warnings found and the number of high/critical severity findings
func parseAndDisplayZizmorOutput(stdout, stderr string, verbose bool) (int, int, error) { //nolint:largefunc
	// Map findings to files for detailed display
	fileFindings := make(map[string][]zizmorFinding)

	// Parse stderr for "completed" messages to get list of files
	completedFiles := []string{}
	scanner := bufio.NewScanner(strings.NewReader(stderr))
	for scanner.Scan() {
		line := scanner.Text()
		// Look for lines like: " INFO audit: zizmor: 🌈 completed ./.github/workflows/pdf-summary.lock.yml"
		if strings.Contains(line, "INFO audit: zizmor: 🌈 completed") {
			parts := strings.Split(line, "completed ")
			if len(parts) == 2 {
				filePath := strings.TrimSpace(parts[1])
				completedFiles = append(completedFiles, filePath)
				// Initialize empty findings slice
				if _, exists := fileFindings[filePath]; !exists {
					fileFindings[filePath] = []zizmorFinding{}
				}
			}
		}
	}

	// Parse JSON findings from stdout
	var findings []zizmorFinding
	totalWarnings := 0
	highSeverityCount := 0
	if stdout != "" && strings.HasPrefix(strings.TrimSpace(stdout), "[") {
		if err := json.Unmarshal([]byte(stdout), &findings); err != nil {
			return 0, 0, fmt.Errorf("failed to parse zizmor JSON output: %w", err)
		}

		// Organize findings by file
		for _, finding := range findings {
			// Track which files this finding affects (avoid duplicates)
			affectedFiles := make(map[string]struct {
			})
			for _, location := range finding.Locations {
				filePath := location.Symbolic.Key.Local.GivenPath
				if filePath == "" {
					filePath = location.Symbolic.Key.Local.VerbatimPath
				}
				if filePath != "" && !setutil.Contains(affectedFiles, filePath) {
					affectedFiles[filePath] = struct {
					}{}
					fileFindings[filePath] = append(fileFindings[filePath], finding)
					totalWarnings++
					if scanfindings.ParseSeverity(finding.Determinations.Severity).AtLeast(scanfindings.SeverityHigh) {
						highSeverityCount++
					}
				}
			}
		}
	}

	// Build the ordered list of files to display findings for.
	// Preserve the stderr "completed" ordering first, then append (in sorted order)
	// any finding paths absent from that list.  This handles two failure modes:
	//  (a) the zizmor Docker image changes its log format and no "completed"
	//      lines are emitted at all — completedFiles stays empty and we fall
	//      back entirely to sorted fileFindings keys.
	//  (b) the log format is partially intact — some "completed" lines arrive
	//      but not all — so findings for the unlisted files would otherwise be
	//      silently dropped.
	listedSet := make(map[string]struct{}, len(completedFiles))
	for _, fp := range completedFiles {
		listedSet[fp] = struct{}{}
	}
	var extraFiles []string
	for fp := range fileFindings {
		if _, seen := listedSet[fp]; !seen {
			extraFiles = append(extraFiles, fp)
		}
	}
	sort.Strings(extraFiles)
	displayFiles := append(completedFiles, extraFiles...)

	// Display reformatted output for each file with findings
	for _, filePath := range displayFiles {
		findings := fileFindings[filePath]
		count := len(findings)

		// Skip files with 0 warnings
		if count == 0 {
			continue
		}

		// Read file content for context display
		fileContent, err := os.ReadFile(filePath)
		var fileLines []string
		if err == nil {
			fileLines = strings.Split(string(fileContent), "\n")
		}

		// Display detailed findings using the shared finding representation
		scanfindings.Render(os.Stderr, zizmorFindingsToShared(filePath, findings, fileLines))
	}

	return totalWarnings, highSeverityCount, nil
}

// zizmorFindingsToShared maps zizmor's native findings onto the shared finding
// representation used by every scanner integration.
func zizmorFindingsToShared(filePath string, findings []zizmorFinding, fileLines []string) []scanfindings.Finding {
	shared := make([]scanfindings.Finding, 0, len(findings))
	for _, finding := range findings {
		// Use the primary location (first location in the list)
		if len(finding.Locations) == 0 {
			continue
		}
		loc := finding.Locations[0]
		// Zizmor uses 0-based indexing, convert to 1-based for user display
		lineNum := loc.Concrete.Location.StartPoint.Row + 1
		colNum := loc.Concrete.Location.StartPoint.Column + 1

		// Build message with URL link if available
		message := scanfindings.FormatMessage(finding.Determinations.Severity, finding.Ident, finding.Desc)
		if finding.URL != "" {
			message = fmt.Sprintf("%s (%s)", message, finding.URL)
		}

		shared = append(shared, scanfindings.Finding{
			RuleID:   finding.Ident,
			Severity: scanfindings.ParseSeverity(finding.Determinations.Severity),
			Message:  message,
			File:     filePath,
			Line:     lineNum,
			Column:   colNum,
			Context:  scanfindings.ContextLines(fileLines, lineNum),
		})
	}
	return shared
}
