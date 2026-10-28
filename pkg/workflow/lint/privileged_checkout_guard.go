package lint

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
	"github.com/github/gh-aw/pkg/workflow/aw"
)

type privilegedMergeProof struct {
	ctx    context.Context
	input  Input
	active map[string]bool
	proven map[string]bool
}

type privilegedMergeJob struct {
	condition any
	needs     []string
}

// Only the exact merge-SHA selector inherits this proof, never PR heads or opt-outs.
func privilegedMergedGuard(ctx context.Context, input Input, block aw.ExecutionBlock, step aw.ExecutionStep) (bool, error) {
	stepCondition, stepKnown := privilegedGuardExpression(step.Fields["if"])
	if stepKnown && privilegedRequiresMerged(stepCondition) {
		return true, nil
	}
	if input.Workflow == nil {
		condition, known := privilegedGuardExpression(block.Fields["if"])
		if known && privilegedRequiresMerged(condition) {
			return true, nil
		}
	}
	id, jobBlock := strings.CutPrefix(block.Path, "jobs.")
	if !jobBlock {
		return false, nil
	}
	proof := privilegedMergeProof{ctx: ctx, input: input, active: make(map[string]bool), proven: make(map[string]bool)}
	if merged, err := proof.job(id); merged || err != nil {
		return merged, err
	}
	job, _, err := proof.lookup(id)
	if err != nil || !stepKnown {
		return false, err
	}
	for _, dependency := range job.needs {
		if privilegedRequiresDependency(stepCondition, dependency, false) {
			if merged, err := proof.job(dependency); merged || err != nil {
				return merged, err
			}
		}
	}
	return false, nil
}

func (proof *privilegedMergeProof) job(id string) (bool, error) {
	if err := proof.ctx.Err(); err != nil {
		return false, err
	}
	if merged, known := proof.proven[id]; known {
		return merged, nil
	}
	if proof.active[id] {
		return false, nil
	}
	proof.active[id] = true
	defer delete(proof.active, id)
	job, exists, err := proof.lookup(id)
	if err != nil || !exists {
		return false, err
	}
	condition, known := privilegedGuardExpression(job.condition)
	merged := known && privilegedRequiresMerged(condition)
	if known && !merged {
		for _, dependency := range job.needs {
			if !privilegedRequiresDependency(condition, dependency, true) {
				continue
			}
			merged, err = proof.job(dependency)
			if err != nil || merged {
				break
			}
		}
	}
	proof.proven[id] = merged
	return merged, err
}

func (proof *privilegedMergeProof) lookup(id string) (privilegedMergeJob, bool, error) {
	if proof.input.Workflow != nil {
		for _, job := range proof.input.Workflow.Jobs {
			if string(job.ID) != id {
				continue
			}
			result := privilegedMergeJob{}
			if job.If != nil {
				condition, err := actions.RenderExpression(job.If, job.ID, proof.input.Workflow, false)
				if err != nil {
					return result, false, fmt.Errorf("cannot inspect merged checkout guard at jobs.%s.if: invalid Actions condition", id)
				}
				result.condition = condition
			}
			for _, dependency := range job.Needs {
				result.needs = append(result.needs, string(dependency))
			}
			return result, true, nil
		}
		return privilegedMergeJob{}, false, nil
	}
	jobs, err := policyMapping(proof.input.Document.Frontmatter["jobs"], "jobs")
	if err != nil {
		return privilegedMergeJob{}, false, err
	}
	value, exists := jobs[id]
	fields, err := policyMapping(value, "jobs."+id)
	if err != nil || !exists {
		return privilegedMergeJob{}, false, err
	}
	var needs []string
	if fields["needs"] != nil {
		needs, _, err = workflowRunStrings(proof.ctx, fields["needs"], true)
	}
	return privilegedMergeJob{condition: fields["if"], needs: needs}, true, err
}

