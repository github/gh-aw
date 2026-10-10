// This file provides shellcheck integration for workflow run step linting.
//
// It extracts run: step scripts from compiled lock files and runs shellcheck
// on each shell snippet, reporting issues and ignoring known false positives
// introduced by GitHub Actions expression syntax.
//
// # Key Functions
//
//   - runShellcheckOnLockFilesAndResources() - Run shellcheck on lock files and explicit shell resources
//   - extractRunStepsFromLockFile() - Parse a lock file and extract run step info
//   - isShellcheckAvailable() - Check whether the shellcheck binary is in PATH
//   - isShellcheckableShell() - True for bash/sh steps; false for pwsh/python/etc.

package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/goccy/go-yaml"
)

var shellcheckLog = logger.New("cli:shellcheck")
var dockerCommandContext = exec.CommandContext

// ghaExpressionRE matches GitHub Actions ${{ ... }} expression syntax so it can
// be replaced with a shell-safe placeholder before linting.  The (?s) flag lets
// '.' cross newline boundaries; the non-greedy *? stops at the earliest '}}'.
var ghaExpressionRE = regexp.MustCompile(`(?s)\$\{\{.*?\}\}`)

// sanitizeGHAExpressions replaces every ${{ ... }} GitHub Actions expression in
// script with the identifier __GHA_EXPR__.  This prevents shellcheck from
// generating spurious parse errors (e.g. SC1073, SC1083) caused by the
// otherwise-invalid dollar-brace-brace substitution syntax.
func sanitizeGHAExpressions(script string) string {
	return ghaExpressionRE.ReplaceAllString(script, "__GHA_EXPR__")
}

// shellcheckDefaultIgnoreCodes lists SC error codes that are false positives
// in GitHub Actions run: scripts and are always suppressed.
//
// Rationale for each code:
//
//	SC2016: "${{ }}" GitHub Actions expression syntax appears in single-quoted
//	        strings which shellcheck flags as unexpanded variable references.
//	        sanitizeGHAExpressions handles most occurrences, but this code is
//	        retained as a safety net for any edge cases the regex may miss.
//	SC1090: "Can't follow non-constant source" – scripts are downloaded and
//	        sourced dynamically at runtime; the source path is not resolvable at
//	        lint time.
//	SC1091: "Not following: shell file doesn't exist" – same reason as SC1090.
//	SC2002: "Useless cat" – style-only advice that does not affect script
//	        correctness and is common in readable workflow pipelines.
//	SC2129: "Consider using { cmd1; cmd2; } >> file" – style note about
//	        consecutive redirects; GITHUB_OUTPUT and GITHUB_STEP_SUMMARY commonly
//	        require individual echo >> appends to handle conditional branches.
//	SC2153: "Possible misspelling: VAR may not be assigned" – env: step-level
//	        variables are set by the GHA runner and are invisible to shellcheck;
//	        this fires as a false positive for every uppercase env: variable.
//	SC2154: "variable is referenced but not assigned" – common false positive in
//	        generated GHA scripts where variables are assigned inside trap strings
//	        (e.g., `trap 'var=$?; ...; echo "$var"' EXIT`) or set by the Actions
//	        runner environment. ShellCheck does not trace assignments inside trap
//	        body strings.
var shellcheckDefaultIgnoreCodes = []string{"SC2016", "SC1090", "SC1091", "SC2002", "SC2129", "SC2153", "SC2154"}

// shellcheckMaxConcurrency is the maximum number of shellcheck processes (or
// Docker containers) that may run simultaneously. It bounds resource usage on
// large workflow sets that would otherwise exhaust process or file-descriptor
// limits.
const shellcheckMaxConcurrency = 8

// runStepInfo captures the information from a single run: step in a lock file
// that is needed to run shellcheck on the script snippet.
type runStepInfo struct {
	// Name is the step's "name" field, used only for diagnostic messages.
	Name string
	// Script is the raw content of the run: field.
	Script string
	// Shell is the effective shell for the step, resolved from the step field,
	// job-level defaults, and workflow-level defaults (in that priority order).
	// An empty string means the GitHub Actions default (bash on Linux runners).
	Shell string
	// LockFile is the absolute path of the lock file that contains this step.
	// For frontmatter-declared resources (which have no lock file), this is
	// empty and Source is used for diagnostic labeling instead.
	LockFile string
	// Source identifies the origin of a frontmatter-declared resource (e.g.
	// the workflow ID or evaluator file path) for diagnostic messages. Empty
	// for steps extracted from a lock file, which use LockFile instead.
	Source string
}

