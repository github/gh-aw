//go:build !integration

package workflow

import (
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

type compiledProgramWorkflow struct {
	Jobs map[string]struct {
		Permissions map[string]string `yaml:"permissions"`
		Steps       []struct {
			ID   string            `yaml:"id"`
			Name string            `yaml:"name"`
			Uses string            `yaml:"uses"`
			Run  string            `yaml:"run"`
			Env  map[string]string `yaml:"env"`
			With map[string]string `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// compileProgramWorkflows writes the given files into .github/workflows, compiles
// `target`, and returns the parsed lock file.
func compileProgramWorkflows(t *testing.T, files map[string]string, target string) (compiledProgramWorkflow, error) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".github", "workflows")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	compiler := NewCompiler(WithVersion("test"))
	compiler.SetApprove(true)
	var compiled compiledProgramWorkflow
	if err := compiler.CompileWorkflow(filepath.Join(dir, target+".md")); err != nil {
		return compiled, err
	}
	content, err := os.ReadFile(filepath.Join(dir, target+".lock.yml"))
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(content, &compiled))
	return compiled, nil
}

const programWorkerSource = "---\non: workflow_dispatch\nengine: none\nprogram: worker.ts\ntools:\n  work-queue:\n    worker: true\nsafe-outputs:\n  noop:\n---\n"

func TestProgramDispatcherPublishesFromProgramJob(t *testing.T) {
	compiled, err := compileProgramWorkflows(t, map[string]string{
		"dispatcher.md": "---\non: workflow_dispatch\nengine: none\nprogram: dispatcher.ts\ntools:\n  work-queue: true\nsafe-outputs:\n  dispatch-workflow:\n    workflows: [worker]\n    max: 3\n---\n",
		"dispatcher.ts": "export default async () => {};\n",
		"worker.md":     programWorkerSource,
		"worker.ts":     "export default async () => {};\n",
	}, "dispatcher")
	require.NoError(t, err)

	require.NotContains(t, compiled.Jobs, "safe_outputs", "publisher programs commit controls themselves")
	require.NotContains(t, compiled.Jobs, "detection")
	agent := compiled.Jobs["agent"]
	require.Equal(t, "write", agent.Permissions["contents"])
	require.Equal(t, "write", agent.Permissions["actions"])

	var program *struct {
		ID   string            `yaml:"id"`
		Name string            `yaml:"name"`
		Uses string            `yaml:"uses"`
		Run  string            `yaml:"run"`
		Env  map[string]string `yaml:"env"`
		With map[string]string `yaml:"with"`
	}
	for i, step := range agent.Steps {
		require.NotContains(t, step.Name, "Install", "no engine or AWF installation in a program job")
		require.NotContains(t, step.Name, "MCP Gateway")
		require.NotEqual(t, "collect_output", step.ID, "publishers ingest no safe outputs")
		if step.ID == "program" {
			program = &agent.Steps[i]
		}
	}
	require.NotNil(t, program)
	require.Equal(t, ".github/workflows/dispatcher.ts", program.Env["GH_AW_PROGRAM"])
	require.Equal(t, "dispatcher", program.Env["GH_AW_WORK_QUEUE_ROLE"])
	require.Equal(t, "3", program.Env["GH_AW_WORK_QUEUE_DISPATCH_BUDGET"])
	require.NotEmpty(t, program.Env["GH_AW_WORK_QUEUE_TOKEN"])
	require.Contains(t, program.Env["GH_AW_WORK_QUEUE_CONTROL_CONFIG"], `"work_queue_workflows":["worker"]`)
	require.Contains(t, program.Env["GH_AW_WORK_QUEUE_CONTROL_CONFIG"], `"workflow_files":{"worker":".lock.yml"}`)
	require.Contains(t, program.With["script"], "run_program.cjs")
}

func TestProgramWorkerHandsFinishToSafeOutputs(t *testing.T) {
	compiled, err := compileProgramWorkflows(t, map[string]string{"worker.md": programWorkerSource, "worker.ts": "export default async () => {};\n"}, "worker")
	require.NoError(t, err)

	agent := compiled.Jobs["agent"]
	require.Equal(t, "read", agent.Permissions["contents"])
	ids := make([]string, 0, len(agent.Steps))
	for _, step := range agent.Steps {
		ids = append(ids, step.ID)
		if step.ID == "program" {
			require.Empty(t, step.Env["GH_AW_WORK_QUEUE_TOKEN"], "workers never hold the queue publisher token")
			require.Equal(t, "worker", step.Env["GH_AW_WORK_QUEUE_ROLE"])
		}
	}
	require.Contains(t, ids, "program")
	require.Contains(t, ids, "collect_output")

	safeOutputs, ok := compiled.Jobs["safe_outputs"]
	require.True(t, ok)
	var safeOutputIDs []string
	for _, step := range safeOutputs.Steps {
		safeOutputIDs = append(safeOutputIDs, step.ID)
	}
	require.Contains(t, safeOutputIDs, "work_queue_finish")
}

func TestProgramConfigurationErrors(t *testing.T) {
	for _, entry := range []struct {
		name   string
		source string
		files  map[string]string
		want   string
	}{
		{"program requires none", "---\non: workflow_dispatch\nengine: copilot\nprogram: p.ts\n---\n", map[string]string{"p.ts": ""}, "program: requires engine: none"},
		{"none requires program", "---\non: workflow_dispatch\nengine: none\n---\n", nil, "engine: none requires program:"},
		{"missing file", "---\non: workflow_dispatch\nengine: none\nprogram: missing.ts\n---\n", nil, "program: missing.ts was not found"},
		{"traversal", "---\non: workflow_dispatch\nengine: none\nprogram: ../p.ts\n---\n", nil, "program"},
		{"agent tools", "---\non: workflow_dispatch\nengine: none\nprogram: p.ts\ntools:\n  github:\n    toolsets: [repos]\n---\n", map[string]string{"p.ts": ""}, "tools.github"},
		{"agent budgets", "---\non: workflow_dispatch\nengine: none\nprogram: p.ts\nmax-turns: 3\nnetwork: defaults\n---\n", map[string]string{"p.ts": ""}, "max-turns, network"},
	} {
		t.Run(entry.name, func(t *testing.T) {
			files := map[string]string{"w.md": entry.source}
			maps.Copy(files, entry.files)
			_, err := compileProgramWorkflows(t, files, "w")
			require.Error(t, err)
			require.Contains(t, err.Error(), entry.want)
		})
	}
}
