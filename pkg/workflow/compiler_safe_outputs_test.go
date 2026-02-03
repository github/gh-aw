//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseOnSection tests command, reaction, and stop-after parsing from frontmatter
func TestParseOnSection(t *testing.T) {
	tests := []struct {
		name                string
		frontmatter         map[string]any
		workflowData        *WorkflowData
		markdownPath        string
		expectedError       bool
		expectedCommand     []string
		expectedReaction    string
		expectedLockAgent   bool
		expectedOn          string
		checkCommandEvents  bool
		expectedOtherEvents map[string]any
	}{
		{
			name: "slash_command trigger with default command from filename",
			frontmatter: map[string]any{
				"on": map[string]any{
					"slash_command": map[string]any{},
				},
			},
			workflowData:     &WorkflowData{},
			markdownPath:     "/path/to/test-workflow.md",
			expectedError:    false,
			expectedCommand:  []string{"test-workflow"},
			expectedReaction: "eyes", // auto-enabled for command triggers
			expectedOn:       "",
		},
		{
			name: "slash_command with explicit command",
			frontmatter: map[string]any{
				"on": map[string]any{
					"slash_command": map[string]any{},
				},
			},
			workflowData: &WorkflowData{
				Command: []string{"custom-cmd"},
			},
			markdownPath:     "/path/to/test.md",
			expectedError:    false,
			expectedCommand:  []string{"custom-cmd"},
			expectedReaction: "eyes",
		},
		{
			name: "deprecated command trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"command": map[string]any{},
				},
			},
			workflowData:     &WorkflowData{},
			markdownPath:     "/path/to/cmd-workflow.md",
			expectedError:    false,
			expectedCommand:  []string{"cmd-workflow"},
			expectedReaction: "eyes",
		},
		{
			name: "slash_command with explicit reaction",
			frontmatter: map[string]any{
				"on": map[string]any{
					"slash_command": map[string]any{},
					"reaction":      "heart",
				},
			},
			workflowData:     &WorkflowData{},
			markdownPath:     "/path/to/test.md",
			expectedError:    false,
			expectedCommand:  []string{"test"},
			expectedReaction: "heart",
		},
		{
			name: "reaction with numeric +1",
			frontmatter: map[string]any{
				"on": map[string]any{
					"reaction": 1,
				},
			},
			workflowData:     &WorkflowData{},
			markdownPath:     "/path/to/test.md",
			expectedError:    false,
			expectedReaction: "+1",
		},
		{
			name: "reaction with numeric -1",
			frontmatter: map[string]any{
				"on": map[string]any{
					"reaction": -1,
				},
			},
			workflowData:     &WorkflowData{},
			markdownPath:     "/path/to/test.md",
			expectedError:    false,
			expectedReaction: "-1",
		},
		{
			name: "reaction none disables reactions",
			frontmatter: map[string]any{
				"on": map[string]any{
					"slash_command": map[string]any{},
					"reaction":      "none",
				},
			},
			workflowData:     &WorkflowData{},
			markdownPath:     "/path/to/test.md",
			expectedError:    false,
			expectedCommand:  []string{"test"},
			expectedReaction: "none",
		},
		{
			name: "invalid reaction value",
			frontmatter: map[string]any{
				"on": map[string]any{
					"reaction": "thumbsup",
				},
			},
			workflowData:  &WorkflowData{},
			markdownPath:  "/path/to/test.md",
			expectedError: true,
		},
		{
			name: "slash_command conflicts with issues",
			frontmatter: map[string]any{
				"on": map[string]any{
					"slash_command": map[string]any{},
					"issues": map[string]any{
						"types": []string{"opened"},
					},
				},
			},
			workflowData:  &WorkflowData{},
			markdownPath:  "/path/to/test.md",
			expectedError: true,
		},
		{
			name: "slash_command conflicts with issue_comment",
			frontmatter: map[string]any{
				"on": map[string]any{
					"slash_command": map[string]any{},
					"issue_comment": map[string]any{
						"types": []string{"created"},
					},
				},
			},
			workflowData:  &WorkflowData{},
			markdownPath:  "/path/to/test.md",
			expectedError: true,
		},
		{
			name: "slash_command allows labeled/unlabeled issues",
			frontmatter: map[string]any{
				"on": map[string]any{
					"slash_command": map[string]any{},
					"issues": map[string]any{
						"types": []any{"labeled", "unlabeled"},
					},
				},
			},
			workflowData:     &WorkflowData{},
			markdownPath:     "/path/to/test.md",
			expectedError:    false,
			expectedCommand:  []string{"test"},
			expectedReaction: "eyes",
		},
		{
			name: "lock-for-agent from issues trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"issues": map[string]any{
						"lock-for-agent": true,
					},
				},
			},
			workflowData:      &WorkflowData{},
			markdownPath:      "/path/to/test.md",
			expectedError:     false,
			expectedLockAgent: true,
		},
		{
			name: "lock-for-agent from issue_comment trigger",
			frontmatter: map[string]any{
				"on": map[string]any{
					"issue_comment": map[string]any{
						"lock-for-agent": true,
					},
				},
			},
			workflowData:      &WorkflowData{},
			markdownPath:      "/path/to/test.md",
			expectedError:     false,
			expectedLockAgent: true,
		},
		{
			name: "stop-after in on section",
			frontmatter: map[string]any{
				"on": map[string]any{
					"stop-after": "1h",
					"push":       map[string]any{},
				},
			},
			workflowData:  &WorkflowData{},
			markdownPath:  "/path/to/test.md",
			expectedError: false,
		},
		{
			name: "command with other events merged",
			frontmatter: map[string]any{
				"on": map[string]any{
					"slash_command": map[string]any{},
					"push": map[string]any{
						"branches": []any{"main"},
					},
				},
			},
			workflowData:        &WorkflowData{},
			markdownPath:        "/path/to/test.md",
			expectedError:       false,
			expectedCommand:     []string{"test"},
			checkCommandEvents:  true,
			expectedOtherEvents: map[string]any{"push": map[string]any{"branches": []any{"main"}}},
		},
		{
			name:          "no on section",
			frontmatter:   map[string]any{},
			workflowData:  &WorkflowData{},
			markdownPath:  "/path/to/test.md",
			expectedError: false,
		},
		{
			name: "on section with string value (not a map)",
			frontmatter: map[string]any{
				"on": "push",
			},
			workflowData:  &WorkflowData{},
			markdownPath:  "/path/to/test.md",
			expectedError: false,
		},
		{
			name: "lock-for-agent false",
			frontmatter: map[string]any{
				"on": map[string]any{
					"issues": map[string]any{
						"lock-for-agent": false,
					},
				},
			},
			workflowData:      &WorkflowData{},
			markdownPath:      "/path/to/test.md",
			expectedError:     false,
			expectedLockAgent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiler{}
			err := c.parseOnSection(tt.frontmatter, tt.workflowData, tt.markdownPath)

			if tt.expectedError {
				require.Error(t, err, "Expected an error but got none")
			} else {
				require.NoError(t, err, "Expected no error but got one")
				if len(tt.expectedCommand) > 0 {
					assert.Equal(t, tt.expectedCommand, tt.workflowData.Command, "Command mismatch")
				}
				if tt.expectedReaction != "" {
					assert.Equal(t, tt.expectedReaction, tt.workflowData.AIReaction, "Reaction mismatch")
				}
				assert.Equal(t, tt.expectedLockAgent, tt.workflowData.LockForAgent, "LockForAgent mismatch")
				if tt.checkCommandEvents {
					assert.NotNil(t, tt.workflowData.CommandOtherEvents, "CommandOtherEvents should be set")
					if tt.expectedOtherEvents != nil {
						// Basic check that other events were extracted
						assert.NotEmpty(t, tt.workflowData.CommandOtherEvents, "CommandOtherEvents should not be empty")
					}
				}
			}
		})
	}
}

