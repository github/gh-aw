//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func tasksWorkflowFixture(engine string) string {
	return "---\non: workflow_dispatch\npermissions:\n  contents: read\nengine:\n" + engine + `
tools:
  bash: false
  cli-proxy: false
  tasks:
    go: true
---
Run the configured tasks to check the source.
`
}

func TestTasksCompileEngines(t *testing.T) {
	for name, engine := range map[string]string{
		"copilot-sdk": "  id: copilot\n  copilot-sdk: true\n",
		"pi-cli":      "  id: pi\n",
		"pi-sdk":      "  id: pi\n  driver: pi_agent_core_driver.cjs\n",
		"pi-rpc":      "  id: pi\n  driver: pi_rpc_driver.cjs\n",
	} {
		t.Run(name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "tasks.md")
			require.NoError(t, os.WriteFile(filename, []byte(tasksWorkflowFixture(engine)), 0600))
			require.NoError(t, NewCompiler().CompileWorkflow(filename))
			content, err := os.ReadFile(strings.TrimSuffix(filename, ".md") + ".lock.yml")
			require.NoError(t, err)
			lock := string(content)
			require.Contains(t, lock, "tasks_runtime.cjs")
			require.Contains(t, lock, "tasks/manifest.json")
			require.Contains(t, lock, `"go.test"`)
			require.Contains(t, lock, `"args":["test","-count=1","./..."]`)
			require.NotContains(t, lock, `mcp-proxy:tasks`)
			require.Contains(t, lock, `"name":"tasks","tools":["run_task"]`)
			if name == "copilot-sdk" {
				require.Contains(t, lock, "tasks(run_task)")
				require.Contains(t, lock, "gh-aw/copilot-sdk")
			}
		})
	}
}

func TestTasksValidationAndRoundTrip(t *testing.T) {
	compiler := NewCompiler()
	data, err := compiler.ParseWorkflowString(tasksWorkflowFixture("  id: pi\n"), "tasks.md")
	require.NoError(t, err)
	require.NoError(t, validateTasks(data))
	require.Contains(t, data.ParsedTools.GetToolNames(), "tasks")
	require.Equal(t, data.Tools["tasks"], data.ParsedTools.ToMap()["tasks"])
	for name, mutate := range map[string]func(*WorkflowData){
		"unsupported engine":   func(d *WorkflowData) { d.EngineConfig.ID = "claude" },
		"no SDK":               func(d *WorkflowData) { d.EngineConfig.ID = "copilot" },
		"custom cwd":           func(d *WorkflowData) { d.EngineConfig.Cwd = "nested" },
		"custom driver":        func(d *WorkflowData) { d.EngineConfig.Driver = "custom.cjs" },
		"custom arguments":     func(d *WorkflowData) { d.EngineConfig.Args = []string{"--help"} },
		"checkout disabled":    func(d *WorkflowData) { d.CheckoutDisabled = true },
		"proxy enabled":        func(d *WorkflowData) { d.Tools["cli-proxy"] = true },
		"reserved environment": func(d *WorkflowData) { d.EngineConfig.Env = map[string]string{"GH_AW_TASKS_MCP": "{}"} },
		"workspace override":   func(d *WorkflowData) { d.EngineConfig.Env = map[string]string{"GITHUB_WORKSPACE": "/tmp"} },
		"sampled execution":    func(d *WorkflowData) { d.UseSamples = true },
	} {
		t.Run(name, func(t *testing.T) {
			fresh, err := compiler.ParseWorkflowString(tasksWorkflowFixture("  id: pi\n"), "tasks.md")
			require.NoError(t, err)
			mutate(fresh)
			require.Error(t, validateTasks(fresh))
		})
	}
	data.IsDetectionRun = true
	require.Empty(t, resolvedWorkflowTasks(data))
	require.Equal(t, "pi", wrapTasksEngineCommand(data, "pi"))
	data.IsDetectionRun = false
	data.IsEvalsRun = true
	require.Empty(t, resolvedWorkflowTasks(data))
}

func TestTasksSharedImports(t *testing.T) {
	dir := t.TempDir()
	shared := "---\ntools:\n  tasks:\n    go: true\n    check:\n      description: Check source\n      command: go\n      args: [vet, ./...]\n---\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte(shared), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "nested.md"), []byte("---\nimports: [shared.md]\n---\n"), 0600))
	for _, test := range []struct {
		name         string
		local        string
		wantManifest bool
	}{
		{"imported", "", true},
		{"disabled", "  tasks: false\n", false},
		{"duplicate", "  tasks:\n    check:\n      description: Check source\n      command: go\n      args: [vet, ./...]\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := "---\non: workflow_dispatch\npermissions:\n  contents: read\nengine: pi\nimports: [nested.md]\ntools:\n  cli-proxy: false\n" + test.local + "---\nCheck source.\n"
			filename := filepath.Join(dir, test.name+".md")
			require.NoError(t, os.WriteFile(filename, []byte(content), 0600))
			require.NoError(t, NewCompiler().CompileWorkflow(filename))
			lock, err := os.ReadFile(filepath.Join(dir, test.name+".lock.yml"))
			require.NoError(t, err)
			require.Equal(t, test.wantManifest, strings.Contains(string(lock), "tasks_runtime.cjs"))
		})
	}
	filename := filepath.Join(dir, "conflict.md")
	conflict := "---\non: workflow_dispatch\npermissions:\n  contents: read\nengine: pi\nimports: [nested.md]\ntools:\n  cli-proxy: false\n  tasks:\n    check:\n      description: Check source\n      command: go\n      args: [build, ./...]\n---\nCheck source.\n"
	require.NoError(t, os.WriteFile(filename, []byte(conflict), 0600))
	err := NewCompiler().CompileWorkflow(filename)
	require.ErrorContains(t, err, "tools.tasks.check")
	require.ErrorContains(t, err, "shared.md")
}

func TestTasksImportInputs(t *testing.T) {
	dir := t.TempDir()
	shared := "---\ninputs:\n  package:\n    type: string\n    default: ./...\ntools:\n  tasks:\n    check:\n      description: Check source\n      command: go\n      args: [vet, '${{ github.aw.inputs.package }}']\n---\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte(shared), 0600))
	main := "---\non: workflow_dispatch\npermissions:\n  contents: read\nengine: pi\nimports:\n  - path: shared.md\n    inputs:\n      package: ./pkg/example/...\ntools:\n  cli-proxy: false\n---\nRun tasks(run_task).\n"
	filename := filepath.Join(dir, "tasks.md")
	require.NoError(t, os.WriteFile(filename, []byte(main), 0600))
	require.NoError(t, NewCompiler().CompileWorkflow(filename))
	lock, err := os.ReadFile(filepath.Join(dir, "tasks.lock.yml"))
	require.NoError(t, err)
	require.Contains(t, string(lock), `"args":["vet","./pkg/example/..."]`)
	require.NotContains(t, string(lock), "github.aw.inputs.package")
}

func TestTasksInvalidCompile(t *testing.T) {
	for _, invalid := range []string{
		"check:\n      description: Invalid\n      command: go\n      args: ['${{ github.sha }}']",
		"constructor:\n      description: Invalid\n      command: go",
		"go.test:\n      description: Invalid\n      command: go",
	} {
		filename := filepath.Join(t.TempDir(), "tasks.md")
		require.NoError(t, os.WriteFile(filename, []byte(strings.Replace(tasksWorkflowFixture("  id: copilot\n  copilot-sdk: true\n"), "go: true", invalid, 1)), 0600))
		require.Error(t, NewCompiler().CompileWorkflow(filename))
	}
}
