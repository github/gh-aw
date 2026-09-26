//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestEngineArgsFieldExtraction(t *testing.T) {
	compiler := NewCompiler()

	t.Run("Engine args field extraction with []any", func(t *testing.T) {
		frontmatter := map[string]any{
			"engine": map[string]any{
				"id":   "copilot",
				"args": []any{"--add-dir", "/"},
			},
		}

		_, config, _ := compiler.ExtractEngineConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected config to be non-nil")
		}
		if config.ID != "copilot" {
			t.Errorf("Expected ID 'copilot', got %s", config.ID)
		}
		if len(config.Args) != 2 {
			t.Errorf("Expected 2 args, got %d", len(config.Args))
		}
		if config.Args[0] != "--add-dir" || config.Args[1] != "/" {
			t.Errorf("Expected [--add-dir /], got %v", config.Args)
		}
	})

	t.Run("Engine args field extraction with []string", func(t *testing.T) {
		frontmatter := map[string]any{
			"engine": map[string]any{
				"id":   "copilot",
				"args": []string{"--verbose", "--debug"},
			},
		}

		_, config, _ := compiler.ExtractEngineConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected config to be non-nil")
		}
		if len(config.Args) != 2 {
			t.Errorf("Expected 2 args, got %d", len(config.Args))
		}
		if config.Args[0] != "--verbose" || config.Args[1] != "--debug" {
			t.Errorf("Expected [--verbose --debug], got %v", config.Args)
		}
	})

	t.Run("Engine without args field", func(t *testing.T) {
		frontmatter := map[string]any{
			"engine": map[string]any{
				"id": "copilot",
			},
		}

		_, config, _ := compiler.ExtractEngineConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected config to be non-nil")
		}
		if config.Args != nil {
			t.Errorf("Expected Args to be nil, got %v", config.Args)
		}
	})

	t.Run("Engine args field with single argument", func(t *testing.T) {
		frontmatter := map[string]any{
			"engine": map[string]any{
				"id":   "copilot",
				"args": []any{"--custom-flag"},
			},
		}

		_, config, _ := compiler.ExtractEngineConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected config to be non-nil")
		}
		if len(config.Args) != 1 {
			t.Errorf("Expected 1 arg, got %d", len(config.Args))
		}
		if config.Args[0] != "--custom-flag" {
			t.Errorf("Expected [--custom-flag], got %v", config.Args)
		}
	})

	t.Run("Engine args with complex arguments", func(t *testing.T) {
		frontmatter := map[string]any{
			"engine": map[string]any{
				"id":   "copilot",
				"args": []any{"--add-dir", "/workspace", "--log-level", "debug"},
			},
		}

		_, config, _ := compiler.ExtractEngineConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected config to be non-nil")
		}
		if len(config.Args) != 4 {
			t.Errorf("Expected 4 args, got %d", len(config.Args))
		}
		expected := []string{"--add-dir", "/workspace", "--log-level", "debug"}
		for i, arg := range expected {
			if config.Args[i] != arg {
				t.Errorf("Expected Args[%d] to be %s, got %s", i, arg, config.Args[i])
			}
		}
	})
}

func TestEnginePermissionModeFieldExtraction(t *testing.T) {
	compiler := NewCompiler()

	t.Run("extracts engine permission-mode", func(t *testing.T) {
		frontmatter := map[string]any{
			"engine": map[string]any{
				"id":              "claude",
				"permission-mode": "auto",
			},
		}

		_, config, _ := compiler.ExtractEngineConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected config to be non-nil")
		}
		if config.PermissionMode != "auto" {
			t.Errorf("Expected permission mode auto, got %q", config.PermissionMode)
		}
	})

	t.Run("permission-mode omitted keeps empty default", func(t *testing.T) {
		frontmatter := map[string]any{
			"engine": map[string]any{
				"id": "claude",
			},
		}

		_, config, _ := compiler.ExtractEngineConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected config to be non-nil")
		}
		if config.PermissionMode != "" {
			t.Errorf("Expected empty permission mode, got %q", config.PermissionMode)
		}
	})
}

func TestEngineContextWindowFieldExtraction(t *testing.T) {
	compiler := NewCompiler()

	t.Run("extracts engine context-window", func(t *testing.T) {
		frontmatter := map[string]any{
			"engine": map[string]any{
				"id":             "pi",
				"context-window": 1000000,
			},
		}

		_, config, _ := compiler.ExtractEngineConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected config to be non-nil")
		}
		if config.ContextWindow != 1000000 {
			t.Errorf("Expected context window 1000000, got %d", config.ContextWindow)
		}
	})

	t.Run("extracts inline engine context-window", func(t *testing.T) {
		frontmatter := map[string]any{
			"engine": map[string]any{
				"runtime":        map[string]any{"id": "pi"},
				"context-window": 1000000,
			},
		}

		_, config, _ := compiler.ExtractEngineConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected config to be non-nil")
		}
		if !config.IsInlineDefinition {
			t.Fatal("Expected inline engine definition")
		}
		if config.ContextWindow != 1000000 {
			t.Errorf("Expected context window 1000000, got %d", config.ContextWindow)
		}
	})

	t.Run("context-window omitted keeps zero default", func(t *testing.T) {
		frontmatter := map[string]any{
			"engine": map[string]any{
				"id": "pi",
			},
		}

		_, config, _ := compiler.ExtractEngineConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected config to be non-nil")
		}
		if config.ContextWindow != 0 {
			t.Errorf("Expected zero context window, got %d", config.ContextWindow)
		}
	})
}

