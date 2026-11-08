//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

func TestDurableEngineConfiguration(t *testing.T) {
	for _, source := range []string{
		`{"pi":{"durable":{"retention-days":7},"model":"openai/gpt-5"}}`,
		`{"id":"pi","durable":{},"model":"openai/gpt-5"}`,
	} {
		t.Run(source, func(t *testing.T) {
			config, model, err := NewCompiler().extractEngineConfigFromJSON(source)
			require.NoError(t, err)
			require.Equal(t, "pi", config.ID)
			require.Equal(t, "openai/gpt-5", model)
			require.Equal(t, &DurableConfig{RetentionDays: 7}, config.Durable)
			require.NoError(t, config.durableError)
		})
	}
	for _, source := range []string{
		`{"id":"pi","durable":"repo"}`,
		`{"pi":{"durable":{"storage":"disk"}}}`,
		`{"pi":{"durable":{"retention-days":0}}}`,
		`{"pi":{"durable":{"retention-days":91}}}`,
		`{"pi":{"durable":{"retention-days":1.5}}}`,
		`{"pi":{"durable":{"retention-days":"7"}}}`,
		`{"pi":{"durable":{"storage":"repo","typo":true}}}`,
		`{"pi":{"durable":null}}`,
	} {
		t.Run(source, func(t *testing.T) {
			config, _, err := NewCompiler().extractEngineConfigFromJSON(source)
			require.NoError(t, err)
			require.Error(t, config.durableError)
		})
	}
}

func TestDurableEngineSchema(t *testing.T) {
	for _, source := range []string{
		`{"pi":{"durable":{}}}`,
		`{"pi":{"durable":{"retention-days":90},"config":"{}","env":{"FOO":"bar"}}}`,
		`{"id":"pi","durable":{"retention-days":1}}`,
	} {
		t.Run(source, func(t *testing.T) {
			var engine any
			require.NoError(t, json.Unmarshal([]byte(source), &engine))
			require.NoError(t, parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on": "workflow_dispatch", "engine": engine,
			}, "durable.md"))
		})
	}
	for _, source := range []string{
		`{"pi":{"durable":"repo"}}`,
		`{"pi":{"durable":{"storage":"disk"}}}`,
		`{"pi":{"durable":{"retention-days":0}}}`,
		`{"pi":{"durable":{"retention-days":91}}}`,
		`{"pi":{"durable":{"retention-days":"7"}}}`,
		`{"pi":{"durable":{"storage":"repo","typo":true}}}`,
		`{"pi":{"durable":{"storage":"repo"}},"id":"copilot"}`,
	} {
		t.Run(source, func(t *testing.T) {
			var engine any
			require.NoError(t, json.Unmarshal([]byte(source), &engine))
			require.Error(t, parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on": "workflow_dispatch", "engine": engine,
			}, "durable.md"))
		})
	}
}

func TestDurableArtifactEnvironment(t *testing.T) {
	data := &WorkflowData{WorkflowID: "continuable", EngineConfig: &EngineConfig{ID: "pi", Durable: &DurableConfig{RetentionDays: 7}}}
	env, err := durableExecutionEnv(data)
	require.NoError(t, err)
	require.Equal(t, "artifact", env["GH_AW_DURABLE_STORAGE"])
	require.Equal(t, "/tmp/gh-aw/agent/durable/pi-durable-continuable.json", env["GH_AW_DURABLE_CHECKPOINT"])
	require.Nil(t, data.RepoMemoryConfig)
	require.Nil(t, data.CacheMemoryConfig)
}