// TestCompilerGenerateJobName tests job name sanitization from Compiler method
func TestCompilerGenerateJobName(t *testing.T) {
	tests := []struct {
		name         string
		workflowName string
		expected     string
	}{
		{
			name:         "simple lowercase name",
			workflowName: "test",
			expected:     "test",
		},
		{
			name:         "uppercase converted to lowercase",
			workflowName: "TestWorkflow",
			expected:     "testworkflow",
		},
		{
			name:         "spaces replaced with hyphens",
			workflowName: "Test Workflow Name",
			expected:     "test-workflow-name",
		},
		{
			name:         "special characters replaced",
			workflowName: "test:workflow.name,here",
			expected:     "test-workflow-name-here",
		},
		{
			name:         "slashes replaced",
			workflowName: "test/workflow\\name",
			expected:     "test-workflow-name",
		},
		{
			name:         "parentheses and at symbol replaced",
			workflowName: "test(workflow)@name",
			expected:     "test-workflow-name",
		},
		{
			name:         "quotes removed",
			workflowName: `test'workflow"name`,
			expected:     "testworkflowname",
		},
		{
			name:         "multiple consecutive hyphens collapsed",
			workflowName: "test---workflow--name",
			expected:     "test-workflow-name",
		},
		{
			name:         "leading and trailing hyphens removed",
			workflowName: "-test-workflow-",
			expected:     "test-workflow",
		},
		{
			name:         "empty string gets prefix",
			workflowName: "",
			expected:     "workflow-",
		},
		{
			name:         "starts with number gets prefix",
			workflowName: "123test",
			expected:     "workflow-123test",
		},
		{
			name:         "starts with letter is valid",
			workflowName: "a123",
			expected:     "a123",
		},
		{
			name:         "starts with underscore is valid",
			workflowName: "_test",
			expected:     "_test",
		},
		{
			name:         "complex real-world name",
			workflowName: "Fix Bug: API Timeout (Issue #123)",
			expected:     "fix-bug-api-timeout-issue-#123", // # is not replaced
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiler{}
			result := c.generateJobName(tt.workflowName)
			assert.Equal(t, tt.expected, result, "Job name mismatch")
		})
	}
}

