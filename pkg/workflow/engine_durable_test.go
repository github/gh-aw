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
			require.False(t, config.DurableDisabled)
			require.NoError(t, config.durableError)
		})
	}
	for _, source := range []string{
		`{"id":"pi","durable":false}`,
		`{"pi":{"durable":false}}`,
	} {
		t.Run(source, func(t *testing.T) {
			config, _, err := NewCompiler().extractEngineConfigFromJSON(source)
			require.NoError(t, err)
			require.Nil(t, config.Durable)
			require.True(t, config.DurableDisabled)
			require.NoError(t, config.durableError)
		})
	}
	for _, source := range []string{
		`{"id":"pi","durable":true}`,
		`{"pi":{"durable":true}}`,
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
		`{"pi":{"durable":false}}`,
		`{"pi":{"durable":{"retention-days":90},"config":"{}","env":{"FOO":"bar"}}}`,
		`{"id":"pi","durable":{"retention-days":1}}`,
		`{"id":"copilot","durable":false}`,
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
		`{"id":"pi","durable":true}`,
		`{"pi":{"durable":true}}`,
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
	require.Contains(t, install, "NPM_CONFIG_IGNORE_SCRIPTS: 'true'")
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
	require.Contains(t, text, "- name: "+DurableRestoreStepName+"\n")
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
			require.Equal(t, 1, strings.Count(text, "- name: "+DurableRestoreStepName+"\n"))
			require.Contains(t, text, "--session-dir /tmp/gh-aw/agent/durable-sessions/pi --session /tmp/gh-aw/agent/durable-sessions/pi/session.jsonl")
			require.NotContains(t, text, "Install Pi durable")
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
			require.Less(t, strings.Index(text, DurableRestoreStepName), strings.Index(text, "Execute Pi CLI"))
			require.Less(t, strings.Index(text, "Stage durable session files"), strings.Index(text, "Redact secrets in logs"))
			require.Less(t, strings.Index(text, "Redact secrets in logs"), strings.Index(text, "Upload durable checkpoint artifact"))
		})
	}
}

func TestCompileBehaviorDefinedEngineDefaultDurable(t *testing.T) {
	dir := t.TempDir()
	defaultDurableDef := filepath.Join(dir, "default-durable-engine.md")
	require.NoError(t, os.WriteFile(defaultDurableDef, []byte(`---
engine:
  id: session-agent
  display-name: Session Agent
  behaviors:
    execution:
      command-name: node
      step-name: Start session agent
    durable:
      default-retention-days: 14
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
imports:
  - default-durable-engine.md
---
Continue the workflow task.
`), 0o600))

	compiler := NewCompiler(WithVersion("dev"))
	stderr := captureStderrOutput(t, func() {
		require.NoError(t, compiler.CompileWorkflow(source))
	})
	require.Contains(t, stderr, "Using experimental feature: engine.durable (from engine default)")

	lock, err := os.ReadFile(filepath.Join(dir, "continuable.lock.yml"))
	require.NoError(t, err)
	text := string(lock)
	require.Contains(t, text, "Continue session agent")
	require.NotContains(t, text, "Start session agent")
	require.Contains(t, text, "- name: "+DurableRestoreStepName+"\n")
	require.Contains(t, text, "Stage durable session files")
	require.Contains(t, text, "Upload durable checkpoint artifact")
	require.Contains(t, text, "|| '14'")
	require.Contains(t, text, `group: "gh-aw-durable-continuable"`)
	require.Contains(t, text, "actions: read")

	// Batch mode records the distinct engine-default experimental feature key.
	batchCompiler := NewCompiler(WithVersion("dev"))
	batchCompiler.SetBatchMode(true)
	require.NoError(t, batchCompiler.CompileWorkflow(source))
	require.Equal(t, 1, batchCompiler.GetExperimentalFeatureUsage()["Using experimental feature: engine.durable (from engine default)"])

	// An engine without default-retention-days stays non-durable when the workflow omits engine.durable.
	optInOnlyDef := filepath.Join(dir, "opt-in-engine.md")
	require.NoError(t, os.WriteFile(optInOnlyDef, []byte(`---
engine:
  id: opt-in-agent
  display-name: Opt-In Agent
  behaviors:
    execution:
      command-name: node
      step-name: Start opt-in agent
    durable:
      files:
        - /tmp/gh-aw/agent/example/sessions
      execution:
        command-name: node
        step-name: Continue opt-in agent
---
`), 0o600))
	nonDurableSource := filepath.Join(dir, "ordinary.md")
	require.NoError(t, os.WriteFile(nonDurableSource, []byte(`---
on: workflow_dispatch
engine:
  id: opt-in-agent
imports:
  - opt-in-engine.md
---
Run once.
`), 0o600))
	require.NoError(t, NewCompiler(WithVersion("dev")).CompileWorkflow(nonDurableSource))
	ordinaryLock, err := os.ReadFile(filepath.Join(dir, "ordinary.lock.yml"))
	require.NoError(t, err)
	require.Contains(t, string(ordinaryLock), "Start opt-in agent")
	require.NotContains(t, string(ordinaryLock), DurableRestoreStepName)
}