// isShellcheckAvailable returns true when the shellcheck binary can be found in PATH.
func isShellcheckAvailable() bool {
	_, err := exec.LookPath("shellcheck")
	return err == nil
}

// isShellcheckableShell returns true for shell values that shellcheck can lint.
// GitHub Actions supports bash (default), sh, pwsh, powershell, python, and
// custom shells. Only bash and sh are valid targets for shellcheck.
func isShellcheckableShell(shell string) bool {
	if shell == "" || strings.EqualFold(shell, "bash") {
		// Empty means GitHub Actions default (bash on Linux/macOS runners).
		return true
	}
	return strings.EqualFold(shell, "sh")
}

// shellcheckShell returns the value to pass to shellcheck's --shell flag.
// When shell is empty the GitHub Actions default (bash) is used.
func shellcheckShell(shell string) string {
	if strings.EqualFold(shell, "sh") {
		return "sh"
	}
	return "bash"
}

// resolveDefaultShell extracts the shell value from a defaults.run block, if
// present.  It returns the shell string (e.g. "bash", "pwsh") or "" when the
// block is absent or the shell field is not set.
func resolveDefaultShell(defaults map[string]any) string {
	if defaults == nil {
		return ""
	}
	run, ok := defaults["run"].(map[string]any)
	if !ok {
		return ""
	}
	shell, _ := run["shell"].(string)
	return shell
}

// extractRunStepsFromJob returns all run: steps in a single job whose
// effective shell is lintable by shellcheck. jobDefaultShell is the shell
// resolved from the job's (or workflow's) defaults.run.shell, used when a
// step does not set its own "shell" field.
func extractRunStepsFromJob(job map[string]any, jobDefaultShell, lockFile string) []runStepInfo {
	var steps []runStepInfo
	rawSteps, ok := job["steps"].([]any)
	if !ok {
		return steps
	}
	for _, stepData := range rawSteps {
		step, ok := stepData.(map[string]any)
		if !ok {
			continue
		}
		runScript, ok := step["run"].(string)
		if !ok || runScript == "" {
			continue
		}

		shell, _ := step["shell"].(string)
		effectiveShell := shell
		if effectiveShell == "" {
			effectiveShell = jobDefaultShell
		}

		if !isShellcheckableShell(effectiveShell) {
			continue
		}
		name, _ := step["name"].(string)
		steps = append(steps, runStepInfo{
			Name:     name,
			Script:   runScript,
			Shell:    effectiveShell,
			LockFile: lockFile,
		})
	}
	return steps
}

// extractRunStepsFromLockFile parses a compiled lock file and returns all
// run: steps whose effective shell is lintable by shellcheck.
//
// The effective shell for a step is resolved in priority order:
//  1. Step-level "shell" field
//  2. Job-level "defaults.run.shell"
//  3. Workflow-level "defaults.run.shell"
//  4. Empty string (GitHub Actions default: bash on Linux/macOS runners)
func extractRunStepsFromLockFile(lockFile string) ([]runStepInfo, error) {
	shellcheckLog.Printf("Extracting run steps from %s", lockFile)

	content, err := os.ReadFile(lockFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read lock file %s: %w", lockFile, err)
	}

	var workflowYAML map[string]any
	if err := yaml.Unmarshal(content, &workflowYAML); err != nil {
		return nil, fmt.Errorf("failed to parse YAML in %s: %w", lockFile, err)
	}

	var steps []runStepInfo

	jobs, ok := workflowYAML["jobs"].(map[string]any)
	if !ok {
		return steps, nil
	}

	// Resolve workflow-level default shell (lowest priority).
	workflowDefaultShell := resolveDefaultShell(func() map[string]any {
		d, _ := workflowYAML["defaults"].(map[string]any)
		return d
	}())

	for _, jobData := range jobs {
		job, ok := jobData.(map[string]any)
		if !ok {
			continue
		}

		// Resolve job-level default shell, falling back to workflow default.
		jobDefaultShell := workflowDefaultShell
		if jobDefaults, ok := job["defaults"].(map[string]any); ok {
			if s := resolveDefaultShell(jobDefaults); s != "" {
				jobDefaultShell = s
			}
		}

		steps = append(steps, extractRunStepsFromJob(job, jobDefaultShell, lockFile)...)
	}

	shellcheckLog.Printf("Found %d shellcheckable run steps in %s", len(steps), lockFile)
	return steps, nil
}

