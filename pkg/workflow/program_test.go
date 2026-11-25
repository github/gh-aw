//go:build !integration

package workflow

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

type compiledProgramWorkflow struct {
	Jobs map[string]struct {
		Permissions map[string]string `yaml:"permissions"`
		Environment any               `yaml:"environment"`
		Steps       []struct {
			ID      string            `yaml:"id"`
			Name    string            `yaml:"name"`
			Uses    string            `yaml:"uses"`
			Run     string            `yaml:"run"`
			Env     map[string]string `yaml:"env"`
			With    map[string]string `yaml:"with"`
			Timeout any               `yaml:"timeout-minutes"`
			If      string            `yaml:"if"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// compileProgramWorkflows writes the given files into .github/workflows, compiles
// `target`, and returns the parsed lock file.
func compileProgramWorkflows(t *testing.T, files map[string]string, target string) (compiledProgramWorkflow, error) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".github", "workflows")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	compiler := NewCompiler(WithVersion("test"))
	compiler.gitRoot = root
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
		ID      string            `yaml:"id"`
		Name    string            `yaml:"name"`
		Uses    string            `yaml:"uses"`
		Run     string            `yaml:"run"`
		Env     map[string]string `yaml:"env"`
		With    map[string]string `yaml:"with"`
		Timeout any               `yaml:"timeout-minutes"`
		If      string            `yaml:"if"`
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
	require.Contains(t, program.With["script"], "await main({ core, github, context, exec, io });")
	require.NotEmpty(t, program.Timeout)
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
			require.Equal(t, "${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}", step.Env["GH_AW_SAFE_OUTPUTS"])
			require.Equal(t, "${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS_CONFIG_PATH }}", step.Env["GH_AW_SAFE_OUTPUTS_CONFIG_PATH"])
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

func TestProgramPublisherValidation(t *testing.T) {
	for _, tc := range []struct {
		name, top, config, want string
	}{
		{"success gate", "", "  require-agent-success: true\n", "safe-outputs.require-agent-success"},
		{"needs gate", "", "  needs: [build]\n", "safe-outputs.needs"},
		{"environments", "environment: one\n", "  environment: two\n", "conflicting environment"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compileProgramWorkflows(t, map[string]string{
				"w.md":      "---\non: workflow_dispatch\nengine: none\nprogram: p.ts\ntools:\n  work-queue: true\n" + tc.top + "safe-outputs:\n  dispatch-workflow:\n    workflows: [worker]\n" + tc.config + "---\n",
				"p.ts":      "",
				"worker.md": programWorkerSource,
				"worker.ts": "",
			}, "w")
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestProgramPublisherPermissions(t *testing.T) {
	data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{workQueueApprovalReads: true}}
	perms := programPublisherPermissions(data)
	level, ok := perms.Get(PermissionIssues)
	require.True(t, ok)
	require.Equal(t, PermissionRead, level)
	_, hasActions := perms.Get(PermissionActions)
	require.False(t, hasActions)
}

func TestProgramDryRunRejectsQueue(t *testing.T) {
	c := NewCompiler()
	c.SetDryRun(true)
	for _, queue := range []any{true, map[string]any{"worker": true}} {
		_, err := c.prepareDryRunWorkflowData(&WorkflowData{Program: "p.ts", Tools: map[string]any{"work-queue": queue}})
		require.ErrorContains(t, err, "--dry-run does not support program workflows with tools.work-queue")
	}
}

func TestProgramPublisherDispatchAppAndEnvironment(t *testing.T) {
	compiled, err := compileProgramWorkflows(t, map[string]string{
		"dispatcher.md": "---\non: workflow_dispatch\nengine: none\nprogram: dispatcher.ts\ntools:\n  work-queue: true\nsafe-outputs:\n  environment: production\n  dispatch-workflow:\n    workflows: [worker]\n    github-app:\n      app-id: ${{ vars.APP_ID }}\n      private-key: ${{ secrets.APP_KEY }}\n---\n",
		"dispatcher.ts": "",
		"worker.md":     programWorkerSource,
		"worker.ts":     "",
	}, "dispatcher")
	require.NoError(t, err)
	job := compiled.Jobs["agent"]
	require.Equal(t, "production", job.Environment)
	var found bool
	for _, step := range job.Steps {
		if step.ID == "dispatch-workflow-app-token" {
			found = true
			require.Equal(t, "write", step.With["permission-actions"])
			require.Empty(t, step.With["permission-contents"])
		}
		if step.ID == "program" {
			require.Contains(t, step.Env["GH_AW_WORK_QUEUE_CONTROL_CONFIG"], "steps.dispatch-workflow-app-token.outputs.token")
		}
	}
	require.True(t, found)
}

func TestProgramPublisherBrokerCleanup(t *testing.T) {
	compiled, err := compileProgramWorkflows(t, map[string]string{
		"w.md":      "---\non: workflow_dispatch\nengine: none\nprogram: p.ts\ntools:\n  work-queue: true\nsafe-outputs:\n  token-broker: true\n  dispatch-workflow:\n    workflows: [worker]\n---\n",
		"p.ts":      "",
		"worker.md": programWorkerSource,
		"worker.ts": "",
	}, "w")
	require.NoError(t, err)
	job := compiled.Jobs["agent"]
	require.Equal(t, "write", job.Permissions["id-token"])
	program, cleanup := -1, -1
	for i, step := range job.Steps {
		if step.ID == "program" {
			program = i
		}
		if step.Name == "Invalidate brokered credentials" {
			cleanup = i
			require.Equal(t, "always()", step.If)
		}
	}
	require.GreaterOrEqual(t, program, 0)
	require.Greater(t, cleanup, program)
}

func TestProgramStepTimeout(t *testing.T) {
	for _, value := range []string{"1", "${{ inputs.timeout }}"} {
		t.Run(value, func(t *testing.T) {
			root := t.TempDir()
			markdown := filepath.Join(root, "workflow.md")
			require.NoError(t, os.WriteFile(markdown, nil, 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(root, "p.ts"), nil, 0o600))
			compiler := NewCompiler()
			compiler.gitRoot, compiler.markdownPath = root, markdown
			data := &WorkflowData{TimeoutMinutes: value, Program: "p.ts"}
			steps, err := compiler.buildProgramStep(data)
			require.NoError(t, err)
			require.Contains(t, strings.Join(steps, ""), "timeout-minutes: "+value)
		})
	}
}

func TestProgramRepositoryPath(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "custom", "workflows")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "programs"), 0o755))
	markdown := filepath.Join(dir, "workflow.md")
	program := filepath.Join(dir, "programs", "p.ts")
	require.NoError(t, os.WriteFile(markdown, nil, 0o600))
	require.NoError(t, os.WriteFile(program, nil, 0o600))
	compiler := NewCompiler()
	compiler.gitRoot, compiler.markdownPath = root, markdown
	got, err := compiler.programRepositoryPath(&WorkflowData{Program: "programs/p.ts"})
	require.NoError(t, err)
	require.Equal(t, "custom/workflows/programs/p.ts", got)

	cwd, err := os.Getwd()
	require.NoError(t, err)
	compiler.markdownPath, err = filepath.Rel(cwd, markdown)
	require.NoError(t, err)
	got, err = compiler.programRepositoryPath(&WorkflowData{Program: "programs/p.ts"})
	require.NoError(t, err)
	require.Equal(t, "custom/workflows/programs/p.ts", got)

	outside := t.TempDir()
	compiler.markdownPath = filepath.Join(outside, "workflow.md")
	_, err = compiler.programRepositoryPath(&WorkflowData{Program: "p.ts"})
	require.ErrorContains(t, err, "inside the repository root")

	compiler.markdownPath = markdown
	require.NoError(t, os.Remove(program))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "p.ts"), nil, 0o600))
	require.NoError(t, os.Symlink(filepath.Join(outside, "p.ts"), program))
	_, err = compiler.programRepositoryPath(&WorkflowData{Program: "programs/p.ts"})
	require.ErrorContains(t, err, "inside the repository root")

	require.NoError(t, os.Remove(markdown))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "workflow.md"), nil, 0o600))
	require.NoError(t, os.Symlink(filepath.Join(outside, "workflow.md"), markdown))
	_, err = compiler.programRepositoryPath(&WorkflowData{Program: "programs/p.ts"})
	require.ErrorContains(t, err, "inside the repository root")
}

func TestProgramCustomWorkflowDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "custom", "workflows")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	markdown := filepath.Join(dir, "workflow.md")
	require.NoError(t, os.WriteFile(markdown, []byte("---\non: workflow_dispatch\nengine: none\nprogram: p.ts\n---\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "p.ts"), nil, 0o600))
	compiler := NewCompiler(WithVersion("test"))
	compiler.gitRoot = root
	compiler.SetApprove(true)
	require.NoError(t, compiler.CompileWorkflow(markdown))
	lock, err := os.ReadFile(filepath.Join(dir, "workflow.lock.yml"))
	require.NoError(t, err)
	var compiled compiledProgramWorkflow
	require.NoError(t, yaml.Unmarshal(lock, &compiled))
	for _, step := range compiled.Jobs["agent"].Steps {
		if step.ID == "program" {
			require.Equal(t, "custom/workflows/p.ts", step.Env["GH_AW_PROGRAM"])
			return
		}
	}
	t.Fatal("program step not found")
}

func TestProgramOutsideRepositoryRejected(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	markdown := filepath.Join(outside, "workflow.md")
	require.NoError(t, os.WriteFile(markdown, []byte("---\non: workflow_dispatch\nengine: none\nprogram: p.ts\n---\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "p.ts"), nil, 0o600))
	compiler := NewCompiler(WithVersion("test"))
	compiler.gitRoot = root
	compiler.SetApprove(true)
	require.ErrorContains(t, compiler.CompileWorkflow(markdown), "inside the repository root")
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
