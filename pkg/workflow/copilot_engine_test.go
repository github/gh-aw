//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestCopilotEngine(t *testing.T) {
	engine := NewCopilotEngine()

	// Test basic properties
	if engine.GetID() != "copilot" {
		t.Errorf("Expected copilot engine ID, got '%s'", engine.GetID())
	}

	if engine.GetDisplayName() != "GitHub Copilot CLI" {
		t.Errorf("Expected 'GitHub Copilot CLI' display name, got '%s'", engine.GetDisplayName())
	}

	if engine.IsExperimental() {
		t.Error("Expected copilot engine to not be experimental")
	}

	if !engine.SupportsToolsAllowlist() {
		t.Error("Expected copilot engine to support tools allowlist")
	}

	if !engine.SupportsHTTPTransport() {
		t.Error("Expected copilot engine to support HTTP transport")
	}

	if engine.SupportsMaxTurns() {
		t.Error("Expected copilot engine to not support max-turns yet")
	}

	if !engine.SupportsPlugins() {
		t.Error("Expected copilot engine to support plugins")
	}

	// Test declared output files (session files are copied to logs folder)
	outputFiles := engine.GetDeclaredOutputFiles()
	if len(outputFiles) != 1 {
		t.Errorf("Expected 1 declared output file, got %d", len(outputFiles))
	}

	if outputFiles[0] != "/tmp/gh-aw/sandbox/agent/logs/" {
		t.Errorf("Expected declared output file to be logs folder, got %s", outputFiles[0])
	}
}

func TestCopilotEngineDefaultDetectionModel(t *testing.T) {
	engine := NewCopilotEngine()

	// Test that GetDefaultDetectionModel returns the expected constant
	defaultModel := engine.GetDefaultDetectionModel()
	if defaultModel != string(constants.DefaultCopilotDetectionModel) {
		t.Errorf("Expected default detection model '%s', got '%s'", string(constants.DefaultCopilotDetectionModel), defaultModel)
	}

	// Verify the expected value
	if defaultModel != "gpt-5.1-codex-mini" {
		t.Errorf("Expected 'gpt-5.1-codex-mini' as default detection model, got '%s'", defaultModel)
	}
}

func TestOtherEnginesNoDefaultDetectionModel(t *testing.T) {
	// Test that other engines return empty string for GetDefaultDetectionModel
	engines := []CodingAgentEngine{
		NewClaudeEngine(),
		NewCodexEngine(),
		NewCustomEngine(),
	}

	for _, engine := range engines {
		defaultModel := engine.GetDefaultDetectionModel()
		if defaultModel != "" {
			t.Errorf("Expected engine '%s' to return empty default detection model, got '%s'", engine.GetID(), defaultModel)
		}
	}
}

func TestOtherEnginesNoPluginSupport(t *testing.T) {
	// Test that only Copilot engine supports plugins
	engines := []CodingAgentEngine{
		NewClaudeEngine(),
		NewCodexEngine(),
		NewCustomEngine(),
	}

	for _, engine := range engines {
		if engine.SupportsPlugins() {
			t.Errorf("Expected engine '%s' to not support plugins, but it does", engine.GetID())
		}
	}
}

func TestCopilotEngineInstallationSteps(t *testing.T) {
	engine := NewCopilotEngine()

	// Test with no version (firewall feature disabled by default)
	workflowData := &WorkflowData{}
	steps := engine.GetInstallationSteps(workflowData)
	// When firewall is disabled: secret validation + install (no Node.js needed with new installer) = 2 steps
	if len(steps) != 2 {
		t.Errorf("Expected 2 installation steps (secret validation + install), got %d", len(steps))
	}

	// Test with version (firewall feature disabled by default)
	workflowDataWithVersion := &WorkflowData{
		EngineConfig: &EngineConfig{Version: "1.0.0"},
	}
	stepsWithVersion := engine.GetInstallationSteps(workflowDataWithVersion)
	// When firewall is disabled: secret validation + install (no Node.js needed with new installer) = 2 steps
	if len(stepsWithVersion) != 2 {
		t.Errorf("Expected 2 installation steps with version (secret validation + install), got %d", len(stepsWithVersion))
	}
}