// TestCompilerMergeSafeJobsFromIncludes tests safe-jobs merging from included files via Compiler
func TestCompilerMergeSafeJobsFromIncludes(t *testing.T) {
	tests := []struct {
		name                string
		topSafeJobs         map[string]*SafeJobConfig
		includedContentJSON string
		expectError         bool
		expectedJobNames    []string
		checkConflict       bool
	}{
		{
			name:                "empty included content",
			topSafeJobs:         map[string]*SafeJobConfig{"job1": {}},
			includedContentJSON: "",
			expectError:         false,
			expectedJobNames:    []string{"job1"},
		},
		{
			name:                "empty JSON object",
			topSafeJobs:         map[string]*SafeJobConfig{"job1": {}},
			includedContentJSON: "{}",
			expectError:         false,
			expectedJobNames:    []string{"job1"},
		},
		{
			name:        "merge with no conflicts",
			topSafeJobs: map[string]*SafeJobConfig{"job1": {}},
			includedContentJSON: `{
				"safe-outputs": {
					"jobs": {
						"job2": {}
					}
				}
			}`,
			expectError:      false,
			expectedJobNames: []string{"job1", "job2"},
		},
		{
			name:        "conflict between top and included jobs",
			topSafeJobs: map[string]*SafeJobConfig{"job1": {}},
			includedContentJSON: `{
				"safe-outputs": {
					"jobs": {
						"job1": {}
					}
				}
			}`,
			expectError:   true,
			checkConflict: true,
		},
		{
			name:                "invalid JSON",
			topSafeJobs:         map[string]*SafeJobConfig{"job1": {}},
			includedContentJSON: "invalid json",
			expectError:         false,
			expectedJobNames:    []string{"job1"}, // Should return original jobs
		},
		{
			name:        "nil top safe-jobs",
			topSafeJobs: nil,
			includedContentJSON: `{
				"safe-outputs": {
					"jobs": {
						"job1": {}
					}
				}
			}`,
			expectError:      false,
			expectedJobNames: []string{}, // Since parseSafeJobsConfig returns empty map for invalid config
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiler{}
			result, err := c.mergeSafeJobsFromIncludes(tt.topSafeJobs, tt.includedContentJSON)

			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
				if tt.checkConflict {
					assert.Contains(t, err.Error(), "conflict", "Error should mention conflict")
				}
			} else {
				require.NoError(t, err, "Expected no error but got one")
				assert.NotNil(t, result, "Result should not be nil")
				// Check job names if specified
				if len(tt.expectedJobNames) > 0 {
					assert.Len(t, result, len(tt.expectedJobNames), "Job count mismatch")
					for _, jobName := range tt.expectedJobNames {
						_, exists := result[jobName]
						assert.True(t, exists, "Job %s should exist", jobName)
					}
				}
			}
		})
	}
}

// TestCompilerMergeSafeJobsFromIncludedConfigs tests safe-jobs merging from included configs via Compiler
func TestCompilerMergeSafeJobsFromIncludedConfigs(t *testing.T) {
	tests := []struct {
		name             string
		topSafeJobs      map[string]*SafeJobConfig
		includedConfigs  []string
		expectError      bool
		expectedJobNames []string
	}{
		{
			name:             "empty included configs",
			topSafeJobs:      map[string]*SafeJobConfig{"job1": {}},
			includedConfigs:  []string{},
			expectError:      false,
			expectedJobNames: []string{"job1"},
		},
		{
			name:        "merge multiple configs without conflicts",
			topSafeJobs: map[string]*SafeJobConfig{"job1": {}},
			includedConfigs: []string{
				`{"jobs": {"job2": {}}}`,
				`{"jobs": {"job3": {}}}`,
			},
			expectError:      false,
			expectedJobNames: []string{"job1", "job2", "job3"}, // Jobs are extracted from included configs
		},
		{
			name:        "skip empty config strings",
			topSafeJobs: map[string]*SafeJobConfig{"job1": {}},
			includedConfigs: []string{
				"",
				"{}",
			},
			expectError:      false,
			expectedJobNames: []string{"job1"},
		},
		{
			name:        "skip invalid JSON",
			topSafeJobs: map[string]*SafeJobConfig{"job1": {}},
			includedConfigs: []string{
				"invalid json",
			},
			expectError:      false,
			expectedJobNames: []string{"job1"},
		},
		{
			name:        "nil top safe-jobs",
			topSafeJobs: nil,
			includedConfigs: []string{
				`{"jobs": {"job1": {}}}`,
			},
			expectError:      false,
			expectedJobNames: []string{}, // Empty map created
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiler{}
			result, err := c.mergeSafeJobsFromIncludedConfigs(tt.topSafeJobs, tt.includedConfigs)

			if tt.expectError {
				assert.Error(t, err, "Expected an error but got none")
			} else {
				require.NoError(t, err, "Expected no error but got one")
				assert.NotNil(t, result, "Result should not be nil")
				// Check job names if specified
				if len(tt.expectedJobNames) > 0 {
					assert.Len(t, result, len(tt.expectedJobNames), "Job count mismatch")
					for _, jobName := range tt.expectedJobNames {
						_, exists := result[jobName]
						assert.True(t, exists, "Job %s should exist", jobName)
					}
				}
			}
		})
	}
}

