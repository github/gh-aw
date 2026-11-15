package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

const pendingIntegrityStepID = "verify_pending_writes"
const pendingNormalizedDecisionsPath = "/tmp/gh-aw/write-gate/write-decisions.normalized.json"

func (c *Compiler) pendingGateEnvironment(data *WorkflowData) (map[string]any, error) {
	lines, err := c.pendingWritesRuntimeEnv(data)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		Env map[string]any `yaml:"env"`
	}
	if err := yaml.Unmarshal([]byte("env:\n"+strings.Join(lines, "")), &wrapper); err != nil {
		return nil, fmt.Errorf("decoding pending-write runtime environment: %w", err)
	}
	wrapper.Env["GH_AW_PENDING_WRITES_REQUIRED"] = "true"
	wrapper.Env["GH_AW_PENDING_WRITES_SHA256"] = "${{ steps.pending_writes.outputs.sha256 }}"
	wrapper.Env["GH_AW_PENDING_WRITE_INPUTS_SHA256"] = "${{ steps.pending_writes.outputs.pending_write_inputs_sha256 }}"
	wrapper.Env["GH_AW_PENDING_WRITES_BINDINGS"] = "${{ steps.pending_writes.outputs.bindings }}"
	return wrapper.Env, nil
}

func (c *Compiler) buildPendingIntegrityStep(data *WorkflowData) (string, error) {
	env, err := c.pendingGateEnvironment(data)
	if err != nil {
		return "", err
	}
	step := map[string]any{
		"name": "Verify pending-write input integrity", "id": pendingIntegrityStepID, "if": "always()",
		"uses": c.getActionPin("actions/github-script"), "env": env,
		"with": map[string]any{"script": `const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');
setupGlobals(core, github, context, exec, io, getOctokit);
const result = await require('${{ runner.temp }}/gh-aw/actions/pending_writes_runtime.cjs').verifyFromEnv();
core.setOutput('allowed', String(result.allow));
core.setOutput('reason', result.reason);`},
	}
	return ConvertStepToYAML(step)
}

func (c *Compiler) bindPendingHandlerEnvironment(data *WorkflowData, lines []string) ([]string, error) {
	var wrapper struct {
		Steps []map[string]any `yaml:"steps"`
	}
	if err := yaml.Unmarshal([]byte("steps:\n"+strings.Join(lines, "")), &wrapper); err != nil {
		return nil, fmt.Errorf("reading pending-write handler steps: %w", err)
	}
	var result []string
	for _, step := range wrapper.Steps {
		if step["id"] == "process_safe_outputs" {
			env, ok := step["env"].(map[string]any)
			if !ok {
				return nil, errors.New("pending-write handler environment is missing")
			}
			bound, err := c.pendingGateEnvironment(data)
			if err != nil {
				return nil, err
			}
			for _, name := range []string{"GH_AW_PENDING_WRITES_REQUIRED", "GH_AW_PENDING_WRITES", "GH_AW_PENDING_WRITES_SHA256", "GH_AW_PENDING_WRITES_BINDINGS", "GH_AW_PENDING_WRITES_SPEC"} {
				env[name] = bound[name]
			}
			env["GH_AW_WRITE_GATE_DECISIONS"] = pendingNormalizedDecisionsPath
			env["GH_AW_WRITE_GATE_DECISIONS_SHA256"] = "${{ steps.write_gate.outputs.write_gate_decisions_sha256 }}"
		}
		rendered, err := ConvertStepToYAML(step)
		if err != nil {
			return nil, fmt.Errorf("binding pending-write handler environment: %w", err)
		}
		result = append(result, rendered)
	}
	return result, nil
}
