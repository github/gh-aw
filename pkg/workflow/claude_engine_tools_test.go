//go:build !integration

package workflow

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestClaudeEngineComputeAllowedTools(t *testing.T) {
	engine := NewClaudeEngine()

	tests := []struct {
		name     string
		tools    map[string]any
		expected string
	}{
		{
			name:     "empty tools",
			tools:    map[string]any{},
			expected: "ExitPlanMode,Glob,Grep,LS,NotebookRead,Read,Task,TodoWrite",
		},
		{
			name: "bash with specific commands (neutral format)",
			tools: map[string]any{
				"bash": []any{"echo", "ls"},
			},
			expected: "Bash(echo),Bash(ls),BashOutput,ExitPlanMode,Glob,Grep,KillBash,LS,NotebookRead,Read,Task,TodoWrite",
		},
		{
			name: "bash with nil value (all commands allowed)",
			tools: map[string]any{
				"bash": nil,
			},
			expected: "Bash,BashOutput,ExitPlanMode,Glob,Grep,KillBash,LS,NotebookRead,Read,Task,TodoWrite",
		},
		{
			name: "neutral web tools",
			tools: map[string]any{
				"web-fetch":  nil,
				"web-search": nil,
			},
			expected: "ExitPlanMode,Glob,Grep,LS,NotebookRead,Read,Task,TodoWrite,WebFetch,WebSearch",
		},
		{
			name: "mcp tools",
			tools: map[string]any{
				"github": map[string]any{
					"allowed": []any{"list_issues", "create_issue"},
				},
			},
			expected: "ExitPlanMode,Glob,Grep,LS,NotebookRead,Read,Task,TodoWrite,mcp__github__create_issue,mcp__github__list_issues",
		},
		{
			name: "github tools without explicit allowed list (should use defaults)",
			tools: map[string]any{
				"github": map[string]any{},
			},
			expected: func() string {
				// Expected to include all default GitHub tools with mcp__github__ prefix
				base := "ExitPlanMode,Glob,Grep,LS,NotebookRead,Read,Task,TodoWrite"
				var githubTools []string
				for _, tool := range constants.DefaultGitHubTools {
					githubTools = append(githubTools, "mcp__github__"+tool)
				}
				// Sort the GitHub tools to match the expected output
				sort.Strings(githubTools)
				return base + "," + strings.Join(githubTools, ",")
			}(),
		},
		{
			name: "cache-memory tool (provides file system access with path-specific cache tools)",
			tools: map[string]any{
				"cache-memory": map[string]any{
					"key": "test-memory-key",
				},
			},
			expected: "Bash(cat /tmp/gh-aw/cache-memory/),Bash(cat > /tmp/gh-aw/cache-memory/),Bash(mkdir -p /tmp/gh-aw/cache-memory/),Bash(mv /tmp/gh-aw/cache-memory/),BashOutput,Edit(/tmp/gh-aw/cache-memory/*),ExitPlanMode,Glob,Grep,KillBash,LS,MultiEdit(/tmp/gh-aw/cache-memory/*),NotebookRead,Read,Read(/tmp/gh-aw/cache-memory/*),Task,TodoWrite,Write(/tmp/gh-aw/cache-memory/*)",
		},
		{
			name: "cache-memory with boolean true",
			tools: map[string]any{
				"cache-memory": true,
			},
			expected: "Bash(cat /tmp/gh-aw/cache-memory/),Bash(cat > /tmp/gh-aw/cache-memory/),Bash(mkdir -p /tmp/gh-aw/cache-memory/),Bash(mv /tmp/gh-aw/cache-memory/),BashOutput,Edit(/tmp/gh-aw/cache-memory/*),ExitPlanMode,Glob,Grep,KillBash,LS,MultiEdit(/tmp/gh-aw/cache-memory/*),NotebookRead,Read,Read(/tmp/gh-aw/cache-memory/*),Task,TodoWrite,Write(/tmp/gh-aw/cache-memory/*)",
		},
		{
			name: "cache-memory with nil value (no value specified)",
			tools: map[string]any{
				"cache-memory": nil,
			},
			expected: "Bash(cat /tmp/gh-aw/cache-memory/),Bash(cat > /tmp/gh-aw/cache-memory/),Bash(mkdir -p /tmp/gh-aw/cache-memory/),Bash(mv /tmp/gh-aw/cache-memory/),BashOutput,Edit(/tmp/gh-aw/cache-memory/*),ExitPlanMode,Glob,Grep,KillBash,LS,MultiEdit(/tmp/gh-aw/cache-memory/*),NotebookRead,Read,Read(/tmp/gh-aw/cache-memory/*),Task,TodoWrite,Write(/tmp/gh-aw/cache-memory/*)",
		},
		{
			name: "cache-memory with github tools",
			tools: map[string]any{
				"cache-memory": true,
				"github": map[string]any{
					"allowed": []any{"get_repository"},
				},
			},
			expected: "Bash(cat /tmp/gh-aw/cache-memory/),Bash(cat > /tmp/gh-aw/cache-memory/),Bash(mkdir -p /tmp/gh-aw/cache-memory/),Bash(mv /tmp/gh-aw/cache-memory/),BashOutput,Edit(/tmp/gh-aw/cache-memory/*),ExitPlanMode,Glob,Grep,KillBash,LS,MultiEdit(/tmp/gh-aw/cache-memory/*),NotebookRead,Read,Read(/tmp/gh-aw/cache-memory/*),Task,TodoWrite,Write(/tmp/gh-aw/cache-memory/*),mcp__github__get_repository",
		},
		{
			name: "cache-memory with unrestricted bash (no extra cache bash commands injected)",
			tools: map[string]any{
				"cache-memory": true,
				"bash":         []any{"*"},
			},
			expected: "Bash,BashOutput,Edit(/tmp/gh-aw/cache-memory/*),ExitPlanMode,Glob,Grep,KillBash,LS,MultiEdit(/tmp/gh-aw/cache-memory/*),NotebookRead,Read,Read(/tmp/gh-aw/cache-memory/*),Task,TodoWrite,Write(/tmp/gh-aw/cache-memory/*)",
		},
		{
			name: "mixed neutral and mcp tools",
			tools: map[string]any{
				"web-fetch":  nil,
				"web-search": nil,
				"github": map[string]any{
					"allowed": []any{"list_issues"},
				},
			},
			expected: "ExitPlanMode,Glob,Grep,LS,NotebookRead,Read,Task,TodoWrite,WebFetch,WebSearch,mcp__github__list_issues",
		},
		{
			name: "custom mcp servers with new format",
			tools: map[string]any{
				"custom_server": map[string]any{
					"type":    "stdio",
					"command": "server",
					"allowed": []any{"tool1", "tool2"},
				},
			},
			expected: "ExitPlanMode,Glob,Grep,LS,NotebookRead,Read,Task,TodoWrite,mcp__custom_server__tool1,mcp__custom_server__tool2",
		},
		{
			name: "mcp server with wildcard access",
			tools: map[string]any{
				"notion": map[string]any{
					"type":    "stdio",
					"command": "notion-server",
					"allowed": []any{"*"},
				},
			},
			expected: "ExitPlanMode,Glob,Grep,LS,NotebookRead,Read,Task,TodoWrite,mcp__notion",
		},
		{
			name: "mixed mcp servers - one with wildcard, one with specific tools",
			tools: map[string]any{
				"notion": map[string]any{
					"type":    "stdio",
					"command": "notion-server",
					"allowed": []any{"*"},
				},
				"github": map[string]any{
					"allowed": []any{"list_issues", "create_issue"},
				},
			},
			expected: "ExitPlanMode,Glob,Grep,LS,NotebookRead,Read,Task,TodoWrite,mcp__github__create_issue,mcp__github__list_issues,mcp__notion",
		},
		{
			name: "bash with * wildcard (should ignore other bash tools)",
			tools: map[string]any{
				"bash": []any{"*"},
			},
			expected: "Bash,BashOutput,ExitPlanMode,Glob,Grep,KillBash,LS,NotebookRead,Read,Task,TodoWrite",
		},
		{
			name: "bash with * wildcard mixed with other commands (should ignore other commands)",
			tools: map[string]any{
				"bash": []any{"echo", "ls", "*", "cat"},
			},
			expected: "Bash,BashOutput,ExitPlanMode,Glob,Grep,KillBash,LS,NotebookRead,Read,Task,TodoWrite",
		},
		{
			name: "bash with * wildcard and other tools",
			tools: map[string]any{
				"bash":      []any{"*"},
				"web-fetch": nil,
				"github": map[string]any{
					"allowed": []any{"list_issues"},
				},
			},
			expected: "Bash,BashOutput,ExitPlanMode,Glob,Grep,KillBash,LS,NotebookRead,Read,Task,TodoWrite,WebFetch,mcp__github__list_issues",
		},
		{
			name: "bash with :* wildcard (should ignore other bash tools)",
			tools: map[string]any{
				"bash": []any{":*"},
			},
			expected: "Bash,BashOutput,ExitPlanMode,Glob,Grep,KillBash,LS,NotebookRead,Read,Task,TodoWrite",
		},
		{
			name: "bash with :* wildcard mixed with other commands (should ignore other commands)",
			tools: map[string]any{
				"bash": []any{"echo", "ls", ":*", "cat"},
			},
			expected: "Bash,BashOutput,ExitPlanMode,Glob,Grep,KillBash,LS,NotebookRead,Read,Task,TodoWrite",
		},
		{
			name: "bash with :* wildcard and other tools",
			tools: map[string]any{
				"bash":      []any{":*"},
				"web-fetch": nil,
				"github": map[string]any{
					"allowed": []any{"list_issues"},
				},
			},
			expected: "Bash,BashOutput,ExitPlanMode,Glob,Grep,KillBash,LS,NotebookRead,Read,Task,TodoWrite,WebFetch,mcp__github__list_issues",
		},
		{
			name: "bash with single command should include implicit tools",
			tools: map[string]any{
				"bash": []any{"ls"},
			},
			expected: "Bash(ls),BashOutput,ExitPlanMode,Glob,Grep,KillBash,LS,NotebookRead,Read,Task,TodoWrite",
		},
		{
			name: "explicit KillBash and BashOutput should not duplicate",
			tools: map[string]any{
				"bash": []any{"echo"},
			},
			expected: "Bash(echo),BashOutput,ExitPlanMode,Glob,Grep,KillBash,LS,NotebookRead,Read,Task,TodoWrite",
		},
		{
			name: "no bash tools means no implicit tools",
			tools: map[string]any{
				"web-fetch":  nil,
				"web-search": nil,
			},
			expected: "ExitPlanMode,Glob,Grep,LS,NotebookRead,Read,Task,TodoWrite,WebFetch,WebSearch",
		},
		// Test cases for new neutral tools format
		{
			name: "neutral bash tool",
			tools: map[string]any{
				"bash": []any{"echo", "ls"},
			},
			expected: "Bash(echo),Bash(ls),BashOutput,ExitPlanMode,Glob,Grep,KillBash,LS,NotebookRead,Read,Task,TodoWrite",
		},
		{
			name: "neutral web-fetch tool",
			tools: map[string]any{
				"web-fetch": nil,
			},
			expected: "ExitPlanMode,Glob,Grep,LS,NotebookRead,Read,Task,TodoWrite,WebFetch",
		},
		{
			name: "neutral web-search tool",
			tools: map[string]any{
				"web-search": nil,
			},
			expected: "ExitPlanMode,Glob,Grep,LS,NotebookRead,Read,Task,TodoWrite,WebSearch",
		},
		{
			name: "neutral edit tool",
			tools: map[string]any{
				"edit": nil,
			},
			expected: "Edit,ExitPlanMode,Glob,Grep,LS,MultiEdit,NotebookEdit,NotebookRead,Read,Task,TodoWrite,Write",
		},
		{
			name: "neutral edit tool explicitly disabled",
			tools: map[string]any{
				"edit": false,
			},
			expected: "ExitPlanMode,Glob,Grep,LS,NotebookRead,Read,Task,TodoWrite",
		},
		{
			name: "mixed neutral and MCP tools",
			tools: map[string]any{
				"web-fetch": nil,
				"bash":      []any{"git status"},
				"github": map[string]any{
					"allowed": []any{"list_issues"},
				},
			},
			expected: "Bash(git status),BashOutput,ExitPlanMode,Glob,Grep,KillBash,LS,NotebookRead,Read,Task,TodoWrite,WebFetch,mcp__github__list_issues",
		},
		{
			name: "all neutral tools together",
			tools: map[string]any{
				"bash":       []any{"echo"},
				"web-fetch":  nil,
				"web-search": nil,
				"edit":       nil,
			},
			expected: "Bash(echo),BashOutput,Edit,ExitPlanMode,Glob,Grep,KillBash,LS,MultiEdit,NotebookEdit,NotebookRead,Read,Task,TodoWrite,WebFetch,WebSearch,Write",
		},
		{
			name: "neutral bash with nil value (all commands)",
			tools: map[string]any{
				"bash": nil,
			},
			expected: "Bash,BashOutput,ExitPlanMode,Glob,Grep,KillBash,LS,NotebookRead,Read,Task,TodoWrite",
		},
		{
			name: "neutral playwright CLI tool does not add MCP permissions",
			tools: map[string]any{
				"playwright": nil,
			},
			expected: "ExitPlanMode,Glob,Grep,LS,NotebookRead,Read,Task,TodoWrite",
		},
		// Wildcard normalization tests - "cmd *" should normalize to "Bash(cmd)"
		{
			name: "bash tool with trailing space-star is normalized to canonical Bash(cmd)",
			tools: map[string]any{
				"bash": []any{"jq *"},
			},
			expected: "Bash(jq),BashOutput,ExitPlanMode,Glob,Grep,KillBash,LS,NotebookRead,Read,Task,TodoWrite",
		},
		{
			name: "community-attribution-style wildcard entries normalize to canonical forms",
			tools: map[string]any{
				"bash": []any{"jq *", "sed *", "awk *"},
			},
			expected: "Bash(awk),Bash(jq),Bash(sed),BashOutput,ExitPlanMode,Glob,Grep,KillBash,LS,NotebookRead,Read,Task,TodoWrite",
		},
		{
			name: "wildcard and non-wildcard forms of same command are both accepted",
			tools: map[string]any{
				"bash": []any{"jq *", "jq"},
			},
			// Claude does not deduplicate tool lists, so both resolve to Bash(jq)
			expected: "Bash(jq),BashOutput,ExitPlanMode,Glob,Grep,KillBash,LS,NotebookRead,Read,Task,TodoWrite",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Extract cache-memory config from tools if present
			compiler := NewCompiler()
			cacheMemoryConfig, _ := compiler.extractCacheMemoryConfigFromMap(tt.tools)
			result := engine.computeAllowedClaudeToolsString(tt.tools, nil, cacheMemoryConfig, nil, nil, nil)

			// Parse expected and actual results into sets for comparison
			expectedTools := make(map[string]struct{})
			if tt.expected != "" {
				for tool := range strings.SplitSeq(tt.expected, ",") {
					expectedTools[strings.TrimSpace(tool)] = struct{}{}
				}
			}

			actualTools := make(map[string]struct{})
			if result != "" {
				for tool := range strings.SplitSeq(result, ",") {
					actualTools[strings.TrimSpace(tool)] = struct{}{}
				}
			}

			// Check if both sets have the same tools
			if len(expectedTools) != len(actualTools) {
				t.Errorf("Expected %d tools, got %d tools. Expected: '%s', Actual: '%s'",
					len(expectedTools), len(actualTools), tt.expected, result)
				return
			}

			for expectedTool := range expectedTools {
				if _, ok := actualTools[expectedTool]; !ok {
					t.Errorf("Expected tool '%s' not found in result: '%s'", expectedTool, result)
				}
			}

			for actualTool := range actualTools {
				if _, ok := expectedTools[actualTool]; !ok {
					t.Errorf("Unexpected tool '%s' found in result: '%s'", actualTool, result)
				}
			}
		})
	}
}