// TestApplyDefaultTools tests default tool application logic
func TestApplyDefaultTools(t *testing.T) {
	tests := []struct {
		name                 string
		tools                map[string]any
		safeOutputs          *SafeOutputsConfig
		sandboxConfig        *SandboxConfig
		networkPermissions   *NetworkPermissions
		expectedGitHub       bool // true if github tool should exist
		expectedEdit         bool
		expectedBash         bool
		checkBashWildcard    bool
		checkGitCommands     bool
		checkBashDefault     bool
		expectedBashCommands []string
	}{
		{
			name:           "nil tools creates github tool",
			tools:          nil,
			expectedGitHub: true,
			expectedEdit:   false,
			expectedBash:   false,
		},
		{
			name:           "empty tools adds github tool",
			tools:          map[string]any{},
			expectedGitHub: true,
			expectedEdit:   false,
			expectedBash:   false,
		},
		{
			name: "github explicitly disabled",
			tools: map[string]any{
				"github": false,
			},
			expectedGitHub: false,
			expectedEdit:   false,
			expectedBash:   false,
		},
		{
			name:  "sandbox enabled adds edit and bash",
			tools: map[string]any{},
			sandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID: "awf",
				},
			},
			expectedGitHub:    true,
			expectedEdit:      true,
			expectedBash:      true,
			checkBashWildcard: false, // Sandbox adds bash but may also include default commands
		},
		{
			name:  "firewall enabled adds edit and bash",
			tools: map[string]any{},
			networkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
			expectedGitHub:    true,
			expectedEdit:      true,
			expectedBash:      true,
			checkBashWildcard: false, // Firewall adds bash but may also include default commands
		},
		{
			name:  "create pull request adds git commands",
			tools: map[string]any{},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectedGitHub:   true,
			expectedEdit:     true,
			expectedBash:     true,
			checkGitCommands: true,
		},
		{
			name:  "push to pull request branch adds git commands",
			tools: map[string]any{},
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expectedGitHub:   true,
			expectedEdit:     true,
			expectedBash:     true,
			checkGitCommands: true,
		},
		{
			name: "bash true converted to wildcard",
			tools: map[string]any{
				"bash": true,
			},
			expectedGitHub:    true,
			expectedBash:      true,
			checkBashWildcard: true,
		},
		{
			name: "bash false removes tool",
			tools: map[string]any{
				"bash": false,
			},
			expectedGitHub: true,
			expectedBash:   false,
		},
		{
			name: "bash nil adds default commands",
			tools: map[string]any{
				"bash": nil,
			},
			expectedGitHub:   true,
			expectedBash:     true,
			checkBashDefault: true,
		},
		{
			name: "bash with existing commands merges defaults",
			tools: map[string]any{
				"bash": []any{"custom:cmd"},
			},
			expectedGitHub:   true,
			expectedBash:     true,
			checkBashDefault: true,
		},
		{
			name: "bash empty array left as-is",
			tools: map[string]any{
				"bash": []any{},
			},
			expectedGitHub:       true,
			expectedBash:         true,
			expectedBashCommands: []string{}, // Empty means no tools allowed
		},
		{
			name: "github with allowed tools",
			tools: map[string]any{
				"github": map[string]any{
					"allowed": []any{"issue_read", "create_issue"},
				},
			},
			expectedGitHub: true,
		},
		{
			name:  "sandbox disabled explicitly",
			tools: map[string]any{},
			sandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Disabled: true,
				},
			},
			expectedGitHub: true,
			expectedEdit:   false,
			expectedBash:   false,
		},
		{
			name: "edit tool already exists",
			tools: map[string]any{
				"edit": map[string]any{},
			},
			sandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID: "awf",
				},
			},
			expectedGitHub: true,
			expectedEdit:   true, // Should still exist
			expectedBash:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiler{}
			result := c.applyDefaultTools(tt.tools, tt.safeOutputs, tt.sandboxConfig, tt.networkPermissions)

			assert.NotNil(t, result, "Result should not be nil")

			// Check github tool
			_, githubExists := result["github"]
			assert.Equal(t, tt.expectedGitHub, githubExists, "GitHub tool existence mismatch")

			// Check edit tool
			_, editExists := result["edit"]
			assert.Equal(t, tt.expectedEdit, editExists, "Edit tool existence mismatch")

			// Check bash tool
			bashTool, bashExists := result["bash"]
			assert.Equal(t, tt.expectedBash, bashExists, "Bash tool existence mismatch")

			if tt.checkBashWildcard && bashExists {
				bashArray, ok := bashTool.([]any)
				require.True(t, ok, "Bash tool should be array")
				require.Len(t, bashArray, 1, "Bash tool should have one element")
				assert.Equal(t, "*", bashArray[0], "Bash tool should have wildcard")
			}

			if tt.checkGitCommands && bashExists {
				bashArray, ok := bashTool.([]any)
				require.True(t, ok, "Bash tool should be array")
				// Convert to strings for easier checking
				var commands []string
				for _, cmd := range bashArray {
					if cmdStr, ok := cmd.(string); ok {
						commands = append(commands, cmdStr)
					}
				}
				// Should have git commands
				hasGitCommand := false
				for _, cmd := range commands {
					if cmd == "git status" || cmd == "git add:*" {
						hasGitCommand = true
						break
					}
				}
				assert.True(t, hasGitCommand, "Should have git commands")
			}

			if tt.checkBashDefault && bashExists {
				bashArray, ok := bashTool.([]any)
				require.True(t, ok, "Bash tool should be array")
				assert.NotEmpty(t, bashArray, "Bash tool should have default commands")
			}

			if len(tt.expectedBashCommands) == 0 && bashExists {
				if bashArray, ok := bashTool.([]any); ok {
					if len(tt.expectedBashCommands) == 0 && len(bashArray) == 0 {
						// This is the expected empty array case
						assert.Empty(t, bashArray, "Bash tool should be empty array")
					}
				}
			}
		})
	}
}

