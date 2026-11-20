package lint

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/taint"
)

// The lexical pass also covers unsupported interpreters and malformed
// expressions. Supplement it with the shared shell/output transport model.
func templateInjectionOutputFlows(ctx context.Context, input Input, diagnostics []Diagnostic) ([]Diagnostic, error) {
	input, hasTransport, err := outputFlowInput(ctx, input)
	if err != nil || !hasTransport {
		return diagnostics, err
	}
	rule := taint.Rule{
		ID: "template-injection", IsSource: templateInjectionSource, IsFilter: templateInjectionFilter,
		IsSink: func(site taint.Site) bool { return site.Kind == taint.ExecutableInterpolation },
		IsExecutableInput: func(site taint.Site) bool {
			key, _ := executableActionInput(site.Uses)
			return key != "" && key == site.Input
		},
	}
	result, err := analyzeUntrustedFlow(ctx, input, rule)
	for _, finding := range result.Findings {
		if diagnosticAt(diagnostics, finding.Site.Path, finding.Site.Line, Error) {
			continue
		}
		var related []RelatedLocation
		for _, source := range finding.Sources {
			related = append(related, RelatedLocation{Path: source.Path})
		}
		diagnostics = append(diagnostics, Diagnostic{
			Path: finding.Site.Path, Line: finding.Site.Line, Related: related,
			Message: "Attacker-controlled Actions value flows through an output into executable script text; bind it through env and read it as data instead",
		})
	}
	diagnostics, err = templateInjectionFlowError(diagnostics, err)
	for _, gap := range result.Gaps {
		diagnostics = append(diagnostics, Diagnostic{Severity: Warning, Path: gap.Path, Line: gap.Line,
			Message: "Incomplete template-injection output-flow analysis: " + gap.Reason})
	}
	return diagnostics, err
}

func outputFlowInput(ctx context.Context, input Input) (Input, bool, error) {
	if err := ctx.Err(); err != nil {
		return input, false, err
	}
	hasTransport := false
	if input.Workflow != nil {
		workflow, err := actions.Resolve(input.Workflow)
		if err != nil {
			return input, false, err
		}
		input.Workflow = workflow
		for _, job := range input.Workflow.Jobs {
			hasTransport = hasTransport || len(job.Outputs) > 0
			for index, step := range job.Steps {
				// Lint must inspect typed run interpolation even though the
				// emitter rejects it. Lower options without emitting that text.
				run, typed := step.(actions.RunStep)
				if typed {
					placeholder := run
					placeholder.Run = "true"
					step = placeholder
				}
				fields, err := actions.LowerStep(step, job.ID, input.Workflow)
				if err != nil {
					return input, false, errors.New("cannot inspect output transport: correct the Actions step before linting")
				}
				if typed {
					fields["run"] = run.Run
					job.Steps[index] = actions.RawStep{Fields: fields}
				}
				hasTransport = hasTransport || hasOutputTransport(fields)
			}
		}
	} else {
		blocks, err := aw.ExecutionBlocks(ctx, input.Document)
		if err != nil {
			return input, false, err
		}
		for _, block := range blocks {
			hasTransport = hasTransport || hasOutputTransport(block.Fields)
			for _, step := range block.Steps {
				hasTransport = hasTransport || hasOutputTransport(step.Fields)
			}
		}
	}
	return input, hasTransport, nil
}

func templateInjectionFlowError(diagnostics []Diagnostic, err error) ([]Diagnostic, error) {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var failures []error
		for _, child := range joined.Unwrap() {
			var failure error
			diagnostics, failure = templateInjectionFlowError(diagnostics, child)
			failures = append(failures, failure)
		}
		return diagnostics, errors.Join(failures...)
	}
	if child := errors.Unwrap(err); child != nil {
		return templateInjectionFlowError(diagnostics, child)
	}
	var expressionError *taint.ExpressionError
	if errors.As(err, &expressionError) {
		if !diagnosticAt(diagnostics, expressionError.Path, expressionError.Line, Warning) {
			diagnostics = append(diagnostics, Diagnostic{Severity: Warning, Path: expressionError.Path, Line: expressionError.Line,
				Message: "Incomplete template-injection output-flow analysis: invalid Actions expression; fix the expression and rerun the check"})
		}
		return diagnostics, nil
	}
	return diagnostics, err
}

func hasOutputTransport(raw any) bool {
	switch value := raw.(type) {
	case string:
		text := strings.ToLower(value)
		return strings.Contains(text, "github_output") || strings.Contains(text, "github.output") || strings.Contains(text, "setoutput")
	case map[string]any:
		for key, child := range value {
			if key == "outputs" || hasOutputTransport(child) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(value, hasOutputTransport)
	}
	return false
}

func diagnosticAt(diagnostics []Diagnostic, path string, line int, severity Severity) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Path == path && diagnostic.Line == line &&
			(diagnostic.Severity == severity || diagnostic.Severity == "" && severity == Error) {
			return true
		}
	}
	return false
}

func analyzeUntrustedFlow(ctx context.Context, input Input, rule taint.Rule) (taint.Result, error) {
	if input.Workflow != nil {
		return taint.Analyze(input.Workflow, rule)
	}
	return taint.AnalyzeAuthoredDocument(ctx, input.Document, rule)
}
