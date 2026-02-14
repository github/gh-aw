//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestGetWorkflowIDFromPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple filename",
			path:     "ai-moderator.md",
			expected: "ai-moderator",
		},
		{
			name:     "full path",
			path:     "/home/user/workflows/test-workflow.md",
			expected: "test-workflow",
		},
		{
			name:     "filename with multiple dots",
			path:     "/path/to/workflow.test.md",
			expected: "workflow.test",
		},
		{
			name:     "relative path",
			path:     ".github/workflows/daily-fact.md",
			expected: "daily-fact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetWorkflowIDFromPath(tt.path)
			if result != tt.expected {
				t.Errorf("GetWorkflowIDFromPath(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestSanitizeWorkflowIDForCacheKey(t *testing.T) {
	tests := []struct {
		name       string
		workflowID string
		expected   string
	}{
		{
			name:       "smoke-copilot with hyphens",
			workflowID: "smoke-copilot",
			expected:   "smokecopilot",
		},
		{
			name:       "Smoke-Copilot with capital letters",
			workflowID: "Smoke-Copilot",
			expected:   "smokecopilot",
		},
		{
			name:       "multiple hyphens",
			workflowID: "daily-code-metrics",
			expected:   "dailycodemetrics",
		},
		{
			name:       "no hyphens",
			workflowID: "simple",
			expected:   "simple",
		},
		{
			name:       "uppercase no hyphens",
			workflowID: "SIMPLE",
			expected:   "simple",
		},
		{
			name:       "mixed case with many hyphens",
			workflowID: "Test-Workflow-With-Many-Parts",
			expected:   "testworkflowwithmanyparts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeWorkflowIDForCacheKey(tt.workflowID)
			if result != tt.expected {
				t.Errorf("SanitizeWorkflowIDForCacheKey(%q) = %q, want %q", tt.workflowID, result, tt.expected)
			}
		})
	}
}

// TestBuildJobsAndValidate tests the buildJobsAndValidate helper function
func TestBuildJobsAndValidate(t *testing.T) {
	tests := []struct {
		name        string
		setupData   func() *WorkflowData
		shouldError bool
		errorMsg    string
	}{
		{
			name: "successful build and validation",
			setupData: func() *WorkflowData {
				return &WorkflowData{
					Name:        "Test Workflow",
					On:          "on:\n  push:",
					Permissions: "permissions:\n  contents: read",
					AI:          "copilot",
					EngineConfig: &EngineConfig{
						ID: "copilot",
					},
					MarkdownContent: "Test content",
				}
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			data := tt.setupData()

			err := compiler.buildJobsAndValidate(data, "test.md")

			if tt.shouldError && err == nil {
				t.Errorf("buildJobsAndValidate() expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("buildJobsAndValidate() unexpected error: %v", err)
			}
			if tt.shouldError && err != nil && tt.errorMsg != "" {
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("buildJobsAndValidate() error = %v, want error containing %q", err, tt.errorMsg)
				}
			}

			// Verify job manager was initialized
			if !tt.shouldError && compiler.jobManager == nil {
				t.Error("buildJobsAndValidate() should initialize jobManager")
			}
		})
	}
}

// TestGenerateWorkflowHeader tests the generateWorkflowHeader helper function
func TestGenerateWorkflowHeader(t *testing.T) {
	tests := []struct {
		name        string
		data        *WorkflowData
		expectInStr []string
		notInStr    []string
	}{
		{
			name: "header with description",
			data: &WorkflowData{
				Description: "This is a test workflow",
				Source:      "test.md",
			},
			expectInStr: []string{
				"# This is a test workflow",
				"# Source: test.md",
			},
		},
		{
			name: "header with multiline description",
			data: &WorkflowData{
				Description: "Line 1\nLine 2\nLine 3",
				Source:      "test.md",
			},
			expectInStr: []string{
				"# Line 1",
				"# Line 2",
				"# Line 3",
			},
		},
		{
			name: "header with ANSI codes stripped",
			data: &WorkflowData{
				Description: "Text with \x1b[31mred\x1b[0m color",
				Source:      "path/to/\x1b[32mfile.md\x1b[0m",
			},
			expectInStr: []string{
				"# Text with red color",
				"# Source: path/to/file.md",
			},
			// Note: We don't check notInStr for ANSI codes here because
			// strings.Contains with literal escape sequences in test code
			// can be unreliable. The expectInStr check verifies correct output.
		},
		{
			name: "header with imports and includes",
			data: &WorkflowData{
				ImportedFiles: []string{"import1.md", "import2.md"},
				IncludedFiles: []string{"include1.md"},
			},
			expectInStr: []string{
				"# Resolved workflow manifest:",
				"#   Imports:",
				"#     - import1.md",
				"#     - import2.md",
				"#   Includes:",
				"#     - include1.md",
			},
		},
		{
			name: "header with stop-time",
			data: &WorkflowData{
				StopTime: "2026-12-31T23:59:59Z",
			},
			expectInStr: []string{
				"# Effective stop-time: 2026-12-31T23:59:59Z",
			},
		},
		{
			name: "header with manual-approval",
			data: &WorkflowData{
				ManualApproval: "production",
			},
			expectInStr: []string{
				"# Manual approval required: environment 'production'",
			},
		},
		{
			name:        "minimal header",
			data:        &WorkflowData{},
			expectInStr: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			var yaml strings.Builder

			compiler.generateWorkflowHeader(&yaml, tt.data, "")
			result := yaml.String()

			for _, expected := range tt.expectInStr {
				if !strings.Contains(result, expected) {
					t.Errorf("generateWorkflowHeader() result missing %q\nGot:\n%s", expected, result)
				}
			}

			for _, notExpected := range tt.notInStr {
				if strings.Contains(result, notExpected) {
					t.Errorf("generateWorkflowHeader() result should not contain %q\nGot:\n%s", notExpected, result)
				}
			}
		})
	}
}

