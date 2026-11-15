package workflow

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
)

const writeGateStepID = "write_gate"
const writeGateInitializeStepID = "initialize_write_gate"

var writeGateStepIDPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

func writeGateSteps(data *WorkflowData) []any {
	if data == nil {
		return nil
	}
	for _, name := range []string{string(constants.SafeOutputsJobName), string(constants.SafeOutputsHyphenJobName)} {
		if config, ok := data.Jobs[name].(map[string]any); ok {
			if steps, ok := config["gate-steps"].([]any); ok {
				return steps
			}
		}
	}
	return nil
}

func hasWriteGateSteps(data *WorkflowData) bool {
	return len(writeGateSteps(data)) > 0
}

func (c *Compiler) validateWriteGateConfiguration(data *WorkflowData) error {
	for name, raw := range data.Jobs {
		config, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		value, exists := config["gate-steps"]
		if !exists {
			continue
		}
		if normalizeBuiltinJobAlias(name) != string(constants.SafeOutputsJobName) {
			return fmt.Errorf("jobs.%s.gate-steps is supported only in safe_outputs. Example: jobs:\n  safe_outputs:\n    gate-steps:\n      - run: ./decide-writes.sh", name)
		}
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("jobs.%s.gate-steps expects an array of step objects, got %T. Example: gate-steps:\n  - run: ./decide-writes.sh", name, value)
		}
	}
	if !hasWritePublicationPath(data) {
		return nil
	}
	config, err := c.loadRepoConfig()
	if err != nil {
		return fmt.Errorf("repository write-gate policy could not be checked: %w. Correct %s before compiling workflows with write outputs", err, RepoConfigFileName)
	}
	if config != nil && config.RequireWriteGate && !hasWriteGateSteps(data) {
		return fmt.Errorf("%s requires a write gate for workflows with write outputs. Import a shared workflow providing jobs.safe_outputs.gate-steps, or add gate-steps:\n  - run: ./decide-writes.sh", RepoConfigFileName)
	}
	return nil
}

func (c *Compiler) buildWriteGatePhaseSteps(data *WorkflowData) ([]string, error) {
	raw := writeGateSteps(data)
	if len(raw) == 0 {
		return nil, nil
	}
	initial, err := c.writeGateRuntimeStep("Initialize write gate", writeGateInitializeStepID, "initialize", nil, "")
	if err != nil {
		return nil, err
	}
	configured, ids, err := c.prepareWriteGateSteps(raw, data)
	if err != nil {
		return nil, err
	}
	outcomes := make([]string, len(ids))
	for index, id := range ids {
		outcomes[index] = "${{ steps." + id + ".outcome }}"
	}
	encoded, err := json.Marshal(outcomes)
	if err != nil {
		return nil, fmt.Errorf("write-gate outcome bindings could not be encoded: %w", err)
	}
	env := map[string]any{
		"GH_AW_WRITE_DECISIONS":          "${{ steps.initialize_write_gate.outputs.path }}",
		"GH_AW_WRITE_GATE_STEP_OUTCOMES": string(encoded),
		"GH_AW_WRITE_GATE_DRY_RUN":       fmt.Sprint(data.DryRun),
	}
	verdict, err := c.writeGateRuntimeStep("Validate write gate verdict", writeGateStepID, "main", env, "always()")
	if err != nil {
		return nil, err
	}
	return append(append([]string{initial}, configured...), verdict), nil
}

func (c *Compiler) prepareWriteGateSteps(raw []any, data *WorkflowData) ([]string, []string, error) {
	if data.DryRun {
		return nil, nil, nil
	}
	steps := make([]any, len(raw))
	ids := make([]string, len(raw))
	seen := map[string]bool{writeGateStepID: true, writeGateInitializeStepID: true}
	for index, value := range raw {
		step, ok := value.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("gate-steps[%d] expects a step object. Example: - run: ./decide-writes.sh", index)
		}
		copy := maps.Clone(step)
		id, err := writeGateConfiguredStepID(copy, index, seen)
		if err != nil {
			return nil, nil, err
		}
		ids[index], copy["id"] = id, id
		env, _ := copy["env"].(map[string]any)
		env = maps.Clone(env)
		if env == nil {
			env = make(map[string]any)
		}
		env["GH_AW_WRITE_DECISIONS"] = "${{ steps.initialize_write_gate.outputs.path }}"
		copy["env"] = env
		c.warnWriteGateCredentials(step)
		steps[index] = copy
	}
	pinned, err := c.extractPinnedJobSteps("gate-steps", "safe_outputs", map[string]any{"gate-steps": steps}, data)
	return pinned, ids, err
}

