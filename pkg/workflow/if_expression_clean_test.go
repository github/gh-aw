//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestIfExpressionCleanHandling tests that if expressions are handled cleanly
// without the "if: " prefix being added too early, and that multiline expressions
// use YAML folded style properly
func TestIfExpressionCleanHandling(t *testing.T) {
	tests := []struct {
		name          string
		expression    string
		isMultiline   bool
		expectedYAML  string
		expectsPrefix bool
	}{
		{
			name:          "simple single line expression",
			expression:    "github.event_name == 'push'",
			isMultiline:   false,
			expectedYAML:  "    if: github.event_name == 'push'",
			expectsPrefix: true,
		},
		{
			name: "multiline expression with YAML folded style",
			expression: `github.event_name == 'issues' ||
github.event_name == 'pull_request' ||
github.event_name == 'issue_comment'`,
			isMultiline: true,
			expectedYAML: `    if: >
      github.event_name == 'issues' ||
      github.event_name == 'pull_request' ||
      github.event_name == 'issue_comment'`,
			expectsPrefix: true,
		},
		{
			name:          "empty expression",
			expression:    "",
			isMultiline:   false,
			expectedYAML:  "",
			expectsPrefix: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a job with the expression
			job := &Job{
				Name:   "test-job",
				If:     tt.expression, // Should store just the expression, not prefixed
				RunsOn: "runs-on: ubuntu-latest",
				Steps:  []string{"      - name: Test Step\n        run: echo test\n"},
			}

			// Create job manager and render
			jm := NewJobManager()
			err := jm.AddJob(job)
			if err != nil {
				t.Fatalf("Failed to add job: %v", err)
			}

			yaml := jm.renderJob(job)

			if tt.expression == "" {
				// Empty expressions should not render if condition
				if strings.Contains(yaml, "if:") {
					t.Errorf("Empty expression should not render if condition, but got:\n%s", yaml)
				}
				return
			}

			// Check that the YAML contains the expected format
			if !strings.Contains(yaml, tt.expectedYAML) {
				t.Errorf("Expected YAML to contain:\n%s\n\nBut got:\n%s", tt.expectedYAML, yaml)
			}

			// Ensure we don't have double prefixes
			if strings.Contains(yaml, "if: if:") {
				t.Errorf("Found double 'if: if:' prefix in YAML:\n%s", yaml)
			}
		})
	}
}

// TestMultilineExpressionYAMLFolding tests that complex expressions use YAML folded style
func TestMultilineExpressionYAMLFolding(t *testing.T) {
	// Create a multiline disjunction expression
	terms := []ConditionNode{
		&ExpressionNode{Expression: "github.event_name == 'issues'", Description: "Handle issues"},
		&ExpressionNode{Expression: "github.event_name == 'pull_request'", Description: "Handle PRs"},
		&ExpressionNode{Expression: "github.event_name == 'issue_comment'", Description: "Handle comments"},
	}

	disjunction := &DisjunctionNode{
		Terms:     terms,
		Multiline: true,
	}

	rendered := disjunction.Render()

	// Should render multiline
	if !strings.Contains(rendered, "\n") {
		t.Errorf("Expected multiline rendering, but got single line: %s", rendered)
	}

	// Should contain comments
	if !strings.Contains(rendered, "# Handle issues") {
		t.Errorf("Expected comment 'Handle issues' in rendered output: %s", rendered)
	}

	// Test that this can be used in a job and rendered with YAML folded style
	job := &Job{
		Name:   "multiline-job",
		If:     rendered,
		RunsOn: "runs-on: ubuntu-latest",
		Steps:  []string{"      - name: Test Step\n        run: echo test\n"},
	}

	jm := NewJobManager()
	err := jm.AddJob(job)
	if err != nil {
		t.Fatalf("Failed to add job: %v", err)
	}

	yaml := jm.renderJob(job)

	// Should use folded style for multiline
	if !strings.Contains(yaml, "if: >") {
		t.Errorf("Expected YAML folded style 'if: >' for multiline expression, got:\n%s", yaml)
	}
}

// TestJobStructStoreSeparatesExpression tests that the Job struct stores expressions
// separately from the "if: " prefix
func TestJobStructStoreSeparatesExpression(t *testing.T) {
	expression := "github.event_name == 'push'"

	job := &Job{
		If: expression, // Should be just the expression
	}

	// The job struct should store just the expression
	if job.If != expression {
		t.Errorf("Expected job.If to be '%s', got '%s'", expression, job.If)
	}

	// The job struct should NOT contain the "if: " prefix
	if strings.HasPrefix(job.If, "if: ") {
		t.Errorf("Job.If should not contain 'if: ' prefix, got '%s'", job.If)
	}
}