func writeTempScriptFile(script string) (string, error) {
	// Sanitize GitHub Actions ${{ ... }} expressions before writing the script.
	sanitizedScript := sanitizeGHAExpressions(script)

	tmpFile, err := os.CreateTemp("", "gh-aw-shellcheck-*.sh")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file for shellcheck: %w", err)
	}
	if _, err := tmpFile.WriteString(sanitizedScript); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write shellcheck temp file: %w", err)
	}
	tmpFile.Close()
	return tmpFile.Name(), nil
}

// runShellcheckOnScript writes script to a temporary file and invokes shellcheck.
// It returns any findings as a byte slice (ready to write to stderr) and a
// non-nil error when shellcheck reports one or more issues. Callers are
// responsible for writing the returned output to stderr so that concurrent
// calls do not interleave their diagnostic blocks.
func runShellcheckOnScript(info runStepInfo, ignoreCodes []string, verbose bool) ([]byte, error) {
	shellcheckLog.Printf("Running shellcheck on step %q (shell=%s)", info.Name, info.Shell)

	tmpPath, err := writeTempScriptFile(info.Script)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpPath)

	args := []string{
		"--shell=" + shellcheckShell(info.Shell),
		"--format=gcc",
	}
	for _, code := range ignoreCodes {
		args = append(args, "--exclude="+code)
	}
	args = append(args, tmpPath)

	if verbose {
		shellcheckLog.Printf("Invoking: shellcheck %s", strings.Join(args, " "))
	}

	// #nosec G204 -- shellcheck is a trusted system binary; args are built
	// from controlled values (shell name and SC codes). The temp file path is
	// OS-generated and not user-controlled.
	cmd := exec.Command("shellcheck", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmdErr := cmd.Run()

	// Replace the temp file path with "script" so that reported line numbers
	// are clearly relative to the run: script snippet rather than the lock
	// file.  The enclosing header message ("shellcheck findings in <lock>")
	// already identifies the originating file and step.
	findings := strings.ReplaceAll(stdout.String(), tmpPath, "script")
	if stderr.Len() > 0 {
		findings += strings.ReplaceAll(stderr.String(), tmpPath, "script")
	}

	var out bytes.Buffer
	if findings != "" {
		fmt.Fprintf(&out, "%s\n", console.FormatWarningMessage("shellcheck findings in "+stepLabel(info)+":"))
		fmt.Fprint(&out, findings)
	}

	if cmdErr != nil {
		var exitErr *exec.ExitError
		if errors.As(cmdErr, &exitErr) && exitErr.ExitCode() == 1 {
			// Exit code 1 means shellcheck found issues; already printed above.
			return out.Bytes(), fmt.Errorf("shellcheck found issues in %s", stepLabel(info))
		}
		return out.Bytes(), fmt.Errorf("shellcheck failed: %w", cmdErr)
	}

	return out.Bytes(), nil
}

func stepLabel(info runStepInfo) string {
	if info.LockFile == "" {
		source := info.Source
		if source == "" {
			source = "(unknown)"
		}
		if info.Name != "" {
			return source + " (step: " + info.Name + ")"
		}
		return source
	}
	if info.Name != "" {
		return filepath.Base(info.LockFile) + " (step: " + info.Name + ")"
	}
	return filepath.Base(info.LockFile)
}