func writeGateConfiguredStepID(step map[string]any, index int, seen map[string]bool) (string, error) {
	id := fmt.Sprintf("aw_write_gate_%d", index+1)
	if value, exists := step["id"]; exists {
		var ok bool
		id, ok = value.(string)
		if !ok || !writeGateStepIDPattern.MatchString(id) {
			return "", fmt.Errorf("gate-steps[%d].id expects a valid step identifier. Example: id: check_policy", index)
		}
	}
	key := strings.ToLower(id)
	if seen[key] {
		return "", fmt.Errorf("gate-steps[%d].id %q conflicts with another gate or compiler-owned step. Use a unique identifier, for example: id: check_policy", index, id)
	}
	seen[key] = true
	return id, nil
}

func (c *Compiler) warnWriteGateCredentials(step map[string]any) {
	text := fmt.Sprint(step)
	if strings.Contains(text, "secrets.") || strings.Contains(text, "secrets[") ||
		strings.Contains(text, "github.token") || strings.Contains(text, "GITHUB_TOKEN") || strings.Contains(text, "GH_TOKEN") {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr("jobs.safe_outputs.gate-steps references credentials. Gate steps share a write-capable job token; keep them data-only and do not mint write tokens before the verdict."))
		c.IncrementWarningCount()
	}
}

func (c *Compiler) writeGateRuntimeStep(name, id, entry string, env map[string]any, condition string) (string, error) {
	step := map[string]any{
		"name": name,
		"id":   id,
		"uses": c.getActionPin("actions/github-script"),
		"with": map[string]any{"script": `const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
setupGlobals(core, github, context, exec, io, getOctokit);
const { ` + entry + ` } = require('${{ runner.temp }}/gh-aw/actions/write_gate_verdict.cjs');
` + entry + `();`},
	}
	if len(env) > 0 {
		step["env"] = env
	}
	if condition != "" {
		step["if"] = condition
	}
	return ConvertStepToYAML(step)
}

func writeGateExecutionCondition() ConditionNode {
	return BuildAnd(
		BuildEquals(BuildPropertyAccess("steps.write_gate.outcome"), BuildStringLiteral("success")),
		BuildOr(
			BuildEquals(BuildPropertyAccess("steps.write_gate.outputs.write_gate"), BuildStringLiteral("allow")),
			BuildEquals(BuildPropertyAccess("steps.write_gate.outputs.write_gate"), BuildStringLiteral("partial")),
		),
	)
}

func (c *Compiler) guardWriteGateSteps(lines []string) ([]string, error) {
	if len(lines) == 0 {
		return lines, nil
	}
	var steps []map[string]any
	if err := yaml.Unmarshal([]byte(strings.Join(lines, "")), &steps); err != nil {
		return nil, fmt.Errorf("write-gate publication steps could not be parsed: %w", err)
	}
	result := make([]string, 0, len(steps))
	for _, step := range steps {
		condition := writeGateExecutionCondition()
		if previous, exists := step["if"]; exists {
			switch value := previous.(type) {
			case string:
				condition = BuildAnd(condition, &ExpressionNode{Expression: c.extractExpressionFromIfString(value)})
			case bool, int, int64, uint64, float64:
				condition = BuildAnd(condition, &ExpressionNode{Expression: fmt.Sprint(value)})
			default:
				return nil, fmt.Errorf("publication step if expects a string, boolean or number, got %T", previous)
			}
		}
		step["if"] = RenderCondition(condition)
		rendered, err := ConvertStepToYAML(step)
		if err != nil {
			return nil, fmt.Errorf("write-gate publication step could not be rendered: %w", err)
		}
		result = append(result, rendered)
	}
	return result, nil
}

func (c *Compiler) insertWriteGatePhase(data *WorkflowData, steps, tokens []string, prefix string, outputs map[string]string) ([]string, error) {
	gate, err := c.buildWriteGatePhaseSteps(data)
	if err != nil {
		return nil, err
	}
	index := c.calculatePreambleInsertIndex(steps, data, prefix)
	for index < len(steps) && !strings.HasPrefix(steps[index], stepNameLinePrefix) {
		index++
	}
	guarded, err := c.guardWriteGateSteps(append(append([]string{}, tokens...), steps[index:]...))
	if err != nil {
		return nil, err
	}
	outputs["write_gate"] = "${{ steps.write_gate.outcome == 'success' && steps.write_gate.outputs.write_gate || 'deny' }}"
	outputs["write_gate_reason"] = "${{ (steps.write_gate.outcome == 'success' || steps.write_gate.outputs.write_gate == 'deny') && steps.write_gate.outputs.write_gate_reason || 'gate_error' }}"
	result := append(append([]string{}, steps[:index]...), gate...)
	return append(result, guarded...), nil
}
