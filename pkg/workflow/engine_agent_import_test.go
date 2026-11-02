//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
)

// TestCopilotEngineWithAgentFromEngineConfig tests that copilot engine includes --agent flag when specified in engine.agent
func TestCopilotEngineWithAgentFromEngineConfig(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID:    "copilot",
			Agent: "my-custom-agent",
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	// Copilot CLI expects agent identifier
	if !strings.Contains(stepContent, `--agent my-custom-agent`) {
		t.Errorf("Expected '--agent my-custom-agent' in copilot command, got:\n%s", stepContent)
	}
}

// TestCopilotEngineWithAgentFromImports tests that agent imports do NOT set --agent flag
// Agent imports only import markdown content, not agent configuration
func TestCopilotEngineWithAgentFromImports(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID: "copilot",
		},
		AgentFile: ".github/agents/test-agent.md",
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	// Agent imports should NOT set --agent flag (only engine.agent does)
	if strings.Contains(stepContent, `--agent`) {
		t.Errorf("Did not expect '--agent' flag when only AgentFile is set (without engine.agent), got:\n%s", stepContent)
	}
}

// TestCopilotEngineAgentOnlyFromEngineConfig tests that --agent flag is only set by engine.agent
func TestCopilotEngineAgentOnlyFromEngineConfig(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID:    "copilot",
			Agent: "explicit-agent",
		},
		AgentFile: ".github/agents/import-agent.md",
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	// Should only use explicit agent from engine.agent
	if !strings.Contains(stepContent, `--agent explicit-agent`) {
		t.Errorf("Expected '--agent explicit-agent' in copilot command, got:\n%s", stepContent)
	}
	// Should not use agent from imports
	if strings.Contains(stepContent, `--agent import-agent`) {
		t.Errorf("Did not expect '--agent import-agent' when engine.agent is set, got:\n%s", stepContent)
	}
}

// TestCopilotEngineWithoutAgentFlag tests that copilot engine works without agent file
func TestCopilotEngineWithoutAgentFlag(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID: "copilot",
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	if strings.Contains(stepContent, "--agent") {
		t.Errorf("Did not expect '--agent' flag when agent file is not specified, got:\n%s", stepContent)
	}
}

// TestClaudeEngineWithAgentFromImports tests that claude engine does NOT handle agent files
// natively — agent file content is prepended to prompt.txt by the compiler in the activation
// job, so the engine step always reads the standard prompt.txt path.
func TestClaudeEngineWithAgentFromImports(t *testing.T) {
	engine := NewClaudeEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID: "claude",
		},
		AgentFile: ".github/agents/test-agent.md",
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	// Claude does not handle the agent file natively — no awk or AGENT_CONTENT/PROMPT_TEXT
	// variable juggling should appear in the step.
	if strings.Contains(stepContent, "AGENT_CONTENT") {
		t.Errorf("Claude must NOT handle agent file natively (AGENT_CONTENT found in step); the compiler handles it:\n%s", stepContent)
	}
	if strings.Contains(stepContent, "awk") {
		t.Errorf("Claude must NOT invoke awk for agent file reading (found in step); the compiler handles it:\n%s", stepContent)
	}
	if strings.Contains(stepContent, "PROMPT_TEXT") {
		t.Errorf("Claude must NOT use a PROMPT_TEXT shell variable (found in step); the compiler handles it:\n%s", stepContent)
	}

	// The engine still reads the standard prompt.txt (which has agent content prepended by the compiler).
	// With harness: --prompt-file is used instead of shell expansion.
	if !strings.Contains(stepContent, "--prompt-file /tmp/gh-aw/aw-prompts/prompt.txt") {
		t.Errorf("Expected standard prompt.txt reading in claude command, got:\n%s", stepContent)
	}

	// The engine reports that it does not support native agent file handling.
	if engine.GetCapabilities().NativeAgentFile {
		t.Errorf("Claude engine should report NativeAgentFile=false")
	}
}

