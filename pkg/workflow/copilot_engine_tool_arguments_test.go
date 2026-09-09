//go:build !integration

package workflow

import (
	"testing"
)

func TestCopilotEngineComputeToolArguments(t *testing.T) {
	engine := NewCopilotEngine()

	tests := []struct {
		name         string
		tools        map[string]any
		safeOutputs  *SafeOutputsConfig
		mcpScripts   *MCPScriptsConfig
		workflowData *WorkflowData
		expected     []string
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
			// safeoutputs is always CLI-mounted when safe-outputs is configured, so
			// shell(safeoutputs:*) is also added to the restricted bash allowlist.
			expected: []string{"--allow-tool", "safeoutputs", "--allow-tool", "shell(git status)", "--allow-tool", "shell(npm test)", "--allow-tool", "shell(safeoutputs:*)", "--allow-tool", "write"},
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
			name: "web-fetch tool",
			tools: map[string]any{
				"web-fetch": nil,
			},
			expected: []string{"--allow-tool", "web_fetch"},
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
		// Stem command tests - commands that Copilot CLI matches with subcommands
		{
			name: "stem command gets wildcard suffix",
			tools: map[string]any{
				"bash": []any{"dotnet"},
			},
			expected: []string{"--allow-tool", "shell(dotnet:*)"},
		},
		{
			name: "multiple stem commands get wildcard suffix",
			tools: map[string]any{
				"bash": []any{"cargo", "go", "npm"},
			},
			expected: []string{"--allow-tool", "shell(cargo:*)", "--allow-tool", "shell(go:*)", "--allow-tool", "shell(npm:*)"},
		},
		{
			name: "stem command with space does not get wildcard",
			tools: map[string]any{
				"bash": []any{"dotnet build"},
			},
			expected: []string{"--allow-tool", "shell(dotnet build)"},
		},
		{
			name: "stem command with explicit colon does not get wildcard",
			tools: map[string]any{
				"bash": []any{"git:checkout"},
			},
			expected: []string{"--allow-tool", "shell(git:checkout)"},
		},
		{
			name: "non-stem command does not get wildcard",
			tools: map[string]any{
				"bash": []any{"echo", "ls"},
			},
			expected: []string{"--allow-tool", "shell(echo)", "--allow-tool", "shell(ls)"},
		},
		{
			name: "curl and wget get wildcard as stem commands",
			tools: map[string]any{
				"bash": []any{"curl", "wget"},
			},
			expected: []string{"--allow-tool", "shell(curl:*)", "--allow-tool", "shell(wget:*)"},
		},
		{
			name: "mixed stem and non-stem commands",
			tools: map[string]any{
				"bash": []any{"dotnet", "echo", "npm", "curl", "git status"},
			},
			expected: []string{"--allow-tool", "shell(curl:*)", "--allow-tool", "shell(dotnet:*)", "--allow-tool", "shell(echo)", "--allow-tool", "shell(git status)", "--allow-tool", "shell(npm:*)"},
		},
		{
			name: "all stem commands get wildcard",
			tools: map[string]any{
				"bash": []any{"git", "gh", "npm", "yarn", "cargo", "go", "pip", "dotnet", "flutter"},
			},
			expected: []string{
				"--allow-tool", "shell(cargo:*)",
				"--allow-tool", "shell(dotnet:*)",
				"--allow-tool", "shell(flutter:*)",
				"--allow-tool", "shell(gh:*)",
				"--allow-tool", "shell(git:*)",
				"--allow-tool", "shell(go:*)",
				"--allow-tool", "shell(npm:*)",
				"--allow-tool", "shell(pip:*)",
				"--allow-tool", "shell(yarn:*)",
			},
		},
		{
			name: "stem command with existing :* wildcard passes through",
			tools: map[string]any{
				"bash": []any{"git:*"},
			},
			expected: []string{"--allow-tool", "shell(git:*)"},
		},
		{
			name: "cli-proxy with restricted bash allows safeoutputs cli",
			tools: map[string]any{
				"bash": []any{"echo"},
			},
			safeOutputs: &SafeOutputsConfig{
				NoOp: &NoOpConfig{},
			},
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					NoOp: &NoOpConfig{},
				},
				ParsedTools: &Tools{
					CLIProxy: true,
				},
			},
			expected: []string{"--allow-tool", "safeoutputs", "--allow-tool", "shell(echo)", "--allow-tool", "shell(safeoutputs:*)"},
		},
		{
			name: "cli-proxy with restricted bash allows mcpscripts cli",
			tools: map[string]any{
				"bash": []any{"python3 *"},
			},
			mcpScripts: &MCPScriptsConfig{
				Tools: map[string]*MCPScriptToolConfig{
					"query": {Name: "query", Description: "test", Script: "return {};"},
				},
			},
			workflowData: &WorkflowData{
				MCPScripts: &MCPScriptsConfig{
					Tools: map[string]*MCPScriptToolConfig{
						"query": {Name: "query", Description: "test", Script: "return {};"},
					},
				},
				ParsedTools: &Tools{
					CLIProxy: true,
				},
			},
			expected: []string{"--allow-tool", "mcpscripts", "--allow-tool", "shell(mcpscripts:*)", "--allow-tool", "shell(python3)"},
		},
		{
			name: "cli-proxy with restricted bash allows all mounted mcp clis",
			tools: map[string]any{
				"bash":       []any{"echo"},
				"playwright": true,
				"mymcp": map[string]any{
					"command": "npx",
					"args":    []any{"-y", "@acme/mcp-server"},
				},
			},
			safeOutputs: &SafeOutputsConfig{
				NoOp: &NoOpConfig{},
			},
			mcpScripts: &MCPScriptsConfig{
				Tools: map[string]*MCPScriptToolConfig{
					"query": {Name: "query", Description: "test", Script: "return {};"},
				},
			},
			workflowData: &WorkflowData{
				ParsedTools: &Tools{
					CLIProxy: true,
				},
			},
			expected: []string{
				"--allow-tool", "mcpscripts",
				"--allow-tool", "mymcp",
				"--allow-tool", "safeoutputs",
				"--allow-tool", "shell(echo)",
				"--allow-tool", "shell(mcpscripts:*)",
				"--allow-tool", "shell(mymcp:*)",
				"--allow-tool", "shell(playwright-cli:*)",
				"--allow-tool", "shell(safeoutputs:*)",
			},
		},
		{
			name: "cli-proxy with nil workflow data still allows mounted mcp clis",
			tools: map[string]any{
				"bash":           []any{"echo"},
				"cli-proxy":      true,
				"playwright":     true,
				"custom-mcp-cli": map[string]any{"command": "npx", "args": []any{"-y", "@acme/custom-mcp"}},
			},
			safeOutputs: &SafeOutputsConfig{
				NoOp: &NoOpConfig{},
			},
			expected: []string{
				"--allow-tool", "custom-mcp-cli",
				"--allow-tool", "safeoutputs",
				"--allow-tool", "shell(custom-mcp-cli:*)",
				"--allow-tool", "shell(echo)",
				"--allow-tool", "shell(playwright-cli:*)",
				"--allow-tool", "shell(safeoutputs:*)",
			},
		},
		{
			name: "github gh-proxy with restricted bash allows gh cli",
			tools: map[string]any{
				"bash": []any{"echo"},
				"github": map[string]any{
					"mode": "gh-proxy",
				},
			},
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"bash": []any{"echo"},
					"github": map[string]any{
						"mode": "gh-proxy",
					},
				},
			},
			expected: []string{"--allow-tool", "github", "--allow-tool", "shell(echo)", "--allow-tool", "shell(gh:*)"},
		},
		// Playwright CLI mode tests - playwright-cli must be auto-allowed when bash is restricted.
		{
			name: "playwright cli mode with restricted bash auto-allows playwright-cli",
			tools: map[string]any{
				"bash": []any{"echo"},
				"playwright": map[string]any{
					"mode": "cli",
				},
			},
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"bash": []any{"echo"},
					"playwright": map[string]any{
						"mode": "cli",
					},
				},
			},
			expected: []string{"--allow-tool", "shell(echo)", "--allow-tool", "shell(playwright-cli:*)"},
		},
		{
			name: "playwright cli mode with unrestricted bash does not add playwright-cli",
			tools: map[string]any{
				"bash": nil,
				"playwright": map[string]any{
					"mode": "cli",
				},
			},
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"bash": nil,
					"playwright": map[string]any{
						"mode": "cli",
					},
				},
			},
			expected: []string{"--allow-tool", "shell"},
		},
		{
			name: "playwright cli mode with wildcard bash does not add playwright-cli",
			tools: map[string]any{
				"bash": []any{"*"},
				"playwright": map[string]any{
					"mode": "cli",
				},
			},
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"bash": []any{"*"},
					"playwright": map[string]any{
						"mode": "cli",
					},
				},
			},
			expected: []string{"--allow-all-tools"},
		},
		{
			name: "playwright default mode with restricted bash auto-allows playwright-cli",
			tools: map[string]any{
				"bash":       []any{"echo"},
				"playwright": true,
			},
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"bash":       []any{"echo"},
					"playwright": true,
				},
			},
			expected: []string{"--allow-tool", "shell(echo)", "--allow-tool", "shell(playwright-cli:*)"},
		},
		// Single-quote sanitization tests - commands with single quotes are truncated
		// to safe prefixes to avoid Copilot CLI startup crashes.
		{
			name: "bash tool with single-quoted jq filter is truncated to prefix",
			tools: map[string]any{
				"bash": []any{"jq '.data[] | {id, billing}' /tmp/file.json"},
			},
			expected: []string{"--allow-tool", "shell(jq)"},
		},
		{
			name: "bash tool with single-quoted filter and leading space trimmed",
			tools: map[string]any{
				"bash": []any{"jq '[.data[] | keys] | add | unique' /tmp/file.json"},
			},
			expected: []string{"--allow-tool", "shell(jq)"},
		},
		{
			name: "bash tool without single quotes passes through unchanged",
			tools: map[string]any{
				"bash": []any{"jq . /tmp/file.json"},
			},
			expected: []string{"--allow-tool", "shell(jq . /tmp/file.json)"},
		},
		{
			name: "multiple bash tools: single-quoted ones truncated, others unchanged",
			tools: map[string]any{
				"bash": []any{
					"jq '.data[]' /tmp/file.json",
					"jq . /tmp/other.json",
					"cat /tmp/file.json",
				},
			},
			expected: []string{
				"--allow-tool", "shell(cat /tmp/file.json)",
				// shell(jq . ...) sorts before shell(jq) because ' ' (32) < ')' (41)
				"--allow-tool", "shell(jq . /tmp/other.json)",
				"--allow-tool", "shell(jq)",
			},
		},
		{
			name: "multiple single-quoted tools with same prefix are deduplicated",
			tools: map[string]any{
				"bash": []any{
					"jq '.filter1'",
					"jq '.filter2'",
					"jq '.filter3'",
				},
			},
			// All three sanitize to "jq" → deduplication yields exactly one shell(jq)
			expected: []string{"--allow-tool", "shell(jq)"},
		},
		// Wildcard normalization tests - "cmd *" is normalized to canonical "cmd" form
		{
			name: "bash tool with trailing space-star is normalized to canonical prefix",
			tools: map[string]any{
				"bash": []any{"jq *"},
			},
			expected: []string{"--allow-tool", "shell(jq)"},
		},
		{
			name: "bash tool with trailing space-star on multi-word command is normalized",
			tools: map[string]any{
				"bash": []any{"gh issue list *"},
			},
			expected: []string{"--allow-tool", "shell(gh issue list)"},
		},
		{
			name: "community-attribution-style wildcard entries are normalized to canonical forms",
			tools: map[string]any{
				"bash": []any{"jq *", "sed *", "awk *", "cat *"},
			},
			expected: []string{
				"--allow-tool", "shell(awk)",
				"--allow-tool", "shell(cat)",
				"--allow-tool", "shell(jq)",
				"--allow-tool", "shell(sed)",
			},
		},
		{
			name: "wildcard and non-wildcard forms of same command are deduplicated",
			tools: map[string]any{
				"bash": []any{"jq *", "jq"},
			},
			// Both normalize to shell(jq); deduplication yields exactly one entry.
			expected: []string{"--allow-tool", "shell(jq)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.computeCopilotToolArguments(tt.tools, tt.safeOutputs, tt.mcpScripts, tt.workflowData)

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
