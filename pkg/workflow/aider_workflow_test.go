//go:build !integration && !windows

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func loadAiderSample(t *testing.T) *EngineDefinition {
	t.Helper()
	sourceContent, err := os.ReadFile("../../.github/workflows/shared/aider.md")
	require.NoError(t, err)
	var frontmatter struct {
		Engine EngineDefinition `yaml:"engine"`
	}
	parts := strings.SplitN(string(sourceContent), "---", 3)
	require.Len(t, parts, 3)
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &frontmatter))
	require.NotNil(t, frontmatter.Engine.Behaviors)
	return &frontmatter.Engine
}

func aiderHarnessCommand(t *testing.T, tempDir, command string, args ...string) *exec.Cmd {
	t.Helper()
	harnessPath := filepath.Join(tempDir, "aider_harness.cjs")
	require.NoError(t, os.WriteFile(harnessPath, []byte(loadAiderSample(t).Behaviors.HarnessScript), 0o600))
	actionsDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	require.NoError(t, os.Symlink(filepath.Join(actionsDir, "process_runner.cjs"), filepath.Join(tempDir, "process_runner.cjs")))
	reflectPath, err := json.Marshal(filepath.Join(actionsDir, "awf_reflect.cjs"))
	require.NoError(t, err)
	reflectSource := "module.exports = { ...require(" + string(reflectPath) + `),
fetchAWFReflect: async () => JSON.parse(process.env.GH_AW_AIDER_TEST_REFLECT || "{}") };`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "awf_reflect.cjs"), []byte(reflectSource), 0o600))
	promptPath := filepath.Join(tempDir, "prompt with spaces.md")
	require.NoError(t, os.WriteFile(promptPath, []byte("test prompt"), 0o600))
	cmd := exec.Command("node", append([]string{harnessPath, command}, args...)...)
	cmd.Env = append(os.Environ(),
		"GH_AW_PROMPT="+promptPath, "AIDER_MODEL=openai/test-model",
		"GH_AW_LLM_PROVIDER=openai", "OPENAI_API_KEY=test-key", "AWF_REFLECT_ENABLED=0",
		"GITHUB_WORKSPACE="+tempDir, "GH_AW_ENGINE_CWD="+tempDir, "HOME="+tempDir,
	)
	return cmd
}

func TestAiderHarnessPreservesProcessFailureDetails(t *testing.T) {
	runHarness := func(t *testing.T, command string, exitCode int) string {
		t.Helper()
		cmd := aiderHarnessCommand(t, t.TempDir(), command)
		output, err := cmd.CombinedOutput()
		var exitError *exec.ExitError
		require.ErrorAs(t, err, &exitError)
		assert.Equal(t, exitCode, exitError.ExitCode())
		return string(output)
	}

	tempDir := t.TempDir()
	t.Run("spawn error", func(t *testing.T) {
		assert.Contains(t, runHarness(t, filepath.Join(tempDir, "missing-aider"), 1), "ENOENT")
	})
	exitPath := filepath.Join(tempDir, "exit-aider")
	require.NoError(t, os.WriteFile(exitPath, []byte("#!/bin/sh\nexit 7\n"), 0o700))
	t.Run("exit code", func(t *testing.T) {
		assert.Contains(t, runHarness(t, exitPath, 7), "Aider execution failed with exit code 7")
	})
	signalPath := filepath.Join(tempDir, "signal-aider")
	require.NoError(t, os.WriteFile(signalPath, []byte("#!/bin/sh\nkill -TERM $$\n"), 0o700))
	t.Run("signal", func(t *testing.T) {
		assert.Contains(t, runHarness(t, signalPath, 143), "signal=SIGTERM")
	})
	liteLLMPath := filepath.Join(tempDir, "litellm-aider")
	require.NoError(t, os.WriteFile(liteLLMPath, []byte("#!/bin/sh\necho 'litellm.APIError: unavailable' >&2\n"), 0o700))
	t.Run("LiteLLM error", func(t *testing.T) {
		assert.Contains(t, runHarness(t, liteLLMPath, 1), "Aider execution reported a LiteLLM error")
	})
}

func TestAiderCompiledHarnesses(t *testing.T) {
	for _, path := range []string{
		"../../.github/workflows/shared/aider.md",
		"../../.github/workflows/smoke-aider.lock.yml",
		"../../.github/workflows/daily-code-debt-aider.lock.yml",
		"../../.github/workflows/daily-go-test-stubs-aider.lock.yml",
		"../../.github/workflows/engine-conformance-aider.lock.yml",
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read Aider workflow file %s: %v", path, err)
		}
		config := string(content)

		for _, expected := range []string{
			`require("./process_runner.cjs")`,
			"fetchAWFReflect({ logger: log })",
			"process.exitCode = result.exitCode",
			"onStdoutLine: observeLine",
			"Aider execution reported a LiteLLM error",
		} {
			if !strings.Contains(config, expected) {
				t.Errorf("expected %s to preserve Aider failure detail %q", path, expected)
			}
		}
	}
}