// TestClaudeEngineWithoutAgentFile tests that claude engine works without agent file
func TestClaudeEngineWithoutAgentFile(t *testing.T) {
	engine := NewClaudeEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID: "claude",
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	// Should not have agent content extraction
	if strings.Contains(stepContent, "AGENT_CONTENT") {
		t.Errorf("Did not expect AGENT_CONTENT when agent file is not specified, got:\n%s", stepContent)
	}

	// Should still have the standard prompt (via --prompt-file with harness).
	if !strings.Contains(stepContent, "--prompt-file /tmp/gh-aw/aw-prompts/prompt.txt") {
		t.Errorf("Expected standard prompt reading in claude command, got:\n%s", stepContent)
	}
}

// TestCodexEngineWithAgentFromImports tests that codex engine does NOT handle agent file natively
// and instead relies on the compiler to include the agent file content in prompt.txt
func TestCodexEngineWithAgentFromImports(t *testing.T) {
	engine := NewCodexEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID: "codex",
		},
		AgentFile: ".github/agents/test-agent.md",
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	// Codex does not handle the agent file natively — no awk or AGENT_CONTENT variable
	// juggling should appear in the step.
	if strings.Contains(stepContent, "AGENT_CONTENT") {
		t.Errorf("Codex must NOT handle agent file natively (AGENT_CONTENT found in step); the compiler handles it:\n%s", stepContent)
	}
	if strings.Contains(stepContent, "awk") {
		t.Errorf("Codex must NOT invoke awk for agent file reading (found in step); the compiler handles it:\n%s", stepContent)
	}

	// The engine still reads the standard prompt.txt (which has agent content prepended by the compiler)
	// via --prompt-file when using the default harness.
	if !strings.Contains(stepContent, "--prompt-file /tmp/gh-aw/aw-prompts/prompt.txt") {
		t.Errorf("Expected --prompt-file reading in codex command, got:\n%s", stepContent)
	}

	// The engine reports that it does not support native agent file handling.
	if engine.GetCapabilities().NativeAgentFile {
		t.Errorf("Codex engine should report NativeAgentFile=false")
	}
}

// TestCodexEngineWithoutAgentFile tests that codex engine works without agent file
func TestCodexEngineWithoutAgentFile(t *testing.T) {
	engine := NewCodexEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID: "codex",
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	// Should not have agent content extraction
	if strings.Contains(stepContent, "AGENT_CONTENT") {
		t.Errorf("Did not expect AGENT_CONTENT when agent file is not specified, got:\n%s", stepContent)
	}

	// Should have the standard instruction reading via --prompt-file (harness handles prompt reading)
	if !strings.Contains(stepContent, "--prompt-file /tmp/gh-aw/aw-prompts/prompt.txt") {
		t.Errorf("Expected --prompt-file reading in codex command, got:\n%s", stepContent)
	}
}

// TestCodexEngineDoesNotSupportNativeAgentFile verifies that the Codex engine declares
// it does not handle agent files natively, so the compiler knows to prepend the agent file
// content to prompt.txt during the activation job instead.
func TestCodexEngineDoesNotSupportNativeAgentFile(t *testing.T) {
	engine := NewCodexEngine()
	if engine.GetCapabilities().NativeAgentFile {
		t.Errorf("Codex engine should report NativeAgentFile=false; the compiler handles agent file injection")
	}
}