// runShellcheckOnScriptViaDocker runs shellcheck on a script snippet using the Docker
// container image (ShellcheckImage). The script is piped to the container via stdin,
// so no temporary file or volume mount is required.
//
// This is the fallback path when the shellcheck binary is not installed locally
// (e.g. on Windows). It mirrors the behaviour of runShellcheckOnScript: findings are
// returned as a byte slice and an error is returned when shellcheck reports issues.
// Callers are responsible for writing the returned output to stderr so that concurrent
// calls do not interleave their diagnostic blocks.
func runShellcheckOnScriptViaDocker(ctx context.Context, info runStepInfo, ignoreCodes []string, verbose bool) ([]byte, error) {
	shellcheckLog.Printf("Running shellcheck via Docker on step %q (shell=%s)", info.Name, info.Shell)

	var out bytes.Buffer

	sanitizedScript := sanitizeGHAExpressions(info.Script)

	args := []string{
		"run",
		"--rm",
		"-i",
		ShellcheckImage,
		"--shell=" + shellcheckShell(info.Shell),
		"--format=gcc",
	}
	for _, code := range ignoreCodes {
		args = append(args, "--exclude="+code)
	}
	args = append(args, "-") // read script from stdin

	if verbose {
		shellcheckLog.Printf("Invoking: docker %s", strings.Join(args, " "))
	}

	// #nosec G204 -- ShellcheckImage is a SHA-pinned constant; all other args are
	// built from controlled values (shell name, SC codes, and the literal "-").
	cmd := dockerCommandContext(ctx, "docker", args...)
	cmd.Stdin = strings.NewReader(sanitizedScript)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// In gcc format with stdin, shellcheck prefixes findings with "-:LINE:COL: ...".
	// Replace the leading "-:" (stdin indicator) with "script:" so that reported
	// positions are clearly relative to the run: script snippet.
	findings := strings.ReplaceAll(stdout.String(), "-:", "script:")
	if findings != "" {
		fmt.Fprintf(&out, "%s\n", console.FormatWarningMessage("shellcheck findings in "+stepLabel(info)+":"))
		fmt.Fprint(&out, findings)
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() == 1 {
				// Exit code 1 means shellcheck found issues; already printed above.
				return out.Bytes(), fmt.Errorf("shellcheck found issues in %s", stepLabel(info))
			}
		}
		stderrText := strings.TrimSpace(strings.ReplaceAll(stderr.String(), "-:", "script:"))
		if stderrText != "" {
			return out.Bytes(), fmt.Errorf("shellcheck (docker) failed: %w: %s", err, stderrText)
		}
		return out.Bytes(), fmt.Errorf("shellcheck (docker) failed: %w", err)
	}

	return out.Bytes(), nil
}

// runShellcheckOnLockFilesAndResources extracts run: steps from each lock file,
// combines them with explicit shell resources, and runs shellcheck on the shell
// snippets in parallel. It uses shellcheckDefaultIgnoreCodes to suppress known
// false positives from GitHub Actions expression syntax.
//
// When the shellcheck binary is not installed, it falls back to the Docker
// container (ShellcheckImage) if Docker is available. This allows shellcheck
// to run on systems without a native shellcheck installation (e.g. Windows).
// The fallback is lazy: the Docker image is only invoked when there are scripts
// to lint and the binary is absent.
//
// Steps across all lock files are collected first, then run in parallel using
// goroutines bounded by shellcheckMaxConcurrency. Each goroutine captures its
// diagnostic output; after all goroutines finish the output is written to
// stderr in step order so diagnostic blocks are never interleaved.
//
// In both strict and non-strict modes every step is checked before returning.
// When strict is true, any step failure causes a non-nil error to be returned
// after all steps have been checked (reports-all-then-errors). In non-strict
// mode, all failures are printed as warnings and nil is returned.
func runShellcheckOnLockFilesAndResources(ctx context.Context, lockFiles []string, resources []workflow.ShellScriptResource, verbose bool, strict bool) error {
	if len(lockFiles) == 0 && len(resources) == 0 {
		return nil
	}

	useDocker, available := shellcheckDockerFallback(ctx)
	if !available {
		return nil
	}

	allSteps := collectShellcheckSteps(lockFiles, resources)
	if len(allSteps) == 0 {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Running shellcheck on run steps (0 run steps found in lock files)"))
		return nil
	}
	return runShellcheckOnSteps(ctx, allSteps, lockFiles, verbose, strict, useDocker)
}