// TestCompilerNeedsGitCommands tests git command detection helper
func TestCompilerNeedsGitCommands(t *testing.T) {
	tests := []struct {
		name        string
		safeOutputs *SafeOutputsConfig
		expected    bool
	}{
		{
			name:        "nil safe outputs",
			safeOutputs: nil,
			expected:    false,
		},
		{
			name:        "empty safe outputs",
			safeOutputs: &SafeOutputsConfig{},
			expected:    false,
		},
		{
			name: "create pull requests needs git",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expected: true,
		},
		{
			name: "push to pull request branch needs git",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expected: true,
		},
		{
			name: "both create and push need git",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests:      &CreatePullRequestsConfig{},
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expected: true,
		},
		{
			name: "other safe outputs don't need git",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
				AddComments:  &AddCommentsConfig{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := needsGitCommands(tt.safeOutputs)
			assert.Equal(t, tt.expected, result, "Git command detection mismatch")
		})
	}
}

// TestCompilerIsSandboxEnabled tests sandbox detection logic helper
func TestCompilerIsSandboxEnabled(t *testing.T) {
	tests := []struct {
		name               string
		sandboxConfig      *SandboxConfig
		networkPermissions *NetworkPermissions
		expected           bool
	}{
		{
			name:               "nil configs",
			sandboxConfig:      nil,
			networkPermissions: nil,
			expected:           false,
		},
		{
			name: "sandbox explicitly disabled",
			sandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Disabled: true,
				},
			},
			expected: false,
		},
		{
			name: "sandbox AWF enabled",
			sandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID: "awf",
				},
			},
			expected: true,
		},
		{
			name: "sandbox SRT enabled via ID",
			sandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID: "srt",
				},
			},
			expected: true,
		},
		{
			name: "sandbox SRT enabled via Type (legacy)",
			sandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeSRT,
				},
			},
			expected: true,
		},
		{
			name: "sandbox default type enabled",
			sandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID: "default",
				},
			},
			expected: true,
		},
		{
			name: "legacy type field SRT",
			sandboxConfig: &SandboxConfig{
				Type: SandboxTypeSRT,
			},
			expected: true,
		},
		{
			name: "legacy type field runtime",
			sandboxConfig: &SandboxConfig{
				Type: SandboxTypeRuntime,
			},
			expected: true,
		},
		{
			name:          "firewall enabled auto-enables sandbox",
			sandboxConfig: nil,
			networkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
			expected: true,
		},
		{
			name: "firewall disabled",
			networkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: false,
				},
			},
			expected: false,
		},
		{
			name: "sandbox disabled overrides firewall",
			sandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Disabled: true,
				},
			},
			networkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
			expected: false,
		},
		{
			name: "unsupported sandbox type",
			sandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID: "unknown",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSandboxEnabled(tt.sandboxConfig, tt.networkPermissions)
			assert.Equal(t, tt.expected, result, "Sandbox detection mismatch")
		})
	}
}

// TestParseOnSectionWithParsedFrontmatter tests parseOnSection using cached parsed frontmatter
func TestParseOnSectionWithParsedFrontmatter(t *testing.T) {
	tests := []struct {
		name              string
		parsedFrontmatter *FrontmatterConfig
		expectedReaction  string
		expectedError     bool
	}{
		{
			name: "cached on field used",
			parsedFrontmatter: &FrontmatterConfig{
				On: map[string]any{
					"reaction": "rocket",
				},
			},
			expectedReaction: "rocket",
			expectedError:    false,
		},
		{
			name: "cached on with slash_command",
			parsedFrontmatter: &FrontmatterConfig{
				On: map[string]any{
					"slash_command": map[string]any{},
					"reaction":      "heart",
				},
			},
			expectedReaction: "heart",
			expectedError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiler{}
			workflowData := &WorkflowData{
				ParsedFrontmatter: tt.parsedFrontmatter,
			}
			err := c.parseOnSection(map[string]any{}, workflowData, "/path/to/test.md")

			if tt.expectedError {
				assert.Error(t, err, "Expected an error but got none")
			} else {
				require.NoError(t, err, "Expected no error but got one")
				if tt.expectedReaction != "" {
					assert.Equal(t, tt.expectedReaction, workflowData.AIReaction, "Reaction mismatch")
				}
			}
		})
	}
}

