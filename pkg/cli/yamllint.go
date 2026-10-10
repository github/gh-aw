package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/scanfindings"
)

var yamllintLog = logger.New("cli:yamllint")

// yamllintDefaultConfig is the inline yamllint configuration used for lock files.
// It disables rules that produce excessive noise on generated YAML output.
const yamllintDefaultConfig = `{extends: default, rules: {line-length: disable, document-start: disable, truthy: {check-keys: false}, comments: {require-starting-space: true, min-spaces-from-content: 1}}}`

// yamllintParsableRegex matches a single line of yamllint --format parsable output:
//
//	{file}:{line}:{col}: [{level}] {message} ({rule})
var yamllintParsableRegex = regexp.MustCompile(`^(.+):(\d+):(\d+): \[(error|warning)\] (.+) \(([^)]+)\)$`)

// runYamllintOnFiles runs yamllint on one or more .lock.yml files using Docker.
func runYamllintOnFiles(lockFiles []string, verbose bool, strict bool) error {
	if len(lockFiles) == 0 {
		return nil
	}

	yamllintLog.Printf("Running yamllint on %d file(s): %v (verbose=%t, strict=%t)", len(lockFiles), lockFiles, verbose, strict)

	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return fmt.Errorf("failed to find git root: %w", err)
	}

	if !filepath.IsAbs(gitRoot) {
		return fmt.Errorf("git root must be an absolute path, got: %s", gitRoot)
	}

	relPaths, err := buildYamllintContainerPaths(gitRoot, lockFiles)
	if err != nil {
		return err
	}

	// #nosec G204 -- gitRoot is validated as an absolute path (from git rev-parse, a trusted source).
	// relPaths are repository-relative paths that have been cleaned, validated to stay within
	// gitRoot, and prefixed with "./" to prevent option injection. exec.Command passes args
	// directly to the OS (no shell), preventing shell injection.
	// yamllintDefaultConfig is a compile-time constant with no user-controlled content.
	dockerArgs := buildYamllintDockerArgs(gitRoot, relPaths, strict)

	if len(lockFiles) == 1 {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Running yamllint on "+relPaths[0]))
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage(fmt.Sprintf("Running yamllint on %d files", len(lockFiles))))
	}

	if verbose {
		dockerCmd := buildYamllintVerboseCommand(gitRoot, relPaths, strict)
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Run yamllint directly: "+dockerCmd))
	}

	cmd := exec.Command("docker", dockerArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	totalIssues, parseErr := parseAndDisplayYamllintOutput(stdout.String())
	if parseErr != nil {
		yamllintLog.Printf("Failed to parse yamllint output: %v", parseErr)
		if stdout.Len() > 0 {
			fmt.Fprint(os.Stderr, stdout.String())
		}
		if stderr.Len() > 0 {
			fmt.Fprint(os.Stderr, stderr.String())
		}
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return classifyYamllintExit(exitErr.ExitCode(), strict, totalIssues, yamllintFileDescription(lockFiles))
		}
		return fmt.Errorf("yamllint failed: %w", err)
	}

	return nil
}

func buildYamllintDockerArgs(gitRoot string, relPaths []string, strict bool) []string {
	dockerArgs := []string{
		"run",
		"--rm",
		"-v", gitRoot + ":/workdir",
		"-w", "/workdir",
		YamllintImage,
		"-d", yamllintDefaultConfig,
		"--format", "parsable",
	}
	if strict {
		dockerArgs = append(dockerArgs, "--strict")
	}
	return append(dockerArgs, relPaths...)
}

func buildYamllintVerboseCommand(gitRoot string, relPaths []string, strict bool) string {
	args := append([]string{"docker"}, buildYamllintDockerArgs(gitRoot, relPaths, strict)...)
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, quoteYamllintShellArg(arg))
	}
	return strings.Join(quoted, " ")
}