// TestCodexEngineAWFWithAgentFileReadsPromptTxt verifies that when an agent file is used
// with the firewall (AWF) enabled, the codex command reads from prompt.txt (not from a
// AGENT_CONTENT shell variable). The compiler prepends the agent file content to prompt.txt
// in the activation job.
func TestCodexEngineAWFWithAgentFileReadsPromptTxt(t *testing.T) {
	engine := NewCodexEngine()

	agentSandbox := &AgentSandboxConfig{Type: SandboxTypeAWF}
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID: "codex",
		},
		AgentFile: ".github/agents/test-agent.md",
		SandboxConfig: &SandboxConfig{
			Agent: agentSandbox,
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
	if len(steps) == 0 {
		t.Fatal("Expected at least one step")
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	// No AGENT_CONTENT shell variable anywhere in the step.
	if strings.Contains(stepContent, "AGENT_CONTENT") {
		t.Errorf("AGENT_CONTENT must not appear in the Codex AWF step; compiler handles agent file injection:\n%s", stepContent)
	}
	if strings.Contains(stepContent, "awk") {
		t.Errorf("awk must not appear in the Codex AWF step; compiler handles agent file injection:\n%s", stepContent)
	}

	// The container command must still pass prompt.txt via --prompt-file (harness handles reading).
	if !strings.Contains(stepContent, "--prompt-file /tmp/gh-aw/aw-prompts/prompt.txt") {
		t.Errorf("Expected codex to use --prompt-file in AWF mode, got:\n%s", stepContent)
	}
}

// TestGeminiEngineDoesNotSupportNativeAgentFile verifies that the Gemini engine declares
// it does not handle agent files natively, so the compiler knows to prepend the agent file
// content to prompt.txt during the activation job instead.
func TestGeminiEngineDoesNotSupportNativeAgentFile(t *testing.T) {
	engine := NewGeminiEngine()
	if engine.GetCapabilities().NativeAgentFile {
		t.Errorf("Gemini engine should report NativeAgentFile=false; the compiler handles agent file injection")
	}
}

// TestGeminiEngineWithAgentFromImports tests that Gemini engine does NOT handle agent file natively
// and instead relies on the compiler to include the agent file content in prompt.txt
func TestGeminiEngineWithAgentFromImports(t *testing.T) {
	engine := NewGeminiEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID: "gemini",
		},
		AgentFile: ".github/agents/test-agent.md",
		Tools:     map[string]any{},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	// GetExecutionSteps returns a settings step + execution step for Gemini
	if len(steps) == 0 {
		t.Fatal("Expected at least one execution step")
	}

	// Combine all step content for inspection
	var allContent strings.Builder
	for _, step := range steps {
		allContent.WriteString(strings.Join([]string(step), "\n"))
		allContent.WriteString("\n")
	}
	combined := allContent.String()

	// Gemini does not handle the agent file natively — no awk or AGENT_CONTENT
	if strings.Contains(combined, "AGENT_CONTENT") {
		t.Errorf("Gemini must NOT handle agent file natively (AGENT_CONTENT found in steps); the compiler handles it:\n%s", combined)
	}
	if strings.Contains(combined, "awk") {
		t.Errorf("Gemini must NOT invoke awk for agent file reading (found in steps); the compiler handles it:\n%s", combined)
	}

	// The execution step must read the prompt from prompt.txt
	if !strings.Contains(combined, `"$(cat /tmp/gh-aw/aw-prompts/prompt.txt)"`) {
		t.Errorf("Expected Gemini to read from prompt.txt, got:\n%s", combined)
	}
}

// TestGeminiEngineAWFWithAgentFileReadsPromptTxt verifies that when an agent file is used
// with the firewall (AWF) enabled, the gemini command reads from prompt.txt (not from a
// AGENT_CONTENT shell variable). The compiler prepends the agent file content to prompt.txt
// in the activation job.
func TestGeminiEngineAWFWithAgentFileReadsPromptTxt(t *testing.T) {
	engine := NewGeminiEngine()

	agentSandbox := &AgentSandboxConfig{Type: SandboxTypeAWF}
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID: "gemini",
		},
		AgentFile: ".github/agents/test-agent.md",
		SandboxConfig: &SandboxConfig{
			Agent: agentSandbox,
		},
		Tools: map[string]any{},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
	if len(steps) == 0 {
		t.Fatal("Expected at least one step")
	}

	var allContent strings.Builder
	for _, step := range steps {
		allContent.WriteString(strings.Join([]string(step), "\n"))
		allContent.WriteString("\n")
	}
	combined := allContent.String()

	// No AGENT_CONTENT shell variable anywhere in the steps.
	if strings.Contains(combined, "AGENT_CONTENT") {
		t.Errorf("AGENT_CONTENT must not appear in the Gemini AWF steps; compiler handles agent file injection:\n%s", combined)
	}
	if strings.Contains(combined, "awk") {
		t.Errorf("awk must not appear in the Gemini AWF steps; compiler handles agent file injection:\n%s", combined)
	}

	// The command must still read from prompt.txt.
	if !strings.Contains(combined, `"$(cat /tmp/gh-aw/aw-prompts/prompt.txt)"`) {
		t.Errorf("Expected gemini to read from prompt.txt in AWF mode, got:\n%s", combined)
	}
}