// TestCompilerMergeSafeJobsFromIncludesEdgeCases tests edge cases for safe-jobs merging
func TestCompilerMergeSafeJobsFromIncludesEdgeCases(t *testing.T) {
	tests := []struct {
		name                string
		topSafeJobs         map[string]*SafeJobConfig
		includedContentJSON string
		expectNil           bool
		expectError         bool
	}{
		{
			name: "malformed safe-outputs structure",
			includedContentJSON: `{
				"safe-outputs": "not a map"
			}`,
			topSafeJobs: map[string]*SafeJobConfig{"job1": {}},
			expectError: false, // Should handle gracefully
		},
		{
			name: "safe-outputs.jobs not a map",
			includedContentJSON: `{
				"safe-outputs": {
					"jobs": "not a map"
				}
			}`,
			topSafeJobs: map[string]*SafeJobConfig{"job1": {}},
			expectError: false,
		},
		{
			name: "deeply nested invalid structure",
			includedContentJSON: `{
				"safe-outputs": {
					"jobs": {
						"job1": {
							"nested": {
								"invalid": "structure"
							}
						}
					}
				}
			}`,
			topSafeJobs: map[string]*SafeJobConfig{"job2": {}},
			expectError: false, // Should merge without error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiler{}
			result, err := c.mergeSafeJobsFromIncludes(tt.topSafeJobs, tt.includedContentJSON)

			if tt.expectError {
				assert.Error(t, err, "Expected an error but got none")
			} else {
				require.NoError(t, err, "Expected no error but got one")
				if !tt.expectNil {
					assert.NotNil(t, result, "Result should not be nil")
				}
			}
		})
	}
}

// TestApplyDefaultToolsGitHubConfigPreservation tests that GitHub tool config is preserved
func TestApplyDefaultToolsGitHubConfigPreservation(t *testing.T) {
	tests := []struct {
		name            string
		tools           map[string]any
		expectedMode    string
		expectedVersion string
		expectedAllowed []string
		checkConfig     bool
	}{
		{
			name: "github config with mode and version preserved",
			tools: map[string]any{
				"github": map[string]any{
					"mode":    "local",
					"version": "1.0.0",
				},
			},
			checkConfig:     true,
			expectedMode:    "local",
			expectedVersion: "1.0.0",
		},
		{
			name: "github config with allowed tools preserved",
			tools: map[string]any{
				"github": map[string]any{
					"allowed": []any{"issue_read", "create_issue"},
				},
			},
			checkConfig:     true,
			expectedAllowed: []string{"issue_read", "create_issue"},
		},
		{
			name: "github nil creates default config",
			tools: map[string]any{
				"github": nil,
			},
			checkConfig: false, // Default config, nothing specific to check
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiler{}
			result := c.applyDefaultTools(tt.tools, nil, nil, nil)

			githubTool, exists := result["github"]
			require.True(t, exists, "GitHub tool should exist")

			if tt.checkConfig {
				githubMap, ok := githubTool.(map[string]any)
				require.True(t, ok, "GitHub tool should be a map")

				if tt.expectedMode != "" {
					assert.Equal(t, tt.expectedMode, githubMap["mode"], "Mode mismatch")
				}
				if tt.expectedVersion != "" {
					assert.Equal(t, tt.expectedVersion, githubMap["version"], "Version mismatch")
				}
				if len(tt.expectedAllowed) > 0 {
					allowedAny, exists := githubMap["allowed"]
					require.True(t, exists, "Allowed field should exist")
					allowedSlice, ok := allowedAny.([]any)
					require.True(t, ok, "Allowed should be a slice")
					var allowedStrs []string
					for _, item := range allowedSlice {
						if str, ok := item.(string); ok {
							allowedStrs = append(allowedStrs, str)
						}
					}
					assert.Equal(t, tt.expectedAllowed, allowedStrs, "Allowed tools mismatch")
				}
			}
		})
	}
}

// TestApplyDefaultToolsBashWithGitAndDefaults tests bash tool with both git commands and defaults
func TestApplyDefaultToolsBashWithGitAndDefaults(t *testing.T) {
	c := &Compiler{}

	// Test that git commands are added when needed, but not default commands
	tools := map[string]any{}
	safeOutputs := &SafeOutputsConfig{
		CreatePullRequests: &CreatePullRequestsConfig{},
	}

	result := c.applyDefaultTools(tools, safeOutputs, nil, nil)

	bashTool, exists := result["bash"]
	require.True(t, exists, "Bash tool should exist")

	bashArray, ok := bashTool.([]any)
	require.True(t, ok, "Bash tool should be array")
	require.NotEmpty(t, bashArray, "Bash tool should have commands")

	// Check for git commands
	hasGitCommand := false
	for _, cmd := range bashArray {
		if cmdStr, ok := cmd.(string); ok {
			if cmdStr == "git status" || cmdStr == "git add:*" {
				hasGitCommand = true
				break
			}
		}
	}
	assert.True(t, hasGitCommand, "Should have git commands when CreatePullRequests is configured")
}