func yamllintFileDescription(lockFiles []string) string {
	if len(lockFiles) == 1 {
		return filepath.Base(lockFiles[0])
	}
	return "workflows"
}

func classifyYamllintExit(exitCode int, strict bool, totalIssues int, fileDescription string) error {
	yamllintLog.Printf("yamllint exited with code %d (issues=%d)", exitCode, totalIssues)
	if exitCode == 1 || (exitCode == 2 && strict && totalIssues > 0) {
		if strict {
			return fmt.Errorf("strict mode: yamllint found %d issue(s) in %s - workflows must have no yamllint issues in strict mode", totalIssues, fileDescription)
		}
		return nil
	}
	return fmt.Errorf("yamllint failed with exit code %d on %s", exitCode, fileDescription)
}

func quoteYamllintShellArg(arg string) string {
	if arg != "" && !strings.ContainsAny(arg, " \t\n\"'`$\\;|&<>*?!#~") {
		return arg
	}
	return strconv.Quote(arg)
}

func buildYamllintContainerPaths(gitRoot string, lockFiles []string) ([]string, error) {
	relPaths := make([]string, 0, len(lockFiles))
	for _, lockFile := range lockFiles {
		absLockFile, err := filepath.Abs(lockFile)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve absolute path for %s: %w", lockFile, err)
		}
		relPath, err := filepath.Rel(gitRoot, absLockFile)
		if err != nil {
			return nil, fmt.Errorf("failed to get relative path for %s: %w", lockFile, err)
		}
		cleanRelPath := filepath.Clean(relPath)
		if cleanRelPath == ".." || strings.HasPrefix(cleanRelPath, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("yamllint file path %s is outside repository root", lockFile)
		}
		relPaths = append(relPaths, "./"+filepath.ToSlash(cleanRelPath))
	}
	return relPaths, nil
}

// parseAndDisplayYamllintOutput parses yamllint --format parsable output and displays findings.
// Returns the total number of issues found.
func parseAndDisplayYamllintOutput(stdout string) (int, error) {
	if strings.TrimSpace(stdout) == "" {
		return 0, nil
	}

	totalIssues := 0
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		issue, err := parseYamllintLine(line)
		if err != nil {
			yamllintLog.Printf("Failed to parse yamllint line %q: %v", line, err)
			fmt.Fprintf(os.Stderr, "%s\n", console.FormatWarningMessage("Failed to parse yamllint output line: "+line))
			fmt.Fprintln(os.Stderr, line)
			continue
		}

		totalIssues++

		scanfindings.Render(os.Stderr, []scanfindings.Finding{issue})
	}

	if err := scanner.Err(); err != nil {
		return totalIssues, fmt.Errorf("failed to scan yamllint output: %w", err)
	}

	return totalIssues, nil
}

// parseYamllintLine parses a single line of yamllint --format parsable output
// into the shared finding representation.
// Expected format: {file}:{line}:{col}: [{level}] {message} ({rule})
func parseYamllintLine(line string) (scanfindings.Finding, error) {
	matches := yamllintParsableRegex.FindStringSubmatch(line)
	if matches == nil {
		return scanfindings.Finding{}, fmt.Errorf("line does not match yamllint parsable format: %q", line)
	}

	lineNum, err := strconv.Atoi(matches[2])
	if err != nil {
		return scanfindings.Finding{}, fmt.Errorf("failed to parse line number %q: %w", matches[2], err)
	}

	colNum, err := strconv.Atoi(matches[3])
	if err != nil {
		return scanfindings.Finding{}, fmt.Errorf("failed to parse column number %q: %w", matches[3], err)
	}

	level := matches[4]
	rule := matches[6]

	return scanfindings.Finding{
		RuleID:   rule,
		Severity: scanfindings.ParseSeverity(level),
		Message:  fmt.Sprintf("[%s] %s (%s)", level, matches[5], rule),
		File:     matches[1],
		Line:     lineNum,
		Column:   colNum,
	}, nil
}