// TestAgentFileValidation tests compile-time validation of agent file existence
func TestAgentFileValidation(t *testing.T) {
	// Create a temporary directory structure that mimics a repository
	tmpDir, err := os.MkdirTemp("", "agent-validation-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create the directory structure: .github/agents/ and .github/workflows/
	agentsDir := filepath.Join(tmpDir, ".github", "agents")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("Failed to create agents directory: %v", err)
	}
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create a valid agent file
	agentContent := `---
on: push
title: Test Agent
---

# Test Agent Instructions

This is a test agent file.
`
	validAgentFilePath := filepath.Join(agentsDir, "valid-agent.md")
	if err := os.WriteFile(validAgentFilePath, []byte(agentContent), 0644); err != nil {
		t.Fatalf("Failed to create valid agent file: %v", err)
	}

	// Test 1: Valid agent file
	t.Run("valid_agent_file", func(t *testing.T) {
		compiler := NewCompiler()
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			AgentFile: validAgentFilePath,
		}

		workflowPath := filepath.Join(workflowsDir, "test.md")
		err := compiler.validateAgentFile(workflowData, workflowPath)
		if err != nil {
			t.Errorf("Expected no error for valid agent file, got: %v", err)
		}
	})

	// Test 2: Non-existent agent file
	t.Run("nonexistent_agent_file", func(t *testing.T) {
		compiler := NewCompiler()
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			AgentFile: filepath.Join(agentsDir, "nonexistent.md"),
		}

		workflowPath := filepath.Join(workflowsDir, "test.md")
		err := compiler.validateAgentFile(workflowData, workflowPath)
		if err == nil {
			t.Error("Expected error for non-existent agent file, got nil")
		} else if !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("Expected 'does not exist' error, got: %v", err)
		}
	})

	// Test 3: No agent file specified
	t.Run("no_agent_file", func(t *testing.T) {
		compiler := NewCompiler()
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
		}

		workflowPath := filepath.Join(workflowsDir, "test.md")
		err := compiler.validateAgentFile(workflowData, workflowPath)
		if err != nil {
			t.Errorf("Expected no error when agent file not specified, got: %v", err)
		}
	})

	// Test 4: Nil engine config
	t.Run("nil_engine_config", func(t *testing.T) {
		compiler := NewCompiler()
		workflowData := &WorkflowData{}

		workflowPath := filepath.Join(workflowsDir, "test.md")
		err := compiler.validateAgentFile(workflowData, workflowPath)
		if err != nil {
			t.Errorf("Expected no error when engine config is nil, got: %v", err)
		}
	})
}

