//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestCopilotEngineHarnessScript(t *testing.T) {
	engine := NewCopilotEngine()

	t.Run("GetHarnessScriptName returns copilot_harness.cjs", func(t *testing.T) {
		if engine.GetHarnessScriptName() != "copilot_harness.cjs" {
			t.Errorf("Expected 'copilot_harness.cjs', got '%s'", engine.GetHarnessScriptName())
		}
	})

	t.Run("Execution step uses driver in non-sandbox mode", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:         "test-workflow",
			EngineConfig: &EngineConfig{ID: "copilot"},
			Tools:        make(map[string]any),
			SafeOutputs:  nil,
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one step")
		}

		stepContent := strings.Join([]string(steps[0]), "\n")

		// The driver should be used in the command
		if !strings.Contains(stepContent, "copilot_harness.cjs") {
			t.Errorf("Expected copilot_harness.cjs in execution step, got:\n%s", stepContent)
		}
		if !strings.Contains(stepContent, nodeRuntimeResolutionCommand) {
			t.Errorf("Expected runtime node resolution logic in execution step, got:\n%s", stepContent)
		}

		// Driver should appear before the copilot args
		driverIdx := strings.Index(stepContent, "copilot_harness.cjs")
		promptIdx := strings.Index(stepContent, "--prompt")
		if driverIdx == -1 || promptIdx == -1 {
			t.Fatal("Could not find both copilot_harness.cjs and --prompt in step")
		}
		if driverIdx > promptIdx {
			t.Error("Expected copilot_harness.cjs to appear before --prompt")
		}
	})

	for _, tt := range []struct {
		name    string
		runtime AgentRuntime
	}{
		{name: "Docker"},
	} {
		t.Run("AWF execution stages activated Copilot CLI binary for "+tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				Name: "test-workflow",
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						ID:      "awf",
						Runtime: tt.runtime,
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			}

			steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
			stepContent := strings.Join([]string(steps[0]), "\n")

			if !strings.Contains(stepContent, `GH_AW_COPILOT_SRC="$(command -v copilot 2>/dev/null || true)"`) {
				t.Fatalf("Expected AWF setup to resolve the activated Copilot CLI binary, got:\n%s", stepContent)
			}
			if !strings.Contains(stepContent, `cp "$GH_AW_COPILOT_SRC" "$GH_AW_COPILOT_BIN"`) {
				t.Fatalf("Expected AWF setup to stage the Copilot CLI binary in its mounted directory, got:\n%s", stepContent)
			}
			mountedCopilotPath := `"` + constants.GhAwRootDirShell + `/bin/copilot"`
			if !strings.Contains(stepContent, `copilot_harness.cjs" `+mountedCopilotPath) {
				t.Fatalf("Expected harness to use mounted Copilot CLI path %q, got:\n%s", mountedCopilotPath, stepContent)
			}
			mount := `--mount "${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro"`
			if !strings.Contains(stepContent, mount) {
				t.Fatalf("Expected AWF to mount the staged Copilot CLI directory, got:\n%s", stepContent)
			}
			if strings.Contains(stepContent, `copilot_harness.cjs" `+constants.CopilotBinaryPath) {
				t.Fatalf("Expected harness to avoid the fixed Copilot CLI path %q, got:\n%s", constants.CopilotBinaryPath, stepContent)
			}
		})
	}

	t.Run("AWF custom command does not require installed Copilot CLI binary", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID:      "copilot",
				Command: "custom-copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
		stepContent := strings.Join([]string(steps[0]), "\n")

		if strings.Contains(stepContent, "GH_AW_COPILOT_BIN") {
			t.Fatalf("Expected custom command to avoid resolving the installed Copilot CLI binary, got:\n%s", stepContent)
		}
	})

	t.Run("Execution step uses configured custom driver instead of built-in", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID:            "copilot",
				HarnessScript: "custom_copilot_harness.cjs",
			},
			Tools: make(map[string]any),
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one step")
		}

		stepContent := strings.Join([]string(steps[0]), "\n")

		if !strings.Contains(stepContent, "custom_copilot_harness.cjs") {
			t.Errorf("Expected custom driver in execution step, got:\n%s", stepContent)
		}
		if strings.Contains(stepContent, "actions/copilot_harness.cjs") {
			t.Errorf("Expected built-in driver to be replaced, got:\n%s", stepContent)
		}
	})

	t.Run("CopilotEngine implements HarnessProvider interface", func(t *testing.T) {
		var _ HarnessProvider = engine
	})

	t.Run("Execution serializes engine.command into shell script", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID:      "copilot",
				Command: `bash -lc 'echo custom command'`,
			},
			Tools: make(map[string]any),
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one step")
		}

		stepContent := strings.Join([]string(steps[0]), "\n")

		if !strings.Contains(stepContent, `copilot_harness.cjs" /tmp/gh-aw/engine-command.sh`) {
			t.Errorf("Expected driver to run serialized engine command script, got:\n%s", stepContent)
		}
		if !strings.Contains(stepContent, "cat > /tmp/gh-aw/engine-command.sh <<'GH_AW_ENGINE_COMMAND_EOF'") {
			t.Errorf("Expected step to serialize engine.command into script via heredoc, got:\n%s", stepContent)
		}
		if !strings.Contains(stepContent, "GH_AW_ENGINE_COMMAND_EOF") {
			t.Errorf("Expected step to include heredoc delimiter for script serialization, got:\n%s", stepContent)
		}
		if !strings.Contains(stepContent, `sudo chown -R "$(id -u):$(id -g)" "$HOME/.copilot"`) {
			t.Errorf("Expected step to fix ownership for $HOME/.copilot in custom engine.command mode, got:\n%s", stepContent)
		}
	})
}