func TestAiderSampleProviderExecution(t *testing.T) {
	def := loadAiderSample(t)
	require.Equal(t, "0.86.2", def.Version)
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	assert.False(t, engine.GetCapabilities().MCP)
	assert.Nil(t, def.Behaviors.ConfigFile, "repository Aider configuration must not be overwritten")
	assert.NotContains(t, def.Behaviors.HarnessScript, "172.30.0.30")
	assert.Equal(t, "aider_log_parser", engine.GetLogParserScriptId())
	assert.Contains(t, engine.GetLogParserScriptSource(), "sourceEngine")
	for _, test := range []struct{ model, provider string }{
		{"copilot/auto", "github"},
		{"openai/gpt-5", "openai"},
		{"codex/gpt-5", "openai"},
		{"anthropic/claude-sonnet-4-5", "anthropic"},
	} {
		t.Run(test.model, func(t *testing.T) {
			data := &WorkflowData{Name: "Aider", Model: test.model, EngineConfig: &EngineConfig{Version: def.Version}}
			steps := engine.GetExecutionSteps(data, "/tmp/gh-aw/agent-stdio.log")
			require.NotEmpty(t, steps)
			execution := strings.Join(steps[len(steps)-1], "\n")
			assert.Contains(t, execution, "AIDER_MODEL: "+test.model)
			assert.Contains(t, execution, "GH_AW_LLM_PROVIDER: "+test.provider)
			assert.Contains(t, execution, "AWF_REFLECT_ENABLED: 1")
			assert.Contains(t, execution, "aider_harness.cjs")
			assert.NotContains(t, execution, "--openai-api-base")
		})
	}
}

func TestAiderHarnessStreamsLargeOutput(t *testing.T) {
	for _, reportedError := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "early LiteLLM error"}[reportedError], func(t *testing.T) {
			dir := t.TempDir()
			fixture := filepath.Join(dir, "output.cjs")
			source := `process.stdout.write(("x".repeat(1023) + "\n").repeat(2048)); process.stderr.write("stderr preserved\n");`
			if reportedError {
				source = `process.stdout.write("litellm.APIError: unavailable\n");` + source
			}
			require.NoError(t, os.WriteFile(fixture, []byte(source), 0o600))
			cmd := aiderHarnessCommand(t, dir, "node", fixture)
			output, err := cmd.CombinedOutput()
			if reportedError {
				require.Error(t, err)
				assert.Contains(t, string(output), "Aider execution reported a LiteLLM error")
			} else {
				require.NoError(t, err, "%s", output)
			}
			assert.Greater(t, len(output), 2*1024*1024)
			assert.Contains(t, string(output), "stderr preserved")
			assert.NotContains(t, string(output), "ENOBUFS")
		})
	}
}

func TestAiderWorkflowsUseSafeoutputsCLI(t *testing.T) {
	for _, path := range []string{
		"../../.github/workflows/daily-code-debt-aider.md",
		"../../.github/workflows/daily-go-test-stubs-aider.md",
		"../../.github/workflows/smoke-aider.md",
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read Aider workflow %s: %v", path, err)
		}
		workflow := string(content)
		if !strings.Contains(workflow, "safeoutputs ") {
			t.Errorf("expected workflow %s to use the safeoutputs CLI", path)
		}
		if strings.Contains(workflow, "GH_AW_SAFE_OUTPUTS") {
			t.Errorf("workflow %s must not write directly to GH_AW_SAFE_OUTPUTS", path)
		}

		lockPath := strings.TrimSuffix(path, ".md") + ".lock.yml"
		lockContent, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("failed to read Aider lock file %s: %v", lockPath, err)
		}
		lock := string(lockContent)
		if !strings.Contains(lock, `GH_AW_MCP_CLI_SERVERS='["safeoutputs"]'`) {
			t.Errorf("expected safeoutputs MCP CLI to be mounted for %s", path)
		}
		if strings.Contains(lock, `--mount "${RUNNER_TEMP}/gh-aw/safeoutputs:${RUNNER_TEMP}/gh-aw/safeoutputs:rw"`) {
			t.Errorf("Aider execution must not mount the safe-output directory read-write for %s", path)
		}
	}
}