// TestInvalidAgentFilePathGeneratesFailingStep tests that engines that do NOT handle agent files
// natively (Claude, Codex, Gemini) rely on the compiler's validateAgentFile to reject malicious
// paths at compile time. Engine steps should proceed normally and never reference agent file paths.
func TestInvalidAgentFilePathGeneratesFailingStep(t *testing.T) {
	maliciousPath := `.github/agents/a";id;"b.md`

	// Codex does not handle agent files natively; path validation is done by the compiler
	// at compile time (validateAgentFile). The engine step should proceed normally and never
	// reference the agent file path directly.
	t.Run("codex_ignores_agent_path_in_step_for_invalid_path", func(t *testing.T) {
		engine := NewCodexEngine()
		workflowData := &WorkflowData{
			Name:      "test-workflow",
			AgentFile: maliciousPath,
		}
		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")

		if len(steps) != 1 {
			t.Fatalf("Expected exactly 1 step, got %d", len(steps))
		}
		content := strings.Join([]string(steps[0]), "\n")
		// Must NOT reference the malicious path at all in the generated step
		if strings.Contains(content, maliciousPath) {
			t.Errorf("Codex step must not reference the agent file path directly, got:\n%s", content)
		}
		if strings.Contains(content, "awk") {
			t.Errorf("Codex step must not invoke awk for agent file reading, got:\n%s", content)
		}
	})

	// Claude does not handle agent files natively; path validation is done by the compiler
	// at compile time (validateAgentFile). The engine step should proceed normally and never
	// reference the agent file path directly.
	t.Run("claude_ignores_agent_path_in_step_for_invalid_path", func(t *testing.T) {
		engine := NewClaudeEngine()
		workflowData := &WorkflowData{
			Name:      "test-workflow",
			AgentFile: maliciousPath,
		}
		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")

		if len(steps) != 1 {
			t.Fatalf("Expected exactly 1 step, got %d", len(steps))
		}
		content := strings.Join([]string(steps[0]), "\n")
		// Must NOT reference the malicious path at all in the generated step
		if strings.Contains(content, maliciousPath) {
			t.Errorf("Claude step must not reference the agent file path directly, got:\n%s", content)
		}
		if strings.Contains(content, "awk") {
			t.Errorf("Claude step must not invoke awk for agent file reading, got:\n%s", content)
		}
	})
}

// TestCheckoutWithAgentFromImports tests that checkout step is added when agent file is imported
func TestCheckoutWithAgentFromImports(t *testing.T) {
	t.Run("checkout_added_with_agent", func(t *testing.T) {
		compiler := NewCompiler()
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			AgentFile:   "/path/to/agent.md",
			Permissions: "permissions:\n  contents: read\n",
		}

		shouldCheckout := compiler.shouldAddCheckoutStep(workflowData)
		if !shouldCheckout {
			t.Error("Expected checkout to be added when agent file is specified")
		}
	})

	t.Run("checkout_added_with_agent_no_contents_permission", func(t *testing.T) {
		compiler := NewCompiler()
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			AgentFile:   "/path/to/agent.md",
			Permissions: "permissions:\n  issues: read\n",
		}

		shouldCheckout := compiler.shouldAddCheckoutStep(workflowData)
		if !shouldCheckout {
			t.Error("Expected checkout to be added when agent file is specified, even without contents permission")
		}
	})

	t.Run("checkout_added_without_agent", func(t *testing.T) {
		compiler := NewCompiler()
		// Set action mode to release
		compiler.SetActionMode(ActionModeRelease)

		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			Permissions: "permissions:\n  issues: read\n",
		}

		shouldCheckout := compiler.shouldAddCheckoutStep(workflowData)
		if !shouldCheckout {
			t.Error("Expected checkout to be added (always needed unless already in custom steps)")
		}
	})

	t.Run("checkout_with_custom_steps_containing_checkout", func(t *testing.T) {
		compiler := NewCompiler()
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			AgentFile:   "/path/to/agent.md",
			CustomSteps: "steps:\n  - uses: actions/checkout@v4\n",
		}

		shouldCheckout := compiler.shouldAddCheckoutStep(workflowData)
		if shouldCheckout {
			t.Error("Expected checkout NOT to be added when custom steps already contain checkout, even with agent file")
		}
	})
}