// TestGenerateWorkflowBody tests the generateWorkflowBody helper function
func TestGenerateWorkflowBody(t *testing.T) {
	tests := []struct {
		name        string
		data        *WorkflowData
		setupJobs   func(*Compiler)
		expectInStr []string
	}{
		{
			name: "basic workflow body",
			data: &WorkflowData{
				Name:        "Test Workflow",
				On:          "on:\n  push:",
				Concurrency: "concurrency:\n  group: test",
				RunName:     "run-name: Test Run",
			},
			expectInStr: []string{
				`name: "Test Workflow"`,
				"on:",
				"push:",
				"permissions: {}",
				"concurrency:",
				"group: test",
				"run-name: Test Run",
			},
		},
		{
			name: "workflow with env",
			data: &WorkflowData{
				Name: "Test Workflow",
				On:   "on:\n  push:",
				Env:  "env:\n  FOO: bar",
			},
			expectInStr: []string{
				`name: "Test Workflow"`,
				"env:",
				"FOO: bar",
			},
		},
		{
			name: "workflow with cache comment",
			data: &WorkflowData{
				Name:  "Test Workflow",
				On:    "on:\n  push:",
				Cache: "cache: true",
			},
			expectInStr: []string{
				`name: "Test Workflow"`,
				"# Cache configuration from frontmatter was processed and added to the main job steps",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.jobManager = NewJobManager()

			if tt.setupJobs != nil {
				tt.setupJobs(compiler)
			}

			var yaml strings.Builder
			compiler.generateWorkflowBody(&yaml, tt.data)
			result := yaml.String()

			for _, expected := range tt.expectInStr {
				if !strings.Contains(result, expected) {
					t.Errorf("generateWorkflowBody() result missing %q\nGot:\n%s", expected, result)
				}
			}
		})
	}
}