func TestEngineDefaultDurableOverridesAndOptOut(t *testing.T) {
	dir := t.TempDir()
	defPath := filepath.Join(dir, "default-durable-engine.md")
	require.NoError(t, os.WriteFile(defPath, []byte(`---
engine:
  id: session-agent
  display-name: Session Agent
  behaviors:
    execution:
      command-name: node
      step-name: Start session agent
    durable:
      default-retention-days: 14
      files:
        - /tmp/gh-aw/agent/example/sessions
      execution:
        command-name: node
        step-name: Continue session agent
---
`), 0o600))

	t.Run("imported engine without main engine field uses engine default", func(t *testing.T) {
		source := filepath.Join(dir, "implicit-import.md")
		require.NoError(t, os.WriteFile(source, []byte("---\non: workflow_dispatch\nimports:\n  - default-durable-engine.md\n---\nTask.\n"), 0o600))
		require.NoError(t, NewCompiler(WithVersion("dev")).CompileWorkflow(source))
		lock, err := os.ReadFile(filepath.Join(dir, "implicit-import.lock.yml"))
		require.NoError(t, err)
		require.Contains(t, string(lock), "|| '14'")
		require.Contains(t, string(lock), "Continue session agent")
	})

	t.Run("explicit retention-days wins over engine default", func(t *testing.T) {
		source := filepath.Join(dir, "explicit-30.md")
		require.NoError(t, os.WriteFile(source, []byte("---\non: workflow_dispatch\nengine:\n  id: session-agent\n  durable:\n    retention-days: 30\nimports:\n  - default-durable-engine.md\n---\nTask.\n"), 0o600))
		compiler := NewCompiler(WithVersion("dev"))
		stderr := captureStderrOutput(t, func() {
			require.NoError(t, compiler.CompileWorkflow(source))
		})
		require.Contains(t, stderr, "Using experimental feature: engine.durable")
		require.NotContains(t, stderr, "(from engine default)")
		lock, err := os.ReadFile(filepath.Join(dir, "explicit-30.lock.yml"))
		require.NoError(t, err)
		require.Contains(t, string(lock), "|| '30'")
		require.NotContains(t, string(lock), "|| '14'")
	})

	t.Run("explicit empty durable object uses explicit 7-day default", func(t *testing.T) {
		source := filepath.Join(dir, "explicit-empty.md")
		require.NoError(t, os.WriteFile(source, []byte("---\non: workflow_dispatch\nengine:\n  id: session-agent\n  durable: {}\nimports:\n  - default-durable-engine.md\n---\nTask.\n"), 0o600))
		require.NoError(t, NewCompiler(WithVersion("dev")).CompileWorkflow(source))
		lock, err := os.ReadFile(filepath.Join(dir, "explicit-empty.lock.yml"))
		require.NoError(t, err)
		require.Contains(t, string(lock), "|| '7'")
		require.NotContains(t, string(lock), "|| '14'")
	})

	t.Run("durable false opts out of engine default", func(t *testing.T) {
		source := filepath.Join(dir, "opt-out.md")
		markdown := "---\non: workflow_dispatch\nengine:\n  id: session-agent\n  durable: false\nimports:\n  - default-durable-engine.md\n---\nTask.\n"
		require.NoError(t, os.WriteFile(source, []byte(markdown), 0o600))
		compiler := NewCompiler(WithVersion("dev"))
		stderr := captureStderrOutput(t, func() {
			require.NoError(t, compiler.CompileWorkflow(source))
		})
		require.NotContains(t, stderr, "engine.durable")
		lock, err := os.ReadFile(filepath.Join(dir, "opt-out.lock.yml"))
		require.NoError(t, err)
		text := string(lock)
		require.Contains(t, text, "Start session agent")
		require.NotContains(t, text, "Continue session agent")
		require.NotContains(t, text, DurableRestoreStepName)
		require.NotContains(t, text, "gh-aw-durable-")

		// String API parity for opt-out.
		strCompiler := NewCompiler(WithVersion("dev"))
		data, err := strCompiler.ParseWorkflowString(markdown, source)
		require.NoError(t, err)
		yamlOut, err := strCompiler.CompileToYAML(data, source)
		require.NoError(t, err)
		require.Contains(t, yamlOut, "Start session agent")
		require.NotContains(t, yamlOut, DurableRestoreStepName)
	})
}