func TestCompileDurableRejectsUnsupportedEngineOrDriver(t *testing.T) {
	for _, test := range []struct{ engine, tools, want string }{
		{"  pi:\n    durable: {}\n    driver: custom.cjs", "", "does not support custom"},
		{"  id: copilot\n  durable: {}\n  driver: custom.cjs", "", "does not support custom"},
	} {
		t.Run(test.want+test.engine, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "durable-invalid.md")
			markdown := "---\non: workflow_dispatch\nengine:\n" + test.engine + "\n"
			if test.tools != "" {
				markdown += "tools:\n" + test.tools + "\n"
			}
			markdown += "---\nContinue the pending task.\n"
			require.NoError(t, os.WriteFile(source, []byte(markdown), 0o600))
			err := NewCompiler(WithVersion("dev")).CompileWorkflow(source)
			require.ErrorContains(t, err, test.want)
			_, err = os.Stat(filepath.Join(dir, "durable-invalid.lock.yml"))
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestDurableWorkflowConcurrency(t *testing.T) {
	for _, test := range []struct {
		name     string
		features map[string]any
		queue    bool
	}{
		{name: "default", queue: true},
		{name: "enabled", features: map[string]any{"group-concurrency-queue": true}, queue: true},
		{name: "disabled", features: map[string]any{"group-concurrency-queue": false}},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := &WorkflowData{
				WorkflowID:   "continuable",
				On:           "on:\n  issues:\n  workflow_dispatch:\n",
				EngineConfig: &EngineConfig{ID: "pi", Durable: &DurableConfig{RetentionDays: 7}},
				Features:     test.features,
			}
			concurrency := GenerateConcurrencyConfig(data, false)
			require.Contains(t, concurrency, `group: "gh-aw-durable-continuable"`)
			require.Contains(t, concurrency, "cancel-in-progress: false")
			require.NotContains(t, concurrency, "github.event.issue.number")
			require.Equal(t, test.queue, strings.Contains(concurrency, "queue: max"))
		})
	}
}

func TestDurableWorkflowID(t *testing.T) {
	for _, test := range []struct {
		name, workflowID, workflowName, want string
	}{
		{name: "dotted ID", workflowID: "workflow.test", want: "workflow-test"},
		{name: "dotted fallback name", workflowName: "Workflow.Test", want: "Workflow-Test"},
		{name: "existing separators", workflowID: "workflow_test-1", want: "workflow_test-1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := &WorkflowData{WorkflowID: test.workflowID, Name: test.workflowName}
			require.Equal(t, test.want, durableWorkflowID(data))
			require.Regexp(t, `^[a-zA-Z0-9_-]+$`, durableWorkflowID(data))
			require.Equal(t, "pi-durable-"+test.want+".json", durableCheckpointName(data))
		})
	}
}

func TestCompileDurableDottedWorkflowFilename(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "workflow.test.md")
	require.NoError(t, os.WriteFile(source, []byte(`---
on: workflow_dispatch
engine:
  id: copilot
  durable: {}
---
Continue the workflow task.
`), 0o600))
	require.NoError(t, NewCompiler(WithVersion("dev")).CompileWorkflow(source))
	lock, err := os.ReadFile(filepath.Join(dir, "workflow.test.lock.yml"))
	require.NoError(t, err)
	text := string(lock)
	require.Contains(t, text, `group: "gh-aw-durable-workflow-test"`)
	require.Contains(t, text, "pi-durable-workflow-test.json")
	require.NotContains(t, text, "pi-durable-workflow.test.json")
}

func TestBehaviorDefinedDurablePlan(t *testing.T) {
	definition := &EngineDefinition{ID: "custom-durable", Behaviors: &EngineBehaviorDefinition{
		Execution: &EngineExecutionDefinition{CommandName: "ordinary", StepName: "Ordinary invocation"},
		Durable: &EngineDurableDefinition{
			Files: []string{"$GH_AW_DURABLE_CHECKPOINT"},
			Installation: []EngineInstallationDefinition{{
				PackageManager: "npm", PackageName: "example-durable", Version: "1.2.3",
				StepName: "Install durable dependency", VerifyCommand: "example-durable --version", VerifyStepName: "Verify durable dependency",
			}},
			Execution:     &EngineExecutionDefinition{CommandName: "example-durable", StepName: "Durable invocation"},
			HarnessScript: `console.log(process.env.GH_AW_DURABLE_CHECKPOINT)`,
		},
	}}
	engine, err := NewBehaviorDefinedEngine(definition)
	require.NoError(t, err)
	data := &WorkflowData{
		WorkflowID:   "custom",
		EngineConfig: &EngineConfig{ID: definition.ID, Durable: &DurableConfig{RetentionDays: 7}},
	}
	plan, err := engine.GetDurableExecutionPlan(data, constants.AgentStdioLogPath)
	require.NoError(t, err)
	install := durableStepsText(plan.InstallationSteps)
	execution := durableStepsText(plan.ExecutionSteps)
	require.Contains(t, install, "example-durable@1.2.3")
	require.Contains(t, install, "--ignore-scripts")
	require.Contains(t, install, "example-durable --version")
	require.Contains(t, execution, "Durable invocation")
	require.Contains(t, execution, "GH_AW_DURABLE_CHECKPOINT")
	require.Contains(t, execution, "GH_AW_DURABLE_RESUMED")
	require.Equal(t, []string{"/tmp/gh-aw/agent/durable/pi-durable-custom.json"}, plan.Files)
	require.NotContains(t, execution, "Ordinary invocation")
	require.Equal(t, "ordinary", engine.behavior().Execution.CommandName)
	definition.Behaviors.Durable.Installation[0].VerifyCommand = ""
	_, err = engine.GetDurableExecutionPlan(data, constants.AgentStdioLogPath)
	require.ErrorContains(t, err, "verify-command")
	definition.Behaviors.Durable = nil
	_, err = engine.GetDurableExecutionPlan(data, constants.AgentStdioLogPath)
	require.ErrorContains(t, err, "does not support durable execution")
}

