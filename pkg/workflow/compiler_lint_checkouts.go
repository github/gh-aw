package workflow

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/goccy/go-yaml"
)

// Only the exact compiler-owned PR checkout/authentication block may retain
// credentials without artipacked warnings. Authored steps and other rules still lint.
func (c *Compiler) safeOutputRetainedCheckoutLintPaths(graph *actions.Workflow) (map[string]struct{}, error) {
	if c.prCheckoutLintBlock == "" {
		return nil, nil
	}
	var expected []map[string]any
	if err := yaml.Unmarshal([]byte(c.prCheckoutLintBlock), &expected); err != nil {
		return nil, fmt.Errorf("parse compiler-owned safe-output checkout steps for linting: %w", err)
	}
	if len(expected) == 0 {
		return nil, nil
	}
	for _, job := range graph.Jobs {
		if job.ID != "safe_outputs" {
			continue
		}
		match := -1
		for start := range job.Steps {
			if !matchesPRCheckoutBlock(job.Steps, start, expected) {
				continue
			}
			if match >= 0 {
				return nil, nil // Ambiguous ownership must not suppress an authored copy.
			}
			match = start
		}
		if match < 0 {
			return nil, nil
		}
		paths := make(map[string]struct{})
		for index, step := range expected {
			uses, isAction := step["uses"].(string)
			with, hasInputs := step["with"].(map[string]any)
			if !isAction || !hasInputs {
				continue
			}
			if strings.HasPrefix(uses, "actions/checkout@") && with["persist-credentials"] == true {
				paths[fmt.Sprintf("jobs.safe_outputs.steps[%d].with.persist-credentials", match+index)] = struct{}{}
			}
		}
		return paths, nil
	}
	return nil, nil
}

func matchesPRCheckoutBlock(steps []actions.Step, start int, expected []map[string]any) bool {
	if len(expected) > len(steps)-start {
		return false
	}
	for index, fields := range expected {
		step, ok := steps[start+index].(actions.RawStep)    //nolint:uncheckedsliceindex // The block length is checked above.
		if !ok || !reflect.DeepEqual(step.Fields, fields) { //nolint:reflectdeepequalusage // Raw YAML fields contain nested, heterogeneous maps and slices.
			return false
		}
	}
	return true
}
