package lint

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
)

type conditionField struct {
	path, text string
}

// conditionFields collects non-empty job and step if conditions. The compiled
// Actions graph is authoritative when supplied; AW conditions are inspected
// only when linting authored source on its own. ruleID prefixes errors.
func conditionFields(ctx context.Context, input Input, ruleID string) ([]conditionField, error) {
	var fields []conditionField
	add := func(path string, value any) {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			fields = append(fields, conditionField{path: path, text: text})
		}
	}
	if input.Workflow == nil {
		blocks, err := aw.ExecutionBlocks(ctx, input.Document)
		for _, block := range blocks {
			add(block.FieldPath("if"), block.Fields["if"])
			for _, step := range block.Steps {
				add(step.Path+".if", step.Fields["if"])
			}
		}
		return fields, err
	}
	var failures []error
	for _, job := range input.Workflow.Jobs {
		if err := ctx.Err(); err != nil {
			return fields, errors.Join(append(failures, err)...)
		}
		path := "jobs." + string(job.ID)
		if job.If != nil {
			value, err := actions.RenderExpression(job.If, job.ID, input.Workflow, false)
			if err != nil {
				failures = append(failures, fmt.Errorf("%s cannot render the condition at %s.if; correct the job before linting", ruleID, path))
			} else {
				add(path+".if", "${{ "+value+" }}")
			}
		}
		for index, step := range job.Steps {
			stepPath := fmt.Sprintf("%s.steps[%d]", path, index)
			lowered, err := actions.LowerStep(step, job.ID, input.Workflow)
			if err != nil {
				failures = append(failures, fmt.Errorf("%s cannot inspect %s: invalid Actions step; correct the step before linting", ruleID, stepPath))
				continue
			}
			add(stepPath+".if", lowered["if"])
		}
	}
	return fields, errors.Join(failures...)
}
