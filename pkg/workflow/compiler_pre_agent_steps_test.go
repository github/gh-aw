//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"gopkg.in/yaml.v3"
)

type compiledWorkflowJobs struct {
	Jobs map[string]map[string]any `yaml:"jobs"`
}

func TestPreAgentStepsGeneration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "pre-agent-steps-test")

	testContent := `---
on: push
permissions:
  contents: read
checkout:
  force-clean-git-credentials: true
pre-agent-steps:
  - name: Finalize prompt context
    run: echo "finalize"
engine: claude
strict: false
---

Test pre-agent-steps.
`

	testFile := filepath.Join(tmpDir, "test-pre-agent-steps.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with pre-agent-steps: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test-pre-agent-steps.lock.yml")
	lockBytes, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}
	lockContent := string(lockBytes)

	if !strings.Contains(lockContent, "- name: Finalize prompt context") {
		t.Error("Expected pre-agent-step to be in generated workflow")
	}

	startMCPGatewayIndex := indexInNonCommentLines(lockContent, "- name: Start MCP Gateway")
	cleanCheckoutCredentialsIndex := indexInNonCommentLines(lockContent, "- name: Clean git credentials after checkout")
	preAgentStepIndex := indexInNonCommentLines(lockContent, "- name: Finalize prompt context")
	aiStepIndex := indexInNonCommentLines(lockContent, "- name: Execute Claude Code CLI")
	if startMCPGatewayIndex == -1 || cleanCheckoutCredentialsIndex == -1 || preAgentStepIndex == -1 || aiStepIndex == -1 {
		t.Fatal("Could not find expected steps in generated workflow")
	}
	if cleanCheckoutCredentialsIndex >= preAgentStepIndex {
		t.Errorf("Clean git credentials after checkout (%d) should appear before pre-agent-step (%d)", cleanCheckoutCredentialsIndex, preAgentStepIndex)
	}
	if preAgentStepIndex >= startMCPGatewayIndex {
		t.Errorf("Pre-agent-step (%d) should appear before Start MCP Gateway (%d)", preAgentStepIndex, startMCPGatewayIndex)
	}
	if preAgentStepIndex >= aiStepIndex {
		t.Errorf("Pre-agent-step (%d) should appear before AI execution step (%d)", preAgentStepIndex, aiStepIndex)
	}
	var compiled compiledWorkflowJobs
	if err := yaml.Unmarshal(lockBytes, &compiled); err != nil {
		t.Fatalf("Failed to parse generated lock file as YAML: %v", err)
	}
	for jobName, job := range compiled.Jobs {
		rawSteps, hasSteps := job["steps"]
		if !hasSteps {
			continue
		}
		steps, ok := rawSteps.([]any)
		if !ok {
			t.Fatalf("Expected %s job steps to decode as a YAML sequence, got %T", jobName, rawSteps)
		}
		if len(steps) == 0 {
			t.Fatalf("Expected %s job to omit empty steps blocks instead of emitting steps: []", jobName)
		}
	}
}