func TestClaudeEngineComputeAllowedToolsDeduplicatesNormalizedBashEntries(t *testing.T) {
	engine := NewClaudeEngine()

	tools := map[string]any{
		"bash": []any{"jq *", "jq"},
	}

	cacheMemoryConfig, err := NewCompiler().extractCacheMemoryConfigFromMap(tools)
	if err != nil {
		t.Fatalf("extract cache-memory config: %v", err)
	}

	result := engine.computeAllowedClaudeToolsString(tools, nil, cacheMemoryConfig, nil, nil, nil)
	expected := "Bash(jq),BashOutput,ExitPlanMode,Glob,Grep,KillBash,LS,NotebookRead,Read,Task,TodoWrite"
	if result != expected {
		t.Fatalf("unexpected allowed tools\nwant: %s\ngot:  %s", expected, result)
	}
}

func TestClaudeEngineComputeAllowedToolsWithSafeOutputs(t *testing.T) {
	engine := NewClaudeEngine()

	tests := []struct {
		name        string
		tools       map[string]any
		safeOutputs *SafeOutputsConfig
		expected    string
	}{
		{
			name:  "SafeOutputs with no tools - should add Write permission",
			tools: map[string]any{
				// Using neutral tools instead of claude section
			},
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")}},
			},
			expected: "ExitPlanMode,Glob,Grep,LS,NotebookRead,Read,Task,TodoWrite,Write,mcp__safeoutputs",
		},
		{
			name: "SafeOutputs with general Write permission - should not add specific Write",
			tools: map[string]any{
				"edit": nil, // This provides Write capabilities
			},
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")}},
			},
			expected: "Edit,ExitPlanMode,Glob,Grep,LS,MultiEdit,NotebookEdit,NotebookRead,Read,Task,TodoWrite,Write,mcp__safeoutputs",
		},
		{
			name:  "No SafeOutputs - should not add Write permission",
			tools: map[string]any{
				// Using neutral tools instead of claude section
			},
			safeOutputs: nil,
			expected:    "ExitPlanMode,Glob,Grep,LS,NotebookRead,Read,Task,TodoWrite",
		},
		{
			name: "SafeOutputs with multiple output types",
			tools: map[string]any{
				"bash": nil, // This provides Bash, BashOutput, KillBash
				"edit": nil,
			},
			safeOutputs: &SafeOutputsConfig{
				CreateIssues:       &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")}},
				AddComments:        &AddCommentsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")}},
				CreatePullRequests: &CreatePullRequestsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")}},
			},
			expected: "Bash,BashOutput,Edit,ExitPlanMode,Glob,Grep,KillBash,LS,MultiEdit,NotebookEdit,NotebookRead,Read,Task,TodoWrite,Write,mcp__safeoutputs",
		},
		{
			name: "SafeOutputs with MCP tools",
			tools: map[string]any{
				"github": map[string]any{
					"allowed": []any{"create_issue", "create_pull_request"},
				},
			},
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")}},
			},
			expected: "ExitPlanMode,Glob,Grep,LS,NotebookRead,Read,Task,TodoWrite,Write,mcp__github__create_issue,mcp__github__create_pull_request,mcp__safeoutputs",
		},
		{
			name: "SafeOutputs with neutral tools and create-pull-request",
			tools: map[string]any{
				"bash":      []any{"echo", "ls"},
				"web-fetch": nil,
				"edit":      nil,
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")}},
			},
			expected: "Bash(echo),Bash(ls),BashOutput,Edit,ExitPlanMode,Glob,Grep,KillBash,LS,MultiEdit,NotebookEdit,NotebookRead,Read,Task,TodoWrite,WebFetch,Write,mcp__safeoutputs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Extract cache-memory config from tools if present
			compiler := NewCompiler()
			cacheMemoryConfig, _ := compiler.extractCacheMemoryConfigFromMap(tt.tools)
			result := engine.computeAllowedClaudeToolsString(tt.tools, tt.safeOutputs, cacheMemoryConfig, nil, nil, nil)

			// Split both expected and result into slices and check each tool is present
			expectedTools := strings.Split(tt.expected, ",")
			resultTools := strings.Split(result, ",")

			// Check that all expected tools are present
			for _, expectedTool := range expectedTools {
				if expectedTool == "" {
					continue // Skip empty strings
				}
				found := slices.Contains(resultTools, expectedTool)
				if !found {
					t.Errorf("Expected tool '%s' not found in result '%s'", expectedTool, result)
				}
			}

			// Check that no unexpected tools are present
			for _, actual := range resultTools {
				if actual == "" {
					continue // Skip empty strings
				}
				found := slices.Contains(expectedTools, actual)
				if !found {
					t.Errorf("Unexpected tool '%s' found in result '%s'", actual, result)
				}
			}
		})
	}
}

