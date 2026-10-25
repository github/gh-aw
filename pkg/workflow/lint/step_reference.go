package lint

import (
	"context"
	"errors"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/goccy/go-yaml"
	"github.com/rhysd/actionlint"
)

func StepReferenceRule() Rule {
	return Rule{
		ID: "step-reference", Description: "Keep literal step references connected to steps in the same job",
		Severity: Warning, Targets: ActionsAST | AWAST, Check: checkStepReference,
	}
}

func checkStepReference(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	blocks, err := stepReferenceBlocks(ctx, input)
	if err != nil {
		return nil, err
	}
	var diagnostics []Diagnostic
	var failures []error
	for _, block := range blocks {
		if err := ctx.Err(); err != nil {
			return diagnostics, err
		}
		found, err := checkStepReferenceBlock(ctx, block, input.Workflow == nil)
		diagnostics = append(diagnostics, found...)
		failures = append(failures, err)
	}
	return diagnostics, errors.Join(failures...)
}

func stepReferenceBlocks(ctx context.Context, input Input) ([]aw.ExecutionBlock, error) {
	if input.Workflow == nil {
		pending, err := authoredImportsPending(input)
		if err != nil {
			return nil, err
		}
		blocks, err := aw.ExecutionBlocks(ctx, input.Document)
		if err != nil {
			return nil, err
		}
		var complete []aw.ExecutionBlock
		for _, block := range blocks {
			section, id, _ := strings.Cut(block.Path, ".")
			if section == "safe-outputs" {
				id, _ = strings.CutPrefix(id, "jobs.")
				if !strings.HasPrefix(block.Path, "safe-outputs.jobs.") {
					continue
				}
			} else if section != "jobs" && section != "safe-jobs" {
				continue
			}
			// These scopes may acquire steps from imports or compiler generation.
			if _, builtin := constants.KnownBuiltInJobNames[strings.ToLower(id)]; pending || builtin ||
				section == "jobs" && block.Fields["restore-memory"] != nil {
				continue
			}
			complete = append(complete, block)
		}
		return complete, nil
	}
	root, err := actions.Lower(input.Workflow)
	if err != nil {
		return nil, errors.New("step-reference requires a valid Actions graph; correct jobs and steps before linting")
	}
	var blocks []aw.ExecutionBlock
	for _, item := range root {
		if item.Key != "jobs" {
			continue
		}
		jobs, ok := item.Value.(yaml.MapSlice)
		if !ok {
			return nil, errors.New("step-reference requires an Actions jobs mapping")
		}
		for _, job := range jobs {
			id, validID := job.Key.(string)
			fields, validFields := job.Value.(map[string]any)
			if !validID || !validFields {
				return nil, errors.New("step-reference requires Actions job mappings with string IDs")
			}
			block := aw.ExecutionBlock{Path: "jobs." + id, Fields: fields}
			steps, ok := block.Fields["steps"].([]map[string]any)
			if !ok && block.Fields["steps"] != nil {
				return nil, errors.New("step-reference requires an Actions step list")
			}
			for _, fields := range steps {
				block.Steps = append(block.Steps, aw.ExecutionStep{Fields: fields})
			}
			blocks = append(blocks, block)
		}
	}
	return blocks, ctx.Err()
}