func TestCopilotEngineExecutionSteps(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	// GetExecutionSteps only returns the execution step, not Squid logs or cleanup
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step (copilot execution), got %d", len(steps))
	}

	// Check the execution step
	stepContent := strings.Join([]string(steps[0]), "\n")

	if !strings.Contains(stepContent, "name: Execute GitHub Copilot CLI") {
		t.Errorf("Expected step name 'Execute GitHub Copilot CLI' in step content:\n%s", stepContent)
	}

	// When firewall is disabled, should use 'copilot' command (not npx)
	if !strings.Contains(stepContent, "copilot") || !strings.Contains(stepContent, "--add-dir /tmp/ --add-dir /tmp/gh-aw/ --add-dir /tmp/gh-aw/agent/ --log-level all --log-dir") {
		t.Errorf("Expected command to contain 'copilot' and '--add-dir /tmp/ --add-dir /tmp/gh-aw/ --add-dir /tmp/gh-aw/agent/ --log-level all --log-dir' in step content:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "/tmp/gh-aw/test.log") {
		t.Errorf("Expected command to contain log file name in step content:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}") {
		t.Errorf("Expected COPILOT_GITHUB_TOKEN environment variable in step content:\n%s", stepContent)
	}

	// Test that GITHUB_HEAD_REF and GITHUB_REF_NAME are present for branch resolution
	if !strings.Contains(stepContent, "GITHUB_HEAD_REF: ${{ github.head_ref }}") {
		t.Errorf("Expected GITHUB_HEAD_REF environment variable in step content:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "GITHUB_REF_NAME: ${{ github.ref_name }}") {
		t.Errorf("Expected GITHUB_REF_NAME environment variable in step content:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "GITHUB_WORKSPACE: ${{ github.workspace }}") {
		t.Errorf("Expected GITHUB_WORKSPACE environment variable in step content:\n%s", stepContent)
	}

	// Test that GH_AW_SAFE_OUTPUTS is not present when SafeOutputs is nil
	if strings.Contains(stepContent, "GH_AW_SAFE_OUTPUTS") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS to not be present when SafeOutputs is nil")
	}

	// Test that --disable-builtin-mcps flag is present
	if !strings.Contains(stepContent, "--disable-builtin-mcps") {
		t.Errorf("Expected --disable-builtin-mcps flag in command, got:\n%s", stepContent)
	}

	// Test that mkdir commands are present for --add-dir directories
	if !strings.Contains(stepContent, "mkdir -p /tmp/") {
		t.Errorf("Expected 'mkdir -p /tmp/' command in step content:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "mkdir -p /tmp/gh-aw/") {
		t.Errorf("Expected 'mkdir -p /tmp/gh-aw/' command in step content:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "mkdir -p /tmp/gh-aw/agent/") {
		t.Errorf("Expected 'mkdir -p /tmp/gh-aw/agent/' command in step content:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "mkdir -p /tmp/gh-aw/sandbox/agent/logs/") {
		t.Errorf("Expected 'mkdir -p /tmp/gh-aw/sandbox/agent/logs/' command in step content:\n%s", stepContent)
	}
}

func TestCopilotEngineExecutionStepsWithOutput(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name:        "test-workflow",
		SafeOutputs: &SafeOutputsConfig{}, // Non-nil to trigger output handling
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	// GetExecutionSteps only returns the execution step
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step (copilot execution), got %d", len(steps))
	}

	// Check the execution step
	stepContent := strings.Join([]string(steps[0]), "\n")

	// Test that GH_AW_SAFE_OUTPUTS is present when SafeOutputs is not nil
	if !strings.Contains(stepContent, "GH_AW_SAFE_OUTPUTS: ${{ env.GH_AW_SAFE_OUTPUTS }}") {
		t.Errorf("Expected GH_AW_SAFE_OUTPUTS environment variable when SafeOutputs is not nil in step content:\n%s", stepContent)
	}
}

func TestCopilotEngineGetLogParserScript(t *testing.T) {
	engine := NewCopilotEngine()
	script := engine.GetLogParserScriptId()

	if script != "parse_copilot_log" {
		t.Errorf("Expected 'parse_copilot_log', got '%s'", script)
	}
}

func TestCopilotEngineGetLogFileForParsing(t *testing.T) {
	engine := NewCopilotEngine()
	logFile := engine.GetLogFileForParsing()

	expected := "/tmp/gh-aw/sandbox/agent/logs/"
	if logFile != expected {
		t.Errorf("Expected '%s', got '%s'", expected, logFile)
	}
}

func TestCopilotEngineComputeToolArguments(t *testing.T) {
	engine := NewCopilotEngine()

	tests := []struct {
		name        string
		tools       map[string]any
		safeOutputs *SafeOutputsConfig
		expected    []string
	}{
		{
			name:     "empty tools",
			tools:    map[string]any{},
			expected: []string{},
		},
		{
			name: "bash with specific commands",
			tools: map[string]any{
				"bash": []any{"echo", "ls"},
			},
			expected: []string{"--allow-tool", "shell(echo)", "--allow-tool", "shell(ls)"},
		},
		{
			name: "bash with wildcard",
			tools: map[string]any{
				"bash": []any{":*"},
			},
			expected: []string{"--allow-all-tools"},
		},
		{
			name: "bash with nil (all commands allowed)",
			tools: map[string]any{
				"bash": nil,
			},
			expected: []string{"--allow-tool", "shell"},
		},
		{
			name: "edit tool",
			tools: map[string]any{
				"edit": nil,
			},
			expected: []string{"--allow-tool", "write"},
		},
		{
			name:  "safe outputs without write (uses MCP)",
			tools: map[string]any{},
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			expected: []string{"--allow-tool", "safeoutputs"},
		},
		{
			name: "mixed tools",
			tools: map[string]any{
				"bash": []any{"git status", "npm test"},
				"edit": nil,
			},
			expected: []string{"--allow-tool", "shell(git status)", "--allow-tool", "shell(npm test)", "--allow-tool", "write"},
		},
		{
			name: "bash with star wildcard",
			tools: map[string]any{
				"bash": []any{"*"},
			},
			expected: []string{"--allow-all-tools"},
		},
		{
			name: "comprehensive with multiple tools",
			tools: map[string]any{
				"bash": []any{"git status", "npm test"},
				"edit": nil,
			},
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			expected: []string{"--allow-tool", "safeoutputs", "--allow-tool", "shell(git status)", "--allow-tool", "shell(npm test)", "--allow-tool", "write"},
		},
		{
			name:  "safe outputs with safe_outputs config",
			tools: map[string]any{},
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			expected: []string{"--allow-tool", "safeoutputs"},
		},
		{
			name:  "safe outputs with safe jobs",
			tools: map[string]any{},
			safeOutputs: &SafeOutputsConfig{
				Jobs: map[string]*SafeJobConfig{
					"my-job": {Name: "test job"},
				},
			},
			expected: []string{"--allow-tool", "safeoutputs"},
		},
		{
			name:  "safe outputs with both safe_outputs and safe jobs",
			tools: map[string]any{},
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
				Jobs: map[string]*SafeJobConfig{
					"my-job": {Name: "test job"},
				},
			},
			expected: []string{"--allow-tool", "safeoutputs"},
		},
		{
			name: "github tool with allowed tools",
			tools: map[string]any{
				"github": map[string]any{
					"allowed": []any{"get_file_contents", "list_commits"},
				},
			},
			expected: []string{"--allow-tool", "github(get_file_contents)", "--allow-tool", "github(list_commits)"},
		},
		{
			name: "github tool with single allowed tool",
			tools: map[string]any{
				"github": map[string]any{
					"allowed": []any{"add_issue_comment"},
				},
			},
			expected: []string{"--allow-tool", "github(add_issue_comment)"},
		},
		{
			name: "github tool with wildcard",
			tools: map[string]any{
				"github": map[string]any{
					"allowed": []any{"*"},
				},
			},
			expected: []string{"--allow-tool", "github"},
		},
		{
			name: "github tool with wildcard and specific tools",
			tools: map[string]any{
				"github": map[string]any{
					"allowed": []any{"*", "get_file_contents", "list_commits"},
				},
			},
			expected: []string{"--allow-tool", "github", "--allow-tool", "github(get_file_contents)", "--allow-tool", "github(list_commits)"},
		},
		{
			name: "github tool with empty allowed array",
			tools: map[string]any{
				"github": map[string]any{
					"allowed": []any{},
				},
			},
			expected: []string{},
		},
		{
			name: "github tool without allowed field",
			tools: map[string]any{
				"github": map[string]any{},
			},
			expected: []string{"--allow-tool", "github"},
		},
		{
			name: "github tool as nil (no config)",
			tools: map[string]any{
				"github": nil,
			},
			expected: []string{"--allow-tool", "github"},
		},
		{
			name: "github tool with multiple allowed tools sorted",
			tools: map[string]any{
				"github": map[string]any{
					"allowed": []any{"update_issue", "add_issue_comment", "create_issue"},
				},
			},
			expected: []string{"--allow-tool", "github(add_issue_comment)", "--allow-tool", "github(create_issue)", "--allow-tool", "github(update_issue)"},
		},
		{
			name: "github tool with bash and edit tools",
			tools: map[string]any{
				"github": map[string]any{
					"allowed": []any{"get_file_contents", "list_commits"},
				},
				"bash": []any{"echo", "ls"},
				"edit": nil,
			},
			expected: []string{"--allow-tool", "github(get_file_contents)", "--allow-tool", "github(list_commits)", "--allow-tool", "shell(echo)", "--allow-tool", "shell(ls)", "--allow-tool", "write"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.computeCopilotToolArguments(tt.tools, tt.safeOutputs, nil, nil)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d arguments, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			for i, expected := range tt.expected {
				if i >= len(result) || result[i] != expected {
					t.Errorf("Expected argument %d to be '%s', got '%s'", i, expected, result[i])
				}
			}
		})
	}
}

func TestCopilotEngineGenerateToolArgumentsComment(t *testing.T) {
	engine := NewCopilotEngine()

	tests := []struct {
		name        string
		tools       map[string]any
		safeOutputs *SafeOutputsConfig
		indent      string
		expected    string
	}{
		{
			name:     "empty tools",
			tools:    map[string]any{},
			indent:   "  ",
			expected: "",
		},
		{
			name: "bash with commands",
			tools: map[string]any{
				"bash": []any{"echo", "ls"},
			},
			indent:   "        ",
			expected: "        # Copilot CLI tool arguments (sorted):\n        # --allow-tool shell(echo)\n        # --allow-tool shell(ls)\n",
		},
		{
			name: "edit tool",
			tools: map[string]any{
				"edit": nil,
			},
			indent:   "        ",
			expected: "        # Copilot CLI tool arguments (sorted):\n        # --allow-tool write\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.generateCopilotToolArgumentsComment(tt.tools, tt.safeOutputs, nil, nil, tt.indent)

			if result != tt.expected {
				t.Errorf("Expected comment:\n%s\nGot:\n%s", tt.expected, result)
			}
		})
	}
}

func TestCopilotEngineExecutionStepsWithToolArguments(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		Tools: map[string]any{
			"bash": []any{"echo", "git status"},
			"edit": nil,
		},
		ParsedTools: NewTools(map[string]any{
			"bash": []any{"echo", "git status"},
			"edit": nil,
		}),
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	// GetExecutionSteps only returns the execution step
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step (copilot execution), got %d", len(steps))
	}

	// Check the execution step contains tool arguments
	stepContent := strings.Join([]string(steps[0]), "\n")

	// Should contain the tool arguments in the command line
	if !strings.Contains(stepContent, "--allow-tool shell(echo)") {
		t.Errorf("Expected step to contain '--allow-tool shell(echo)' in command:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "--allow-tool shell(git status)") {
		t.Errorf("Expected step to contain '--allow-tool shell(git status)' in command:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "--allow-tool write") {
		t.Errorf("Expected step to contain '--allow-tool write' in command:\n%s", stepContent)
	}

	// Should contain the comment showing the tool arguments
	if !strings.Contains(stepContent, "# Copilot CLI tool arguments (sorted):") {
		t.Errorf("Expected step to contain tool arguments comment:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "# --allow-tool shell(echo)") {
		t.Errorf("Expected step to contain comment for shell(echo):\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "# --allow-tool write") {
		t.Errorf("Expected step to contain comment for write:\n%s", stepContent)
	}

	// Should contain --allow-all-paths for edit tool
	if !strings.Contains(stepContent, "--allow-all-paths") {
		t.Errorf("Expected step to contain '--allow-all-paths' for edit tool:\n%s", stepContent)
	}
}

func TestCopilotEngineEditToolAddsAllowAllPaths(t *testing.T) {
	engine := NewCopilotEngine()

	tests := []struct {
		name       string
		tools      map[string]any
		shouldHave bool
	}{
		{
			name: "edit tool present",
			tools: map[string]any{
				"edit": nil,
			},
			shouldHave: true,
		},
		{
			name: "edit tool with other tools",
			tools: map[string]any{
				"edit": nil,
				"bash": []any{"echo"},
			},
			shouldHave: true,
		},
		{
			name: "no edit tool",
			tools: map[string]any{
				"bash": []any{"echo"},
			},
			shouldHave: false,
		},
		{
			name:       "empty tools",
			tools:      map[string]any{},
			shouldHave: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				Name:        "test-workflow",
				Tools:       tt.tools,
				ParsedTools: NewTools(tt.tools), // Populate ParsedTools from Tools map
			}
			steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

			// GetExecutionSteps only returns the execution step
			if len(steps) != 1 {
				t.Fatalf("Expected 1 step, got %d", len(steps))
			}

			stepContent := strings.Join([]string(steps[0]), "\n")

			// Check for --allow-all-paths flag
			hasAllowAllPaths := strings.Contains(stepContent, "--allow-all-paths")

			if tt.shouldHave && !hasAllowAllPaths {
				t.Errorf("Expected step to contain '--allow-all-paths' when edit tool is present, but it was missing:\n%s", stepContent)
			}

			if !tt.shouldHave && hasAllowAllPaths {
				t.Errorf("Expected step to NOT contain '--allow-all-paths' when edit tool is absent, but it was present:\n%s", stepContent)
			}

			// When edit tool is present, verify it's in the command line
			if tt.shouldHave {
				lines := strings.Split(stepContent, "\n")
				foundInCommand := false
				for _, line := range lines {
					// When firewall is disabled, it uses 'copilot' instead of 'npx'
					if strings.Contains(line, "copilot") && strings.Contains(line, "--allow-all-paths") {
						foundInCommand = true
						break
					}
				}
				if !foundInCommand {
					t.Errorf("Expected '--allow-all-paths' in copilot command line:\n%s", stepContent)
				}
			}
		})
	}
}

func TestCopilotEngineShellEscaping(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		Tools: map[string]any{
			"bash": []any{"git add:*", "git commit:*"},
		},
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	// GetExecutionSteps only returns the execution step
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}

	// Get the full command from the execution step
	stepContent := strings.Join([]string(steps[0]), "\n")

	// Find the line that contains the copilot command
	// When firewall is disabled, it uses 'copilot' instead of 'npx'
	lines := strings.Split(stepContent, "\n")
	var copilotCommand string
	for _, line := range lines {
		if strings.Contains(line, "copilot") && strings.Contains(line, "--allow-tool") {
			copilotCommand = strings.TrimSpace(line)
			break
		}
	}

	if copilotCommand == "" {
		t.Fatalf("Could not find copilot command in step content:\n%s", stepContent)
	}

	// Verify that arguments with special characters are properly quoted
	// This test should fail initially, showing the need for escaping
	t.Logf("Generated command: %s", copilotCommand)

	// The command should contain properly escaped arguments with single quotes
	if !strings.Contains(copilotCommand, "'shell(git add:*)'") {
		t.Errorf("Expected 'shell(git add:*)' to be single-quoted in command: %s", copilotCommand)
	}

	if !strings.Contains(copilotCommand, "'shell(git commit:*)'") {
		t.Errorf("Expected 'shell(git commit:*)' to be single-quoted in command: %s", copilotCommand)
	}
}

func TestCopilotEngineInstructionPromptNotEscaped(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		Tools: map[string]any{
			"bash": []any{"git status"},
		},
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	// GetExecutionSteps only returns the execution step
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}

	// Get the full command from the execution step
	stepContent := strings.Join([]string(steps[0]), "\n")

	// Find the line that contains the copilot command
	// When firewall is disabled, it uses 'copilot' instead of 'npx'
	lines := strings.Split(stepContent, "\n")
	var copilotCommand string
	for _, line := range lines {
		if strings.Contains(line, "copilot") && strings.Contains(line, "--prompt") {
			copilotCommand = strings.TrimSpace(line)
			break
		}
	}

	if copilotCommand == "" {
		t.Fatalf("Could not find copilot command in step content:\n%s", stepContent)
	}

	// The $COPILOT_CLI_INSTRUCTION should NOT be wrapped in additional single quotes
	if strings.Contains(copilotCommand, `'"$COPILOT_CLI_INSTRUCTION"'`) {
		t.Errorf("$COPILOT_CLI_INSTRUCTION should not be wrapped in single quotes: %s", copilotCommand)
	}

	// The $COPILOT_CLI_INSTRUCTION should remain double-quoted for variable expansion
	if !strings.Contains(copilotCommand, `"$COPILOT_CLI_INSTRUCTION"`) {
		t.Errorf("$COPILOT_CLI_INSTRUCTION should remain double-quoted: %s", copilotCommand)
	}
}

func TestCopilotEngineRenderGitHubMCPConfig(t *testing.T) {
	tests := []struct {
		name         string
		githubTool   any
		isLast       bool
		expectedStrs []string
	}{
		{
			name:       "GitHub MCP with default version",
			githubTool: nil,
			isLast:     false,
			expectedStrs: []string{
				`"github": {`,
				`"type": "stdio",`,
				`"container": "ghcr.io/github/github-mcp-server:v0.30.3"`,
				`"env": {`,
				`"GITHUB_PERSONAL_ACCESS_TOKEN": "\${GITHUB_MCP_SERVER_TOKEN}"`,
				`},`,
			},
		},
		{
			name: "GitHub MCP with custom version",
			githubTool: map[string]any{
				"version": "v1.2.3",
			},
			isLast: true,
			expectedStrs: []string{
				`"github": {`,
				`"type": "stdio",`,
				`"container": "ghcr.io/github/github-mcp-server:v1.2.3"`,
				`"env": {`,
				`"GITHUB_PERSONAL_ACCESS_TOKEN": "\${GITHUB_MCP_SERVER_TOKEN}"`,
				`}`,
			},
		},
		{
			name: "GitHub MCP with allowed tools",
			githubTool: map[string]any{
				"allowed": []string{"list_workflows", "get_file_contents"},
			},
			isLast: true,
			expectedStrs: []string{
				`"github": {`,
				`"type": "stdio",`,
				`"container": "ghcr.io/github/github-mcp-server:v0.30.3"`,
				`"env": {`,
				`}`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			workflowData := &WorkflowData{}
			// Use unified renderer instead of direct method call
			renderer := NewMCPConfigRenderer(MCPRendererOptions{
				IncludeCopilotFields: true,
				InlineArgs:           true,
				Format:               "json",
				IsLast:               tt.isLast,
			})
			renderer.RenderGitHubMCP(&yaml, tt.githubTool, workflowData)
			output := yaml.String()

			for _, expected := range tt.expectedStrs {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain '%s', but it didn't.\nFull output:\n%s", expected, output)
				}
			}

			// Verify proper ending based on isLast
			if tt.isLast {
				if !strings.HasSuffix(strings.TrimSpace(output), "}") {
					t.Errorf("Expected output to end with '}' when isLast=true, got:\n%s", output)
				}
			} else {
				if !strings.HasSuffix(strings.TrimSpace(output), "},") {
					t.Errorf("Expected output to end with '},' when isLast=false, got:\n%s", output)
				}
			}
		})
	}
}