func TestBehaviorDefinedEngineDefaultRetentionValidation(t *testing.T) {
	for _, days := range []int{1, 7, 90} {
		d := days
		_, err := NewBehaviorDefinedEngine(&EngineDefinition{
			ID: "valid-default",
			Behaviors: &EngineBehaviorDefinition{
				Execution: &EngineExecutionDefinition{CommandName: "node"},
				Durable: &EngineDurableDefinition{
					DefaultRetentionDays: &d,
					Files:                []string{"/tmp/gh-aw/agent/example/sessions"},
					Execution:            &EngineExecutionDefinition{CommandName: "node"},
				},
			},
		})
		require.NoError(t, err)
	}

	for _, invalid := range []int{0, -1, 91} {
		d := invalid
		_, err := NewBehaviorDefinedEngine(&EngineDefinition{
			ID: "invalid-default",
			Behaviors: &EngineBehaviorDefinition{
				Execution: &EngineExecutionDefinition{CommandName: "node"},
				Durable: &EngineDurableDefinition{
					DefaultRetentionDays: &d,
					Files:                []string{"/tmp/gh-aw/agent/example/sessions"},
					Execution:            &EngineExecutionDefinition{CommandName: "node"},
				},
			},
		})
		require.ErrorContains(t, err, "default-retention-days must be an integer from 1 to 90")
	}

	for _, valid := range []any{1, 7, 90} {
		err := parser.ValidateIncludedFileFrontmatterWithSchemaAndLocation(map[string]any{
			"engine": map[string]any{
				"id":           "good-schema-engine",
				"display-name": "Good Schema Engine",
				"behaviors": map[string]any{
					"execution": map[string]any{"command-name": "node"},
					"durable": map[string]any{
						"default-retention-days": valid,
						"files":                  []any{"/tmp/gh-aw/agent/example/sessions"},
						"execution":              map[string]any{"command-name": "node"},
					},
				},
			},
		}, "engine.md")
		require.NoError(t, err, "valid=%v", valid)
	}

	for _, raw := range []any{0, -1, 91, 1.5, "7", true} {
		err := parser.ValidateIncludedFileFrontmatterWithSchemaAndLocation(map[string]any{
			"engine": map[string]any{
				"id":           "bad-schema-engine",
				"display-name": "Bad Schema Engine",
				"behaviors": map[string]any{
					"execution": map[string]any{"command-name": "node"},
					"durable": map[string]any{
						"default-retention-days": raw,
						"files":                  []any{"/tmp/gh-aw/agent/example/sessions"},
						"execution":              map[string]any{"command-name": "node"},
					},
				},
			},
		}, "engine.md")
		require.Error(t, err, "raw=%v", raw)
	}

	// CompileWorkflow fails closed when an imported engine definition has out-of-range default-retention-days
	// or contradicts itself with engine.durable: false alongside default-retention-days.
	for _, defBody := range []string{
		"engine:\n  id: bad-agent\n  display-name: Bad Agent\n  behaviors:\n    execution:\n      command-name: node\n    durable:\n      default-retention-days: 91\n      files:\n        - /tmp/gh-aw/agent/example/sessions\n      execution:\n        command-name: node\n",
		"engine:\n  id: bad-agent\n  display-name: Bad Agent\n  durable: false\n  behaviors:\n    execution:\n      command-name: node\n    durable:\n      default-retention-days: 7\n      files:\n        - /tmp/gh-aw/agent/example/sessions\n      execution:\n        command-name: node\n",
	} {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "engine.md"), []byte("---\n"+defBody+"---\n"), 0o600))
		wf := filepath.Join(dir, "wf.md")
		require.NoError(t, os.WriteFile(wf, []byte("---\non: workflow_dispatch\nengine:\n  id: bad-agent\nimports:\n  - engine.md\n---\nTask.\n"), 0o600))
		require.Error(t, NewCompiler(WithVersion("dev")).CompileWorkflow(wf))
		require.NoFileExists(t, filepath.Join(dir, "wf.lock.yml"))
	}
}