func checkStepReferenceBlock(ctx context.Context, block aw.ExecutionBlock, authored bool) ([]Diagnostic, error) {
	additional, err := stepReferenceSetupSteps(ctx, block, authored)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{})
	for _, step := range append(additional, block.Steps...) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value, exists := step.Fields["id"]
		if !exists {
			continue
		}
		id, ok := value.(string)
		if !ok {
			return nil, errors.New("step-reference requires string step IDs; correct the workflow before linting")
		}
		if strings.Contains(id, "${{") {
			return nil, nil // An unresolved ID could supply any missing producer.
		}
		ids[strings.ToLower(id)] = struct{}{}
	}
	var diagnostics []Diagnostic
	reported := make(map[string]struct{})
	err = walkExpressionScalars(ctx, block.Fields, block.Path, func(path, text string) error {
		if stepReferenceAgentMetadataPath(block.Path, path) {
			return nil
		}
		visit := func(_, _, _ int, node actionlint.ExprNode) error {
			actionlint.VisitExprNode(node, func(child, _ actionlint.ExprNode, entering bool) {
				if !entering || ctx.Err() != nil {
					return
				}
				id, literal := stepReferenceID(child)
				_, alreadyReported := reported[path]
				if _, exists := ids[strings.ToLower(id)]; literal && !exists && !alreadyReported {
					reported[path] = struct{}{}
					diagnostics = append(diagnostics, Diagnostic{
						Path: path, Message: "Step expression references an unknown step; use a step ID declared in the same job",
					})
				}
			})
			return ctx.Err()
		}
		if stepReferenceConditionPath(block.Path, path) && !strings.Contains(text, "${{") {
			lexer := actionlint.NewExprLexer(text + " }}")
			node, err := actionlint.NewExprParser().Parse(lexer)
			if err != nil || lexer.Offset() != len(text+" }}") {
				return &taint.ExpressionError{Path: path, Line: 1}
			}
			return visit(0, 0, 1, node)
		}
		return taint.VisitInterpolations(ctx, text, path, visit)
	})
	return diagnostics, err
}

func stepReferenceAgentMetadataPath(jobPath, path string) bool {
	if !strings.HasPrefix(jobPath, "safe-jobs.") && !strings.HasPrefix(jobPath, "safe-outputs.jobs.") {
		return false
	}
	field, found := strings.CutPrefix(path, jobPath+".")
	return found && (field == "description" || strings.HasPrefix(field, "inputs."))
}

// Custom jobs emit setup/pre-steps in the same Actions job, but the shared
// authored execution traversal only exposes regular steps and post-steps.
func stepReferenceSetupSteps(ctx context.Context, block aw.ExecutionBlock, authored bool) ([]aw.ExecutionStep, error) {
	if !authored || !strings.HasPrefix(block.Path, "jobs.") {
		return nil, nil
	}
	var additional []aw.ExecutionStep
	_, reusable := block.Fields["uses"]
	if !reusable {
		for _, field := range []string{"steps", "setup-steps", "pre-steps"} {
			if _, exists := block.Fields[field]; exists {
				additional = append(additional, aw.ExecutionStep{
					Fields: map[string]any{"id": "ghes-host-config"},
				})
				break
			}
		}
	}
	for _, field := range []string{"setup-steps", "pre-steps"} {
		raw := block.Fields[field]
		if raw == nil {
			continue
		}
		steps, ok := raw.([]any)
		if !ok {
			return nil, errors.New("step-reference requires setup-steps and pre-steps to be step lists")
		}
		for _, raw := range steps {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			fields, ok := raw.(map[string]any)
			if !ok {
				return nil, errors.New("step-reference requires setup-steps and pre-steps to contain step mappings")
			}
			additional = append(additional, aw.ExecutionStep{Fields: fields})
		}
	}
	return additional, nil
}

func stepReferenceConditionPath(jobPath, path string) bool {
	if needsConditionPath(jobPath, path) {
		return true
	}
	if strings.HasPrefix(jobPath, "jobs.") {
		for _, field := range []string{"setup-steps", "pre-steps"} {
			rest, found := strings.CutPrefix(path, jobPath+"."+field+"[")
			if found && strings.HasSuffix(rest, "].if") && strings.Count(rest, ".") == 1 {
				return true
			}
		}
	}
	return false
}

// Visiting every AST node also checks literal accesses nested in computed
// indexes, without treating quoted text or dynamic indexes as producer IDs.
func stepReferenceID(node actionlint.ExprNode) (string, bool) {
	var receiver actionlint.ExprNode
	var id string
	switch node := node.(type) {
	case *actionlint.ObjectDerefNode:
		receiver, id = node.Receiver, node.Property
	case *actionlint.IndexAccessNode:
		key, literal := node.Index.(*actionlint.StringNode)
		if !literal {
			return "", false
		}
		receiver, id = node.Operand, key.Value
	default:
		return "", false
	}
	variable, ok := receiver.(*actionlint.VariableNode)
	return id, ok && strings.EqualFold(variable.Name, "steps")
}