func TestCopilotEngineGitHubToolsShellEscaping(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		Tools: map[string]any{
			"github": map[string]any{
				"allowed": []any{"add_issue_comment", "issue_read"},
			},
		},
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	// GetExecutionSteps only returns the execution step
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}

	// Get the full command from the execution step
	stepContent := strings.Join([]string(steps[0]), "\n")

	// Find the line that contains the copilot command
	// When firewall is disabled, it uses 'copilot' instead of 'npx'
	lines := strings.Split(stepContent, "\n")
	var copilotCommand string
	for _, line := range lines {
		if strings.Contains(line, "copilot") && strings.Contains(line, "--allow-tool") {
			copilotCommand = strings.TrimSpace(line)
			break
		}
	}

	if copilotCommand == "" {
		t.Fatalf("Could not find copilot command in step content:\n%s", stepContent)
	}

	// Verify that GitHub tool arguments are properly single-quoted
	t.Logf("Generated command: %s", copilotCommand)

	// The command should contain properly escaped GitHub tool arguments with single quotes
	if !strings.Contains(copilotCommand, "'github(add_issue_comment)'") {
		t.Errorf("Expected 'github(add_issue_comment)' to be single-quoted in command: %s", copilotCommand)
	}

	if !strings.Contains(copilotCommand, "'github(issue_read)'") {
		t.Errorf("Expected 'github(issue_read)' to be single-quoted in command: %s", copilotCommand)
	}
}