func TestValidateEngineDefinitionDurableConsistencyFailsClosed(t *testing.T) {
	days := 7
	def := &EngineDefinition{
		ID: "invalid-json",
		Behaviors: &EngineBehaviorDefinition{
			Durable: &EngineDurableDefinition{DefaultRetentionDays: &days},
		},
	}

	err := validateEngineDefinitionDurableConsistency("{", def)
	require.ErrorContains(t, err, `engine definition "invalid-json": parse engine JSON`)
}

func TestExplicitDurableKeepsPreviousWorkflowCompatibilityChecks(t *testing.T) {
	for _, frontmatter := range []map[string]any{
		{"on": map[string]any{"workflow_call": map[string]any{}}},
		{"tools": map[string]any{"work-queue": map[string]any{"worker": true}}},
	} {
		data := &WorkflowData{
			EngineConfig: &EngineConfig{
				Durable: &DurableConfig{RetentionDays: defaultDurableRetentionDays},
			},
			RawFrontmatter: frontmatter,
		}
		err := NewCompiler().validateEngineDurable(data)
		if err != nil {
			require.NotContains(t, err.Error(), "does not support workflow_call or work-queue")
		}
	}

	for _, data := range []*WorkflowData{
		{
			EngineConfig: &EngineConfig{
				Durable: &DurableConfig{RetentionDays: defaultDurableRetentionDays},
			},
			On: "workflow_call",
		},
		{
			EngineConfig: &EngineConfig{
				Durable: &DurableConfig{RetentionDays: defaultDurableRetentionDays},
			},
			WorkQueuePolicy: &WorkQueuePolicyConfig{},
		},
	} {
		err := NewCompiler().validateEngineDurable(data)
		require.ErrorContains(t, err, "does not support workflow_call or work-queue")
	}
}

