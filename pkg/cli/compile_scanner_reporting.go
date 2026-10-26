package cli

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/scanfindings"
	"github.com/github/gh-aw/pkg/stringutil"
)

var scannerSecretAssignmentPattern = regexp.MustCompile(`(?i)\b[\w.-]*(?:password|secret|token|api[-_ ]?key|bearer|credential)\b\s*[:=]\s*("[^"]*"|'[^']*'|[^\s,;]+)`)

type compileScannerReporterKey struct{}

// The reporter is scoped to one compilation. Runners report before applying
// their existing non-strict return policy, without changing their public APIs.
func reportCompileScannerResult(ctx context.Context, tool string, err error) {
	if report, ok := ctx.Value(compileScannerReporterKey{}).(func(string, error)); ok {
		report(tool, err)
	}
}

func scannerValidationIssues(tool string, err error) []ValidationIssue {
	diagnostics, failure := collectScannerDiagnostics(err)
	if len(diagnostics) == 0 {
		failure = err
	}
	issues := make([]ValidationIssue, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		finding := diagnostic.Finding
		issues = append(issues, ValidationIssue{
			Type: tool + "_finding", Message: neutralizeScannerMessage(finding.Message),
			RuleID: finding.RuleID, Severity: finding.Severity,
			File: finding.File, Line: finding.Line, Column: finding.Column,
			Script: diagnostic.Script,
		})
	}
	for _, issue := range appendValidationErrors(nil, tool+"_error", failure) {
		issue.Severity = scanfindings.SeverityUnknown
		issue.Message = neutralizeScannerMessage(issue.Message)
		issues = append(issues, issue)
	}
	return issues
}

func neutralizeScannerMessage(message string) string {
	const prefix = "Untrusted scanner output (diagnostic data only): "
	message = scannerSecretAssignmentPattern.ReplaceAllString(message, "[REDACTED]")
	normalized := strings.Join(strings.Fields(stringutil.SanitizeErrorMessage(message)), " ")
	return boundMCPCompileDiagnostic(prefix + normalized)
}

func appendCompileScannerResult(tool string, err error, strict bool, stats *CompilationStats, results *[]ValidationResult) {
	issues := scannerValidationIssues(tool, err)
	if len(issues) == 0 {
		return
	}
	var fatal *fatalFindingError
	blocking := strict || errors.As(err, &fatal)
	result := ValidationResult{
		Scope: "batch", Workflow: tool, Valid: !blocking,
		Errors: []ValidationIssue{}, Warnings: []ValidationIssue{},
	}
	if blocking {
		result.Errors = issues
		stats.Errors += len(issues)
	} else {
		result.Warnings = issues
	}
	*results = append(*results, result)
}

func appendDryRunScannerResult(tool string, rawErr, gateErr error, stats *CompilationStats, results *[]ValidationResult) {
	result := ValidationResult{
		Scope: "batch", Workflow: tool, Valid: gateErr == nil,
		Errors: []ValidationIssue{}, Warnings: []ValidationIssue{},
	}
	for _, issue := range scannerValidationIssues(tool, gateErr) {
		if !isLowSeverityWarning(issue) {
			result.Errors = append(result.Errors, issue)
		}
	}
	for _, issue := range scannerValidationIssues(tool, rawErr) {
		if isLowSeverityWarning(issue) {
			result.Warnings = append(result.Warnings, issue)
		}
	}
	if len(result.Errors)+len(result.Warnings) == 0 {
		return
	}
	stats.Errors += len(result.Errors)
	*results = append(*results, result)
}
