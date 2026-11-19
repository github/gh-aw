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
		step, ok := steps[start+index].(actions.RawStep)                                                                  //nolint:uncheckedsliceindex // The block length is checked above.
		if !ok || !reflect.DeepEqual(normalizeCheckoutRuntimePaths(step.Fields), normalizeCheckoutRuntimePaths(fields)) { //nolint:reflectdeepequalusage // Raw YAML fields contain nested, heterogeneous maps and slices.
			return false
		}
	}
	return true
}

func normalizeCheckoutRuntimePaths(fields map[string]any) map[string]any {
	normalized, _ := normalizeCheckoutRuntimeValue(fields).(map[string]any)
	return normalized
}

func normalizeCheckoutRuntimeValue(value any) any {
	switch typed := value.(type) {
	case string:
		return strings.NewReplacer(
			runtimeRootExpression, "/tmp/gh-aw",
			setupRootExpression, "${{ runner.temp }}/gh-aw",
			"${GH_AW_RUNTIME_ROOT}", "/tmp/gh-aw",
			"${GH_AW_SETUP_ROOT}", "${RUNNER_TEMP}/gh-aw",
			"process.env.RUNNER_TEMP, 'gh-aw',", "process.env.GH_AW_SETUP_ROOT,",
			`process.env.RUNNER_TEMP, "gh-aw",`, "process.env.GH_AW_SETUP_ROOT,",
			"process.env.RUNNER_TEMP, 'gh-aw-setup',", "process.env.GH_AW_SETUP_ROOT,",
		).Replace(typed)
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for key, value := range typed {
			normalized[key] = normalizeCheckoutRuntimeValue(value)
		}
		return normalized
	case []any:
		normalized := make([]any, len(typed))
		for index, value := range typed {
			normalized[index] = normalizeCheckoutRuntimeValue(value)
		}
		return normalized
	default:
		return value
	}
}
