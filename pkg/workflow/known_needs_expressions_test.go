//go:build !integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateKnownNeedsExpressions(t *testing.T) {
	tests := []struct {
		name                    string
		data                    *WorkflowData
		preActivationJobCreated bool
		expectedMinCount        int
		checkExpressions        []string
		notExpectedExprs        []string
	}{
		{
			name:                    "basic pre_activation job only - no command",
			data:                    &WorkflowData{},
			preActivationJobCreated: true,
			expectedMinCount:        1, // Only activated output (no matched_command without command trigger)
			checkExpressions: []string{
				"needs.pre_activation.outputs.activated",
			},
			notExpectedExprs: []string{
				"needs.pre_activation.outputs.matched_command", // No command trigger, so not included
				"needs.activation.outputs.text",                // Activation is the current job
				"needs.agent.outputs.output",                   // Agent runs AFTER activation
				"needs.agent.outputs.detection_success",        // Agent runs AFTER activation (detection is a separate job)
			},
		},
		{
			name: "pre_activation with command - includes matched_command",
			data: &WorkflowData{
				Command: []string{"/bot"},
			},
			preActivationJobCreated: true,
			expectedMinCount:        2, // activated + matched_command
			checkExpressions: []string{
				"needs.pre_activation.outputs.activated",
				"needs.pre_activation.outputs.matched_command",
			},
		},
		{
			name:                    "no pre_activation job - no pre_activation outputs",
			data:                    &WorkflowData{},
			preActivationJobCreated: false,
			expectedMinCount:        0,
			notExpectedExprs: []string{
				"needs.pre_activation.outputs.activated",
				"needs.pre_activation.outputs.matched_command",
			},
		},
		{
			name: "with custom job before activation",
			data: &WorkflowData{
				Jobs: map[string]any{
					"custom_job": map[string]any{
						"runs-on": "ubuntu-latest",
						"needs":   "pre_activation", // Must explicitly depend on pre_activation
					},
				},
			},
			preActivationJobCreated: true,
			checkExpressions: []string{
				"needs.pre_activation.outputs.activated",
				"needs.custom_job.outputs.output",
			},
			notExpectedExprs: []string{
				"needs.agent.outputs.output", // Agent runs AFTER activation
			},
		},
		{
			name: "with custom job before activation and explicit outputs",
			data: &WorkflowData{
				Jobs: map[string]any{
					"select": map[string]any{
						"runs-on": "ubuntu-latest",
						"needs":   "pre_activation",
						"outputs": map[string]any{
							"issue_numbers": "${{ steps.values.outputs.issue_numbers }}",
							"marker":        "${{ steps.values.outputs.marker }}",
						},
					},
				},
			},
			preActivationJobCreated: true,
			checkExpressions: []string{
				"needs.pre_activation.outputs.activated",
				"needs.select.outputs.issue_numbers",
				"needs.select.outputs.marker",
			},
			notExpectedExprs: []string{
				"needs.select.outputs.output",
			},
		},
		{
			name: "with custom job without explicit needs",
			data: &WorkflowData{
				Jobs: map[string]any{
					"custom_job": map[string]any{
						"runs-on": "ubuntu-latest",
						// No needs field - will get needs: activation added automatically
					},
				},
			},
			preActivationJobCreated: true,
			checkExpressions: []string{
				"needs.pre_activation.outputs.activated",
			},
			notExpectedExprs: []string{
				"needs.custom_job.outputs.output", // Runs AFTER activation (auto-added dependency)
			},
		},
		{
			name: "with custom job after activation",
			data: &WorkflowData{
				Jobs: map[string]any{
					"custom_job": map[string]any{
						"runs-on": "ubuntu-latest",
						"needs":   "activation", // Depends on activation - runs AFTER
					},
				},
			},
			preActivationJobCreated: true,
			checkExpressions: []string{
				"needs.pre_activation.outputs.activated",
			},
			notExpectedExprs: []string{
				"needs.custom_job.outputs.output", // Runs AFTER activation
			},
		},
		{
			name: "safe outputs should not be included",
			data: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					CreateIssues: &CreateIssuesConfig{},
				},
			},
			preActivationJobCreated: true,
			checkExpressions: []string{
				"needs.pre_activation.outputs.activated",
			},
			notExpectedExprs: []string{
				"needs.create_issue.outputs.issue_url", // Safe outputs run AFTER activation
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mappings := generateKnownNeedsExpressions(tt.data, tt.preActivationJobCreated)

			// Check minimum count if specified
			if tt.expectedMinCount > 0 {
				assert.GreaterOrEqual(t, len(mappings), tt.expectedMinCount,
					"Should generate at least %d expressions", tt.expectedMinCount)
			}

			// Build a map for easy lookup
			exprMap := make(map[string]*ExpressionMapping)
			for _, mapping := range mappings {
				exprMap[mapping.Content] = mapping
			}

			// Check specific expressions that should be present
			for _, expr := range tt.checkExpressions {
				mapping, found := exprMap[expr]
				assert.True(t, found, "Expected expression %s to be generated", expr)
				if found {
					assert.NotEmpty(t, mapping.EnvVar, "EnvVar should not be empty for %s", expr)
					assert.Contains(t, mapping.EnvVar, "GH_AW_NEEDS_", "EnvVar should have GH_AW_NEEDS_ prefix")
					assert.Equal(t, expr, mapping.Content, "Content should match expression")
				}
			}

			// Check expressions that should NOT be present
			for _, expr := range tt.notExpectedExprs {
				_, found := exprMap[expr]
				assert.False(t, found, "Did not expect expression %s to be generated (runs after activation)", expr)
			}
		})
	}
}