func TestEngineExtensionsFieldExtraction(t *testing.T) {
	compiler := NewCompiler()

	t.Run("Extensions field extraction with []any", func(t *testing.T) {
		frontmatter := map[string]any{
			"engine": map[string]any{
				"id":         "pi",
				"extensions": []any{"@pi/web-search", "@pi/file-browser"},
			},
		}

		_, config, _ := compiler.ExtractEngineConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected config to be non-nil")
		}
		if len(config.Extensions) != 2 {
			t.Errorf("Expected 2 extensions, got %d", len(config.Extensions))
		}
		if config.Extensions[0] != "@pi/web-search" || config.Extensions[1] != "@pi/file-browser" {
			t.Errorf("Expected [@pi/web-search @pi/file-browser], got %v", config.Extensions)
		}
	})

	t.Run("Extensions field extraction with []string", func(t *testing.T) {
		frontmatter := map[string]any{
			"engine": map[string]any{
				"id":         "pi",
				"extensions": []string{"@pi/web-search", "@pi/file-browser"},
			},
		}

		_, config, _ := compiler.ExtractEngineConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected config to be non-nil")
		}
		if len(config.Extensions) != 2 {
			t.Errorf("Expected 2 extensions, got %d", len(config.Extensions))
		}
		if config.Extensions[0] != "@pi/web-search" || config.Extensions[1] != "@pi/file-browser" {
			t.Errorf("Expected [@pi/web-search @pi/file-browser], got %v", config.Extensions)
		}
	})

	t.Run("Extensions field not present", func(t *testing.T) {
		frontmatter := map[string]any{
			"engine": map[string]any{
				"id": "pi",
			},
		}

		_, config, _ := compiler.ExtractEngineConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected config to be non-nil")
		}
		if config.Extensions != nil {
			t.Errorf("Expected Extensions to be nil, got %v", config.Extensions)
		}
	})

	t.Run("Extensions field with unexpected type is ignored", func(t *testing.T) {
		frontmatter := map[string]any{
			"engine": map[string]any{
				"id":         "pi",
				"extensions": "not-an-array",
			},
		}

		_, config, _ := compiler.ExtractEngineConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected config to be non-nil")
		}
		if config.Extensions != nil {
			t.Errorf("Expected Extensions to be nil for unexpected type, got %v", config.Extensions)
		}
	})
}

func TestCopilotEngineArgsInjection(t *testing.T) {
	engine := NewCopilotEngine()

	t.Run("Copilot engine injects args before prompt", func(t *testing.T) {
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID:   "copilot",
				Args: []string{"--add-dir", "/"},
			},
			Tools:       make(map[string]any),
			SafeOutputs: nil,
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one step")
		}

		// Find the execution step
		var executionStep GitHubActionStep
		for _, step := range steps {
			stepStr := strings.Join(step, "\n")
			if strings.Contains(stepStr, "Execute GitHub Copilot CLI") {
				executionStep = step
				break
			}
		}

		if executionStep == nil {
			t.Fatal("Expected to find execution step")
		}

		// Convert step to string for easier inspection
		stepStr := strings.Join(executionStep, "\n")

		// Check that args appear in the command
		if !strings.Contains(stepStr, "--add-dir /") {
			t.Errorf("Expected to find '--add-dir /' in step, got:\n%s", stepStr)
		}

		// Check that args come before --prompt
		addDirIdx := strings.Index(stepStr, "--add-dir /")
		promptIdx := strings.Index(stepStr, "--prompt")
		if addDirIdx == -1 || promptIdx == -1 {
			t.Fatal("Could not find both --add-dir and --prompt in step")
		}
		if addDirIdx > promptIdx {
			t.Error("Expected --add-dir to come before --prompt")
		}
	})

	t.Run("Copilot engine without args", func(t *testing.T) {
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			Tools:       make(map[string]any),
			SafeOutputs: nil,
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one step")
		}

		// Find the execution step
		var executionStep GitHubActionStep
		for _, step := range steps {
			stepStr := strings.Join(step, "\n")
			if strings.Contains(stepStr, "Execute GitHub Copilot CLI") {
				executionStep = step
				break
			}
		}

		if executionStep == nil {
			t.Fatal("Expected to find execution step")
		}

		// Should still have the --prompt flag
		stepStr := strings.Join(executionStep, "\n")
		if !strings.Contains(stepStr, "--prompt") {
			t.Errorf("Expected to find '--prompt' in step")
		}
	})

	t.Run("Copilot engine with multiple args", func(t *testing.T) {
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID:   "copilot",
				Args: []string{"--add-dir", "/workspace", "--verbose"},
			},
			Tools:       make(map[string]any),
			SafeOutputs: nil,
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one step")
		}

		// Find the execution step
		var executionStep GitHubActionStep
		for _, step := range steps {
			stepStr := strings.Join(step, "\n")
			if strings.Contains(stepStr, "Execute GitHub Copilot CLI") {
				executionStep = step
				break
			}
		}

		if executionStep == nil {
			t.Fatal("Expected to find execution step")
		}

		stepStr := strings.Join(executionStep, "\n")

		// Check that all args appear in the command
		if !strings.Contains(stepStr, "--add-dir /workspace") {
			t.Errorf("Expected to find '--add-dir /workspace' in step")
		}
		if !strings.Contains(stepStr, "--verbose") {
			t.Errorf("Expected to find '--verbose' in step")
		}
	})
}

