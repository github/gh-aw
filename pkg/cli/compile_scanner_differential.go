package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/scanfindings"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
)

type scannerDiagnostic struct {
	Finding  scanfindings.Finding
	Identity string
	Script   *ValidationScriptLocation
}

// scannerFindingsError distinguishes findings from execution and parsing failures.
type scannerFindingsError struct {
	err         error
	diagnostics []scannerDiagnostic
}

func (e *scannerFindingsError) Error() string { return e.err.Error() }
func (e *scannerFindingsError) Unwrap() error { return e.err }

func withScannerFindings(err error, findings []scanfindings.Finding) error {
	if err == nil || len(findings) == 0 {
		return err
	}
	diagnostics := make([]scannerDiagnostic, 0, len(findings))
	sources := make(scannerSources)
	for _, finding := range findings {
		diagnostics = append(diagnostics, scannerDiagnostic{
			Finding: finding, Identity: sources.findingIdentity(finding),
		})
	}
	return &scannerFindingsError{err: err, diagnostics: diagnostics}
}

type dryRunScannerBaseline struct {
	results map[string]error
}

// DryRunScannerDifferential reports the finding-level comparison used by the gate.
type DryRunScannerDifferential struct {
	Baseline int `json:"baseline"`
	Existing int `json:"existing"`
	Added    int `json:"added"`
	Ignored  int `json:"ignored"`
}

func (b *dryRunScannerBaseline) filter(tool string, err error) (*DryRunScannerDifferential, error) {
	baseline, baselineErr := collectScannerDiagnostics(b.results[tool])
	if baselineErr != nil {
		return nil, errors.Join(fmt.Errorf("%s baseline scan failed: %w", tool, baselineErr), err)
	}
	current, currentErr := collectScannerDiagnostics(err)
	if currentErr != nil {
		var actionable []scannerDiagnostic
		for _, diagnostic := range current {
			if diagnostic.Finding.Severity != scanfindings.SeverityLow &&
				diagnostic.Finding.Severity != scanfindings.SeverityInfo {
				actionable = append(actionable, diagnostic)
			}
		}
		if len(actionable) > 0 {
			currentErr = errors.Join(currentErr, &scannerFindingsError{
				err:         fmt.Errorf("%s found %d actionable finding(s) while scanner execution also failed", tool, len(actionable)),
				diagnostics: actionable,
			})
		}
		return nil, currentErr
	}
	diff := &DryRunScannerDifferential{Baseline: len(baseline)}
	counts := make(map[string]int)
	for _, diagnostic := range baseline {
		counts[diagnostic.Identity]++
	}
	var actionable []scannerDiagnostic
	for _, diagnostic := range current {
		finding := diagnostic.Finding
		if finding.Severity == scanfindings.SeverityLow || finding.Severity == scanfindings.SeverityInfo {
			diff.Ignored++
		} else if counts[diagnostic.Identity] > 0 {
			counts[diagnostic.Identity]--
			diff.Existing++
			actionable = append(actionable, diagnostic)
		} else {
			diff.Added++
			actionable = append(actionable, diagnostic)
		}
	}
	if len(actionable) > 0 {
		return diff, &scannerFindingsError{
			err:         fmt.Errorf("%s found %d medium-or-higher or unclassified finding(s) (%d existing, %d added)", tool, len(actionable), diff.Existing, diff.Added),
			diagnostics: actionable,
		}
	}
	return diff, nil
}

func collectScannerDiagnostics(err error) ([]scannerDiagnostic, error) {
	if err == nil {
		return nil, nil
	}
	var findings *scannerFindingsError
	if errors.As(err, &findings) && err == findings { //nolint:errorlint // Match this node only; errors.Is would also match a joined descendant.
		if len(findings.diagnostics) == 0 {
			return nil, errors.New("scanner reported findings without usable diagnostics")
		}
		return findings.diagnostics, nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return nil, err
		}
		var diagnostics []scannerDiagnostic
		var failures []error
		for _, child := range children {
			findings, failure := collectScannerDiagnostics(child)
			diagnostics = append(diagnostics, findings...)
			failures = append(failures, failure)
		}
		return diagnostics, errors.Join(failures...)
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if child := wrapped.Unwrap(); child != nil {
			diagnostics, failure := collectScannerDiagnostics(child)
			if failure != nil {
				return diagnostics, err
			}
			return diagnostics, nil
		}
	}
	return nil, err
}