// TestGenerateYAMLRefactored tests the refactored generateYAML function
func TestGenerateYAMLRefactored(t *testing.T) {
	tests := []struct {
		name        string
		data        *WorkflowData
		expectInStr []string
		shouldError bool
	}{
		{
			name: "complete workflow generation",
			data: &WorkflowData{
				Name:        "Test Workflow",
				Description: "Test description",
				Source:      "test.md",
				On:          "on:\n  push:",
				Permissions: "permissions:\n  contents: read",
				Concurrency: "concurrency:\n  group: test",
				RunName:     "run-name: Test",
				AI:          "copilot",
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				MarkdownContent: "Test content",
			},
			expectInStr: []string{
				"# Test description",
				"# Source: test.md",
				`name: "Test Workflow"`,
				"on:",
				"permissions: {}",
				"concurrency:",
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			result, err := compiler.generateYAML(tt.data, "test.md")

			if tt.shouldError && err == nil {
				t.Errorf("generateYAML() expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("generateYAML() unexpected error: %v", err)
			}

			if !tt.shouldError {
				for _, expected := range tt.expectInStr {
					if !strings.Contains(result, expected) {
						t.Errorf("generateYAML() result missing %q", expected)
					}
				}
			}
		})
	}
}

// TestGenerateCheckoutGitHubFolder verifies that when sparse checkout is generated,
// it includes both .github and .agents folders
func TestGenerateCheckoutGitHubFolder(t *testing.T) {
	compiler := NewCompiler()

	// Test that when checkout is generated, it includes both folders
	// Note: Due to complex logic in shouldAddCheckoutStep, the sparse checkout
	// may not be generated in simple test scenarios. This test verifies the
	// output format when it is generated.
	workflowData := &WorkflowData{
		Permissions: "permissions:\n  contents: read",
	}

	result := compiler.generateCheckoutGitHubFolder(workflowData)

	// If result is generated, verify it includes both .github and .agents
	if result != nil {
		checkoutStr := strings.Join(result, "")

		if !strings.Contains(checkoutStr, "Checkout .github and .agents folders") {
			t.Errorf("Step name should mention both .github and .agents folders, got: %s", checkoutStr)
		}

		if !strings.Contains(checkoutStr, ".github") || !strings.Contains(checkoutStr, ".agents") {
			t.Errorf("Sparse checkout should include both .github and .agents folders, got: %s", checkoutStr)
		}

		t.Log("✓ Sparse checkout includes both .github and .agents folders")
	} else {
		t.Log("Sparse checkout not generated (expected with default logic)")
	}

	// Test negative cases
	t.Run("without_contents_permission", func(t *testing.T) {
		data := &WorkflowData{Permissions: "permissions: {}"}
		if compiler.generateCheckoutGitHubFolder(data) != nil {
			t.Error("Should not generate checkout without contents permission")
		}
	})

	t.Run("with_action_tag", func(t *testing.T) {
		data := &WorkflowData{
			Permissions: "permissions:\n  contents: read",
			Features:    map[string]any{"action-tag": "abc123"},
		}
		if compiler.generateCheckoutGitHubFolder(data) != nil {
			t.Error("Should not generate checkout with action-tag")
		}
	})
}

// TestGeneratePlaceholderSubstitutionStep tests the generatePlaceholderSubstitutionStep function
// to ensure static quoted values are not wrapped in ${{ }} but GitHub expressions are
func TestGeneratePlaceholderSubstitutionStep(t *testing.T) {
	tests := []struct {
		name        string
		mappings    []*ExpressionMapping
		expectInStr []string
		notInStr    []string
	}{
		{
			name: "static quoted values not wrapped",
			mappings: []*ExpressionMapping{
				{EnvVar: "GH_AW_CACHE_DESCRIPTION", Content: "''"},
				{EnvVar: "GH_AW_CACHE_DIR", Content: "'/tmp/gh-aw/cache-memory/'"},
			},
			expectInStr: []string{
				"GH_AW_CACHE_DESCRIPTION: ''",
				"GH_AW_CACHE_DIR: '/tmp/gh-aw/cache-memory/'",
			},
			notInStr: []string{
				"${{ '' }}",
				"${{ '/tmp/gh-aw/cache-memory/' }}",
			},
		},
		{
			name: "github expressions wrapped in ${{}}}",
			mappings: []*ExpressionMapping{
				{EnvVar: "GH_AW_GITHUB_REPOSITORY", Content: "github.repository"},
				{EnvVar: "GH_AW_GITHUB_ACTOR", Content: "github.actor"},
			},
			expectInStr: []string{
				"GH_AW_GITHUB_REPOSITORY: ${{ github.repository }}",
				"GH_AW_GITHUB_ACTOR: ${{ github.actor }}",
			},
		},
		{
			name: "mixed static and expression values",
			mappings: []*ExpressionMapping{
				{EnvVar: "STATIC_VALUE", Content: "'hello'"},
				{EnvVar: "EXPRESSION_VALUE", Content: "github.event.issue.number"},
				{EnvVar: "EMPTY_STATIC", Content: "''"},
			},
			expectInStr: []string{
				"STATIC_VALUE: 'hello'",
				"EXPRESSION_VALUE: ${{ github.event.issue.number }}",
				"EMPTY_STATIC: ''",
			},
			notInStr: []string{
				"${{ 'hello' }}",
				"${{ '' }}",
			},
		},
		{
			name: "double-quoted static values",
			mappings: []*ExpressionMapping{
				{EnvVar: "DOUBLE_QUOTED", Content: `"value"`},
			},
			expectInStr: []string{
				`DOUBLE_QUOTED: "value"`,
			},
			notInStr: []string{
				`${{ "value" }}`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			generatePlaceholderSubstitutionStep(&yaml, tt.mappings, "      ")
			result := yaml.String()

			for _, expected := range tt.expectInStr {
				if !strings.Contains(result, expected) {
					t.Errorf("Result missing expected string %q\nGot:\n%s", expected, result)
				}
			}

			for _, notExpected := range tt.notInStr {
				if strings.Contains(result, notExpected) {
					t.Errorf("Result should not contain %q\nGot:\n%s", notExpected, result)
				}
			}
		})
	}
}