func privilegedGuardExpression(value any) (*actionslanguageservice.Expression, bool) {
	if value == nil {
		return nil, true
	}
	if boolean, ok := value.(bool); ok {
		value = strconv.FormatBool(boolean)
	}
	text, ok := value.(string)
	if !ok {
		return nil, false
	}
	if node, wrapped := actionslanguageservice.WrappedExpression(text); wrapped {
		return node, true
	}
	node, err := actionslanguageservice.ParseSyntax(text)
	return node, err == nil && node != nil
}

func privilegedRequiresMerged(node *actionslanguageservice.Expression) bool {
	return privilegedConditionRequires(node, func(value *actionslanguageservice.Expression) bool {
		reference, ok := actionslanguageservice.Reference(value)
		return ok && slices.Equal(reference, []string{"github", "event", "pull_request", "merged"})
	})
}

func privilegedConditionRequires(node *actionslanguageservice.Expression, match func(*actionslanguageservice.Expression) bool) bool {
	if node == nil {
		return false
	}
	if match(node) {
		return true
	}
	required := func(child *actionslanguageservice.Expression) bool { return privilegedConditionRequires(child, match) }
	switch node.Kind {
	case "group":
		return required(node.Child(0))
	case "logical":
		if node.Token.Kind == "&&" {
			return slices.ContainsFunc(node.Children, required)
		}
		if node.Token.Kind == "||" {
			return len(node.Children) > 0 && !slices.ContainsFunc(node.Children, func(child *actionslanguageservice.Expression) bool {
				return !required(child)
			})
		}
	case "binary":
		if node.Token.Kind == "==" {
			left, right := node.Child(0), node.Child(1)
			return (left.Kind == "literal" && left.Token.Kind == "true" && required(right)) ||
				(right.Kind == "literal" && right.Token.Kind == "true" && required(left))
		}
	}
	return false
}

func privilegedRequiresDependency(node *actionslanguageservice.Expression, dependency string, jobCondition bool) bool {
	statusOverride := false
	actionslanguageservice.Walk(node, func(value *actionslanguageservice.Expression) bool {
		if value.Kind == "call" {
			switch strings.ToLower(value.Token.Text) {
			case "always", "success", "failure", "cancelled":
				statusOverride = true
			}
		}
		return true
	})
	if jobCondition && !statusOverride {
		return true // Actions inserts success() for job conditions without a status function.
	}
	notCancelled := privilegedConditionRequires(node, func(value *actionslanguageservice.Expression) bool {
		return value.Kind == "unary" && value.Token.Kind == "!" && privilegedStatusCall(value.Child(0), "cancelled")
	})
	return privilegedConditionRequires(node, func(value *actionslanguageservice.Expression) bool {
		if jobCondition && privilegedStatusCall(value, "success") {
			return true
		}
		if value.Kind != "binary" {
			return false
		}
		status := ""
		switch value.Token.Kind {
		case "==":
			status = "success"
		case "!=":
			if notCancelled {
				status = "skipped"
			}
		}
		return status != "" && (privilegedDependencyResult(value.Child(0), value.Child(1), dependency, status) ||
			privilegedDependencyResult(value.Child(1), value.Child(0), dependency, status))
	})
}

func privilegedDependencyResult(reference, literal *actionslanguageservice.Expression, dependency, status string) bool {
	parts, ok := actionslanguageservice.Reference(reference)
	text, stringValue := literal.Token.Value.(string)
	return ok && slices.Equal(parts, []string{"needs", dependency, "result"}) &&
		literal.Kind == "literal" && literal.Token.Kind == "string" && stringValue && strings.EqualFold(text, status)
}

func privilegedStatusCall(node *actionslanguageservice.Expression, name string) bool {
	for node.Kind == "group" {
		node = node.Child(0)
	}
	return node.Kind == "call" && strings.EqualFold(node.Token.Text, name) && len(node.Children) == 0
}