func TestNormalizeJobNameForEnvVar(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"activation", "ACTIVATION"},
		{"pre_activation", "PRE_ACTIVATION"},
		{"agent", "AGENT"},
		{"my-custom-job", "MY_CUSTOM_JOB"},
		{"job_with_numbers_123", "JOB_WITH_NUMBERS_123"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeJobNameForEnvVar(tt.input)
			assert.Equal(t, tt.expected, result, "Job name normalization failed")
		})
	}
}

func TestGetCustomJobsBeforeActivation(t *testing.T) {
	tests := []struct {
		name         string
		data         *WorkflowData
		expectedJobs []string
	}{
		{
			name:         "no custom jobs",
			data:         &WorkflowData{},
			expectedJobs: []string{},
		},
		{
			name: "job with no dependencies runs AFTER activation (auto-added needs)",
			data: &WorkflowData{
				Jobs: map[string]any{
					"custom_job": map[string]any{
						"runs-on": "ubuntu-latest",
						// No needs field - will get needs: activation added automatically
					},
				},
			},
			expectedJobs: []string{}, // NOT included - runs after activation
		},
		{
			name: "job depending on pre_activation runs before activation",
			data: &WorkflowData{
				Jobs: map[string]any{
					"custom_job": map[string]any{
						"runs-on": "ubuntu-latest",
						"needs":   "pre_activation",
					},
				},
			},
			expectedJobs: []string{"custom_job"}, // Explicitly depends on pre_activation
		},
		{
			name: "job depending on activation runs after",
			data: &WorkflowData{
				Jobs: map[string]any{
					"custom_job": map[string]any{
						"runs-on": "ubuntu-latest",
						"needs":   "activation",
					},
				},
			},
			expectedJobs: []string{}, // Filtered out
		},
		{
			name: "job depending on agent runs after",
			data: &WorkflowData{
				Jobs: map[string]any{
					"custom_job": map[string]any{
						"runs-on": "ubuntu-latest",
						"needs":   "agent",
					},
				},
			},
			expectedJobs: []string{}, // Filtered out
		},
		{
			name: "job depending on both pre_activation and activation runs after",
			data: &WorkflowData{
				Jobs: map[string]any{
					"custom_job": map[string]any{
						"runs-on": "ubuntu-latest",
						"needs":   []string{"pre_activation", "activation"},
					},
				},
			},
			expectedJobs: []string{}, // Filtered out due to activation dependency
		},
		{
			name: "mixed jobs - only pre_activation dependencies included",
			data: &WorkflowData{
				Jobs: map[string]any{
					"job_no_needs": map[string]any{
						"runs-on": "ubuntu-latest",
						// Will get needs: activation added
					},
					"job_pre_activation": map[string]any{
						"runs-on": "ubuntu-latest",
						"needs":   "pre_activation",
					},
					"job_activation": map[string]any{
						"runs-on": "ubuntu-latest",
						"needs":   "activation",
					},
				},
			},
			expectedJobs: []string{"job_pre_activation"}, // Only explicit pre_activation dependency
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobNames := getCustomJobsBeforeActivation(tt.data)
			assert.ElementsMatch(t, tt.expectedJobs, jobNames,
				"Custom jobs before activation mismatch")
		})
	}
}

