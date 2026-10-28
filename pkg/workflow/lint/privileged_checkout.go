package lint

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/rhysd/actionlint"
	"mvdan.cc/sh/v3/syntax"
)

func PrivilegedCheckoutRule() Rule {
	return Rule{
		ID: "privileged-checkout", Description: "Review explicit checkout guard bypasses and PR-controlled source selection on privileged triggers",
		Severity: Warning, Targets: ActionsAST | AWAST, Check: checkPrivilegedCheckouts,
	}
}

func executionPolicyOn(input Input) any {
	if input.Workflow != nil {
		return input.Workflow.On
	}
	return input.Document.Frontmatter["on"]
}

func privilegedCheckoutTrigger(ctx context.Context, on any) (bool, error) {
	for _, name := range []string{"pull_request_target", "workflow_run", "issue_comment", "repository_dispatch", "pull_request_review", "pull_request_review_comment"} {
		if _, found, err := authoredTrigger(ctx, on, name); err != nil || found {
			return found, err
		}
	}
	return false, nil
}

func checkPrivilegedCheckouts(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	privileged, err := privilegedCheckoutTrigger(ctx, executionPolicyOn(input))
	if err != nil || !privileged {
		return nil, err
	}
	blocks, err := executionPolicyBlocks(ctx, input)
	failures := []error{err}
	var diagnostics []Diagnostic
	for _, block := range blocks {
		for _, step := range block.Steps {
			if err := ctx.Err(); err != nil {
				return diagnostics, errors.Join(append(failures, err)...)
			}
			if actionName(step.Fields["uses"]) == "actions/checkout" {
				found, err := privilegedCheckoutAction(ctx, step)
				diagnostics = append(diagnostics, found...)
				failures = append(failures, err)
			}
			found, err := policyShellCalls(ctx, input, block, step, func(call *syntax.CallExpr, script string) ([]Diagnostic, error) {
				return privilegedCheckoutCommand(ctx, call, script, step.Path+".run")
			})
			diagnostics = append(diagnostics, found...)
			failures = append(failures, err)
		}
	}
	return diagnostics, errors.Join(failures...)
}

func privilegedCheckoutAction(ctx context.Context, step aw.ExecutionStep) ([]Diagnostic, error) {
	with, err := policyMapping(step.Fields["with"], step.Path+".with")
	if err != nil {
		return nil, err
	}
	var diagnostics []Diagnostic
	if value, explicit := with["allow-unsafe-pr-checkout"]; explicit {
		path := step.Path + ".with.allow-unsafe-pr-checkout"
		enabled, known, err := actionInputBoolean(ctx, value, path)
		if err != nil {
			return nil, err
		}
		if enabled || !known {
			diagnostics = append(diagnostics, Diagnostic{Path: path,
				Message: "Privileged-trigger checkout explicitly enables or may enable the unsafe PR checkout opt-out; remove it or set literal false and keep actor authorization and immutable revision approval separate",
			})
		}
	}
	for _, key := range []string{"repository", "ref"} {
		text, ok := with[key].(string)
		if !ok {
			continue
		}
		path := step.Path + ".with." + key
		selected, err := privilegedSourceText(ctx, text, path)
		if err != nil {
			return diagnostics, err
		}
		if selected || (key == "ref" && strings.HasPrefix(strings.TrimSpace(text), "refs/pull/")) {
			diagnostics = append(diagnostics, Diagnostic{Path: path,
				Message: "Privileged-trigger checkout selects PR or dispatch-controlled source; prefer a trusted base checkout, or independently authorize the actor and approve an immutable revision before executing code",
			})
		}
	}
	return diagnostics, nil
}

func privilegedSourceText(ctx context.Context, text, path string) (bool, error) {
	expressions, err := taint.AnalyzeText(ctx, text, path, taint.Rule{
		IsSource: func(ref string) bool {
			return strings.HasPrefix(ref, "github.event.pull_request.head.") ||
				strings.HasPrefix(ref, "github.event.client_payload.") ||
				ref == "github.head_ref" || ref == "github.event.workflow_run.head_sha" ||
				ref == "github.event.workflow_run.head_branch" ||
				strings.HasPrefix(ref, "github.event.workflow_run.head_repository.")
		},
		IsFilter: templateInjectionFilter,
	}, nil)
	for _, expression := range expressions {
		if len(expression.Sources) > 0 {
			return true, err
		}
	}
	return false, err
}

func privilegedCheckoutCommand(ctx context.Context, call *syntax.CallExpr, script, path string) ([]Diagnostic, error) {
	if len(call.Args) < 3 {
		return nil, nil
	}
	name, operation := policyShellArgument(call, 0), policyShellArgument(call, 1)
	selected := name == "gh" && operation == "pr" && policyShellArgument(call, 2) == "checkout"
	if name == "git" {
		switch operation {
		case "fetch", "checkout", "switch", "clone":
			for _, word := range call.Args[2:] {
				text := script[word.Pos().Offset():word.End().Offset()]
				tainted, err := privilegedSourceText(ctx, text, path)
				if err != nil {
					return nil, err
				}
				selected = selected || tainted || strings.HasPrefix(policyShellLiteral(word), "refs/pull/")
			}
		}
	}
	if !selected {
		return nil, nil
	}
	return []Diagnostic{{Path: path, Line: int(call.Pos().Line()),
		Message: "Custom Git/gh PR source selection on a privileged trigger does not inherit actions/checkout guards; independently authorize the actor and bind execution to an approved immutable revision",
	}}, nil
}

// Only direct Bash/sh calls are inspected, without evaluating expansions.
func policyShellCalls(ctx context.Context, input Input, block aw.ExecutionBlock, step aw.ExecutionStep, visit func(*syntax.CallExpr, string) ([]Diagnostic, error)) ([]Diagnostic, error) {
	script, ok := step.Fields["run"].(string)
	if !ok {
		return nil, nil
	}
	shell, known := effectiveRunShell(input, block, step)
	if !known || (shell != "" && shell != "bash" && shell != "sh" &&
		!strings.HasPrefix(shell, "bash ") && !strings.HasPrefix(shell, "sh ")) {
		return nil, nil
	}
	path := step.Path + ".run"
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
		return []Diagnostic{{Path: path, Message: "Incomplete privileged-code analysis: shell syntax could not be parsed; correct the script and rerun lint"}}, ctx.Err()
	}
	var diagnostics []Diagnostic
	var failures []error
	syntax.Walk(tree, func(node syntax.Node) bool {
		if ctx.Err() != nil {
			return false
		}
		if call, ok := node.(*syntax.CallExpr); ok {
			found, err := visit(call, script)
			diagnostics = append(diagnostics, found...)
			failures = append(failures, err)
		}
		return true
	})
	return diagnostics, errors.Join(append(failures, ctx.Err())...)
}

func policyShellLiteral(word *syntax.Word) string {
	var text strings.Builder
	var parts func([]syntax.WordPart) bool
	parts = func(values []syntax.WordPart) bool {
		for _, value := range values {
			switch value := value.(type) {
			case *syntax.Lit:
				text.WriteString(value.Value)
			case *syntax.SglQuoted:
				text.WriteString(value.Value)
			case *syntax.DblQuoted:
				if !parts(value.Parts) {
					return false
				}
			default:
				return false
			}
		}
		return true
	}
	if !parts(word.Parts) {
		return ""
	}
	return text.String()
}

func policyShellArgument(call *syntax.CallExpr, index int) string {
	if index < 0 || index >= len(call.Args) {
		return ""
	}
	return policyShellLiteral(call.Args[index])
}
