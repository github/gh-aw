//go:build !integration

// Tests guarding failure instrumentation shared by every agent execution step.
package workflow

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentExecutionExitCodeTrap_Shellcheck(t *testing.T) {
	for _, tt := range []struct {
		name string
		trap string
	}{
		{name: "shared", trap: buildAgentExecutionExitCodeTrap()},
		{name: "Copilot cleanup", trap: buildCopilotSettingsCleanupAndExitCodeTrap()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.True(t, strings.HasPrefix(tt.trap, "gh_aw_exit_code=0\ntrap "),
				"exit code must be declared before the trap:\n%s", tt.trap)

			if _, err := exec.LookPath("shellcheck"); err != nil {
				t.Skip("shellcheck is not installed")
			}
			cmd := exec.Command("shellcheck", "-s", "bash", "-")
			cmd.Stdin = strings.NewReader(tt.trap)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "generated trap must pass ShellCheck:\n%s", output)
		})
	}
}

// TestAgentExecutionExitCodeTrap_EmitsErrorAnnotation pins the generated trap so the
// failure annotation cannot be silently dropped.
func TestAgentExecutionExitCodeTrap_EmitsErrorAnnotation(t *testing.T) {
	got := buildCopilotSettingsCleanupAndExitCodeTrap()

	assert.Contains(t, got, `if [ "$gh_aw_exit_code" -ne 0 ]; then`,
		"trap must guard the annotation on a non-zero exit code:\n%s", got)
	assert.Contains(t, got, `echo "::error::Agent execution exited with code $gh_aw_exit_code"`,
		"trap must emit an error annotation with the exit code:\n%s", got)
	assert.Contains(t, got, `> `+agentExecutionExitCodePath,
		"trap must still persist the exit code for the OTLP conclusion span:\n%s", got)
	assert.Contains(t, got, `rm -f "`+copilotSettingsPath+`"`,
		"trap must still clean up the Copilot settings file:\n%s", got)
}

// TestBashIntegration_AgentExecutionExitCodeTrap runs the generated trap through bash to
// confirm the annotation is printed with the real exit code on failure, that the
// exit code file is still written, and that successful runs stay silent.
func TestBashIntegration_AgentExecutionExitCodeTrap(t *testing.T) {
	trap := buildCopilotSettingsCleanupAndExitCodeTrap()

	tests := []struct {
		name          string
		body          string
		wantExitCode  string
		wantErrorLine bool
	}{
		{name: "success is silent", body: "true\n", wantExitCode: "0"},
		{name: "explicit failure", body: "exit 3\n", wantExitCode: "3", wantErrorLine: true},
		{name: "failing command", body: "set -o pipefail\nfalse | cat\n", wantExitCode: "1", wantErrorLine: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			exitCodeFile := t.TempDir() + "/agent_execution_exit_code.txt"
			script := strings.Replace(trap, agentExecutionExitCodePath, exitCodeFile, 1) + tt.body

			stdout, stderr, _ := runBashWithHome(t, home, script)

			errorLine := "::error::Agent execution exited with code " + tt.wantExitCode
			if tt.wantErrorLine {
				assert.Contains(t, stdout, errorLine,
					"failing run must surface the exit code:\nstdout=%s\nstderr=%s", stdout, stderr)
			} else {
				assert.NotContains(t, stdout, "::error::",
					"successful run must not emit an error annotation:\nstdout=%s", stdout)
			}

			written, err := os.ReadFile(exitCodeFile)
			require.NoError(t, err, "trap must persist the exit code file")
			assert.Equal(t, tt.wantExitCode, string(written),
				"persisted exit code must match the script exit status")
		})
	}
}

// TestCopilotExecutionStep_ContainsExitCodeAnnotation verifies the shared annotation is
// present in the generated step for both the direct and firewall command paths.
func TestCopilotExecutionStep_ContainsExitCodeAnnotation(t *testing.T) {
	tests := []struct {
		name string
		wd   *WorkflowData
	}{
		{
			name: "direct command",
			wd:   &WorkflowData{Name: "direct"},
		},
		{
			name: "firewall command",
			wd: &WorkflowData{
				Name: "firewall",
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewCopilotEngine()
			steps := engine.GetExecutionSteps(tt.wd, "/tmp/gh-aw/test.log")
			stepContent := requireCopilotExecutionStep(t, steps)

			assert.Contains(t, stepContent, "::error::Agent execution exited with code",
				"execution step must surface a non-zero exit code in the log:\n%s", stepContent)
		})
	}
}

func TestAllAgentExecutionSteps_ContainExitCodeAnnotation(t *testing.T) {
	workflowData := &WorkflowData{Name: "agent"}
	tests := []struct {
		name  string
		steps []GitHubActionStep
	}{
		{name: "Claude", steps: NewClaudeEngine().GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")},
		{name: "Codex", steps: NewCodexEngine().GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")},
		{name: "Gemini", steps: NewGeminiEngine().GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")},
		{name: "Pi", steps: NewPiEngine().GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")},
		{
			name: "universal CLI",
			steps: (&UniversalLLMConsumerEngine{}).BuildCLIEngineExecutionSteps(
				workflowData,
				"/tmp/gh-aw/test.log",
				UniversalCLIEngineExecutionConfig{
					DefaultCommandName: "test-cli",
					EngineConstant:     constants.CopilotEngine,
					StepName:           "Execute test CLI",
				},
			),
		},
	}

	behaviorEngine, err := NewBehaviorDefinedEngine(newHarnessEngineDefinition())
	require.NoError(t, err)
	tests = append(tests, struct {
		name  string
		steps []GitHubActionStep
	}{
		name:  "behavior-defined",
		steps: behaviorEngine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log"),
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotEmpty(t, tt.steps)
			var allSteps strings.Builder
			for _, step := range tt.steps {
				allSteps.WriteString(strings.Join(step, "\n"))
				allSteps.WriteString("\n")
			}
			assert.Contains(t, allSteps.String(),
				"::error::Agent execution exited with code",
				"execution step must surface a non-zero exit code in the log")
		})
	}
}

func TestWrapAgentExecutionCommand_PlacesTrapAfterPipefail(t *testing.T) {
	command := wrapAgentExecutionCommand("set -o pipefail\nfalse | cat")

	assert.True(t, strings.HasPrefix(command, "set -o pipefail\ngh_aw_exit_code=0\ntrap "),
		"the trap must follow pipefail setup:\n%s", command)
}
