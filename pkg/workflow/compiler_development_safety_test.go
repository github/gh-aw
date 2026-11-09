//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDryRunRejectsUnsupportedEffects(t *testing.T) {
	for _, test := range []struct {
		name string
		data WorkflowData
	}{
		{"pre-steps", WorkflowData{PreSteps: "- run: echo unsafe"}},
		{"pre-agent-steps", WorkflowData{PreAgentSteps: "- run: echo unsafe"}},
		{"on.steps", WorkflowData{OnSteps: []map[string]any{{"run": "echo unsafe"}}}},
		{"custom MCP servers", WorkflowData{ResolvedMCPServers: map[string]any{"external": map[string]any{"command": "unsafe"}}}},
		{"custom MCP servers", WorkflowData{ParsedTools: &Tools{Custom: map[string]MCPServerConfig{"external": {Command: "unsafe"}}}}},
		{"mcp-scripts", WorkflowData{MCPScripts: &MCPScriptsConfig{}}},
		{"safe-outputs.scripts", WorkflowData{SafeOutputs: &SafeOutputsConfig{Scripts: map[string]*SafeScriptConfig{"publish": {}}}}},
		{"graders.operational-value.run", WorkflowData{Graders: &GradersConfig{Graders: map[string]*GraderDefinition{"operational-value": {Run: "grade.sh"}}}}},
		{"permissions.id-token", WorkflowData{Permissions: "permissions:\n  id-token: write\n"}},
		{"on.permissions.id-token", WorkflowData{OnPermissions: NewPermissionsParser("permissions:\n  id-token: write\n").ToPermissions()}},
		{"jobs.agent.pre-steps", WorkflowData{Jobs: map[string]any{"agent": map[string]any{"pre-steps": []any{map[string]any{"run": "echo unsafe"}}}}}},
		{"jobs.activation.steps", WorkflowData{Jobs: map[string]any{"activation": map[string]any{"steps": []any{map[string]any{"run": "echo unsafe"}}}}}},
		{"jobs.agent.permissions", WorkflowData{Jobs: map[string]any{"agent": map[string]any{"permissions": map[string]any{"id-token": "write"}}}}},
		{"jobs.upload_assets.uses", WorkflowData{Jobs: map[string]any{"upload_assets": map[string]any{"uses": "./worker.yml", "secrets": "inherit"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := NewCompiler()
			require.NoError(t, c.validateDryRunEffects(&test.data), "normal compilation is unchanged")
			c.SetDryRun(true)
			err := c.validateDryRunEffects(&test.data)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.name)
			assert.NotContains(t, err.Error(), "echo unsafe")
		})
	}
	c := NewCompiler()
	c.SetDryRun(true)
	require.NoError(t, c.validateDryRunEffects(&WorkflowData{
		Permissions: "permissions:\n  contents: read\n  copilot-requests: write\n",
		Jobs: map[string]any{
			"custom": map[string]any{"steps": []any{map[string]any{"run": "echo disabled"}}},
		},
	}))
}

func TestDryRunWorkflowStepPreparation(t *testing.T) {
	data := &WorkflowData{
		On: "on: workflow_dispatch", CustomSteps: "steps:\n  - run: echo setup\n",
		PostSteps: "post-steps:\n  - run: echo cleanup\n",
	}
	c := NewCompiler()
	c.SetDryRun(true)
	result, err := c.prepareDryRunWorkflowData(data)
	require.NoError(t, err)
	assert.Empty(t, result.CustomSteps)
	assert.Empty(t, result.PostSteps)
	assert.NotEmpty(t, data.CustomSteps)
	assert.NotEmpty(t, data.PostSteps)
	c.SetDryRun(false)
	result, err = c.prepareDryRunWorkflowData(data)
	require.NoError(t, err)
	assert.Same(t, data, result)
}