func TestGenerateKnownNeedsExpressions_EnvVarFormat(t *testing.T) {
	data := &WorkflowData{}
	mappings := generateKnownNeedsExpressions(data, true)

	require.NotEmpty(t, mappings, "Should generate at least some mappings")

	// Check that all env vars follow the correct format
	for _, mapping := range mappings {
		assert.Contains(t, mapping.EnvVar, "GH_AW_NEEDS_",
			"EnvVar should start with GH_AW_NEEDS_: %s", mapping.EnvVar)
		assert.Contains(t, mapping.EnvVar, "_OUTPUTS_",
			"EnvVar should contain _OUTPUTS_: %s", mapping.EnvVar)

		// Verify the expression content matches the expected format
		assert.Contains(t, mapping.Content, "needs.",
			"Content should contain 'needs.': %s", mapping.Content)
		assert.Contains(t, mapping.Content, ".outputs.",
			"Content should contain '.outputs.': %s", mapping.Content)
	}
}

func TestParseNeedsField(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected []string
	}{
		{
			name:     "string single dependency",
			input:    "activation",
			expected: []string{"activation"},
		},
		{
			name:     "string array",
			input:    []string{"activation", "agent"},
			expected: []string{"activation", "agent"},
		},
		{
			name:     "any array",
			input:    []any{"activation", "agent"},
			expected: []string{"activation", "agent"},
		},
		{
			name:     "empty array",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "nil input",
			input:    nil,
			expected: []string{},
		},
		{
			name:     "invalid type",
			input:    123,
			expected: []string{},
		},
		{
			name:     "mixed any array with non-strings",
			input:    []any{"activation", 123, "agent"},
			expected: []string{"activation", "agent"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseNeedsField(tt.input)
			assert.Equal(t, tt.expected, result, "parseNeedsField result mismatch")
		})
	}
}

func TestKnownNeedsExpressionsIntegration(t *testing.T) {
	// Create a temporary workflow with custom jobs
	tmpDir := t.TempDir()
	workflowPath := tmpDir + "/test-workflow.md"

	workflowContent := `---
engine: copilot
on: issues
permissions:
  issues: read
jobs:
  before_job:
    runs-on: ubuntu-latest
    needs: pre_activation
    steps:
      - run: echo "Before activation"
  after_job:
    runs-on: ubuntu-latest
    needs: activation
    steps:
      - run: echo "After activation"
---

# Test Workflow

This workflow has custom jobs before and after activation.
`
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	require.NoError(t, err, "Failed to create workflow file")

	// Compile the workflow
	compiler := NewCompiler()
	compiler.SetQuiet(true)
	err = compiler.CompileWorkflow(workflowPath)
	require.NoError(t, err, "Compilation failed")

	// Read the compiled workflow
	lockPath := tmpDir + "/test-workflow.lock.yml"
	lockContent, err := os.ReadFile(lockPath)
	require.NoError(t, err, "Failed to read lock file")
	lockStr := string(lockContent)

	// Verify that known needs expressions are in the substitution step
	assert.Contains(t, lockStr, "- name: Substitute placeholders", "Should have substitution step")

	// Find the substitution step and check it has the known needs expressions
	substStepStart := strings.Index(lockStr, "- name: Substitute placeholders")
	require.Positive(t, substStepStart, "Substitution step not found")

	// Get the next 100 lines after the substitution step
	substSection := lockStr[substStepStart:]
	nextStepIdx := strings.Index(substSection[50:], "- name:")
	if nextStepIdx > 0 {
		substSection = substSection[:50+nextStepIdx]
	}

	// Should have pre_activation activated expression in substitution step
	// (workflow uses 'on: issues' which creates a pre_activation job with permission check)
	assert.Contains(t, substSection, "GH_AW_NEEDS_PRE_ACTIVATION_OUTPUTS_ACTIVATED",
		"Substitution step should have pre_activation.outputs.activated")
	// Should NOT have matched_command since this workflow has no command trigger
	assert.NotContains(t, substSection, "GH_AW_NEEDS_PRE_ACTIVATION_OUTPUTS_MATCHED_COMMAND",
		"Substitution step should NOT have pre_activation.outputs.matched_command (no command trigger)")

	// Should have before_job expression in substitution step
	assert.Contains(t, substSection, "GH_AW_NEEDS_BEFORE_JOB_OUTPUTS_OUTPUT",
		"Substitution step should have before_job.outputs.output")

	// Should NOT have after_job expression (it depends on activation)
	assert.NotContains(t, substSection, "GH_AW_NEEDS_AFTER_JOB_OUTPUTS_OUTPUT",
		"Substitution step should NOT have after_job.outputs.output (runs after activation)")

	// Verify prompt creation step does NOT have the known needs expressions
	promptStepStart := strings.Index(lockStr, "- name: Create prompt with built-in context")
	require.Positive(t, promptStepStart, "Prompt creation step not found")

	// Get the prompt creation section (not currently used but kept for potential future checks)
	_ = lockStr[promptStepStart:]

	// Prompt creation should NOT have the known needs expressions (only markdown-extracted ones)
	// We can't make strong assertions here because markdown might contain needs expressions
	// But we can verify the structure is correct
	assert.Contains(t, lockStr, "- name: Create prompt with built-in context",
		"Should have prompt creation step")
}