func TestClaudeEngineArgsInjection(t *testing.T) {
	engine := NewClaudeEngine()

	t.Run("Claude engine injects args before prompt", func(t *testing.T) {
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID:   "claude",
				Args: []string{"--custom-flag", "value"},
			},
			Tools:       make(map[string]any),
			SafeOutputs: nil,
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one step")
		}

		// Find the execution step
		var executionStep GitHubActionStep
		for _, step := range steps {
			stepStr := strings.Join(step, "\n")
			if strings.Contains(stepStr, "Execute Claude Code CLI") {
				executionStep = step
				break
			}
		}

		if executionStep == nil {
			t.Fatal("Expected to find execution step")
		}

		stepStr := strings.Join(executionStep, "\n")

		// Check that args appear in the command
		if !strings.Contains(stepStr, "--custom-flag value") {
			t.Errorf("Expected to find '--custom-flag value' in step, got:\n%s", stepStr)
		}
	})

	t.Run("Claude engine without args", func(t *testing.T) {
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID: "claude",
			},
			Tools:       make(map[string]any),
			SafeOutputs: nil,
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one step")
		}

		// Find the execution step
		var executionStep GitHubActionStep
		for _, step := range steps {
			stepStr := strings.Join(step, "\n")
			if strings.Contains(stepStr, "Execute Claude Code CLI") {
				executionStep = step
				break
			}
		}

		if executionStep == nil {
			t.Fatal("Expected to find execution step")
		}

		// Verify the workflow compiles successfully
		stepStr := strings.Join(executionStep, "\n")
		if stepStr == "" {
			t.Error("Expected non-empty step")
		}
	})
}

func TestCodexEngineArgsInjection(t *testing.T) {
	engine := NewCodexEngine()

	t.Run("Codex engine injects args before instruction", func(t *testing.T) {
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID:   "codex",
				Args: []string{"--custom-flag", "value"},
			},
			Tools:       make(map[string]any),
			SafeOutputs: nil,
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one step")
		}

		// Find the execution step
		var executionStep GitHubActionStep
		for _, step := range steps {
			stepStr := strings.Join(step, "\n")
			if strings.Contains(stepStr, "Execute Codex CLI") {
				executionStep = step
				break
			}
		}

		if executionStep == nil {
			t.Fatal("Expected to find execution step")
		}

		stepStr := strings.Join(executionStep, "\n")

		// Check that args appear in the command before --prompt-file
		if !strings.Contains(stepStr, "--custom-flag value") {
			t.Errorf("Expected to find '--custom-flag value' in step, got:\n%s", stepStr)
		}

		// Check that args come before --prompt-file (the last positional arg supplied by the harness)
		customFlagIdx := strings.Index(stepStr, "--custom-flag value")
		promptFileIdx := strings.Index(stepStr, "--prompt-file")
		if customFlagIdx == -1 || promptFileIdx == -1 {
			t.Fatal("Could not find both --custom-flag and --prompt-file in step")
		}
		if customFlagIdx > promptFileIdx {
			t.Error("Expected --custom-flag to come before --prompt-file")
		}
	})

	t.Run("Codex engine without args", func(t *testing.T) {
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID: "codex",
			},
			Tools:       make(map[string]any),
			SafeOutputs: nil,
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one step")
		}

		// Find the execution step
		var executionStep GitHubActionStep
		for _, step := range steps {
			stepStr := strings.Join(step, "\n")
			if strings.Contains(stepStr, "Execute Codex CLI") {
				executionStep = step
				break
			}
		}

		if executionStep == nil {
			t.Fatal("Expected to find execution step")
		}

		// Verify the workflow compiles successfully
		stepStr := strings.Join(executionStep, "\n")
		if stepStr == "" {
			t.Error("Expected non-empty step")
		}
	})
}
