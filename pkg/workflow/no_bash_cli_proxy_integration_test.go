//go:build integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoRepositoryProfileNoBashCreatePRCompileIntegration(t *testing.T) {
	for _, test := range []struct {
		name          string
		bash          string
		literalTarget bool
	}{
		{name: "false", bash: "false"},
		{name: "empty list", bash: "[]"},
		{name: "literal target and matching allowlist", bash: "false", literalTarget: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "go-repository.md")
			markdown := `---
on: workflow_dispatch
engine:
  id: copilot
  copilot-sdk: true
  tool-profile: go-repository
  harness:
    max-retries: 0
max-tool-denials: 1
max-ai-credits: 1000
permissions:
  contents: read
sandbox:
  agent: awf
tools:
  bash: ` + test.bash + `
  cli-proxy: false
  github:
    mode: local
    min-integrity: none
    allowed: [get_file_contents]
safe-outputs:
  create-pull-request:
  threat-detection: false
---

Update the Go repository using the configured tools.
`
			if test.literalTarget {
				markdown = strings.Replace(markdown, "  create-pull-request:\n", `  create-pull-request:
    target-repo: jpmicrosoft/fam
    allowed-repos: [jpmicrosoft/fam]
`, 1)
			}
			require.NoError(t, os.WriteFile(path, []byte(markdown), 0o600))
			data, err := NewCompiler().ParseWorkflowFile(path)
			require.NoError(t, err)
			assert.True(t, data.BashDisabled)
			if test.literalTarget {
				assert.Equal(t, "jpmicrosoft/fam", data.SafeOutputs.CreatePullRequests.TargetRepoSlug)
				assert.Equal(t, []string{"jpmicrosoft/fam"}, data.SafeOutputs.CreatePullRequests.AllowedRepos)
			}
			require.NoError(t, NewCompiler().CompileWorkflow(path))
			raw, err := os.ReadFile(filepath.Join(dir, "go-repository.lock.yml"))
			require.NoError(t, err)
			compiled := string(raw)
			assert.NotContains(t, compiled, "mcp_cli_tools_with_safeoutputs_prompt.md")
			assert.NotContains(t, compiled, "mcp_cli_tools_prompt.md")
			assert.NotContains(t, compiled, "GH_AW_MCP_CLI_SERVERS_LIST")
			assert.Contains(t, compiled, "<safe-output-tools>")

			var workflow struct {
				Jobs map[string]struct {
					Steps []struct {
						ID   string            `yaml:"id"`
						Name string            `yaml:"name"`
						Run  string            `yaml:"run"`
						Env  map[string]string `yaml:"env"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(raw, &workflow))
			require.Contains(t, workflow.Jobs, "safe_outputs", "publication must retain its separate job")
			found := false
			checkout := false
			sdkInstalledOutsideCheckout := false
			for _, step := range workflow.Jobs["agent"].Steps {
				checkout = checkout || step.Name == "Checkout repository"
				if step.Name == "Install GitHub Copilot SDK (Node.js)" {
					assert.Contains(t, step.Run, copilotSDKRepositoryInstallDir)
					assert.NotContains(t, step.Run, "GITHUB_WORKSPACE")
					sdkInstalledOutsideCheckout = true
				}
				if step.ID != "agentic_execution" {
					continue
				}
				found = true
				var config copilotSDKToolConfig
				require.NoError(t, json.Unmarshal([]byte(step.Env[constants.CopilotSDKToolConfigEnvVar]), &config))
				assert.Equal(t, 2, config.Version)
				assert.False(t, config.Capabilities.Bash)
				assert.False(t, config.Capabilities.CLIProxy)
				assert.True(t, config.Capabilities.Edit)
				assert.True(t, config.Capabilities.MCP)
				assert.Equal(t, []string{"bash", "cli-proxy"}, config.ExplicitlyDisabledTools)
				assert.Equal(t, []string{"github(get_file_contents)", "go_repository", "read", "safeoutputs", "write"}, config.Permissions.AllowedTools)
				if test.literalTarget {
					require.NotNil(t, config.Profile)
					assert.Equal(t, "jpmicrosoft/fam", config.Profile.Policy["target-repo"])
				}
				assert.Equal(t, goRepositoryDefaultBranchExpression, step.Env[goRepositoryDefaultBranchEnv])
				assert.Contains(t, step.Run, `export GH_AW_MCP_CONFIG="${RUNNER_TEMP}/gh-aw/mcp-config/copilot-sdk.json"`)
				assert.Contains(t, step.Run, `export NODE_PATH="${RUNNER_TEMP}/gh-aw/copilot-sdk/node_modules"`)
			}
			assert.True(t, found)
			assert.True(t, checkout, "no-shell model tools must not remove the infrastructure checkout")
			assert.True(t, sdkInstalledOutsideCheckout, "SDK installation must not dirty the initial Go checkout")
		})
	}
}

func TestNoBashSafeOutputsUsesMCPOnlyPromptIntegration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "no-bash-safeoutputs-mcp-only")
	workflowPath := filepath.Join(tmpDir, "no-bash-safeoutputs.md")
	workflowContent := `---
on: issues
name: No Bash Safe Outputs
engine: codex
tools:
  bash: false
  cli-proxy: false
  github:
    mode: local
    min-integrity: none
safe-outputs:
  add-labels:
---

Add a label safely.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o600))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockPath := filepath.Join(tmpDir, "no-bash-safeoutputs.lock.yml")
	compiledBytes, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	compiled := string(compiledBytes)

	assert.Contains(t, compiled, "-c features.shell_tool=false",
		"Codex should receive the no-shell runtime setting when bash is disabled")
	assert.Contains(t, compiled, "Mount MCP servers as CLIs",
		"safeoutputs should still be mounted as a CLI for command-based harnesses")
	assert.Contains(t, compiled, "[mcp_servers.safeoutputs]",
		"safeoutputs must remain available as an MCP server")
	assert.Contains(t, compiled, "<safe-output-tools>",
		"safe output MCP guidance should remain in the prompt")
	assert.NotContains(t, compiled, "mcp_cli_tools_with_safeoutputs_prompt.md",
		"no-shell workflows must not advertise the bash-only safeoutputs CLI prompt")
	assert.NotContains(t, compiled, "mcp_cli_tools_prompt.md",
		"no-shell workflows must not advertise MCP CLI prompts")
	assert.NotContains(t, compiled, "GH_AW_MCP_CLI_SERVERS_LIST",
		"the prompt substitution env should be omitted with the MCP CLI prompt")
}

func TestNoBashShellBackedToolModesRejectedIntegration(t *testing.T) {
	tests := []struct {
		name          string
		tools         string
		errorContains string
	}{
		{
			name: "cli proxy true",
			tools: `  bash: false
  cli-proxy: true
  github:
    mode: local`,
			errorContains: "tools.cli-proxy",
		},
		{
			name: "github gh proxy",
			tools: `  bash: false
  cli-proxy: false
  github:
    mode: gh-proxy`,
			errorContains: "tools.github.mode: gh-proxy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "no-bash-shell-backed-mode")
			workflowPath := filepath.Join(tmpDir, strings.ReplaceAll(tt.name, " ", "-")+".md")
			workflowContent := `---
on: push
name: No Bash Invalid Tools
engine: codex
tools:
` + tt.tools + `
safe-outputs:
  create-issue:
---

Invalid no-shell workflow.
`
			require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o600))

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(workflowPath)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorContains)
		})
	}
}