func TestEngineDefaultDurableSkipsIncompatibleWorkflowShapes(t *testing.T) {
	dir := t.TempDir()
	workflowsDir := filepath.Join(dir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "default-durable-engine.md"), []byte(`---
engine:
  id: session-agent
  display-name: Session Agent
  behaviors:
    execution:
      command-name: node
      step-name: Start session agent
    durable:
      default-retention-days: 7
      files:
        - /tmp/gh-aw/agent/example/sessions
      execution:
        command-name: node
        step-name: Continue session agent
---
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "shared-concurrency.md"), []byte(`---
concurrency:
  group: shared-imported-group
---
`), 0o600))

	cases := []struct {
		name           string
		frontmatter    string
		extraEngine    string
		wantSkipWarn   string
		wantLockSubstr string
		wantExplicitEr string
	}{
		{
			name:         "workflow_call map trigger",
			frontmatter:  "on:\n  workflow_call:\n",
			wantSkipWarn: "workflow_call",
		},
		{
			name:         "workflow_call string trigger",
			frontmatter:  "on: workflow_call\n",
			wantSkipWarn: "workflow_call",
		},
		{
			name:         "work-queue worker",
			frontmatter:  "on:\n  workflow_dispatch:\ntools:\n  work-queue:\n    worker: true\n",
			wantSkipWarn: "work-queue",
		},
		{
			name:           "top-level concurrency group",
			frontmatter:    "on: workflow_dispatch\nconcurrency:\n  group: custom-workflow-group\n",
			wantSkipWarn:   "concurrency",
			wantLockSubstr: "custom-workflow-group",
			wantExplicitEr: "manages workflow concurrency",
		},
		{
			name:           "imported concurrency group",
			frontmatter:    "on: workflow_dispatch\n",
			wantSkipWarn:   "concurrency",
			wantLockSubstr: "shared-imported-group",
			wantExplicitEr: "manages workflow concurrency",
		},
		{
			name:           "engine concurrency",
			frontmatter:    "on: workflow_dispatch\n",
			extraEngine:    "  concurrency: custom-engine-group\n",
			wantSkipWarn:   "concurrency",
			wantLockSubstr: "custom-engine-group",
			wantExplicitEr: "manages workflow concurrency",
		},
		{
			name:           "custom engine command",
			frontmatter:    "on: workflow_dispatch\n",
			extraEngine:    "  command: custom-node\n",
			wantSkipWarn:   "command, driver, or harness",
			wantExplicitEr: "cannot be overridden by command, driver or harness",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			imports := "imports:\n  - default-durable-engine.md\n"
			if tc.name == "imported concurrency group" {
				imports += "  - shared-concurrency.md\n"
			}

			// 1. Engine default skips durable mode cleanly and emits a warning naming why.
			defaultWf := filepath.Join(workflowsDir, strings.ReplaceAll(tc.name, " ", "-")+"-default.md")
			defaultSrc := "---\n" + tc.frontmatter + "engine:\n  id: session-agent\n" + tc.extraEngine + imports + "---\nRun task.\n"
			require.NoError(t, os.WriteFile(defaultWf, []byte(defaultSrc), 0o600))
			compiler := NewCompiler(WithVersion("dev"))
			stderr := captureStderrOutput(t, func() {
				require.NoError(t, compiler.CompileWorkflow(defaultWf))
			})
			require.Contains(t, stderr, tc.wantSkipWarn)
			require.NotContains(t, stderr, "Using experimental feature: engine.durable")
			lockBytes, err := os.ReadFile(strings.TrimSuffix(defaultWf, ".md") + ".lock.yml")
			require.NoError(t, err)
			lock := string(lockBytes)
			require.Contains(t, lock, "Start session agent")
			require.NotContains(t, lock, "Continue session agent")
			require.NotContains(t, lock, DurableRestoreStepName)
			require.NotContains(t, lock, "gh-aw-durable-")
			if tc.wantLockSubstr != "" {
				require.Contains(t, lock, tc.wantLockSubstr)
			}

			// 2. Explicit engine.durable: false compiles cleanly without a skip warning.
			optOutWf := filepath.Join(workflowsDir, strings.ReplaceAll(tc.name, " ", "-")+"-optout.md")
			optOutSrc := "---\n" + tc.frontmatter + "engine:\n  id: session-agent\n  durable: false\n" + tc.extraEngine + imports + "---\nRun task.\n"
			require.NoError(t, os.WriteFile(optOutWf, []byte(optOutSrc), 0o600))
			optOutStderr := captureStderrOutput(t, func() {
				require.NoError(t, NewCompiler(WithVersion("dev")).CompileWorkflow(optOutWf))
			})
			require.NotContains(t, optOutStderr, "default durable mode skipped")

			// 3. Explicit engine.durable: {} preserves today's hard error.
			explicitWf := filepath.Join(workflowsDir, strings.ReplaceAll(tc.name, " ", "-")+"-explicit.md")
			explicitSrc := "---\n" + tc.frontmatter + "engine:\n  id: session-agent\n  durable: {}\n" + tc.extraEngine + imports + "---\nRun task.\n"
			require.NoError(t, os.WriteFile(explicitWf, []byte(explicitSrc), 0o600))
			err = NewCompiler(WithVersion("dev")).CompileWorkflow(explicitWf)
			if tc.wantExplicitEr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantExplicitEr)
			}
		})
	}
}