// TestCompilerGenerateJobNameUnicodeHandling tests that Unicode characters are handled
func TestCompilerGenerateJobNameUnicodeHandling(t *testing.T) {
	tests := []struct {
		name             string
		workflowName     string
		shouldHavePrefix bool
	}{
		{
			name:             "emoji in name",
			workflowName:     "test 🚀 workflow",
			shouldHavePrefix: false, // Emoji is removed, starts with 't'
		},
		{
			name:             "japanese characters",
			workflowName:     "テスト workflow",
			shouldHavePrefix: true, // Starts with non-ASCII, needs prefix
		},
		{
			name:             "accented characters",
			workflowName:     "café workflow",
			shouldHavePrefix: false, // Starts with 'c'
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiler{}
			result := c.generateJobName(tt.workflowName)

			hasPrefix := len(result) > 9 && result[:9] == "workflow-"
			assert.Equal(t, tt.shouldHavePrefix, hasPrefix, "Prefix presence mismatch")
		})
	}
}

// TestCompilerMergeSafeJobsFromIncludedConfigsMultipleConflicts tests multiple config merging with conflicts
func TestCompilerMergeSafeJobsFromIncludedConfigsMultipleConflicts(t *testing.T) {
	c := &Compiler{}

	topSafeJobs := map[string]*SafeJobConfig{
		"job1": {},
	}

	// Test that the extractSafeJobsFromFrontmatter function is actually called
	// and jobs are extracted when the structure has the jobs field directly
	includedConfigs := []string{
		`{"jobs": {"job2": {}}}`,
		`{"jobs": {"job3": {}}}`,
		"invalid",
		"",
	}

	result, err := c.mergeSafeJobsFromIncludedConfigs(topSafeJobs, includedConfigs)

	require.NoError(t, err, "Should handle malformed configs gracefully")
	assert.NotNil(t, result, "Result should not be nil")
	// The function looks for safe-outputs.jobs structure, so configs with just "jobs" will extract jobs
	assert.GreaterOrEqual(t, len(result), 1, "Should have at least original job")
	_, exists := result["job1"]
	assert.True(t, exists, "Original job should still exist")
}