func TestCopilotEngineLogParsingUsesCorrectLogFile(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "copilot-log-parsing-test")

	// Create a test workflow with Copilot engine
	testContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
tools:
  github:
    allowed: [list_issues]
---

# Test Copilot Log Parsing

This workflow tests that Copilot log parsing uses the correct log file path.
`

	testFile := filepath.Join(tmpDir, "test-copilot-log-parsing.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Verify that the log parsing step uses /tmp/gh-aw/sandbox/agent/logs/ instead of agent-stdio.log
	if !strings.Contains(lockStr, "GH_AW_AGENT_OUTPUT: /tmp/gh-aw/sandbox/agent/logs/") {
		t.Error("Expected GH_AW_AGENT_OUTPUT to be set to '/tmp/gh-aw/sandbox/agent/logs/' for Copilot engine")
	}

	// Verify that it's NOT using the agent-stdio.log path for parsing
	if strings.Contains(lockStr, "GH_AW_AGENT_OUTPUT: /tmp/gh-aw/agent-stdio.log") {
		t.Error("Expected GH_AW_AGENT_OUTPUT to NOT use '/tmp/gh-aw/agent-stdio.log' for Copilot engine")
	}

	t.Log("Successfully verified that Copilot log parsing uses /tmp/gh-aw/sandbox/agent/logs/")
}

func TestExtractAddDirPaths(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "empty args",
			args:     []string{},
			expected: []string{},
		},
		{
			name:     "no add-dir flags",
			args:     []string{"--log-level", "debug", "--model", "gpt-4"},
			expected: []string{},
		},
		{
			name:     "single add-dir",
			args:     []string{"--add-dir", "/tmp/"},
			expected: []string{"/tmp/"},
		},
		{
			name:     "multiple add-dir flags",
			args:     []string{"--add-dir", "/tmp/", "--log-level", "debug", "--add-dir", "/tmp/gh-aw/"},
			expected: []string{"/tmp/", "/tmp/gh-aw/"},
		},
		{
			name:     "add-dir at end of args",
			args:     []string{"--log-level", "debug", "--add-dir", "/tmp/gh-aw/agent/"},
			expected: []string{"/tmp/gh-aw/agent/"},
		},
		{
			name:     "all default copilot args",
			args:     []string{"--add-dir", "/tmp/", "--add-dir", "/tmp/gh-aw/", "--add-dir", "/tmp/gh-aw/agent/", "--log-level", "all", "--log-dir", "/tmp/gh-aw/sandbox/agent/logs/"},
			expected: []string{"/tmp/", "/tmp/gh-aw/", "/tmp/gh-aw/agent/"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractAddDirPaths(tt.args)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d paths, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			for i, expected := range tt.expected {
				if i >= len(result) || result[i] != expected {
					t.Errorf("Expected path %d to be '%s', got '%s'", i, expected, result[i])
				}
			}
		})
	}
}

func TestCopilotEngineExecutionStepsWithCacheMemory(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		CacheMemoryConfig: &CacheMemoryConfig{
			Caches: []CacheMemoryEntry{
				{ID: "default"},
				{ID: "session"},
				{ID: "logs"},
			},
		},
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	// GetExecutionSteps only returns the execution step
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	// Test that mkdir commands are present for cache-memory directories
	if !strings.Contains(stepContent, "mkdir -p /tmp/gh-aw/cache-memory/") {
		t.Errorf("Expected 'mkdir -p /tmp/gh-aw/cache-memory/' command for default cache in step content:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "mkdir -p /tmp/gh-aw/cache-memory-session/") {
		t.Errorf("Expected 'mkdir -p /tmp/gh-aw/cache-memory-session/' command for session cache in step content:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "mkdir -p /tmp/gh-aw/cache-memory-logs/") {
		t.Errorf("Expected 'mkdir -p /tmp/gh-aw/cache-memory-logs/' command for logs cache in step content:\n%s", stepContent)
	}

	// Verify --add-dir flags are present for cache directories
	if !strings.Contains(stepContent, "--add-dir /tmp/gh-aw/cache-memory/") {
		t.Errorf("Expected '--add-dir /tmp/gh-aw/cache-memory/' in copilot args")
	}
	if !strings.Contains(stepContent, "--add-dir /tmp/gh-aw/cache-memory-session/") {
		t.Errorf("Expected '--add-dir /tmp/gh-aw/cache-memory-session/' in copilot args")
	}
	if !strings.Contains(stepContent, "--add-dir /tmp/gh-aw/cache-memory-logs/") {
		t.Errorf("Expected '--add-dir /tmp/gh-aw/cache-memory-logs/' in copilot args")
	}
}

func TestCopilotEngineExecutionStepsWithCustomAddDirArgs(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			Args: []string{"--add-dir", "/custom/path/", "--verbose"},
		},
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	// GetExecutionSteps only returns the execution step
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	// Test that mkdir commands are present for custom --add-dir path
	if !strings.Contains(stepContent, "mkdir -p /custom/path/") {
		t.Errorf("Expected 'mkdir -p /custom/path/' command for custom add-dir arg in step content:\n%s", stepContent)
	}

	// Verify the custom --add-dir flag is still present in copilot args
	if !strings.Contains(stepContent, "--add-dir /custom/path/") {
		t.Errorf("Expected '--add-dir /custom/path/' in copilot args")
	}
}

func TestCopilotEngineParseLogMetrics_MultilineJSON(t *testing.T) {
	engine := NewCopilotEngine()

	// Read the test data file with multi-line JSON format
	testDataPath := filepath.Join("test_data", "copilot_debug_log.txt")
	logContent, err := os.ReadFile(testDataPath)
	if err != nil {
		t.Fatalf("Failed to read test data file: %v", err)
	}

	// Parse the log metrics
	metrics := engine.ParseLogMetrics(string(logContent), false)

	// Verify token usage is extracted from multi-line JSON blocks
	// Expected: 1524 + 89 + 1689 + 23 = 3325 tokens total
	expectedTokens := 3325
	if metrics.TokenUsage != expectedTokens {
		t.Errorf("Expected token usage %d, got %d", expectedTokens, metrics.TokenUsage)
	}

	// Verify the parser handles multiple JSON response blocks
	// Log contains 2 responses with usage information
	if metrics.TokenUsage == 0 {
		t.Error("Token usage should not be zero when parsing Copilot debug logs with usage data")
	}
}

func TestCopilotEngineParseLogMetrics_SingleLineJSON(t *testing.T) {
	engine := NewCopilotEngine()

	// Test with single-line JSON wrapped in debug log format (realistic format)
	// This tests backward compatibility with compact JSON logging
	singleLineLog := `2025-09-26T11:13:17.989Z [DEBUG] data:
2025-09-26T11:13:17.990Z [DEBUG] {"usage": {"prompt_tokens": 100, "completion_tokens": 50}}
2025-09-26T11:13:17.990Z [DEBUG] Workflow completed`
	metrics := engine.ParseLogMetrics(singleLineLog, false)

	// Should extract tokens from single-line JSON in data block
	expectedTokens := 150
	if metrics.TokenUsage != expectedTokens {
		t.Errorf("Expected token usage %d from single-line JSON, got %d", expectedTokens, metrics.TokenUsage)
	}
}

func TestCopilotEngineParseLogMetrics_NoTokenData(t *testing.T) {
	engine := NewCopilotEngine()

	// Test with log content that has no token data
	noTokenLog := `2025-09-26T11:13:11.798Z [DEBUG] Using model: claude-sonnet-4
2025-09-26T11:13:12.575Z [DEBUG] Starting workflow
2025-09-26T11:13:18.502Z [DEBUG] Workflow completed`

	metrics := engine.ParseLogMetrics(noTokenLog, false)

	// Token usage should be 0 when no usage data is present
	if metrics.TokenUsage != 0 {
		t.Errorf("Expected token usage 0 when no usage data present, got %d", metrics.TokenUsage)
	}
}

func TestCopilotEngineExtractToolSizes(t *testing.T) {
	engine := NewCopilotEngine()

	tests := []struct {
		name          string
		jsonStr       string
		expectedTools map[string]struct{ inputSize, outputSize int }
		expectError   bool
	}{
		{
			name: "tool call with arguments",
			jsonStr: `{
				"choices": [{
					"message": {
						"role": "assistant",
						"tool_calls": [{
							"id": "call_abc123",
							"type": "function",
							"function": {
								"name": "bash",
								"arguments": "{\"command\":\"echo 'test'\",\"description\":\"Test command\"}"
							}
						}]
					}
				}]
			}`,
			expectedTools: map[string]struct{ inputSize, outputSize int }{
				"bash": {inputSize: 54, outputSize: 0},
			},
		},
		{
			name: "multiple tool calls",
			jsonStr: `{
				"choices": [{
					"message": {
						"tool_calls": [{
							"function": {
								"name": "github",
								"arguments": "{\"owner\":\"githubnext\",\"repo\":\"gh-aw\"}"
							}
						}, {
							"function": {
								"name": "playwright",
								"arguments": "{\"url\":\"https://example.com\",\"action\":\"screenshot\"}"
							}
						}]
					}
				}]
			}`,
			expectedTools: map[string]struct{ inputSize, outputSize int }{
				"github":     {inputSize: 37, outputSize: 0},
				"playwright": {inputSize: 51, outputSize: 0},
			},
		},
		{
			name: "tool call without arguments",
			jsonStr: `{
				"choices": [{
					"message": {
						"tool_calls": [{
							"function": {
								"name": "bash"
							}
						}]
					}
				}]
			}`,
			expectedTools: map[string]struct{ inputSize, outputSize int }{
				"bash": {inputSize: 0, outputSize: 0},
			},
		},
		{
			name: "empty tool_calls array",
			jsonStr: `{
				"choices": [{
					"message": {
						"tool_calls": []
					}
				}]
			}`,
			expectedTools: map[string]struct{ inputSize, outputSize int }{},
		},
		{
			name:          "invalid JSON",
			jsonStr:       `{invalid json}`,
			expectedTools: map[string]struct{ inputSize, outputSize int }{},
			expectError:   true,
		},
		{
			name: "tool call in alternative message format",
			jsonStr: `{
				"message": {
					"tool_calls": [{
						"function": {
							"name": "edit",
							"arguments": "{\"path\":\"/test/file.txt\",\"content\":\"test content\"}"
						}
					}]
				}
			}`,
			expectedTools: map[string]struct{ inputSize, outputSize int }{
				"edit": {inputSize: 50, outputSize: 0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolCallMap := make(map[string]*ToolCallInfo)
			engine.extractToolCallSizes(tt.jsonStr, toolCallMap, true)

			// Verify tool count
			if len(toolCallMap) != len(tt.expectedTools) {
				t.Errorf("Expected %d tools, got %d: %v", len(tt.expectedTools), len(toolCallMap), toolCallMap)
			}

			// Verify each tool's sizes
			for toolName, expectedSizes := range tt.expectedTools {
				toolInfo, exists := toolCallMap[toolName]
				if !exists {
					t.Errorf("Expected tool '%s' not found in tool call map", toolName)
					continue
				}

				if toolInfo.MaxInputSize != expectedSizes.inputSize {
					t.Errorf("Tool '%s': expected MaxInputSize %d, got %d",
						toolName, expectedSizes.inputSize, toolInfo.MaxInputSize)
				}

				if toolInfo.MaxOutputSize != expectedSizes.outputSize {
					t.Errorf("Tool '%s': expected MaxOutputSize %d, got %d",
						toolName, expectedSizes.outputSize, toolInfo.MaxOutputSize)
				}
			}
		})
	}
}

func TestCopilotEngineExtractToolSizes_MaxTracking(t *testing.T) {
	engine := NewCopilotEngine()
	toolCallMap := make(map[string]*ToolCallInfo)

	// First call with smaller arguments
	json1 := `{
		"choices": [{
			"message": {
				"tool_calls": [{
					"function": {
						"name": "bash",
						"arguments": "{\"cmd\":\"ls\"}"
					}
				}]
			}
		}]
	}`
	engine.extractToolCallSizes(json1, toolCallMap, false)

	// Second call with larger arguments
	json2 := `{
		"choices": [{
			"message": {
				"tool_calls": [{
					"function": {
						"name": "bash",
						"arguments": "{\"command\":\"echo 'This is a much longer command with more content'\"}"
					}
				}]
			}
		}]
	}`
	engine.extractToolCallSizes(json2, toolCallMap, false)

	// Third call with smaller arguments again
	json3 := `{
		"choices": [{
			"message": {
				"tool_calls": [{
					"function": {
						"name": "bash",
						"arguments": "{\"cmd\":\"pwd\"}"
					}
				}]
			}
		}]
	}`
	engine.extractToolCallSizes(json3, toolCallMap, false)

	// Verify that MaxInputSize tracks the maximum
	bashInfo, exists := toolCallMap["bash"]
	if !exists {
		t.Fatal("bash tool not found in tool call map")
	}

	// Should have tracked the largest input size (from json2)
	expectedMaxInput := len("{\"command\":\"echo 'This is a much longer command with more content'\"}")
	if bashInfo.MaxInputSize != expectedMaxInput {
		t.Errorf("Expected MaxInputSize %d (from largest call), got %d", expectedMaxInput, bashInfo.MaxInputSize)
	}

	// Call count should be 3
	if bashInfo.CallCount != 3 {
		t.Errorf("Expected CallCount 3, got %d", bashInfo.CallCount)
	}
}

func TestCopilotEngineParseLogMetrics_WithToolSizes(t *testing.T) {
	engine := NewCopilotEngine()

	// Log with tool calls containing size information
	logWithTools := `2025-09-26T11:13:17.989Z [DEBUG] response (Request-ID 00000-4ceedfde):
