package lint

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/rhysd/actionlint"
	"mvdan.cc/sh/v3/syntax"
)

func UnquotedCommandAssignmentRule() Rule {
	return Rule{
		ID: "unquoted-command-assignment", Description: "Detect unquoted assignments that resemble command invocations",
		Severity: Warning, Targets: ActionsAST | AWAST, Check: checkUnquotedCommandAssignments,
	}
}

var assignmentCommands = map[string]struct{}{
	"awk": {}, "cat": {}, "date": {}, "echo": {}, "grep": {}, "ls": {},
	"mkdir": {}, "pr": {}, "printf": {}, "sed": {},
}

func checkUnquotedCommandAssignments(ctx context.Context, input Input) ([]Diagnostic, error) {
	blocks, err := executionPolicyBlocks(ctx, input)
	failures := []error{err}
	var diagnostics []Diagnostic
	check := func(script, path string, source ASTTargets) {
		found, err := unquotedCommandAssignments(ctx, script, path)
		for i := range found {
			found[i].Source = source
		}
		diagnostics = append(diagnostics, found...)
		failures = append(failures, err)
	}
	for _, block := range blocks {
		for _, step := range block.Steps {
			script, ok := step.Fields["run"].(string)
			if !ok {
				continue
			}
			shell, known := policyRunShell(input, block, step)
			if known && posixRunShell(shell) {
				source := ActionsAST
				if input.Workflow == nil {
					source = AWAST
				}
				check(script, step.Path+".run", source)
			}
		}
	}

	scripts := input.ResolvedMCPScripts
	if scripts == nil && input.Document != nil {
		scripts = make(map[string]string)
		tools, ok := input.Document.Frontmatter["mcp-scripts"].(map[string]any)
		if !ok {
			tools = nil
		}
		for name, value := range tools {
			fields, ok := value.(map[string]any)
			if !ok {
				continue
			}
			if script, ok := fields["run"].(string); ok {
				scripts[name] = script
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(scripts)) {
		check(scripts[name], "mcp-scripts."+name+".run", AWAST)
	}
	return diagnostics, errors.Join(failures...)
}

func unquotedCommandAssignments(ctx context.Context, script, path string) ([]Diagnostic, error) {
	rendered := []byte(script)
	err := taint.VisitInterpolations(ctx, script, path, func(start, end, _ int, _ actionlint.ExprNode) error {
		maskBashRange(rendered[start:end], 'x')
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	tree, err := syntax.NewParser().Parse(bytes.NewReader(rendered), "")
	if err != nil {
		return nil, ctx.Err()
	}
	var diagnostics []Diagnostic
	syntax.Walk(tree, func(node syntax.Node) bool {
		if ctx.Err() != nil {
			return false
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}
		for _, assign := range call.Assigns {
			if assign.Value == nil || len(assign.Value.Parts) != 1 {
				continue
			}
			for _, part := range assign.Value.Parts {
				literal, ok := part.(*syntax.Lit)
				if !ok {
					continue
				}
				if _, found := assignmentCommands[literal.Value]; found {
					diagnostics = append(diagnostics, Diagnostic{
						Path: path, Line: int(assign.Pos().Line()),
						Message: "Unquoted assignment resembles a command invocation (ShellCheck SC2209); quote the literal or use $(command) to capture output",
					})
				}
			}
		}
		return true
	})
	return diagnostics, ctx.Err()
}