// TestCustomJobIfConditionHandling tests that custom jobs properly handle if conditions
// from frontmatter, including when they contain the "if: " prefix
func TestCustomJobIfConditionHandling(t *testing.T) {
	tests := []struct {
		name               string
		ifConditionInYAML  string
		expectedExpression string
	}{
		{
			name:               "if condition with prefix",
			ifConditionInYAML:  "if: github.event.issue.number",
			expectedExpression: "github.event.issue.number",
		},
		{
			name:               "if condition without prefix",
			ifConditionInYAML:  "github.event.issue.number",
			expectedExpression: "github.event.issue.number",
		},
		{
			name:               "complex if condition with prefix",
			ifConditionInYAML:  "if: always()",
			expectedExpression: "always()",
		},
		{
			name:               "complex if condition without prefix",
			ifConditionInYAML:  "${{ always() }}",
			expectedExpression: "${{ always() }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a compiler
			compiler := &Compiler{
				verbose: false,
			}

			// Simulate custom job config map as it would come from frontmatter
			configMap := map[string]any{
				"if":      tt.ifConditionInYAML,
				"runs-on": "ubuntu-latest",
				"steps": []any{
					map[string]any{
						"name": "Test Step",
						"run":  "echo test",
					},
				},
			}

			// Simulate building a custom job (like buildCustomJobs does)
			job := &Job{
				Name: "test-custom-job",
			}

			// Extract if condition using the same logic as buildCustomJobs
			if ifCond, hasIf := configMap["if"]; hasIf {
				if ifStr, ok := ifCond.(string); ok {
					job.If = compiler.extractExpressionFromIfString(ifStr)
				}
			}

			// Verify the expression was extracted correctly
			if job.If != tt.expectedExpression {
				t.Errorf("Expected expression '%s', got '%s'", tt.expectedExpression, job.If)
			}

			// Ensure we don't have the "if: " prefix in the job struct
			if strings.HasPrefix(job.If, "if: ") {
				t.Errorf("Job.If should not contain 'if: ' prefix, got '%s'", job.If)
			}

			// Render the job and check for double prefixes
			jm := NewJobManager()
			err := jm.AddJob(job)
			if err != nil {
				t.Fatalf("Failed to add job: %v", err)
			}

			yaml := jm.renderJob(job)

			// Ensure we don't have double prefixes in the rendered YAML
			if strings.Contains(yaml, "if: if:") {
				t.Errorf("Found double 'if: if:' prefix in YAML:\n%s", yaml)
			}

			// Ensure we do have the correct single prefix
			expectedLine := "    if: " + tt.expectedExpression
			if !strings.Contains(yaml, expectedLine) {
				t.Errorf("Expected YAML to contain '%s', but got:\n%s", expectedLine, yaml)
			}
		})
	}
}

// TestLongExpressionBreaking tests that expressions longer than 120 characters
// are automatically broken into multiple lines using YAML folded style
func TestLongExpressionBreaking(t *testing.T) {
	tests := []struct {
		name               string
		expression         string
		expectMultiline    bool
		expectedContains   []string
		expectedNotContain []string
	}{
		{
			name:            "short expression stays single line",
			expression:      "github.event_name == 'push'",
			expectMultiline: false,
			expectedContains: []string{
				"if: github.event_name == 'push'",
			},
			expectedNotContain: []string{
				"if: >",
			},
		},
		{
			name:            "long expression gets broken into multiline",
			expression:      "github.event_name == 'issues' || github.event_name == 'pull_request' || github.event_name == 'issue_comment' || github.event_name == 'discussion'",
			expectMultiline: true,
			expectedContains: []string{
				"if: >",
				"github.event_name == 'issues' ||",
				"github.event_name == 'pull_request' ||",
			},
			expectedNotContain: []string{
				"if: github.event_name == 'issues' || github.event_name == 'pull_request' || github.event_name == 'issue_comment' || github.event_name == 'discussion'",
			},
		},
		{
			name:            "very long expression with function calls",
			expression:      "contains(github.event.issue.labels.*.name, 'bug') && contains(github.event.issue.labels.*.name, 'priority-high') && github.event.action == 'opened'",
			expectMultiline: true,
			expectedContains: []string{
				"if: >",
				"contains(github.event.issue.labels.*.name, 'bug') &&",
			},
			expectedNotContain: []string{
				"if: contains(github.event.issue.labels.*.name, 'bug') && contains(github.event.issue.labels.*.name, 'priority-high')",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a job with the long expression
			job := &Job{
				Name:   "test-job",
				If:     tt.expression,
				RunsOn: "runs-on: ubuntu-latest",
				Steps:  []string{"      - name: Test Step\n        run: echo test\n"},
			}

			// Create job manager and render
			jm := NewJobManager()
			err := jm.AddJob(job)
			if err != nil {
				t.Fatalf("Failed to add job: %v", err)
			}

			yaml := jm.renderJob(job)
			t.Logf("Generated YAML:\n%s", yaml)

			// Check expected content
			for _, expected := range tt.expectedContains {
				if !strings.Contains(yaml, expected) {
					t.Errorf("Expected YAML to contain '%s', but got:\n%s", expected, yaml)
				}
			}

			// Check not expected content
			for _, notExpected := range tt.expectedNotContain {
				if strings.Contains(yaml, notExpected) {
					t.Errorf("Expected YAML to NOT contain '%s', but got:\n%s", notExpected, yaml)
				}
			}

			// Verify multiline expectation
			hasYamlFoldedStyle := strings.Contains(yaml, "if: >")
			if tt.expectMultiline && !hasYamlFoldedStyle {
				t.Errorf("Expected multiline rendering with 'if: >' for expression longer than 120 chars")
			} else if !tt.expectMultiline && hasYamlFoldedStyle {
				t.Errorf("Expected single line rendering, but got multiline 'if: >' style")
			}

			// Ensure no double prefixes
			if strings.Contains(yaml, "if: if:") {
				t.Errorf("Found double 'if: if:' prefix in YAML:\n%s", yaml)
			}
		})
	}
}