func prepareDryRunScannerBaseline(ctx context.Context, config *CompileConfig, files []string, workflowDir string, compiler *workflow.Compiler) error {
	if !config.DryRun {
		return nil
	}
	baseline := &dryRunScannerBaseline{results: make(map[string]error)}
	config.dryRunBaseline = baseline
	workflowDir = resolveDryRunWorkflowDir(files, workflowDir)
	config.dryRunWorkflowDir = workflowDir
	lockFiles, shellcheckResources, err := collectDryRunBaselineInputs(files, config, compiler)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Scanning pre-compilation inputs for the dry-run warning baseline"))
	baselineConfig := *config
	baselineConfig.DryRun = false
	baselineConfig.Strict = true
	baselineConfig.JSONOutput = true
	opts := batchToolsOptions{
		workflowDir: workflowDir, dryRunZizmor: true,
		lockFilesForActionlint: lockFiles, lockFilesForZizmor: lockFiles,
		lockFilesForDirTools: lockFiles, lockFilesForSyft: lockFiles,
		lockFilesForGrype: lockFiles, lockFilesForGrant: lockFiles,
		lockFilesForYamllint: lockFiles, lockFilesForShellcheck: lockFiles,
		shellcheckResources: shellcheckResources,
		reportResult:        func(tool string, _ int, err error) { baseline.results[tool] = err },
	}
	var results []ValidationResult
	_, err = runBatchExternalTools(ctx, baselineConfig, opts, &CompilationStats{}, &results)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Findings and scanner failures are retained separately for the final gate.
	compileDevelopmentLog.Printf("Captured dry-run scanner baseline: tools=%d, diagnostics=%t", len(baseline.results), err != nil)
	return nil
}

func resolveDryRunWorkflowDir(files []string, workflowDir string) string {
	if workflowDir != "" {
		return workflowDir
	}
	for _, file := range files {
		resolved, err := resolveWorkflowFile(file, false)
		if err == nil {
			return filepath.Dir(resolved)
		}
	}
	return ""
}

func collectDryRunBaselineInputs(files []string, config *CompileConfig, compiler *workflow.Compiler) ([]string, []workflow.ShellScriptResource, error) {
	var lockFiles []string
	for _, file := range files {
		resolved, err := resolveWorkflowFile(file, false)
		if err != nil {
			continue // The ordinary compile pipeline reports source resolution errors.
		}
		lockFile := stringutil.MarkdownToLockFile(resolved)
		info, err := os.Stat(lockFile)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("cannot read dry-run baseline %s: %w", lockFile, err)
		}
		if !info.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("dry-run baseline %s must be a regular lock file", lockFile)
		}
		lockFiles = append(lockFiles, lockFile)
	}
	if !config.shellcheckEnabled() || config.NoEmit {
		return lockFiles, nil, nil
	}
	for _, lockFile := range lockFiles {
		if _, err := extractRunStepsFromLockFile(lockFile); err != nil {
			return nil, nil, fmt.Errorf("cannot extract ShellCheck baseline from %s: %w", lockFile, err)
		}
	}
	var resources []workflow.ShellScriptResource
	originalRepositorySlug := compiler.GetRepositorySlug()
	defer compiler.SetRepositorySlug(originalRepositorySlug)
	for _, file := range files {
		resolved, err := resolveWorkflowFile(file, false)
		if err != nil {
			continue // The ordinary compile pipeline reports source resolution errors.
		}
		setWorkflowFileContext(compiler, resolved)
		data, err := compiler.ParseWorkflowFile(resolved)
		if err != nil {
			var sharedErr *workflow.SharedWorkflowError
			var redirectErr *workflow.RedirectOnlyWorkflowError
			if errors.As(err, &sharedErr) || errors.As(err, &redirectErr) {
				continue
			}
			return nil, nil, fmt.Errorf("cannot extract ShellCheck resource baseline from %s: %w", resolved, err)
		}
		resources = append(resources, data.ShellScriptResources()...)
	}
	return lockFiles, resources, nil
}

var scannerShellcheckRulePattern = regexp.MustCompile(`^SC\d+$`)