2025-09-26T11:13:17.989Z [DEBUG] data:
2025-09-26T11:13:17.990Z [DEBUG] {
2025-09-26T11:13:17.990Z [DEBUG]   "choices": [
2025-09-26T11:13:17.990Z [DEBUG]     {
2025-09-26T11:13:17.990Z [DEBUG]       "message": {
2025-09-26T11:13:17.990Z [DEBUG]         "tool_calls": [
2025-09-26T11:13:17.990Z [DEBUG]           {
2025-09-26T11:13:17.990Z [DEBUG]             "function": {
2025-09-26T11:13:17.990Z [DEBUG]               "name": "github",
2025-09-26T11:13:17.990Z [DEBUG]               "arguments": "{\"owner\":\"githubnext\",\"repo\":\"gh-aw\",\"method\":\"list_issues\"}"
2025-09-26T11:13:17.990Z [DEBUG]             }
2025-09-26T11:13:17.990Z [DEBUG]           }
2025-09-26T11:13:17.990Z [DEBUG]         ]
2025-09-26T11:13:17.990Z [DEBUG]       }
2025-09-26T11:13:17.990Z [DEBUG]     }
2025-09-26T11:13:17.990Z [DEBUG]   ],
2025-09-26T11:13:17.990Z [DEBUG]   "usage": {
2025-09-26T11:13:17.990Z [DEBUG]     "prompt_tokens": 100,
2025-09-26T11:13:17.990Z [DEBUG]     "completion_tokens": 50
2025-09-26T11:13:17.990Z [DEBUG]   }
2025-09-26T11:13:17.990Z [DEBUG] }
2025-09-26T11:13:18.000Z [DEBUG] Executing tool: github`

	metrics := engine.ParseLogMetrics(logWithTools, false)

	// Verify token usage
	if metrics.TokenUsage != 150 {
		t.Errorf("Expected token usage 150, got %d", metrics.TokenUsage)
	}

	// Verify tool info was extracted
	if len(metrics.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(metrics.ToolCalls))
	}

	githubTool := metrics.ToolCalls[0]
	if githubTool.Name != "github" {
		t.Errorf("Expected tool name 'github', got '%s'", githubTool.Name)
	}

	// Verify input size was extracted
	expectedInputSize := len("{\"owner\":\"githubnext\",\"repo\":\"gh-aw\",\"method\":\"list_issues\"}")
	if githubTool.MaxInputSize != expectedInputSize {
		t.Errorf("Expected MaxInputSize %d, got %d", expectedInputSize, githubTool.MaxInputSize)
	}

	// Output size should be 0 (not extracted from current log format)
	if githubTool.MaxOutputSize != 0 {
		t.Errorf("Expected MaxOutputSize 0, got %d", githubTool.MaxOutputSize)
	}
}

func TestCopilotEngineSkipInstallationWithCommand(t *testing.T) {
	engine := NewCopilotEngine()

	// Test with custom command - should skip installation
	workflowData := &WorkflowData{
		EngineConfig: &EngineConfig{Command: "/usr/local/bin/custom-copilot"},
	}
	steps := engine.GetInstallationSteps(workflowData)

	if len(steps) != 0 {
		t.Errorf("Expected 0 installation steps when command is specified, got %d", len(steps))
	}
}

func TestCopilotEnginePluginDiscoveryInSandboxMode(t *testing.T) {
	engine := NewCopilotEngine()

	tests := []struct {
		name                    string
		plugins                 []string
		networkPermissions      *NetworkPermissions
		shouldIncludeCopilotDir bool
	}{
		{
			name:    "plugins with firewall enabled",
			plugins: []string{"github/auto-agentics"},
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"api.github.com"},
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
			shouldIncludeCopilotDir: true,
		},
		{
			name:    "multiple plugins with firewall enabled",
			plugins: []string{"github/auto-agentics", "github/test-plugin"},
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"api.github.com"},
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
			shouldIncludeCopilotDir: true,
		},
		{
			name:    "no plugins with firewall enabled",
			plugins: []string{},
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"api.github.com"},
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
			shouldIncludeCopilotDir: false,
		},
		{
			name:                    "plugins without firewall (non-sandbox mode)",
			plugins:                 []string{"github/auto-agentics"},
			networkPermissions:      nil, // No network permissions = firewall disabled
			shouldIncludeCopilotDir: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				Name: "test-workflow",
				PluginInfo: &PluginInfo{
					Plugins: tt.plugins,
				},
				NetworkPermissions: tt.networkPermissions,
			}
			steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

			// GetExecutionSteps only returns the execution step
			if len(steps) != 1 {
				t.Fatalf("Expected 1 step, got %d", len(steps))
			}

			stepContent := strings.Join([]string(steps[0]), "\n")

			// Check for --add-dir /home/runner/.copilot/ in the copilot command
			hasCopilotDir := strings.Contains(stepContent, "--add-dir /home/runner/.copilot/")

			if tt.shouldIncludeCopilotDir && !hasCopilotDir {
				t.Errorf("Expected step to contain '--add-dir /home/runner/.copilot/' when plugins are declared in sandbox mode, but it was missing:\n%s", stepContent)
			}

			if !tt.shouldIncludeCopilotDir && hasCopilotDir {
				t.Errorf("Expected step to NOT contain '--add-dir /home/runner/.copilot/' when conditions not met, but it was present:\n%s", stepContent)
			}

			// When plugins are declared in sandbox mode, verify the directory is added after workspace
			if tt.shouldIncludeCopilotDir {
				// Check that both workspace and copilot directories are present
				if !strings.Contains(stepContent, "--add-dir \"${GITHUB_WORKSPACE}\"") {
					t.Errorf("Expected workspace directory in --add-dir:\n%s", stepContent)
				}

				// Verify the ordering - copilot dir should appear after workspace dir
				workspaceIdx := strings.Index(stepContent, "--add-dir \"${GITHUB_WORKSPACE}\"")
				copilotIdx := strings.Index(stepContent, "--add-dir /home/runner/.copilot/")
				if copilotIdx <= workspaceIdx {
					t.Errorf("Expected copilot directory to appear after workspace directory in --add-dir flags")
				}
			}
		})
	}
}

func TestCopilotEnginePluginDiscoveryWithSRT(t *testing.T) {
	engine := NewCopilotEngine()

	// Test with SRT enabled (via sandbox config)
	workflowData := &WorkflowData{
		Name: "test-workflow",
		PluginInfo: &PluginInfo{
			Plugins: []string{"github/auto-agentics"},
		},
		SandboxConfig: &SandboxConfig{
			Type: "sandbox-runtime",
		},
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	// GetExecutionSteps only returns the execution step
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	// Should include --add-dir /home/runner/.copilot/ when SRT is enabled with plugins
	if !strings.Contains(stepContent, "--add-dir /home/runner/.copilot/") {
		t.Errorf("Expected step to contain '--add-dir /home/runner/.copilot/' when plugins are declared with SRT enabled, but it was missing:\n%s", stepContent)
	}
}