// TestApplyDefaultToolsComplexScenarios tests complex tool configurations
func TestApplyDefaultToolsComplexScenarios(t *testing.T) {
	tests := []struct {
		name               string
		tools              map[string]any
		safeOutputs        *SafeOutputsConfig
		sandboxConfig      *SandboxConfig
		networkPermissions *NetworkPermissions
		validateFunc       func(*testing.T, map[string]any)
	}{
		{
			name:  "sandbox and git commands both needed",
			tools: map[string]any{},
			sandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{ID: "awf"},
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			validateFunc: func(t *testing.T, result map[string]any) {
				// Should have edit tool
				_, editExists := result["edit"]
				assert.True(t, editExists, "Edit tool should exist")

				// Should have bash - when sandbox adds wildcard first, git command logic skips
				bashTool, bashExists := result["bash"]
				require.True(t, bashExists, "Bash tool should exist")

				bashArray, ok := bashTool.([]any)
				require.True(t, ok, "Bash tool should be array")
				require.NotEmpty(t, bashArray, "Bash should have commands")

				// When sandbox is enabled first, it adds wildcard, so git commands are not added
				// (wildcard already allows all commands including git)
				hasWildcard := false
				for _, cmd := range bashArray {
					if cmdStr, ok := cmd.(string); ok && cmdStr == "*" {
						hasWildcard = true
						break
					}
				}
				// Either has wildcard (sandbox first) or has git commands (git first)
				// In this case sandbox runs first, so wildcard should be present
				assert.True(t, hasWildcard || len(bashArray) > 5, "Should have wildcard or multiple commands")
			},
		},
		{
			name: "existing bash with custom commands merged with git",
			tools: map[string]any{
				"bash": []any{"custom:command"},
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			validateFunc: func(t *testing.T, result map[string]any) {
				bashTool, exists := result["bash"]
				require.True(t, exists, "Bash tool should exist")

				bashArray, ok := bashTool.([]any)
				require.True(t, ok, "Bash tool should be array")

				// Should have both custom and git commands
				hasCustom := false
				hasGit := false
				for _, cmd := range bashArray {
					if cmdStr, ok := cmd.(string); ok {
						if cmdStr == "custom:command" {
							hasCustom = true
						}
						if cmdStr == "git status" {
							hasGit = true
						}
					}
				}
				assert.True(t, hasCustom, "Should preserve custom command")
				assert.True(t, hasGit, "Should add git command")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiler{}
			result := c.applyDefaultTools(tt.tools, tt.safeOutputs, tt.sandboxConfig, tt.networkPermissions)

			assert.NotNil(t, result, "Result should not be nil")
			if tt.validateFunc != nil {
				tt.validateFunc(t, result)
			}
		})
	}
}

// TestParseOnSectionReactionMapFormat tests reaction with map format
func TestParseOnSectionReactionMapFormat(t *testing.T) {
	// This test covers the case where reaction might be provided as a map
	// though the current implementation expects string or int
	c := &Compiler{}
	workflowData := &WorkflowData{}

	// Test that invalid type (map) is handled
	frontmatter := map[string]any{
		"on": map[string]any{
			"reaction": map[string]any{
				"type": "heart",
			},
		},
	}

	err := c.parseOnSection(frontmatter, workflowData, "/path/to/test.md")

	// The parseReactionValue function should return an error for map type
	assert.Error(t, err, "Should error on map type reaction")
}

// TestCompilerNeedsGitCommandsAllOutputTypes tests all safe output types for git command requirements
func TestCompilerNeedsGitCommandsAllOutputTypes(t *testing.T) {
	// Comprehensive test of all safe output types
	allOutputTypes := &SafeOutputsConfig{
		CreateIssues:            &CreateIssuesConfig{},
		AddComments:             &AddCommentsConfig{},
		AddLabels:               &AddLabelsConfig{},
		CreatePullRequests:      &CreatePullRequestsConfig{},
		PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
		CreateDiscussions:       &CreateDiscussionsConfig{},
	}

	// Should need git commands because of CreatePullRequests and PushToPullRequestBranch
	result := needsGitCommands(allOutputTypes)
	assert.True(t, result, "Should need git commands with PR-related outputs")

	// Test without PR-related outputs
	nonPROutputs := &SafeOutputsConfig{
		CreateIssues:      &CreateIssuesConfig{},
		AddComments:       &AddCommentsConfig{},
		AddLabels:         &AddLabelsConfig{},
		CreateDiscussions: &CreateDiscussionsConfig{},
	}

	result = needsGitCommands(nonPROutputs)
	assert.False(t, result, "Should not need git commands without PR-related outputs")
}

// TestCompilerIsSandboxEnabledPrecedence tests precedence rules for sandbox detection
func TestCompilerIsSandboxEnabledPrecedence(t *testing.T) {
	// Test that disabled flag takes precedence over all other settings
	config := &SandboxConfig{
		Agent: &AgentSandboxConfig{
			ID:       "awf",
			Type:     SandboxTypeSRT,
			Disabled: true,
		},
		Type: SandboxTypeSRT,
	}
	networkPerms := &NetworkPermissions{
		Firewall: &FirewallConfig{Enabled: true},
	}

	result := isSandboxEnabled(config, networkPerms)
	assert.False(t, result, "Disabled flag should take precedence over all other settings")

	// Test that ID field takes precedence over Type field
	config2 := &SandboxConfig{
		Agent: &AgentSandboxConfig{
			ID:   "awf",
			Type: "unknown",
		},
	}

	result = isSandboxEnabled(config2, nil)
	assert.True(t, result, "ID field should take precedence over Type field")
}

// TestCompilerGenerateJobNameAllSpecialChars tests all special character replacements
func TestCompilerGenerateJobNameAllSpecialChars(t *testing.T) {
	c := &Compiler{}

	// Test string with all special characters
	input := `Test:Workflow.Name,Here/There\Back@Email'Single"Double()Parens`
	result := c.generateJobName(input)

	// Should only contain lowercase letters, numbers, hyphens, and underscores
	assert.NotContains(t, result, ":", "Colon should be removed")
	assert.NotContains(t, result, ".", "Period should be removed")
	assert.NotContains(t, result, ",", "Comma should be removed")
	assert.NotContains(t, result, "/", "Forward slash should be removed")
	assert.NotContains(t, result, "\\", "Backslash should be removed")
	assert.NotContains(t, result, "@", "At symbol should be removed")
	assert.NotContains(t, result, "'", "Single quote should be removed")
	assert.NotContains(t, result, "\"", "Double quote should be removed")
	assert.NotContains(t, result, "(", "Open paren should be removed")
	assert.NotContains(t, result, ")", "Close paren should be removed")
	assert.NotContains(t, result, "--", "Multiple hyphens should be collapsed")
}

// TestCompilerMergeSafeJobsFromIncludesJSONEdgeCases tests JSON parsing edge cases
func TestCompilerMergeSafeJobsFromIncludesJSONEdgeCases(t *testing.T) {
	tests := []struct {
		name                string
		includedContentJSON string
		expectSuccess       bool
	}{
		{
			name:                "null value",
			includedContentJSON: "null",
			expectSuccess:       true, // Should handle gracefully
		},
		{
			name:                "array instead of object",
			includedContentJSON: "[]",
			expectSuccess:       true,
		},
		{
			name:                "number",
			includedContentJSON: "123",
			expectSuccess:       true,
		},
		{
			name:                "boolean",
			includedContentJSON: "true",
			expectSuccess:       true,
		},
		{
			name:                "truncated JSON",
			includedContentJSON: `{"safe-outputs": {`,
			expectSuccess:       true, // Invalid JSON handled gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiler{}
			topSafeJobs := map[string]*SafeJobConfig{"job1": {}}

			result, err := c.mergeSafeJobsFromIncludes(topSafeJobs, tt.includedContentJSON)

			if tt.expectSuccess {
				require.NoError(t, err, "Should handle edge case gracefully")
				assert.NotNil(t, result, "Result should not be nil")
				// Should preserve original jobs
				_, exists := result["job1"]
				assert.True(t, exists, "Original job should be preserved")
			} else {
				require.Error(t, err, "Should error on invalid input")
			}
		})
	}
}

// TestApplyDefaultToolsNilInputRecovery tests that nil inputs are handled safely
func TestApplyDefaultToolsNilInputRecovery(t *testing.T) {
	c := &Compiler{}

	// All nil inputs should not panic
	result := c.applyDefaultTools(nil, nil, nil, nil)
	assert.NotNil(t, result, "Should return non-nil map")

	// Should create github tool by default
	_, githubExists := result["github"]
	assert.True(t, githubExists, "Should create default github tool")
}