const actionlintShellcheckMessagePrefix = "shellcheck reported issue in this script: "

func normalizeScannerFile(file string) string {
	file = filepath.Clean(file)
	if isScannerWorkflowFile(file) {
		if !filepath.IsAbs(file) {
			if gitRoot, err := gitutil.FindGitRoot(); err == nil {
				file = filepath.Join(gitRoot, file)
			}
		}
		if abs, err := filepath.Abs(file); err == nil {
			file = abs
		}
		if resolved, err := filepath.EvalSymlinks(file); err == nil {
			file = resolved
		}
	}
	return file
}

func (sources scannerSources) findingIdentity(finding scanfindings.Finding) string {
	file := normalizeScannerFile(finding.File)
	anchor := strconv.Itoa(finding.Line) + ":" + strconv.Itoa(finding.Column)
	if len(finding.Locations) > 0 {
		var locations []string
		for _, location := range finding.Locations {
			locations = append(locations, sources.locationIdentity(location))
		}
		anchor = scannerValueDigest(fmt.Sprintf("%q", locations))
	} else if source := sources.load(file); source != nil {
		if location := source.locate(finding.Line, finding.Column); location != nil {
			anchor = source.identity(location, finding.Line)
		} else {
			anchor += "\x00" + source.digest
		}
	}
	return scannerValueDigest(fmt.Sprintf("%q", []string{file, finding.RuleID, finding.Severity.String(), finding.Message, anchor}))
}

func (sources scannerSources) locationIdentity(location scanfindings.SourceLocation) string {
	file := normalizeScannerFile(location.File)
	anchor := fmt.Sprintf("%d:%d", location.Line, location.Column)
	path := location.Path
	if source := sources.load(file); source != nil {
		if digest, found := source.nodes[path]; found && path != "" {
			anchor = digest
			path = source.pathIdentity(path)
		} else {
			anchor += "\x00" + source.digest
		}
	}
	return scannerValueDigest(fmt.Sprintf("%q", []string{file, location.Role, path, anchor}))
}

func (sources scannerSources) shellcheckIdentity(issue actionlintError) (string, bool) {
	message := strings.TrimPrefix(issue.Message, actionlintShellcheckMessagePrefix)
	rule, severity, line, column, ok := parseActionlintShellcheckMessage(message)
	if !ok {
		return "", false
	}
	file := normalizeScannerFile(issue.Filepath)
	source := sources.load(file)
	if source == nil {
		return "", false
	}
	location := source.locate(issue.Line, issue.Column)
	if location == nil || !location.run {
		return "", false
	}
	script, ok := location.value.(string)
	if !ok {
		return "", false
	}
	scriptLine, err := strconv.Atoi(line)
	if err != nil || scriptLine < 1 {
		return "", false
	}
	lines := strings.Split(script, "\n")
	var affectedLine string
	found := false
	for index, candidate := range lines {
		if index == scriptLine-1 {
			affectedLine = strings.TrimSpace(candidate)
			found = true
			break
		}
	}
	if !found {
		return "", false
	}
	for range 4 {
		_, message, _ = strings.Cut(message, ":")
	}
	identity := strings.Join([]string{
		file, "shellcheck", rule, severity, column, message,
		source.pathIdentity(location.path), affectedLine,
	}, "\x00")
	return identity, true
}

func parseActionlintShellcheckMessage(message string) (rule, severity, line, column string, ok bool) {
	message = strings.TrimPrefix(message, actionlintShellcheckMessagePrefix)
	rule, message, ok = strings.Cut(message, ":")
	if !ok || !scannerShellcheckRulePattern.MatchString(rule) {
		return "", "", "", "", false
	}
	severity, message, ok = strings.Cut(message, ":")
	if !ok || severity != "error" && severity != "warning" && severity != "info" && severity != "note" {
		return "", "", "", "", false
	}
	line, message, ok = strings.Cut(message, ":")
	if !ok {
		return "", "", "", "", false
	}
	column, _, ok = strings.Cut(message, ":")
	if !ok {
		return "", "", "", "", false
	}
	if value, err := strconv.Atoi(line); err != nil || value < 1 {
		return "", "", "", "", false
	}
	if value, err := strconv.Atoi(column); err != nil || value < 1 {
		return "", "", "", "", false
	}
	return rule, severity, line, column, true
}
