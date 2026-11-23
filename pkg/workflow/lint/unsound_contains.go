package lint

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/goccy/go-yaml"
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
	var diagnostics []Diagnostic
	visit := func(path string, value any) error {
		found, err := unsoundContainsCondition(ctx, path, value)
		if found {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: unsoundContainsMessage})
		}
		return err
	}
	if input.Workflow != nil {
		root, err := actions.Lower(input.Workflow)
		if err != nil {
			return nil, errors.New("unsound-contains cannot lower the Actions AST; correct the workflow before linting")
		}
		return diagnostics, unsoundContainsActions(ctx, root, visit)
	}
	blocks, err := aw.ExecutionBlocks(ctx, input.Document)
	failures := []error{err}
	for _, block := range blocks {
		if err := ctx.Err(); err != nil {
			return diagnostics, err
		}
		failures = append(failures, visit(block.FieldPath("if"), block.Fields["if"]))
		for _, step := range block.Steps {
			failures = append(failures, visit(step.Path+".if", step.Fields["if"]))
		}
	}
	return diagnostics, errors.Join(failures...)
}

func unsoundContainsActions(ctx context.Context, root yaml.MapSlice, visit func(string, any) error) error {
	var failures []error
	for _, item := range root {
		if item.Key != "jobs" {
			continue
		}
		jobs, _ := item.Value.(yaml.MapSlice)
		for _, job := range jobs {
			if err := ctx.Err(); err != nil {
				return errors.Join(append(failures, err)...)
			}
			id, _ := job.Key.(string)
			fields, _ := job.Value.(map[string]any)
			path := "jobs." + id
			failures = append(failures, visit(path+".if", fields["if"]))
			steps, _ := fields["steps"].([]map[string]any)
			for index, step := range steps {
				failures = append(failures, visit(fmt.Sprintf("%s.steps[%d].if", path, index), step["if"]))
			}
		}
	}
	return errors.Join(failures...)
}

// unsoundContainsCondition parses bare and ${{ }}-wrapped conditions alike.
func unsoundContainsCondition(ctx context.Context, path string, value any) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	text, ok := value.(string)
	if !ok {
		return false, nil
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
