package lint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/rhysd/actionlint"
	"mvdan.cc/sh/v3/syntax"
)

func BashCasePatternRule() Rule {
	return Rule{
		ID: "bash-case-pattern", Description: "Use balanced case patterns inside command substitutions for Bash 3.2 compatibility",
		Severity: Warning, Targets: ActionsAST | AWAST, Check: checkBashCasePatterns,
	}
}

func checkBashCasePatterns(ctx context.Context, input Input) ([]Diagnostic, error) {
	blocks, err := executionPolicyBlocks(ctx, input)
	failures := []error{err}
	var diagnostics []Diagnostic
	for _, block := range blocks {
		for _, step := range block.Steps {
			if err := ctx.Err(); err != nil {
				return diagnostics, errors.Join(append(failures, err)...)
			}
			script, ok := step.Fields["run"].(string)
			if !ok {
				continue
			}
			shell, shellKnown := effectiveRunShell(input, block, step)
			if !shellKnown {
				continue
			}
			if shell != "" && shell != "bash" && !strings.HasPrefix(shell, "bash ") {
				continue
			}
			found, err := bashCasePatterns(ctx, script, step.Path+".run")
			diagnostics = append(diagnostics, found...)
			failures = append(failures, err)
		}
	}
	return diagnostics, errors.Join(failures...)
}

func effectiveRunShell(input Input, block aw.ExecutionBlock, step aw.ExecutionStep) (string, bool) {
	if value := step.Fields["shell"]; value != nil {
		shell, ok := value.(string)
		if !ok {
			return "", false
		}
		if shell != "" {
			return shell, true
		}
	}

	if input.Workflow != nil {
		for _, job := range input.Workflow.Jobs {
			if block.Path != "jobs."+string(job.ID) {
				continue
			}
			if shell, configured, valid := runDefaultShell(job.Defaults); configured {
				return shell, valid
			}
			break
		}
		if shell, configured, valid := runDefaultShell(input.Workflow.Defaults); configured {
			return shell, valid
		}
	} else if input.Document != nil {
		if shell, configured, valid := runDefaultShell(block.Fields["defaults"]); configured {
			return shell, valid
		}
		if shell, configured, valid := runDefaultShell(input.Document.Frontmatter["defaults"]); configured {
			return shell, valid
		}
	}
	return "bash", true
}

func runDefaultShell(value any) (shell string, configured bool, valid bool) {
	defaults, ok := value.(map[string]any)
	if !ok {
		return "", false, false
	}
	run, ok := defaults["run"].(map[string]any)
	if !ok {
		return "", false, false
	}
	value, configured = run["shell"]
	if !configured || value == nil {
		return "", false, false
	}
	shell, valid = value.(string)
	return shell, true, valid
}

func bashCasePatterns(ctx context.Context, script, path string) ([]Diagnostic, error) {
	// Expressions are runner-provided data, not shell grammar. Keep byte offsets
	// and line numbers intact without evaluating or reproducing their values.
	rendered := []byte(script)
	err := taint.VisitInterpolations(ctx, script, path, func(start, end, _ int, _ actionlint.ExprNode) error {
		maskBashRange(rendered[start:end], 'x')
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	tree, err := syntax.NewParser(syntax.KeepComments(true)).Parse(bytes.NewReader(rendered), "")
	if err != nil {
		// Shell syntax errors and other interpreters belong to their own checks.
		return nil, ctx.Err()
	}
	syntax.Walk(tree, func(node syntax.Node) bool {
		if comment, ok := node.(*syntax.Comment); ok {
			maskBashRange(rendered[comment.Pos().Offset():comment.End().Offset()], ' ')
		}
		return true
	})
	var diagnostics []Diagnostic
	var stack []bool
	depth := 0
	syntax.Walk(tree, func(node syntax.Node) bool {
		if ctx.Err() != nil {
			return false
		}
		if node == nil {
			index := len(stack) - 1
			if index >= 0 && index < len(stack) {
				if stack[index] {
					depth--
				}
				stack = stack[:index]
			}
			return true
		}
		substitution, ok := node.(*syntax.CmdSubst)
		nested := ok && !substitution.Backquotes
		stack = append(stack, nested)
		if nested {
			depth++
		}
		item, ok := node.(*syntax.CaseItem)
		if !ok || depth == 0 {
			return true
		}
		prefix := strings.TrimRight(string(rendered[:item.Pos().Offset()]), " \t\r\n")
		if !strings.HasSuffix(prefix, "(") {
			diagnostics = append(diagnostics, Diagnostic{
				Path: path, Line: int(item.Pos().Line()),
				Message: fmt.Sprintf("Case pattern at column %d inside command substitution may fail to parse under Bash 3.2; use (pattern) instead of pattern), keeping intentional glob expansions unquoted", item.Pos().Col()),
			})
		}
		return true
	})
	return diagnostics, ctx.Err()
}

func maskBashRange(text []byte, fill byte) {
	for i, value := range text {
		if value != '\n' && value != '\r' {
			text[i] = fill
		}
	}
}