func TestBehaviorDefinedDurableSessionFilesWithoutLibrary(t *testing.T) {
	definition := &EngineDefinition{ID: "session-agent", Behaviors: &EngineBehaviorDefinition{
		Execution: &EngineExecutionDefinition{CommandName: "session-agent"},
		Durable: &EngineDurableDefinition{
			Files:         []string{"/tmp/gh-aw/agent/example/sessions"},
			Execution:     &EngineExecutionDefinition{CommandName: "session-agent", StepName: "Continue sessions"},
			HarnessScript: `if (process.env.GH_AW_DURABLE_RESUMED === "true") console.log("Continue the restored session");`,
		},
	}}
	engine, err := NewBehaviorDefinedEngine(definition)
	require.NoError(t, err)
	data := &WorkflowData{WorkflowID: "custom", EngineConfig: &EngineConfig{ID: definition.ID, Durable: &DurableConfig{RetentionDays: 7}}}
	plan, err := engine.GetDurableExecutionPlan(data, constants.AgentStdioLogPath)
	require.NoError(t, err)
	require.Empty(t, plan.InstallationSteps)
	require.Equal(t, definition.Behaviors.Durable.Files, plan.Files)
	execution := durableStepsText(plan.ExecutionSteps)
	require.Contains(t, execution, "GH_AW_DURABLE_RESUMED")
	require.Contains(t, execution, "GH_AW_DURABLE_PARENT_RUN_ID")
	require.Contains(t, execution, "Continue sessions")
	require.Contains(t, execution, "Continue the restored session")
	for _, files := range [][]string{
		nil, {"/etc/passwd"}, {"/tmp/gh-aw/agent"}, {"/tmp/gh-aw/agent/durable"},
		{"/tmp/gh-aw/agent/durable/bundle"}, {"/tmp/gh-aw/agent/example/../credentials"},
		{"/tmp/gh-aw/agent/example", "/tmp/gh-aw/agent/example/sessions"},
	} {
		definition.Behaviors.Durable.Files = files
		_, err := engine.GetDurableExecutionPlan(data, constants.AgentStdioLogPath)
		require.Error(t, err)
	}
}

func TestBehaviorDefinedDurableRejectsCaptureModeChanges(t *testing.T) {
	for _, protected := range []bool{false, true} {
		t.Run(strconv.FormatBool(protected), func(t *testing.T) {
			definition := &EngineDefinition{ID: "capture-agent", Behaviors: &EngineBehaviorDefinition{
				Execution: &EngineExecutionDefinition{CommandName: "capture-agent", ProtectedSessionCapture: protected},
				LogParser: `function parseLog() { return { logEntries: [] }; }`,
				Durable: &EngineDurableDefinition{
					Files:     []string{"/tmp/gh-aw/agent/example/sessions"},
					Execution: &EngineExecutionDefinition{CommandName: "capture-agent", ProtectedSessionCapture: !protected},
				},
			}}
			engine, err := NewBehaviorDefinedEngine(definition)
			require.NoError(t, err)
			data := &WorkflowData{WorkflowID: "custom", EngineConfig: &EngineConfig{ID: definition.ID, Durable: &DurableConfig{RetentionDays: 7}}}
			_, err = engine.GetDurableExecutionPlan(data, constants.AgentStdioLogPath)
			require.ErrorContains(t, err, "protected-session-capture")
		})
	}
}

