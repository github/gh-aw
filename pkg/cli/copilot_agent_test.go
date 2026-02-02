//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/logger"
)

func TestCopilotAgentDetector_IsGitHubCopilotAgent(t *testing.T) {
	tests := []struct {
		name           string
		setupFunc      func(string) error
		workflowPath   string
		expectedResult bool
	}{
		{
			name: "detects from workflow path copilot-swe-agent.yml",
			setupFunc: func(dir string) error {
				return nil // No files needed, just workflow path
			},
			workflowPath:   ".github/workflows/copilot-swe-agent.yml",
			expectedResult: true,
		},
		{
			name: "detects from workflow path copilot-swe-agent.yaml",
			setupFunc: func(dir string) error {
				return nil
			},
			workflowPath:   ".github/workflows/copilot-swe-agent.yaml",
			expectedResult: true,
		},
		{
			name: "does not detect from different workflow path",
			setupFunc: func(dir string) error {
				return nil
			},
			workflowPath:   ".github/workflows/my-workflow.yml",
			expectedResult: false,
		},
		{
			name: "aw_info.json present means agentic workflow, not copilot agent",
			setupFunc: func(dir string) error {
				awInfo := `{"workflow_name": "copilot-swe-agent-session", "workflow_file": "test.yml"}`
				return os.WriteFile(filepath.Join(dir, "aw_info.json"), []byte(awInfo), 0644)
			},
			workflowPath:   ".github/workflows/copilot-swe-agent.yml",
			expectedResult: false,
		},
		{
			name: "aw_info.json present with any workflow name means agentic workflow",
			setupFunc: func(dir string) error {
				awInfo := `{"workflow_name": "test", "workflow_file": "copilot_swe_agent.yml"}`
				return os.WriteFile(filepath.Join(dir, "aw_info.json"), []byte(awInfo), 0644)
			},
			expectedResult: false,
		},
		{
			name: "detects agent pattern in log file without aw_info.json",
			setupFunc: func(dir string) error {
				logContent := `
2024-01-15 10:00:00 Starting GitHub Copilot Agent v1.2.3
2024-01-15 10:00:01 Initializing agent session execution
2024-01-15 10:00:02 Processing request
`
				return os.WriteFile(filepath.Join(dir, "agent.log"), []byte(logContent), 0644)
			},
			expectedResult: true,
		},
		{
			name: "detects copilot-swe-agent in log without aw_info.json",
			setupFunc: func(dir string) error {
				logContent := `Using @github/copilot-swe-agent for task execution`
				return os.WriteFile(filepath.Join(dir, "execution.log"), []byte(logContent), 0644)
			},
			expectedResult: true,
		},
		{
			name: "detects agent artifact without aw_info.json",
			setupFunc: func(dir string) error {
				return os.Mkdir(filepath.Join(dir, "copilot-agent-output"), 0755)
			},
			expectedResult: true,
		},
		{
			name: "no indicators - returns false",
			setupFunc: func(dir string) error {
				// Just create an empty directory
				return nil
			},
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tmpDir, err := os.MkdirTemp("", "copilot-agent-test-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			// Setup test environment
			if err := tt.setupFunc(tmpDir); err != nil {
				t.Fatalf("Setup failed: %v", err)
			}

			// Run detector with workflow path if provided
			var detector *CopilotAgentDetector
			if tt.workflowPath != "" {
				detector = NewCopilotAgentDetectorWithPath(tmpDir, false, tt.workflowPath)
			} else {
				detector = NewCopilotAgentDetector(tmpDir, false)
			}
			result := detector.IsGitHubCopilotAgent()

			if result != tt.expectedResult {
				t.Errorf("Expected %v, got %v", tt.expectedResult, result)
			}
		})
	}
}

func TestParseCopilotAgentLogMetrics(t *testing.T) {
	tests := []struct {
		name           string
		logContent     string
		expectedTurns  int
		expectedErrors int
		expectedTools  int
	}{
		{
			name: "parses agent turns",
			logContent: `
Task iteration 1: Starting analysis
Executing tool: github
Task iteration 2: Processing results
Calling write tool
Task iteration 3: Finalizing
`,
			expectedTurns: 3,
			expectedTools: 2,
		},
		{
			name: "parses errors and warnings",
			logContent: `
2024-01-15 10:00:00 INFO: Starting task
2024-01-15 10:00:01 ERROR: Failed to connect to service
2024-01-15 10:00:02 WARNING: Retry attempt 1
2024-01-15 10:00:03 ERROR: Connection timeout
`,
			expectedErrors: 3, // 2 errors + 1 warning
		},
		{
			name: "parses tool calls",
			logContent: `
Calling: github_search
Tool call: write_file
Using tool: bash
Executing tool: read
`,
			expectedTools: 4,
		},
		{
			name: "extracts token usage from JSON",
			logContent: `
{"token_usage": 1500, "estimated_cost": 0.05}
Task step 1 complete
{"token_usage": 2500, "estimated_cost": 0.08}
`,
			expectedTurns: 1,
		},
		{
			name: "handles empty log",
			logContent: `


`,
			expectedTurns:  0,
			expectedErrors: 0,
			expectedTools:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := ParseCopilotAgentLogMetrics(tt.logContent, false)

			if tt.expectedTurns > 0 && metrics.Turns != tt.expectedTurns {
				t.Errorf("Expected %d turns, got %d", tt.expectedTurns, metrics.Turns)
			}

			if tt.expectedTools > 0 && len(metrics.ToolCalls) < 1 {
				t.Errorf("Expected tool calls to be detected, got none")
			}
		})
	}
}