func TestClaudeEngineComputeAllowedToolsWithSandboxAllowWrite(t *testing.T) {
	engine := NewClaudeEngine()
	cacheMemoryConfig, err := NewCompiler().extractCacheMemoryConfigFromMap(map[string]any{})
	if err != nil {
		t.Fatalf("extract cache-memory config: %v", err)
	}

	sandboxConfig := &SandboxConfig{
		Agent: &AgentSandboxConfig{
			Runtime: AgentRuntimeCloudHypervisor,
			Config: &SandboxRuntimeConfig{
				Filesystem: &SRTFilesystemConfig{
					AllowWrite: []string{"/tmp"},
				},
			},
		},
	}

	got := engine.computeAllowedClaudeToolsString(map[string]any{}, nil, cacheMemoryConfig, nil, nil, sandboxConfig)
	want := "Edit(/tmp/*),ExitPlanMode,Glob,Grep,LS,MultiEdit(/tmp/*),NotebookRead,Read,Read(/tmp/*),Task,TodoWrite,Write(/tmp/*)"
	if got != want {
		t.Fatalf("unexpected allowed tools\nwant: %s\ngot:  %s", want, got)
	}
}

func TestClaudeEngineSandboxAllowWriteNarrowsDefaultTmpAccess(t *testing.T) {
	engine := NewClaudeEngine()
	cacheMemoryConfig, err := NewCompiler().extractCacheMemoryConfigFromMap(map[string]any{})
	if err != nil {
		t.Fatalf("extract cache-memory config: %v", err)
	}

	sandboxConfig := &SandboxConfig{
		Agent: &AgentSandboxConfig{
			Runtime: AgentRuntimeCloudHypervisor,
			Config: &SandboxRuntimeConfig{
				Filesystem: &SRTFilesystemConfig{
					AllowWrite: []string{"/workspace"},
				},
			},
		},
	}

	got := engine.computeAllowedClaudeToolsString(map[string]any{}, nil, cacheMemoryConfig, nil, nil, sandboxConfig)
	if !strings.Contains(got, "Write(/workspace/*)") {
		t.Fatalf("expected workspace write permission in %q", got)
	}
	if strings.Contains(got, "Write(/tmp/*)") {
		t.Fatalf("unexpected broad tmp write permission in %q", got)
	}
}