func TestCompileDurableImportedSessionEngine(t *testing.T) {
	dir := t.TempDir()
	definition := filepath.Join(dir, "session-engine.md")
	require.NoError(t, os.WriteFile(definition, []byte(`---
engine:
  id: session-agent
  behaviors:
    execution:
      command-name: node
      step-name: Start session agent
    durable:
      files:
        - /tmp/gh-aw/agent/example/sessions
      execution:
        command-name: node
        step-name: Continue session agent
      harness-script: |
        console.log(process.env.GH_AW_DURABLE_RESUMED);
---
`), 0o600))
	source := filepath.Join(dir, "continuable.md")
	require.NoError(t, os.WriteFile(source, []byte(`---
on: workflow_dispatch
engine:
  id: session-agent
  durable: {}
imports:
  - session-engine.md
---
Continue the workflow task.
`), 0o600))
	require.NoError(t, NewCompiler(WithVersion("dev")).CompileWorkflow(source))
	lock, err := os.ReadFile(filepath.Join(dir, "continuable.lock.yml"))
	require.NoError(t, err)
	text := string(lock)
	require.Contains(t, text, "Continue session agent")
	require.Contains(t, text, "Restore durable workflow state")
	require.Contains(t, text, "Stage durable session files")
	require.Contains(t, text, "GH_AW_DURABLE_FILES")
	require.Contains(t, text, "/tmp/gh-aw/agent/example/sessions")
	require.Contains(t, text, "GH_AW_DURABLE_RESUMED")
	require.NotContains(t, text, "Install Pi durable")
}

func durableStepsText(steps []GitHubActionStep) string {
	var text strings.Builder
	for _, step := range steps {
		text.WriteString(strings.Join(step, "\n"))
		text.WriteByte('\n')
	}
	return text.String()
}

func TestCompilePiDurableArtifactLifecycle(t *testing.T) {
	for _, days := range []int{1, 7, 90} {
		t.Run(strconv.Itoa(days), func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "continuable.md")
			markdown := fmt.Sprintf("---\non: workflow_dispatch\nengine:\n  pi:\n    durable:\n      retention-days: %d\n---\nContinue the pending task.\n", days)
			require.NoError(t, os.WriteFile(source, []byte(markdown), 0o600))
			require.NoError(t, NewCompiler(WithVersion("dev")).CompileWorkflow(source))
			lock, err := os.ReadFile(filepath.Join(dir, "continuable.lock.yml"))
			require.NoError(t, err)
			text := string(lock)
			require.Contains(t, text, "--session-dir /tmp/gh-aw/agent/durable-sessions/pi --session /tmp/gh-aw/agent/durable-sessions/pi/session.jsonl")
			require.NotContains(t, text, "Install Pi durable")
			require.Equal(t, 1, strings.Count(text, "- name: Restore durable workflow state"))
			require.Contains(t, text, "await restoreCheckpoint(")
			require.NotContains(t, text, "Restore durable checkpoint artifact")
			require.NotContains(t, text, "restore_durable_files")
			require.Contains(t, text, "Stage durable session files")
			require.Contains(t, text, "GH_AW_DURABLE_RESUMED")
			require.Contains(t, text, "Upload durable checkpoint artifact")
			require.Contains(t, text, fmt.Sprintf("|| '%d'", days))
			require.Contains(t, text, "actions: read")
			require.Contains(t, text, "TRACEPARENT:")
			require.Contains(t, text, "pi-streaming.jsonl")
			require.NotContains(t, text, "Create cache-memory directory")
			require.NotContains(t, text, "Push repo-memory")
			require.Less(t, strings.Index(text, "Restore durable workflow state"), strings.Index(text, "Execute Pi CLI"))
			require.Less(t, strings.Index(text, "Stage durable session files"), strings.Index(text, "Redact secrets in logs"))
			require.Less(t, strings.Index(text, "Redact secrets in logs"), strings.Index(text, "Upload durable checkpoint artifact"))
		})
	}
}