func TestPreAgentStepsImportsMergeOrder(t *testing.T) {
	tmpDir := testutil.TempDir(t, "pre-agent-steps-imports-test")

	sharedContent := `---
pre-agent-steps:
  - name: Imported pre-agent step
    run: echo "imported"
---

Shared steps.
`
	sharedFile := filepath.Join(tmpDir, "shared.md")
	if err := os.WriteFile(sharedFile, []byte(sharedContent), 0644); err != nil {
		t.Fatal(err)
	}

	mainContent := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared.md
pre-agent-steps:
  - name: Main pre-agent step
    run: echo "main"
engine: claude
strict: false
---

Main workflow.
`
	mainFile := filepath.Join(tmpDir, "main.md")
	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(mainFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with imported pre-agent-steps: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "main.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}
	lockContent := string(content)

	importedIdx := indexInNonCommentLines(lockContent, "- name: Imported pre-agent step")
	mainIdx := indexInNonCommentLines(lockContent, "- name: Main pre-agent step")
	startMCPGatewayIdx := indexInNonCommentLines(lockContent, "- name: Start MCP Gateway")
	aiStepIdx := indexInNonCommentLines(lockContent, "- name: Execute Claude Code CLI")
	if importedIdx == -1 || mainIdx == -1 || startMCPGatewayIdx == -1 || aiStepIdx == -1 {
		t.Fatal("Could not find expected pre-agent, MCP gateway, and AI steps in generated workflow")
	}
	if importedIdx >= mainIdx {
		t.Errorf("Imported pre-agent-step (%d) should appear before main pre-agent-step (%d)", importedIdx, mainIdx)
	}
	if mainIdx >= startMCPGatewayIdx {
		t.Errorf("Main pre-agent-step (%d) should appear before Start MCP Gateway (%d)", mainIdx, startMCPGatewayIdx)
	}
	if mainIdx >= aiStepIdx {
		t.Errorf("Main pre-agent-step (%d) should appear before AI execution step (%d)", mainIdx, aiStepIdx)
	}
}

func TestImportedPreAgentStepsRunAfterPRBaseRestore(t *testing.T) {
	const (
		stepNamePrefix      = "- name: "
		baseRestoreStepName = "Restore agent config folders from base branch"
	)

	testCases := []struct {
		engine        string
		agentFolder   string
		executionStep string
		tools         string
	}{
		{engine: "claude", agentFolder: ".claude", executionStep: "Execute Claude Code CLI"},
		{engine: "copilot", agentFolder: ".github", executionStep: "Execute GitHub Copilot CLI"},
		{engine: "codex", agentFolder: ".codex", executionStep: "Execute Codex CLI"},
		{engine: "gemini", agentFolder: ".gemini", executionStep: "Execute Gemini CLI"},
		{
			engine:        "pi",
			agentFolder:   ".pi",
			executionStep: "Execute Pi CLI",
			tools: `tools:
  github:
    mode: gh-proxy
  cli-proxy: true
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.engine, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "pre-agent-steps-pr-restore-"+tc.engine)
			sharedDir := filepath.Join(tmpDir, "shared")
			if err := os.MkdirAll(sharedDir, 0755); err != nil {
				t.Fatal(err)
			}

			sharedContent := `---
pre-agent-steps:
  - name: Restore APM packages
    run: echo "restore apm"
---

Shared APM-style steps.
`
			if err := os.WriteFile(filepath.Join(sharedDir, "apm.md"), []byte(sharedContent), 0644); err != nil {
				t.Fatal(err)
			}

			mainContent := `---
on:
  pull_request:
    types: [opened]
permissions:
  contents: read
  issues: read
  pull-requests: read
imports:
  - ./shared/apm.md
engine: ` + tc.engine + `
` + tc.tools + `strict: false
---

Main workflow.
`
			mainFile := filepath.Join(tmpDir, "main.md")
			if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(mainFile); err != nil {
				t.Fatalf("Unexpected error compiling workflow with imported pre-agent-steps in PR context: %v", err)
			}

			content, err := os.ReadFile(filepath.Join(tmpDir, "main.lock.yml"))
			if err != nil {
				t.Fatalf("Failed to read generated lock file: %v", err)
			}
			lockContent := string(content)

			restoreBaseIdx := indexInNonCommentLines(lockContent, stepNamePrefix+baseRestoreStepName)
			restoreAPMIdx := indexInNonCommentLines(lockContent, stepNamePrefix+"Restore APM packages")
			aiStepIdx := indexInNonCommentLines(lockContent, stepNamePrefix+tc.executionStep)
			if restoreBaseIdx == -1 || restoreAPMIdx == -1 || aiStepIdx == -1 {
				t.Fatal("Could not find expected base-restore, pre-agent, and AI steps in generated workflow")
			}
			restoreStepStart := strings.Index(lockContent, stepNamePrefix+baseRestoreStepName)
			if restoreStepStart == -1 {
				t.Fatal("Could not find base restore step")
			}
			restoreStep := lockContent[restoreStepStart:]
			// Generated job steps use six spaces; stop at the next top-level step.
			if nextStep := strings.Index(restoreStep, "\n      - name: "); nextStep != -1 {
				restoreStep = restoreStep[:nextStep]
			}
			const agentFoldersPrefix = "GH_AW_AGENT_FOLDERS: \""
			agentFoldersStart := strings.Index(restoreStep, agentFoldersPrefix)
			if agentFoldersStart == -1 {
				t.Fatal("Could not find protected agent folders in base restore step")
			}
			agentFolders, _, found := strings.Cut(restoreStep[agentFoldersStart+len(agentFoldersPrefix):], "\"")
			if !found {
				t.Fatal("Could not parse protected agent folders in base restore step")
			}
			if !strings.Contains(" "+agentFolders+" ", " "+tc.agentFolder+" ") {
				t.Errorf("Expected base restore to include %s", tc.agentFolder)
			}
			// Base restore must run first so it cannot clobber APM-restored skills.
			if restoreBaseIdx >= restoreAPMIdx {
				t.Errorf("Base restore step (%d) should appear before APM restore step (%d)", restoreBaseIdx, restoreAPMIdx)
			}
			if restoreAPMIdx >= aiStepIdx {
				t.Errorf("Imported pre-agent step (%d) should appear before AI execution step (%d)", restoreAPMIdx, aiStepIdx)
			}
		})
	}
}
