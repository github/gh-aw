package lint

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/rhysd/actionlint"
)

// UnsoundContainsRule flags job and step conditions that gate on
// contains('<literal>', <controllable context>). With a string haystack,
// contains performs a substring match, so a crafted ref, actor, or input that
// is a fragment of the literal satisfies the gate (zizmor unsound-contains).
func UnsoundContainsRule() Rule {
	return Rule{
		ID:          "unsound-contains",
		Description: "Avoid substring contains() checks of controllable contexts in conditions",
		Severity:    Warning,
		Targets:     ActionsAST | AWAST,
		Check:       checkUnsoundContains,
	}
}

const unsoundContainsMessage = "contains() with a literal string performs a substring match, so a crafted value of a controllable context can satisfy this condition; " +
	"compare against an array with contains(fromJSON('[\"a\",\"b\"]'), value) or use explicit == comparisons"

func checkUnsoundContains(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	conditions, err := conditionFields(ctx, input, "unsound-contains")
	var diagnostics []Diagnostic
	failures := []error{err}
	for _, condition := range conditions {
		if err := ctx.Err(); err != nil {
			return diagnostics, errors.Join(append(failures, err)...)
		}
		found, err := unsoundContainsCondition(ctx, condition.path, condition.text)
		if found {
			diagnostics = append(diagnostics, Diagnostic{Path: condition.path, Message: unsoundContainsMessage})
		}
		failures = append(failures, err)
	}
	return diagnostics, errors.Join(failures...)
}

// unsoundContainsCondition parses bare and ${{ }}-wrapped conditions alike.
func unsoundContainsCondition(ctx context.Context, path, text string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	found := false
	inspect := func(_, _, _ int, node actionlint.ExprNode) error {
		actionlint.VisitExprNode(node, func(child, _ actionlint.ExprNode, entering bool) {
			if entering && unsoundContainsCall(child) {
				found = true
			}
		})
		return ctx.Err()
	}
	if !strings.Contains(text, "${{") {
		if _, err := strconv.ParseBool(strings.TrimSpace(text)); err == nil {
			return false, nil
		}
		lexer := actionlint.NewExprLexer(text + " }}")
		node, err := actionlint.NewExprParser().Parse(lexer)
		if err != nil || lexer.Offset() != len(text+" }}") {
			return false, &taint.ExpressionError{Path: path, Line: 1}
		}
		return found, inspect(0, 0, 1, node)
	}
	err := taint.VisitInterpolations(ctx, text, path, inspect)
	return found, err
}

func unsoundContainsCall(node actionlint.ExprNode) bool {
	call, ok := node.(*actionlint.FuncCallNode)
	if !ok || !strings.EqualFold(call.Callee, "contains") || len(call.Args) != 2 {
		return false
	}
	if _, literal := call.Args[0].(*actionlint.StringNode); !literal {
		return false
	}
	return unsoundContainsControllable(call.Args[1])
}

// unsoundContainsControllable recognizes direct references to contexts whose
// values can be chosen by whoever pushes a ref, opens a PR, or dispatches.
func unsoundContainsControllable(node actionlint.ExprNode) bool {
	parts, ok := unsoundContainsReference(node)
	if !ok || len(parts) < 2 {
		return false
	}
	switch parts[0] {
	case "inputs":
		return true
	case "github":
		switch parts[1] {
		case "ref", "ref_name", "head_ref", "base_ref", "actor", "triggering_actor":
			return len(parts) == 2
		case "event":
			return len(parts) > 2
		}
	}
	return false
}

func unsoundContainsReference(node actionlint.ExprNode) ([]string, bool) {
	switch node := node.(type) {
	case *actionlint.VariableNode:
		return []string{strings.ToLower(node.Name)}, true
	case *actionlint.ObjectDerefNode:
		parts, ok := unsoundContainsReference(node.Receiver)
		return append(parts, strings.ToLower(node.Property)), ok
	case *actionlint.IndexAccessNode:
		parts, ok := unsoundContainsReference(node.Operand)
		key, literal := node.Index.(*actionlint.StringNode)
		if !literal {
			// A computed index still selects untrusted data under event/inputs.
			return append(parts, "*"), ok
		}
		return append(parts, strings.ToLower(key.Value)), ok
	}
	return nil, false
}
