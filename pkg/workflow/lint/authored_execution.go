package lint

import (
	"context"
	"errors"
	"fmt"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/taint"
)

func checkAuthoredActionPins(ctx context.Context, input Input) ([]Diagnostic, error) {
	severity, err := integrityPinSeverity(input)
	if err != nil {
		return nil, err
	}
	blocks, err := aw.ExecutionBlocks(ctx, input.Document)
	failures := []error{err}
	var diagnostics []Diagnostic
	check := func(fields map[string]any, path string, reusable bool) {
		raw, exists := fields["uses"]
		if !exists {
			return
		}
		uses, ok := raw.(string)
		if !ok {
			failures = append(failures, fmt.Errorf("action-pin requires a string at %s", path))
		} else if diagnostic := integrityPinDiagnostic(uses, reusable, severity, path); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
	}
	for _, block := range blocks {
		if err := ctx.Err(); err != nil {
			return diagnostics, errors.Join(append(failures, err)...)
		}
		if block.Path != "" {
			check(block.Fields, block.FieldPath("uses"), true)
		}
		for _, step := range block.Steps {
			if err := ctx.Err(); err != nil {
				return diagnostics, errors.Join(append(failures, err)...)
			}
			check(step.Fields, step.Path+".uses", false)
		}
	}
	return diagnostics, errors.Join(failures...)
}

func checkAuthoredTimeouts(ctx context.Context, input Input) ([]Diagnostic, error) {
	blocks, err := aw.ExecutionBlocks(ctx, input.Document)
	var diagnostics []Diagnostic
	for _, block := range blocks {
		if ctx.Err() != nil {
			return diagnostics, errors.Join(err, ctx.Err())
		}
		value, exists := block.Fields["timeout-minutes"]
		if !exists {
			continue
		}
		positive := false
		switch n := value.(type) {
		case int:
			positive = n > 0
		case int64:
			positive = n > 0
		case uint64:
			positive = n > 0
		}
		if !positive {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    block.FieldPath("timeout-minutes"),
				Message: "Explicit timeout must be a literal positive integer; choose a value greater than zero",
			})
		}
	}
	return diagnostics, err
}

func checkAuthoredPermissions(ctx context.Context, input Input, severity Severity) ([]Diagnostic, error) {
	blocks, err := aw.ExecutionBlocks(ctx, input.Document)
	failures := []error{err}
	var diagnostics []Diagnostic
	for _, block := range blocks {
		found, err := permissionScope(ctx, block.Fields["permissions"], block.FieldPath("permissions"), severity)
		diagnostics = append(diagnostics, found...)
		failures = append(failures, err)
	}
	return diagnostics, errors.Join(failures...)
}

func checkAuthoredTemplateInjection(ctx context.Context, input Input) ([]Diagnostic, error) {
	checker := templateInjectionChecker{source: AWAST, rule: taint.Rule{
		ID: "template-injection", IsSource: templateInjectionSource, IsFilter: templateInjectionFilter,
	}}
	parent, err := checker.rawEnvironment(ctx, input.Document.Frontmatter["env"], "env", nil)
	if err != nil {
		return checker.diagnostics, err
	}
	blocks, err := aw.ExecutionBlocks(ctx, input.Document)
	failures := []error{err}
	for _, block := range blocks {
		bindings := parent
		if block.Path != "" {
			bindings, err = checker.rawEnvironment(ctx, block.Fields["env"], block.FieldPath("env"), parent)
			if err != nil {
				failures = append(failures, err)
				continue
			}
		}
		for _, step := range block.Steps {
			err := checker.step(ctx, actions.RawStep{Fields: step.Fields}, step.Path, "", nil, bindings)
			failures = append(failures, err)
		}
	}
	diagnostics, flowErr := templateInjectionOutputFlows(ctx, input, checker.diagnostics)
	return diagnostics, errors.Join(append(failures, flowErr)...)
}
