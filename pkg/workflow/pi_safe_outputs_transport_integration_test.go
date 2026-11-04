//go:build integration

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// TestPiSafeOutputsPromptStatesCLIIsOnlyTransportIntegration verifies that a compiled
// Pi workflow with cli-proxy enabled never describes the safeoutputs CLI as an
// optional alternative to a direct tool call.
func TestPiSafeOutputsPromptStatesCLIIsOnlyTransportIntegration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "pi-safeoutputs-cli-only")
	workflowDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowDir, 0o700))
	workflowPath := filepath.Join(workflowDir, "pi-safeoutputs.md")
	workflowContent := `---
on: issues
name: Pi Safe Outputs
engine: pi
tools:
  bash: ["*"]
  cli-proxy: true
safe-outputs:
  create-pull-request:
---

Fix the bug and open a pull request.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o600))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockPath := filepath.Join(workflowDir, "pi-safeoutputs.lock.yml")
	compiledBytes, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	compiled := string(compiledBytes)

	assert.NotContains(t, compiled, "optional equivalent transport",
		"CLI-proxy Pi workflows must not describe the safeoutputs CLI as an optional transport")
	assert.NotContains(t, compiled, "you may use that CLI form instead",
		"CLI-proxy Pi workflows must not describe the safeoutputs CLI as an alternative form")
	assert.Contains(t, compiled, safeOutputsCLIOnlyTransportPromptFile,
		"CLI-proxy Pi workflows must load the CLI-only safe-output transport prompt")
	assert.NotContains(t, compiled, safeOutputsMCPTransportPromptFile,
		"CLI-proxy Pi workflows must not load the MCP-safe-output transport prompt")

	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string            `yaml:"name"`
				Env  map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(compiledBytes, &workflow))
	var promptEnv map[string]string
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if step.Name == "Create prompt with built-in context" {
				promptEnv = step.Env
			}
		}
	}
	require.NotNil(t, promptEnv, "compiled workflow should include the prompt creation step")
	promptEnv["GITHUB_WORKSPACE"] = tmpDir
	env, err := json.Marshal(promptEnv)
	require.NoError(t, err)
	cmd := exec.Command("node", "-e", `
const fs = require('node:fs');
const { parseConfig, renderPrompt } = require('./actions/setup/js/create_prompt.cjs');
const env = JSON.parse(fs.readFileSync(0, 'utf8'));
process.stdout.write(renderPrompt(parseConfig(env.GH_AW_PROMPT_CONFIG), env, './actions/setup/md'));
`)
	cmd.Dir = filepath.Join("..", "..")
	cmd.Stdin = strings.NewReader(string(env))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	prompt := string(output)
	assert.Contains(t, prompt, "`safeoutputs --help`, by itself")
	assert.NotContains(t, prompt, "command -v safeoutputs")
	assert.Contains(t, prompt, "single-quoted")
	assert.Contains(t, prompt, "Stdin is also supported")
	assert.Contains(t, prompt, `safeoutputs add_comment '{"item_number":42,"body":"### Result\n\nIt\u0027s ready.`)
	assert.NotContains(t, prompt, "jq -Rs")
	assert.NotContains(t, prompt, "command -v node")
	assert.Contains(t, prompt, "no supported transport is available for this engine")
	assert.Contains(t, prompt, "`report_missing_tool`")
}

func TestPiSafeOutputsPromptUsesMCPTransportIntegration(t *testing.T) {
	for _, driver := range []string{"", "pi_agent_core_driver.cjs", "pi_rpc_driver.cjs"} {
		for _, transport := range []struct {
			name  string
			tools string
		}{
			{"default", "  bash: [\"echo\"]"},
			{"native", "  bash: [\"echo\"]\n  cli-proxy: false"},
			{"gh-proxy", "  bash: [\"echo\"]\n  github:\n    mode: gh-proxy"},
			{"no-shell", "  bash: false\n  cli-proxy: false"},
		} {
			t.Run(driver+"/"+transport.name, func(t *testing.T) {
				tmpDir := testutil.TempDir(t, "pi-safeoutputs-mcp")
				workflowPath := filepath.Join(tmpDir, "pi-safeoutputs.md")
				engine := "engine: pi"
				if driver != "" {
					engine = "engine:\n  id: pi\n  driver: " + driver
				}
				workflowContent := fmt.Sprintf(`---
on: issues
name: Pi Safe Outputs
%s
tools:
%s
safe-outputs:
  create-pull-request:
---

Fix the bug and open a pull request.
`, engine, transport.tools)
				require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o600))

				compiler := NewCompiler()
				require.NoError(t, compiler.CompileWorkflow(workflowPath))

				compiledBytes, err := os.ReadFile(filepath.Join(tmpDir, "pi-safeoutputs.lock.yml"))
				require.NoError(t, err)
				compiled := string(compiledBytes)

				assert.Contains(t, compiled, safeOutputsMCPTransportPromptFile)
				assert.NotContains(t, compiled, safeOutputsCLIOnlyTransportPromptFile)
				assert.Contains(t, compiled, "export GH_AW_PI_NATIVE_MCP=1")
			})
		}
	}
}

// TestCopilotSafeOutputsPromptKeepsOptionalCLITransportIntegration verifies that engines
// with native MCP tool-calling keep the transport-neutral wording.
func TestCopilotSafeOutputsPromptKeepsOptionalCLITransportIntegration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "copilot-safeoutputs-optional-cli")
	workflowPath := filepath.Join(tmpDir, "copilot-safeoutputs.md")
	workflowContent := `---
on: issues
name: Copilot Safe Outputs
engine: copilot
tools:
  bash: ["echo"]
safe-outputs:
  create-pull-request:
---

Fix the bug and open a pull request.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o600))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockPath := filepath.Join(tmpDir, "copilot-safeoutputs.lock.yml")
	compiledBytes, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	compiled := string(compiledBytes)

	assert.Contains(t, compiled, safeOutputsMCPTransportPromptFile,
		"MCP-capable workflows should load the MCP-safe-output transport prompt")
	assert.NotContains(t, compiled, safeOutputsCLIOnlyTransportPromptFile,
		"MCP-capable workflows must not load the CLI-only safe-output transport prompt")
}