func shellcheckDockerFallback(ctx context.Context) (useDocker bool, available bool) {
	if isShellcheckAvailable() {
		return false, true
	}
	if !IsDockerAvailable(ctx) {
		shellcheckLog.Print("shellcheck binary not found and Docker is unavailable; skipping run step linting")
		return false, false
	}
	shellcheckLog.Print("shellcheck binary not found in PATH; using Docker container fallback")
	return true, true
}

func collectShellcheckSteps(lockFiles []string, resources []workflow.ShellScriptResource) []runStepInfo {
	var steps []runStepInfo
	for _, lockFile := range lockFiles {
		lockSteps, err := extractRunStepsFromLockFile(lockFile)
		if err != nil {
			shellcheckLog.Printf("Failed to extract run steps from %s: %v", lockFile, err)
			fmt.Fprintf(os.Stderr, "%s\n", console.FormatWarningMessage("shellcheck: could not parse "+filepath.Base(lockFile)+": "+err.Error()))
			continue
		}
		steps = append(steps, lockSteps...)
	}
	for _, resource := range resources {
		if resource.Script == "" || !isShellcheckableShell(resource.Shell) {
			continue
		}
		steps = append(steps, runStepInfo{
			Name:   resource.Name,
			Script: resource.Script,
			Shell:  resource.Shell,
			Source: resource.Source,
		})
	}
	return steps
}

func runShellcheckOnSteps(ctx context.Context, allSteps []runStepInfo, lockFiles []string, verbose bool, strict bool, useDocker bool) error {
	shellcheckLog.Printf("Running shellcheck on %d run step resource(s) from %d lock file(s) (strict=%t, docker=%t)", len(allSteps), len(lockFiles), strict, useDocker)
	if len(lockFiles) == 1 {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage("Running shellcheck on run steps in "+filepath.Base(lockFiles[0])))
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage(fmt.Sprintf("Running shellcheck on %d run step resources", len(allSteps))))
	}
	results := runShellcheckWorkers(ctx, allSteps, verbose, useDocker)
	return reportShellcheckResults(allSteps, results, strict)
}

type shellcheckResult struct {
	err    error
	output []byte
}

func runShellcheckWorkers(ctx context.Context, steps []runStepInfo, verbose bool, useDocker bool) []shellcheckResult {
	results := make([]shellcheckResult, len(steps))
	sem := make(chan struct{}, shellcheckMaxConcurrency)
	var wg sync.WaitGroup
	for i, step := range steps {
		wg.Add(1)
		go func(idx int, s runStepInfo) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					results[idx] = shellcheckResult{
						err: fmt.Errorf("shellcheck worker panic in step %q: %v", s.Name, r),
					}
				}
			}()
			// Acquire semaphore slot; abort if context is cancelled while waiting.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			var stepOutput []byte
			var stepErr error
			if useDocker {
				stepOutput, stepErr = runShellcheckOnScriptViaDocker(ctx, s, shellcheckDefaultIgnoreCodes, verbose)
			} else {
				stepOutput, stepErr = runShellcheckOnScript(s, shellcheckDefaultIgnoreCodes, verbose)
			}
			results[idx] = shellcheckResult{err: stepErr, output: stepOutput}
		}(i, step)
	}
	wg.Wait()
	return results
}

func reportShellcheckResults(steps []runStepInfo, results []shellcheckResult, strict bool) error {
	var totalIssues int
	var firstErr error
	for i, r := range results {
		if len(r.output) > 0 {
			os.Stderr.Write(r.output) //nolint:errcheck
		}
		if r.err != nil {
			totalIssues++
			shellcheckLog.Printf("shellcheck issue in step %q: %v", steps[i].Name, r.err)
			if firstErr == nil {
				firstErr = r.err
			}
		}
	}

	shellcheckLog.Printf("shellcheck complete: steps=%d, issues=%d", len(steps), totalIssues)

	if strict && firstErr != nil {
		return fmt.Errorf("strict mode: shellcheck found issues in run steps: %w", firstErr)
	}
	return nil
}