func TestDryRunCompilesWorkflowStepsAndPostSteps(t *testing.T) {
	for _, imported := range []bool{false, true} {
		t.Run(map[bool]string{false: "local", true: "imported"}[imported], func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "workflow.md")
			source := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
network:
  allowed: [defaults]
steps:
  - name: Set up trusted test fixture
    run: echo setup
    env:
      HOST_SECRET: ${{ secrets.HOST_SECRET }}
  - uses: actions/create-github-app-token@64ab729a6294000f5aa4a4bc0b73d1f32bc45510
    id: create-token
    with:
      app-id: "123"
      private-key: ${{ secrets.HOST_APP_KEY }}
      repositories: ${{ github.repository }}
      permission-contents: read
post-steps:
  - name: Clean up trusted test fixture
    if: always()
    run: echo cleanup
---
Analyze the repository.
`
			if imported {
				_, steps, found := strings.Cut(source, "steps:\n")
				require.True(t, found)
				require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte("---\nsteps:\n"+steps), 0600))
				source = "---\non: workflow_dispatch\nengine: copilot\npermissions:\n  contents: read\nnetwork:\n  allowed: [defaults]\nimports: [shared.md]\n---\nAnalyze the repository.\n"
			}
			require.NoError(t, os.WriteFile(path, []byte(source), 0600))

			c := NewCompiler()
			c.SetStrictMode(true)
			c.SetDryRun(true)
			require.NoError(t, c.CompileWorkflow(path))

			lock, err := os.ReadFile(filepath.Join(dir, "workflow.lock.yml"))
			require.NoError(t, err)
			for _, effect := range []string{"echo setup", "echo cleanup", "HOST_SECRET", "HOST_APP_KEY", "uses: actions/create-github-app-token", "id: create-token"} {
				assert.NotContains(t, string(lock), effect)
			}
			assert.Contains(t, string(lock), "name: Execute GitHub Copilot")
			var compiled struct {
				Jobs map[string]struct {
					Permissions map[string]string
				}
			}
			require.NoError(t, yaml.Unmarshal(lock, &compiled))
			require.Contains(t, compiled.Jobs, "agent")
			assert.NotEqual(t, "write", compiled.Jobs["agent"].Permissions["id-token"])

			c.SetDryRun(false)
			require.NoError(t, c.CompileWorkflow(path))
			lock, err = os.ReadFile(filepath.Join(dir, "workflow.lock.yml"))
			require.NoError(t, err)
			assert.Contains(t, string(lock), "echo setup")
			assert.Contains(t, string(lock), "echo cleanup")
			assert.Contains(t, string(lock), "HOST_SECRET")
			assert.Contains(t, string(lock), "HOST_APP_KEY")
			require.NoError(t, yaml.Unmarshal(lock, &compiled))
			assert.Equal(t, "write", compiled.Jobs["agent"].Permissions["id-token"])
		})
	}
}

func TestDryRunCustomJobsAreInert(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "dry-run"}[enabled], func(t *testing.T) {
			c := NewCompiler()
			c.SetDryRun(enabled)
			for _, name := range []string{"cleanup", "publish_summary", "reusable", "agent"} {
				job := &Job{
					Name: name, If: "${{ always() }}", Permissions: "permissions:\n  issues: write\n",
					Needs: []string{"activation"}, Outputs: map[string]string{"result": "${{ steps.publish.outputs.result }}"},
					Env:   map[string]string{"GH_AW_SAFE_OUTPUTS_STAGED": "${{ inputs.staged }}"},
					Steps: []string{"      - id: publish\n        run: gh issue close 1\n"},
				}
				if name == "reusable" {
					job.Steps = nil
					job.Uses = "./.github/workflows/worker.yml"
					job.SecretsInherit = true
				}
				require.NoError(t, c.jobManager.AddJob(job))
			}
			data := &WorkflowData{
				Jobs: map[string]any{"cleanup": nil, "reusable": nil, "agent": nil},
				SafeOutputs: &SafeOutputsConfig{
					Jobs: map[string]*SafeJobConfig{"publish-summary": {}},
				},
			}
			c.disableDryRunCustomJobs(data)
			for _, name := range []string{"cleanup", "publish_summary", "reusable", "agent"} {
				job, exists := c.jobManager.GetJob(name)
				require.True(t, exists)
				if enabled && name != "agent" {
					assert.Equal(t, "${{ !always() }}", job.If)
					assert.Equal(t, "permissions: {}", job.Permissions)
					assert.Empty(t, job.Uses)
					assert.False(t, job.SecretsInherit)
					assert.Empty(t, job.Env)
					assert.Equal(t, "${{ '' }}", job.Outputs["result"])
					assert.NotContains(t, strings.Join(job.Steps, ""), "gh issue close")
					assert.Nil(t, job.Actions)
				} else {
					assert.Equal(t, "${{ always() }}", job.If)
					assert.Contains(t, job.Permissions, "issues: write")
				}
				assert.Equal(t, []string{"activation"}, job.Needs)
			}
		})
	}
}

func TestDryRunCustomJobCompilation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.md")
	source := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
jobs:
  cleanup:
    runs-on: ubuntu-latest
    permissions:
      issues: write
    steps:
      - run: gh issue close 1
  remote:
    uses: ./.github/workflows/worker.yml
    secrets: inherit
safe-outputs:
  jobs:
    publish:
      description: Publish the result
      permissions:
        issues: write
      steps:
        - run: gh issue create --title unsafe --body unsafe
          env:
            GH_AW_SAFE_OUTPUTS_STAGED: "false"
---
Analyze the repository.
`
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	c := NewCompiler()
	c.SetSkipValidation(true)
	c.SetApprove(true)
	c.SetDryRun(true)
	require.NoError(t, c.CompileWorkflow(path))
	lock, err := os.ReadFile(filepath.Join(dir, "custom.lock.yml"))
	require.NoError(t, err)
	assert.NotContains(t, string(lock), "gh issue close")
	assert.NotContains(t, string(lock), "gh issue create")
	assert.NotContains(t, string(lock), "secrets: inherit")
	assert.NotContains(t, string(lock), "issues: write")
	assert.NotContains(t, string(lock), "./.github/workflows/worker.yml")
	assert.Equal(t, 3, strings.Count(string(lock), "name: Custom job disabled in dry-run"))
	c.SetDryRun(false)
	require.NoError(t, c.CompileWorkflow(path))
	lock, err = os.ReadFile(filepath.Join(dir, "custom.lock.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(lock), "gh issue close")
	assert.Contains(t, string(lock), "gh issue create")
	assert.Contains(t, string(lock), "secrets: inherit")
}

func TestDryRunRejectsImportedHostEffects(t *testing.T) {
	for _, declaration := range []string{
		"pre-steps:\n  - run: echo host-script\n",
		"mcp-servers:\n  external:\n    command: npx\n    args: [external-server]\n    allowed: [read]\n",
		"permissions:\n  id-token: write\n",
	} {
		t.Run(strings.Split(declaration, ":")[0], func(t *testing.T) {
			dir := t.TempDir()
			importPath := filepath.Join(dir, "shared.md")
			path := filepath.Join(dir, "workflow.md")
			require.NoError(t, os.WriteFile(importPath, []byte("---\n"+declaration+"---\nShared configuration.\n"), 0600))
			source := `---
on: workflow_dispatch
engine: copilot
imports: [shared.md]
---
Analyze the repository.
`
			if strings.HasPrefix(declaration, "permissions:") {
				source = strings.Replace(source, "engine: copilot\n", "engine: copilot\n"+declaration, 1)
			}

			require.NoError(t, os.WriteFile(path, []byte(source), 0600))
			c := NewCompiler()
			c.SetSkipValidation(true)
			c.SetApprove(true)
			c.SetDryRun(true)
			require.ErrorContains(t, c.CompileWorkflow(path), "--dry-run cannot safely execute")
			require.NoFileExists(t, filepath.Join(dir, "workflow.lock.yml"))
		})
	}
}

func TestDryRunRejectsReusableCallUnderOptionalBuiltinName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.md")
	require.NoError(t, os.WriteFile(path, []byte(`---
on: workflow_dispatch
engine: copilot
jobs:
  upload_assets:
    uses: ./.github/workflows/worker.yml
    secrets: inherit
---
Analyze the repository.
`), 0600))
	c := NewCompiler()
	c.SetSkipValidation(true)
	c.SetApprove(true)
	c.SetDryRun(true)
	require.ErrorContains(t, c.CompileWorkflow(path), "jobs.upload_assets.uses")
	require.NoFileExists(t, filepath.Join(dir, "workflow.lock.yml"))
	c.SetDryRun(false)
	require.ErrorContains(t, c.CompileWorkflow(path), "jobs.upload_assets: this name is reserved")
	require.NoFileExists(t, filepath.Join(dir, "workflow.lock.yml"))
}
