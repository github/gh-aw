//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========================================
// Failure Reporting and Data Mode Tests
// ========================================

// TestReportFailureAsIssueWithCategoriesFilter tests that report-failure-as-issue
// parsing supports both bool (legacy) and array of category strings
func TestReportFailureAsIssueWithCategoriesFilter(t *testing.T) {
	tests := []struct {
		name                     string
		reportValue              any
		expectBool               *bool
		expectString             string
		expectCategories         []string
		expectExcludedCategories []string
	}{
		{
			name:        "boolean true",
			reportValue: true,
			expectBool:  boolPtr(true),
		},
		{
			name:        "boolean false",
			reportValue: false,
			expectBool:  boolPtr(false),
		},
		{
			name:         "templatable expression",
			reportValue:  "${{ inputs.report-failure-as-issue }}",
			expectString: "${{ inputs.report-failure-as-issue }}",
		},
		{
			name:             "array of categories",
			reportValue:      []any{"agent_failure", "missing_safe_outputs"},
			expectCategories: []string{"agent_failure", "missing_safe_outputs"},
		},
		{
			name:             "array with one category",
			reportValue:      []any{"agent_failure"},
			expectCategories: []string{"agent_failure"},
		},
		{
			name:                     "array with excluded categories",
			reportValue:              []any{"!inference_access_error", "!ai_credits_rate_limit_error"},
			expectExcludedCategories: []string{"inference_access_error", "ai_credits_rate_limit_error"},
		},
		{
			name:                     "array with mixed include and exclude categories",
			reportValue:              []any{"agent_failure", "missing_safe_outputs", "!unknown_model_ai_credits"},
			expectCategories:         []string{"agent_failure", "missing_safe_outputs"},
			expectExcludedCategories: []string{"unknown_model_ai_credits"},
		},
		{
			name:             "array including daily_ai_credits_unknown category",
			reportValue:      []any{"daily_ai_credits_exceeded", "daily_ai_credits_unknown"},
			expectCategories: []string{"daily_ai_credits_exceeded", "daily_ai_credits_unknown"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			// Simulate the parsing by calling extractSafeOutputsConfig
			frontmatter := map[string]any{
				"safe-outputs": map[string]any{
					"report-failure-as-issue": tt.reportValue,
					"create-issue":            nil, // Enable safe outputs
				},
			}

			config := compiler.extractSafeOutputsConfig(frontmatter)
			require.NotNil(t, config, "SafeOutputsConfig should be created")

			if tt.expectBool != nil {
				require.NotNil(t, config.ReportFailureAsIssue, "ReportFailureAsIssue should be set")
				assert.Equal(t, strconv.FormatBool(*tt.expectBool), config.ReportFailureAsIssue.String(), "Boolean value should match")
			}
			if tt.expectString != "" {
				require.NotNil(t, config.ReportFailureAsIssue, "ReportFailureAsIssue should be set")
				assert.Equal(t, tt.expectString, config.ReportFailureAsIssue.String(), "String value should match")
			}

			if len(tt.expectCategories) > 0 {
				assert.Equal(t, tt.expectCategories, config.ReportFailureAsIssueCategories, "Categories should match")
			}
			if len(tt.expectExcludedCategories) > 0 {
				assert.Equal(t, tt.expectExcludedCategories, config.ReportFailureAsIssueExcludedCategories, "Excluded categories should match")
			}
		})
	}
}

// TestReportFailedJobsConfig tests parsing of the report-failed-jobs global flag
func TestReportFailedJobsConfig(t *testing.T) {
	tests := []struct {
		name       string
		value      any
		expectNil  bool
		expectBool bool
	}{
		{
			name:       "explicit false",
			value:      false,
			expectBool: false,
		},
		{
			name:       "explicit true",
			value:      true,
			expectBool: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			frontmatter := map[string]any{
				"safe-outputs": map[string]any{
					"report-failed-jobs": tt.value,
					"create-issue":       nil, // Enable safe outputs
				},
			}

			config := compiler.extractSafeOutputsConfig(frontmatter)
			require.NotNil(t, config, "SafeOutputsConfig should be created")
			require.NotNil(t, config.ReportFailedJobs, "ReportFailedJobs should be set")
			assert.Equal(t, tt.expectBool, *config.ReportFailedJobs, "ReportFailedJobs value should match")
		})
	}
}

// TestReportFailedJobsSchemaValidation ensures that report-failed-jobs is accepted by the
// JSON schema during a real compile (not just extractSafeOutputsConfig), guarding against
// regressions where the field exists in the Go config type but is missing from
// main_workflow_schema.json.
func TestReportFailedJobsSchemaValidation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "report-failed-jobs-schema-test")

	testContent := `---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-issue:
    max: 1
  report-failed-jobs: false
timeout-minutes: 5
---

# Test Workflow

Create an issue.
`

	testFile := filepath.Join(tmpDir, "test-report-failed-jobs.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644), "Failed to write test workflow markdown")

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Workflow with safe-outputs.report-failed-jobs should compile without errors")
}

// TestDataModeConfig tests parsing of the data structured-output global field
func TestDataModeConfig(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{
			name:  "boolean true enables any object",
			value: true,
		},
		{
			name:  "boolean false disables data mode",
			value: false,
		},
		{
			name:  "templatable expression",
			value: "${{ inputs.enable-data }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			frontmatter := map[string]any{
				"safe-outputs": map[string]any{
					"data":         tt.value,
					"create-issue": nil, // Enable safe outputs
				},
			}

			config := compiler.extractSafeOutputsConfig(frontmatter)
			require.NotNil(t, config, "SafeOutputsConfig should be created")
			assert.Equal(t, tt.value, config.Data, "Data value should match")
		})
	}
}