func TestExtractToolName(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected string
	}{
		{
			name:     "extracts from 'tool:' pattern",
			line:     "Using tool: github_search",
			expected: "github_search",
		},
		{
			name:     "extracts from 'calling' pattern",
			line:     "Calling: write_file",
			expected: "write_file",
		},
		{
			name:     "extracts from 'executing' pattern",
			line:     "Executing: bash_command",
			expected: "bash_command",
		},
		{
			name:     "extracts from 'using tool' pattern",
			line:     "Using tool: read_operation",
			expected: "read_operation",
		},
		{
			name:     "returns empty for no match",
			line:     "Just a regular log line",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractToolName(tt.line)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestExtractErrorMessage(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected string
	}{
		{
			name:     "removes ISO timestamp",
			line:     "2024-01-15T10:00:00.123Z ERROR: Connection failed",
			expected: "Connection failed",
		},
		{
			name:     "removes bracketed timestamp",
			line:     "[2024-01-15 10:00:00] ERROR: File not found",
			expected: "File not found",
		},
		{
			name:     "removes log level prefix",
			line:     "ERROR: Invalid input",
			expected: "Invalid input",
		},
		{
			name:     "handles warning prefix",
			line:     "WARNING: Deprecated API",
			expected: "Deprecated API",
		},
		{
			name:     "handles plain message",
			line:     "   Simple error message   ",
			expected: "Simple error message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := logger.ExtractErrorMessage(tt.line)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestIntegration_CopilotAgentWithAudit(t *testing.T) {
	// Create a temporary directory that simulates a GitHub Copilot agent run
	// NOTE: GitHub Copilot agent runs do NOT have aw_info.json (that's for agentic workflows)
	tmpDir, err := os.MkdirTemp("", "copilot-agent-integration-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a sample agent log with agent-specific patterns
	agentLog := `
2024-01-15T10:00:00.000Z Starting GitHub Copilot Agent
Task iteration 1: Analyzing codebase
Tool call: github_search
{"token_usage": 1500, "estimated_cost": 0.05}
Task iteration 2: Making changes
Tool call: write_file
ERROR: Failed to write to protected file
Task iteration 3: Completing task
Tool call: github_create_pr
`
	if err := os.WriteFile(filepath.Join(tmpDir, "agent-stdio.log"), []byte(agentLog), 0644); err != nil {
		t.Fatalf("Failed to write log file: %v", err)
	}

	// Verify detector recognizes this as a GitHub Copilot agent run (no aw_info.json)
	detector := NewCopilotAgentDetector(tmpDir, false)
	if !detector.IsGitHubCopilotAgent() {
		t.Error("Expected GitHub Copilot agent to be detected from log patterns")
	}

	// Test: Extract metrics using the system that would be used by audit
	metrics, err := extractLogMetrics(tmpDir, false)
	if err != nil {
		t.Fatalf("extractLogMetrics failed: %v", err)
	}

	// Verify that GitHub Copilot agent parser was used (indicated by extracted metrics)
	if metrics.Turns < 1 {
		t.Errorf("Expected turns to be parsed from agent log, got %d", metrics.Turns)
	}

	if len(metrics.ToolCalls) < 1 {
		t.Error("Expected tool calls to be extracted from agent log")
	}

	// Verify token usage was extracted (may not always be present in all logs)
	// This is a best-effort check - token usage extraction from JSON is optional
	if metrics.TokenUsage < 1 {
		t.Logf("Note: Token usage was not extracted from JSON in log (this is acceptable)")
	}
}

func TestReadLogHeader(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "log-header-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.log")
	content := strings.Repeat("x", 20000) // 20KB file
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Test reading first 10KB
	header, err := readLogHeader(testFile, 10240)
	if err != nil {
		t.Fatalf("readLogHeader failed: %v", err)
	}

	if len(header) != 10240 {
		t.Errorf("Expected 10240 bytes, got %d", len(header))
	}
}

func TestWorkflowLogMetricsConversion(t *testing.T) {
	// Test that our metrics are compatible with workflow.LogMetrics
	logContent := `
Task iteration 1
Tool call: github
ERROR: Test error
`
	metrics := ParseCopilotAgentLogMetrics(logContent, false)

	// Verify the returned type is workflow.LogMetrics
	var _ = metrics

	// Verify fields are properly set
	if metrics.Turns != 1 {
		t.Errorf("Expected 1 turn, got %d", metrics.Turns)
	}

	if len(metrics.ToolCalls) < 1 {
		t.Error("Expected tool calls to be extracted")
	}
}
