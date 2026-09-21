//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestCompileWorkflow_ArcDindStagesCopilotCLIOnlyForCopilot(t *testing.T) {
	const copilotStagingStepName = "Copy Copilot CLI to daemon-visible path"

	for _, tc := range []struct {
		engine    string
		wantStage bool
	}{
		{engine: "claude"},
		{engine: "gemini"},
		{engine: "pi"},
		{engine: "copilot", wantStage: true},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			workflow := fmt.Sprintf(`---
on: workflow_dispatch
engine: %s
runner:
  topology: arc-dind
network:
  allowed:
    - defaults
---

# Test
`, tc.engine)
			testFile := filepath.Join(testutil.TempDir(t, tc.engine+"-arc-dind-test"), "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(workflow), 0644); err != nil {
				t.Fatal(err)
			}

			if err := NewCompiler().CompileWorkflow(testFile); err != nil {
				t.Fatalf("compile %s workflow: %v", tc.engine, err)
			}

			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("read lock file: %v", err)
			}
			if got := strings.Contains(string(lockContent), copilotStagingStepName); got != tc.wantStage {
				t.Fatalf("Copilot CLI staging = %v, want %v", got, tc.wantStage)
			}
		})
	}

	t.Run("behavior-defined", func(t *testing.T) {
		engine, err := NewBehaviorDefinedEngine(&EngineDefinition{
			ID:          "custom",
			DisplayName: "Custom",
			Behaviors: &EngineBehaviorDefinition{
				Installation: &EngineInstallationDefinition{
					PackageManager:   "npm",
					PackageName:      "custom",
					Version:          "1.0.0",
					StepName:         "Install Custom",
					IncludeNodeSetup: true,
				},
				Execution: &EngineExecutionDefinition{
					CommandName: "custom",
					StepName:    "Execute Custom",
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		steps := engine.GetInstallationSteps(&WorkflowData{
			Name:         "test",
			EngineConfig: &EngineConfig{ID: "custom"},
			RunnerConfig: &RunnerConfig{Topology: RunnerTopologyArcDind},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
		})
		if strings.Contains(strings.Join(flattenSteps(steps), "\n"), copilotStagingStepName) {
			t.Fatal("behavior-defined workflow must not stage the Copilot CLI")
		}
	})
}