func TestCopilotGeneratedHarnessCommandSupportsSpacedRunnerPaths(t *testing.T) {
	engine := NewCopilotEngine()
	runnerTemp := filepath.Join(t.TempDir(), "self hosted runner temp")
	toolcacheBin := filepath.Join(t.TempDir(), "custom toolcache", "copilot-cli", "1.2.3", "x64", "bin")
	if err := os.MkdirAll(toolcacheBin, 0o755); err != nil {
		t.Fatal(err)
	}

	cachedCopilot := filepath.Join(toolcacheBin, "copilot")
	if err := os.WriteFile(cachedCopilot, []byte("#!/usr/bin/env bash\nprintf 'copilot %s\\n' \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	nodeDir := filepath.Join(t.TempDir(), "custom node runtime")
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nodePath := filepath.Join(nodeDir, "node")
	if err := os.WriteFile(nodePath, []byte("#!/usr/bin/env bash\nprintf '<%s>\\n' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	harnessPath := filepath.Join(runnerTemp, "gh-aw", "actions", "copilot_harness.cjs")
	mountedCopilotPath := filepath.Join(runnerTemp, "gh-aw", "bin", "copilot")
	sdkDriverPath := filepath.Join(runnerTemp, "gh-aw", "actions", "copilot_sdk_driver.cjs")

	testCases := []struct {
		name         string
		workflowData *WorkflowData
		wantArgs     []string
	}{
		{
			name:         "CLI",
			workflowData: &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot"}},
			wantArgs:     []string{harnessPath, mountedCopilotPath},
		},
		{
			name:         "SDK",
			workflowData: &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot", CopilotSDK: true}},
			wantArgs:     []string{harnessPath, nodePath, sdkDriverPath, mountedCopilotPath},
		},
		{
			name:         "SDK arbitrary driver",
			workflowData: &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot", CopilotSDK: true, Driver: "custom-driver"}},
			wantArgs:     []string{harnessPath, "custom-driver", mountedCopilotPath},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			commandName, _ := engine.resolveCopilotCommand(tc.workflowData, true)
			command := copilotBinaryPathSetup + "\n" + engine.buildCopilotExecPrefix(tc.workflowData, commandName)
			cmd := exec.Command("bash", "-c", command)
			cmd.Env = append(os.Environ(),
				"GH_AW_NODE_BIN="+nodePath,
				"RUNNER_TEMP="+runnerTemp,
				"PATH="+toolcacheBin+":"+os.Getenv("PATH"),
			)

			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Expected generated harness command to preserve spaced paths: %v\n%s", err, output)
			}
			var wantOutput strings.Builder
			for _, arg := range tc.wantArgs {
				wantOutput.WriteString("<" + arg + ">\n")
			}
			if got := string(output); got != wantOutput.String() {
				t.Fatalf("Expected generated harness arguments:\n%s\ngot:\n%s", wantOutput.String(), got)
			}
			if _, err := os.Stat(mountedCopilotPath); err != nil {
				t.Fatalf("Expected Copilot CLI at mounted runner path: %v", err)
			}
		})
	}
}

func TestBuildEngineCommandScriptSetup(t *testing.T) {
	setup := buildEngineCommandScriptSetup("/usr/local/bin/custom-copilot")

	if !strings.Contains(setup, "umask 0177") {
		t.Fatalf("Expected restrictive umask in script setup, got:\n%s", setup)
	}
	if !strings.Contains(setup, `GH_AW_PREV_UMASK="$(umask)"`) {
		t.Fatalf("Expected script setup to preserve original umask, got:\n%s", setup)
	}
	if !strings.Contains(setup, `umask "$GH_AW_PREV_UMASK"`) {
		t.Fatalf("Expected script setup to restore original umask, got:\n%s", setup)
	}
	if !strings.Contains(setup, "chmod 700 /tmp/gh-aw/engine-command.sh") {
		t.Fatalf("Expected owner-only execute permissions, got:\n%s", setup)
	}
	if !strings.Contains(setup, "cat > /tmp/gh-aw/engine-command.sh <<'GH_AW_ENGINE_COMMAND_EOF'") {
		t.Fatalf("Expected heredoc-based script materialization, got:\n%s", setup)
	}
	if !strings.Contains(setup, "set -eo pipefail") {
		t.Fatalf("Expected script strict mode without -u, got:\n%s", setup)
	}
	if strings.Contains(setup, "set -euo pipefail") {
		t.Fatalf("Expected script strict mode to drop -u, got:\n%s", setup)
	}
	if !strings.Contains(setup, "set +o histexpand") {
		t.Fatalf("Expected histexpand disabled in engine command script, got:\n%s", setup)
	}
	if !strings.Contains(setup, `/usr/local/bin/custom-copilot "$@"`) {
		t.Fatalf("Expected custom command to forward driver args, got:\n%s", setup)
	}
}