func TestParseNeedsFieldArrayTypes(t *testing.T) {
	// Test with needs as array in jobs config (realistic scenario)
	tests := []struct {
		name     string
		input    any
		expected []string
	}{
		{
			name:     "array with single item",
			input:    []any{"pre_activation"},
			expected: []string{"pre_activation"},
		},
		{
			name:     "array with multiple items",
			input:    []any{"job1", "job2", "job3"},
			expected: []string{"job1", "job2", "job3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseNeedsField(tt.input)
			assert.Equal(t, tt.expected, result, "parseNeedsField result mismatch")
		})
	}
}

// ========================================
// filterExpressionsForActivation Tests
// ========================================

func TestFilterExpressionsForActivation(t *testing.T) {
	customJobs := map[string]any{
		"precompute": map[string]any{"runs-on": "ubuntu-latest"},
		"config":     map[string]any{"runs-on": "ubuntu-latest"},
	}

	tests := []struct {
		name                 string
		mappings             []*ExpressionMapping
		customJobs           map[string]any
		beforeActivationJobs []string
		expectedContents     []string
		excludedContents     []string
	}{
		{
			name:       "nil customJobs returns all mappings unchanged",
			customJobs: nil,
			mappings: []*ExpressionMapping{
				{Content: "needs.precompute.outputs.action", EnvVar: "GH_AW_NEEDS_PRECOMPUTE_OUTPUTS_ACTION"},
				{Content: "github.event.issue.number", EnvVar: "GH_AW_GITHUB_EVENT_ISSUE_NUMBER"},
			},
			beforeActivationJobs: nil,
			expectedContents:     []string{"needs.precompute.outputs.action", "github.event.issue.number"},
		},
		{
			name:                 "empty mappings returns empty slice",
			customJobs:           customJobs,
			mappings:             []*ExpressionMapping{},
			beforeActivationJobs: []string{"precompute"},
			expectedContents:     []string{},
		},
		{
			name:       "job in beforeActivationJobs is kept",
			customJobs: customJobs,
			mappings: []*ExpressionMapping{
				{Content: "needs.precompute.outputs.action", EnvVar: "GH_AW_NEEDS_PRECOMPUTE_OUTPUTS_ACTION"},
			},
			beforeActivationJobs: []string{"precompute"},
			expectedContents:     []string{"needs.precompute.outputs.action"},
		},
		{
			name:       "job NOT in beforeActivationJobs is filtered out",
			customJobs: customJobs,
			mappings: []*ExpressionMapping{
				{Content: "needs.config.outputs.release_tag", EnvVar: "GH_AW_NEEDS_CONFIG_OUTPUTS_RELEASE_TAG"},
			},
			beforeActivationJobs: []string{"precompute"}, // config is not before activation
			excludedContents:     []string{"needs.config.outputs.release_tag"},
		},
		{
			name:       "nil beforeActivationJobs filters all custom job expressions",
			customJobs: customJobs,
			mappings: []*ExpressionMapping{
				{Content: "needs.precompute.outputs.action", EnvVar: "GH_AW_NEEDS_PRECOMPUTE_OUTPUTS_ACTION"},
				{Content: "needs.config.outputs.release_tag", EnvVar: "GH_AW_NEEDS_CONFIG_OUTPUTS_RELEASE_TAG"},
				{Content: "github.event.issue.number", EnvVar: "GH_AW_GITHUB_EVENT_ISSUE_NUMBER"},
			},
			beforeActivationJobs: nil,
			// All custom job expressions are dropped; non-needs.* expression is kept
			excludedContents: []string{"needs.precompute.outputs.action", "needs.config.outputs.release_tag"},
			expectedContents: []string{"github.event.issue.number"},
		},
		{
			name:       "non-custom-job needs.* expression is always kept",
			customJobs: customJobs,
			mappings: []*ExpressionMapping{
				{Content: "needs.pre_activation.outputs.activated", EnvVar: "GH_AW_NEEDS_PRE_ACTIVATION_OUTPUTS_ACTIVATED"},
				{Content: "needs.precompute.outputs.action", EnvVar: "GH_AW_NEEDS_PRECOMPUTE_OUTPUTS_ACTION"},
			},
			beforeActivationJobs: []string{"precompute"},
			// pre_activation is not in customJobs so it's kept; precompute is in beforeActivationJobs so it's kept
			expectedContents: []string{
				"needs.pre_activation.outputs.activated",
				"needs.precompute.outputs.action",
			},
		},
		{
			name:       "non-needs expression is always kept regardless of beforeActivationJobs",
			customJobs: customJobs,
			mappings: []*ExpressionMapping{
				{Content: "github.event.issue.number", EnvVar: "GH_AW_GITHUB_EVENT_ISSUE_NUMBER"},
				{Content: "steps.sanitized.outputs.text", EnvVar: "GH_AW_STEPS_SANITIZED_OUTPUTS_TEXT"},
				{Content: "needs.config.outputs.release_tag", EnvVar: "GH_AW_NEEDS_CONFIG_OUTPUTS_RELEASE_TAG"},
			},
			beforeActivationJobs: nil,
			expectedContents: []string{
				"github.event.issue.number",
				"steps.sanitized.outputs.text",
			},
			excludedContents: []string{"needs.config.outputs.release_tag"},
		},
		{
			name:       "malformed needs expression without second dot is kept",
			customJobs: customJobs,
			mappings: []*ExpressionMapping{
				{Content: "needs.precompute", EnvVar: "GH_AW_NEEDS_PRECOMPUTE"},
			},
			beforeActivationJobs: nil,
			// No second dot → parsing fails → kept as-is (conservative)
			expectedContents: []string{"needs.precompute"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterExpressionsForActivation(tt.mappings, tt.customJobs, tt.beforeActivationJobs)

			resultSet := make(map[string]struct{}, len(result))
			for _, m := range result {
				resultSet[m.Content] = struct{}{}
			}

			for _, expected := range tt.expectedContents {
				assert.Contains(t, resultSet, expected,
					"Expected expression %q to be kept in result, got: %v", expected, resultSet)
			}
			for _, excluded := range tt.excludedContents {
				assert.NotContains(t, resultSet, excluded,
					"Expected expression %q to be filtered out of result, got: %v", excluded, resultSet)
			}
		})
	}
}