// TestCompilerIncludesAgentFileViaImportPaths verifies that when a non-native engine (Claude)
// is used with an agent file, the agent file path is included in the prompt via the standard
// ImportPaths/runtime-import mechanism (Step 1b in generatePrompt), so that prompt.txt
// already contains the agent file content when the engine reads it.
func TestCompilerIncludesAgentFileViaImportPaths(t *testing.T) {
	agentFilePath := ".github/agents/my-agent.md"

	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, ".github", "workflows", "test.md")
	if err := os.MkdirAll(filepath.Dir(workflowFile), 0o755); err != nil {
		t.Fatalf("Failed to create workflow directory: %v", err)
	}
	if err := os.WriteFile(workflowFile, []byte("# Do the thing\n"), 0o644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Simulate what the orchestrator populates: the agent file is in ImportPaths (no inputs).
	workflowData := &WorkflowData{
		Name: "test-workflow",
		AI:   "claude",
		EngineConfig: &EngineConfig{
			ID: "claude",
		},
		AgentFile: agentFilePath,
		// ImportPaths mirrors what import_bfs.go populates for agent files without inputs.
		ImportPaths: []string{agentFilePath},
	}

	compiler := NewCompiler()
	compiler.markdownPath = workflowFile

	var buf strings.Builder
	compiler.generatePrompt(&buf, workflowData, false, nil)
	generated := buf.String()
	plan := promptRenderPlan(t, generated)

	// The runtime-import macro for the agent file must appear in the generated YAML (exactly once).
	agentImportMacro := "{{#runtime-import " + agentFilePath + "}}"
	count := strings.Count(generated, agentImportMacro)
	if count == 0 {
		t.Errorf("Expected runtime-import macro %q in generated prompt YAML, got:\n%s", agentImportMacro, generated)
	} else if count > 1 {
		t.Errorf("Expected runtime-import macro %q exactly once, but found %d occurrences:\n%s", agentImportMacro, count, generated)
	}

	// The agent file import must appear before the main workflow markdown import.
	mainWorkflowMacro := "{{#author-prompt .github/workflows/test.md}}"
	agentIdx := strings.Index(plan, agentImportMacro)
	mainIdx := strings.Index(plan, mainWorkflowMacro)
	if mainIdx == -1 {
		t.Errorf("Expected main workflow runtime-import macro %q in generated prompt YAML, got:\n%s", mainWorkflowMacro, generated)
	} else if agentIdx > mainIdx {
		t.Errorf("Agent file runtime-import macro must appear before main workflow macro in prompt:\n%s", generated)
	}
}

func TestGeneratePromptFallsBackWhenPromptImportsIsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, ".github", "workflows", "test.md")
	if err := os.MkdirAll(filepath.Dir(workflowFile), 0o755); err != nil {
		t.Fatalf("Failed to create workflow directory: %v", err)
	}
	if err := os.WriteFile(workflowFile, []byte("# Main workflow\n"), 0o644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	workflowData := &WorkflowData{
		Name:             "test-workflow",
		AI:               "claude",
		EngineConfig:     &EngineConfig{ID: "claude"},
		ImportedMarkdown: "Legacy parameterized content.",
		ImportPaths:      []string{"shared/legacy.md"},
		PromptImports:    []parser.PromptImportEntry{},
	}

	compiler := NewCompiler()
	compiler.markdownPath = workflowFile

	var buf strings.Builder
	compiler.generatePrompt(&buf, workflowData, false, nil)
	generated := buf.String()

	if !strings.Contains(generated, "Legacy parameterized content.") {
		t.Fatalf("Expected legacy ImportedMarkdown content to be emitted when PromptImports is empty, got:\n%s", generated)
	}
	if !strings.Contains(generated, "{{#runtime-import shared/legacy.md}}") {
		t.Fatalf("Expected legacy ImportPaths runtime macro to be emitted when PromptImports is empty, got:\n%s", generated)
	}
}

func TestResolveMarkdownArtifactsPreservesNilPromptImports(t *testing.T) {
	tmpDir := t.TempDir()
	workflowPath := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(workflowPath, []byte("# Test\n"), 0o644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	artifacts, err := compiler.resolveMarkdownArtifacts(
		"# Test",
		tmpDir,
		workflowPath,
		&parser.FrontmatterResult{Frontmatter: map[string]any{}},
		&parser.ImportsResult{},
		nil,
	)
	if err != nil {
		t.Fatalf("resolveMarkdownArtifacts returned error: %v", err)
	}

	if artifacts.promptImports != nil {
		t.Fatalf("Expected promptImports to remain nil when source PromptImports is nil, got %#v", artifacts.promptImports)
	}
}
