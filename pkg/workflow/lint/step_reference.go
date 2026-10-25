package lint

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/goccy/go-yaml"
	"github.com/rhysd/actionlint"
)

func StepReferenceRule() Rule {
	return Rule{
		ID:          "step-reference",
		Description: "Validate same-job step references against declared step IDs",
		Severity:    Warning,
		Targets:     ActionsAST | AWAST,
		Check:       checkStepReference,
	}
}

func checkStepReference(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	if input.Workflow != nil {
		root, err := actions.Lower(input.Workflow)
		if err != nil {
			return nil, errors.New("step-reference requires a valid Actions graph; correct the workflow before linting")
		}
		var diagnostics []Diagnostic
		for _, item := range root {
			if item.Key != "jobs" {
				continue
			}
			jobs, ok := item.Value.(yaml.MapSlice)
			if !ok {
				return diagnostics, errors.New("step-reference requires Actions jobs mapping")
			}
			for _, entry := range jobs {
				id, ok := entry.Key.(string)
				if !ok {
					continue
				}
				fields, ok := entry.Value.(map[string]any)
				if !ok {
					return diagnostics, errors.New("step-reference requires Actions job mappings with string IDs")
				}
				found, err := stepReferenceDiagnostics(ctx, fields, "jobs."+id, declaredStepIDs(fields))
				diagnostics = append(diagnostics, found...)
				if err != nil {
					return diagnostics, err
				}
			}
		}
		return diagnostics, nil
	}
	blocks, err := aw.ExecutionBlocks(ctx, input.Document)
	if err != nil {
		return nil, err
	}
	var diagnostics []Diagnostic
	for _, block := range blocks {
		if block.Path == "" {
			continue
		}
		found, err := stepReferenceDiagnostics(ctx, block.Fields, block.Path, declaredStepIDs(block.Fields))
		diagnostics = append(diagnostics, found...)
		if err != nil {
			return diagnostics, err
		}
	}
	return diagnostics, nil
}

func declaredStepIDs(fields map[string]any) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, key := range []string{"steps", "pre-steps", "post-steps"} {
		collectStepIDs(ids, fields[key])
	}
	return ids
}

func collectStepIDs(ids map[string]struct{}, value any) {
	switch value := value.(type) {
	case nil:
		return
	case []any:
		for _, item := range value {
			collectStepIDs(ids, item)
		}
	case []map[string]any:
		for _, item := range value {
			collectStepIDs(ids, item)
		}
	case map[string]any:
		if raw, exists := value["id"]; exists {
			if stepID, ok := raw.(string); ok {
				ids[strings.ToLower(strings.TrimSpace(stepID))] = struct{}{}
			}
		}
	case yaml.MapSlice:
		for _, item := range value {
			key, ok := item.Key.(string)
			if !ok || key != "id" {
				continue
			}
			if stepID, ok := item.Value.(string); ok {
				ids[strings.ToLower(strings.TrimSpace(stepID))] = struct{}{}
			}
		}
	}
}

func stepReferenceDiagnostics(ctx context.Context, value any, path string, declared map[string]struct{}) ([]Diagnostic, error) {
	var diagnostics []Diagnostic
	seen := make(map[string]struct{})
	err := walkExpressionScalars(ctx, value, path, func(path, text string) error {
		if text == "" {
			return nil
		}
		visit := func(_, _, _ int, node actionlint.ExprNode) error {
			actionlint.VisitExprNode(node, func(child, _ actionlint.ExprNode, entering bool) {
				if !entering || ctx.Err() != nil {
					return
				}
				stepID, ok := stepReferenceID(child)
				if !ok {
					return
				}
				stepID = strings.TrimSpace(stepID)
				if stepID == "" {
					return
				}
				if _, exists := declared[strings.ToLower(stepID)]; exists {
					return
				}
				key := path + "\n" + stepID
				if _, reported := seen[key]; reported {
					return
				}
				seen[key] = struct{}{}
				diagnostics = append(diagnostics, Diagnostic{
					Path:    path,
					Message: fmt.Sprintf("Step reference %q is not declared in this job; add a step with that id or remove the reference", stepID),
				})
			})
			return ctx.Err()
		}
		if strings.Contains(text, "${{") {
			return taint.VisitInterpolations(ctx, text, path, visit)
		}
		if strings.HasSuffix(path, ".if") {
			lexer := actionlint.NewExprLexer(text + " }}")
			node, err := actionlint.NewExprParser().Parse(lexer)
			if err != nil || lexer.Offset() != len(text+" }}") {
				return nil
			}
			return visit(0, 0, 1, node)
		}
		return nil
	})
	return diagnostics, err
}

func stepReferenceID(node actionlint.ExprNode) (string, bool) {
	switch value := node.(type) {
	case *actionlint.ObjectDerefNode:
		receiver, ok := value.Receiver.(*actionlint.VariableNode)
		if !ok || !strings.EqualFold(receiver.Name, "steps") {
			return "", false
		}
		return strings.TrimSpace(value.Property), true
	case *actionlint.IndexAccessNode:
		receiver, ok := value.Operand.(*actionlint.VariableNode)
		if !ok || !strings.EqualFold(receiver.Name, "steps") {
			return "", false
		}
		key, ok := value.Index.(*actionlint.StringNode)
		if !ok {
			return "", false
		}
		return strings.TrimSpace(key.Value), true
	default:
		return "", false
	}
}