// TestFilterExpressionsForActivationEndToEnd verifies that a workflow with a custom job
// that depends on both pre_activation and activation does not produce expression references
// to that job in the activation prompt's substitution step.
func TestFilterExpressionsForActivationEndToEnd(t *testing.T) {
	workflowContent := `---
engine: copilot
on: issues
permissions:
  issues: read
jobs:
  precompute:
    runs-on: ubuntu-latest
    needs: pre_activation
    outputs:
      action: ${{ steps.detect.outputs.action }}
    steps:
      - id: detect
        run: echo "action=bot" >> "$GITHUB_OUTPUT"
  config:
    runs-on: ubuntu-latest
    needs: ["pre_activation", "activation"]
    outputs:
      release_tag: ${{ steps.tag.outputs.release_tag }}
    steps:
      - id: tag
        run: echo "release_tag=v1.0.0" >> "$GITHUB_OUTPUT"
---

# Test Workflow

Action: ${{ needs.precompute.outputs.action }}
Release tag: ${{ needs.config.outputs.release_tag }}
`
	tmpDir := t.TempDir()
	workflowPath := tmpDir + "/test-filter-workflow.md"
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0644))

	compiler := NewCompiler()
	compiler.SetQuiet(true)
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockContent, err := os.ReadFile(tmpDir + "/test-filter-workflow.lock.yml")
	require.NoError(t, err)
	lockStr := string(lockContent)

	// activation must depend on precompute (it runs before activation and its output is referenced)
	activationSection := extractJobSection(lockStr, "activation")
	require.NotEmpty(t, activationSection, "activation job section should exist")
	assert.Contains(t, activationSection, "precompute", "activation should depend on precompute")

	// config should NOT be in activation's needs (it depends on activation itself)
	assert.NotContains(t, activationSection[:strings.Index(activationSection, "runs-on")],
		"config", "activation should NOT depend on config")

	// The substitution step in the activation job should reference precompute's outputs
	substIdx := strings.Index(lockStr, "- name: Substitute placeholders")
	require.Positive(t, substIdx)
	substSection := lockStr[substIdx:]
	if nextStep := strings.Index(substSection[50:], "- name:"); nextStep > 0 {
		substSection = substSection[:50+nextStep]
	}
	assert.Contains(t, substSection, "GH_AW_NEEDS_PRECOMPUTE_OUTPUTS_ACTION",
		"Substitution step should reference precompute's output (it runs before activation)")
	assert.NotContains(t, substSection, "GH_AW_NEEDS_CONFIG_OUTPUTS_RELEASE_TAG",
		"Substitution step should NOT reference config's output (it runs after activation)")
}