func TestClaudeEngineAddsTmpByDefault(t *testing.T) {
	engine := NewClaudeEngine()
	cacheMemoryConfig, err := NewCompiler().extractCacheMemoryConfigFromMap(map[string]any{})
	if err != nil {
		t.Fatalf("extract cache-memory config: %v", err)
	}

	sandboxConfig := &SandboxConfig{
		Agent: &AgentSandboxConfig{
			Type: SandboxTypeAWF,
		},
	}

	got := engine.computeAllowedClaudeToolsString(map[string]any{}, nil, cacheMemoryConfig, nil, nil, sandboxConfig)
	want := "Edit(/tmp/*),ExitPlanMode,Glob,Grep,LS,MultiEdit(/tmp/*),NotebookRead,Read,Read(/tmp/*),Task,TodoWrite,Write(/tmp/*)"
	if got != want {
		t.Fatalf("unexpected allowed tools\nwant: %s\ngot:  %s", want, got)
	}
}

func TestGenerateAllowedToolsComment(t *testing.T) {
	engine := NewClaudeEngine()

	tests := []struct {
		name            string
		allowedToolsStr string
		indent          string
		expected        string
	}{
		{
			name:            "empty allowed tools",
			allowedToolsStr: "",
			indent:          "  ",
			expected:        "",
		},
		{
			name:            "single tool",
			allowedToolsStr: "Bash",
			indent:          "  ",
			expected:        "  # Allowed tools (sorted):\n  # - Bash\n",
		},
		{
			name:            "multiple tools",
			allowedToolsStr: "Bash,Edit,Read",
			indent:          "    ",
			expected:        "    # Allowed tools (sorted):\n    # - Bash\n    # - Edit\n    # - Read\n",
		},
		{
			name:            "tools with special characters",
			allowedToolsStr: "Bash(echo),mcp__github__issue_read,Write",
			indent:          "      ",
			expected:        "      # Allowed tools (sorted):\n      # - Bash(echo)\n      # - mcp__github__issue_read\n      # - Write\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.generateAllowedToolsComment(tt.allowedToolsStr, tt.indent)
			if result != tt.expected {
				t.Errorf("Expected comment:\n%q\nBut got:\n%q", tt.expected, result)
			}
		})
	}
}
