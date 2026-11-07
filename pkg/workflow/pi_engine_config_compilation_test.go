//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestCompilePiDispatchModel(t *testing.T) {
	for _, expression := range []string{"${{ inputs.model }}", "${{ github.event.inputs.model }}"} {
		for _, firewall := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/firewall=%t", expression, firewall), func(t *testing.T) {
				dir := t.TempDir()
				source := filepath.Join(dir, "pi-dispatch.md")
				sandboxConfig := ""
				if !firewall {
					sandboxConfig = "strict: false\nfeatures:\n  dangerously-disable-sandbox-agent: true\nsandbox:\n  agent: false\n"
				}
				markdown := fmt.Sprintf(`---
on:
  workflow_dispatch:
    inputs:
      model:
        type: choice
        options: [grok-4.7, gpt-6.1-sol, claude-opus-5.5]
permissions:
  contents: read
  copilot-requests: write
checkout: false
engine:
  id: pi
  model: "copilot/%s"
%stools:
  github: false
safe-outputs:
  noop:
  threat-detection: false
---
Report status.
`, expression, sandboxConfig)
				require.NoError(t, os.WriteFile(source, []byte(markdown), 0o600))
				require.NoError(t, NewCompiler(WithVersion("dev")).CompileWorkflow(source))
				lock, err := os.ReadFile(filepath.Join(dir, "pi-dispatch.lock.yml"))
				require.NoError(t, err)
				var workflow struct {
					Jobs map[string]struct {
						Steps []struct {
							ID  string            `yaml:"id"`
							Run string            `yaml:"run"`
							Env map[string]string `yaml:"env"`
						} `yaml:"steps"`
					} `yaml:"jobs"`
				}
				require.NoError(t, yaml.Unmarshal(lock, &workflow))
				found := false
				for _, job := range workflow.Jobs {
					for _, step := range job.Steps {
						require.NotContains(t, step.Run, expression, "Model input must never be embedded in shell source")
						if step.ID != "agentic_execution" {
							continue
						}
						found = true
						require.Equal(t, "copilot/"+expression, step.Env["GH_AW_PI_MODEL"])
						require.NotContains(t, step.Env, "GH_AW_PI_MODEL_ID")
						require.Equal(t, "github-copilot", step.Env["GH_AW_PI_NATIVE_PROVIDER"])
						if firewall {
							require.Contains(t, step.Run, `--model "aw-gateway/${GH_AW_PI_MODEL#*/}"`)
							require.Contains(t, step.Run, "GH_AW_LLM_PROVIDER=github")
						} else {
							require.Contains(t, step.Run, `--model "${GH_AW_PI_NATIVE_PROVIDER}/${GH_AW_PI_MODEL#*/}"`)
						}
					}
				}
				require.True(t, found, "Compiled workflow must contain the pi execution step")
			})
		}
	}
}

func TestCompileDispatchModelAcrossEngines(t *testing.T) {
	tests := []struct {
		engine        string
		model         string
		modelEnv      string
		modelEnvValue string
	}{
		{engine: "copilot", model: "${{ inputs.model }}", modelEnv: "COPILOT_MODEL", modelEnvValue: "${{ inputs.model }}"},
		{engine: "claude", model: "${{ inputs.model }}", modelEnv: "ANTHROPIC_MODEL", modelEnvValue: "${{ inputs.model }}"},
		{engine: "codex", model: "copilot/${{ inputs.model }}", modelEnv: "GH_AW_MODEL_AGENT_CODEX", modelEnvValue: "${{ inputs.model }}"},
		{engine: "gemini", model: "${{ inputs.model }}", modelEnv: "GEMINI_MODEL", modelEnvValue: "${{ inputs.model }}"},
		{engine: "agy", model: "${{ inputs.model }}", modelEnv: "GH_AW_AGY_MODEL", modelEnvValue: "${{ inputs.model }}"},
		{engine: "pi", model: "copilot/${{ inputs.model }}", modelEnv: "GH_AW_PI_MODEL", modelEnvValue: "copilot/${{ inputs.model }}"},
	}
	expressions := []string{"${{ inputs.model }}", "${{ github.event.inputs.model }}"}

	for _, tt := range tests {
		for _, expression := range expressions {
			t.Run(tt.engine+"/"+expression, func(t *testing.T) {
				dir := t.TempDir()
				source := filepath.Join(dir, "dispatch-model.md")
				model := strings.ReplaceAll(tt.model, "${{ inputs.model }}", expression)
				modelEnvValue := strings.ReplaceAll(tt.modelEnvValue, "${{ inputs.model }}", expression)
				markdown := fmt.Sprintf(`---
on:
  workflow_dispatch:
    inputs:
      model:
        type: string
permissions:
  contents: read
  copilot-requests: write
checkout: false
engine:
  id: %s
  model: "%s"
tools:
  github: false
safe-outputs:
  noop:
  threat-detection: false
---
Report status.
`, tt.engine, model)
				require.NoError(t, os.WriteFile(source, []byte(markdown), 0o600))
				require.NoError(t, NewCompiler(WithVersion("dev")).CompileWorkflow(source))
				lock, err := os.ReadFile(filepath.Join(dir, "dispatch-model.lock.yml"))
				require.NoError(t, err)
				var workflow struct {
					Jobs map[string]struct {
						Steps []struct {
							ID  string            `yaml:"id"`
							Run string            `yaml:"run"`
							Env map[string]string `yaml:"env"`
						} `yaml:"steps"`
					} `yaml:"jobs"`
				}
				require.NoError(t, yaml.Unmarshal(lock, &workflow))

				found := false
				for _, job := range workflow.Jobs {
					for _, step := range job.Steps {
						if step.Env[tt.modelEnv] != modelEnvValue {
							continue
						}
						found = true
						require.NotContains(t, step.Run, expression, "Dispatch model must not be embedded in the shell command")
						if tt.engine == "pi" {
							require.NotContains(t, step.Env, "GH_AW_PI_MODEL_ID")
							require.Contains(t, step.Run, `--model "aw-gateway/${GH_AW_PI_MODEL#*/}"`)
							require.Contains(t, strings.ReplaceAll(step.Run, "${GH_AW_PI_MODEL#*/}", expression),
								`--model "aw-gateway/`+expression+`"`)
						}
					}
				}
				require.True(t, found, "Compiled workflow must pass the dispatch model through %s", tt.modelEnv)
			})
		}
	}
}

func TestCompilePiRejectsInvalidNestedConfiguration(t *testing.T) {
	for _, config := range []string{
		`{"model":{"maxTokenz":32000}}`,
		`{"model":{"api":"unsupported"}}`,
		`{"mcp":{"exposure":"unsupported"}}`,
		`{"mcp":{"toolExposure":{"github":"direct"}}}`,
	} {
		t.Run(config, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "pi-invalid.md")
			markdown := "---\non: workflow_dispatch\nengine:\n  id: pi\n  config: |\n    " + config + "\n---\nReport status.\n"
			require.NoError(t, os.WriteFile(source, []byte(markdown), 0o600))
			err := NewCompiler(WithVersion("dev")).CompileWorkflow(source)
			require.ErrorContains(t, err, "invalid nested configuration")
			_, statErr := os.Stat(filepath.Join(dir, "pi-invalid.lock.yml"))
			require.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}
