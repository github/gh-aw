//go:build !integration

package workflow

import "testing"

func TestReferencesCustomJobOutputs(t *testing.T) {
	c := &Compiler{}

	tests := []struct {
		name       string
		condition  string
		customJobs map[string]any
		expected   bool
	}{
		{
			name:       "empty condition",
			condition:  "",
			customJobs: map[string]any{"ast_grep": nil},
			expected:   false,
		},
		{
			name:       "no custom jobs",
			condition:  "needs.ast_grep.outputs.found_patterns == 'true'",
			customJobs: nil,
			expected:   false,
		},
		{
			name:       "references custom job output",
			condition:  "needs.ast_grep.outputs.found_patterns == 'true'",
			customJobs: map[string]any{"ast_grep": nil},
			expected:   true,
		},
		{
			name:       "references custom job result",
			condition:  "needs.my_job.result == 'success'",
			customJobs: map[string]any{"my_job": nil},
			expected:   true,
		},
		{
			name:       "does not reference custom job",
			condition:  "github.event.action == 'opened'",
			customJobs: map[string]any{"ast_grep": nil},
			expected:   false,
		},
		{
			name:       "references standard job not custom",
			condition:  "needs.activation.outputs.text != ''",
			customJobs: map[string]any{"ast_grep": nil},
			expected:   false,
		},
		{
			name:       "complex condition with custom job",
			condition:  "(needs.pre_activation.outputs.activated == 'true') && (needs.ast_grep.outputs.found_patterns == 'true')",
			customJobs: map[string]any{"ast_grep": nil},
			expected:   true,
		},
		{
			name:       "multiple custom jobs but only one referenced",
			condition:  "needs.job_a.outputs.done == 'true'",
			customJobs: map[string]any{"job_a": nil, "job_b": nil, "job_c": nil},
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.referencesCustomJobOutputs(tt.condition, tt.customJobs)
			if result != tt.expected {
				t.Errorf("referencesCustomJobOutputs(%q, %v) = %v, want %v", tt.condition, tt.customJobs, result, tt.expected)
			}
		})
	}
}

func TestJobDependsOnPreActivation(t *testing.T) {
	tests := []struct {
		name      string
		jobConfig map[string]any
		expected  bool
	}{
		{
			name:      "empty config",
			jobConfig: map[string]any{},
			expected:  false,
		},
		{
			name: "needs pre_activation as string",
			jobConfig: map[string]any{
				"needs": "pre_activation",
			},
			expected: true,
		},
		{
			name: "needs pre_activation in array",
			jobConfig: map[string]any{
				"needs": []any{"pre_activation"},
			},
			expected: true,
		},
		{
			name: "needs pre_activation in array with others",
			jobConfig: map[string]any{
				"needs": []any{"other_job", "pre_activation", "another_job"},
			},
			expected: true,
		},
		{
			name: "needs activation (not pre_activation)",
			jobConfig: map[string]any{
				"needs": "activation",
			},
			expected: false,
		},
		{
			name: "needs array without pre_activation",
			jobConfig: map[string]any{
				"needs": []any{"activation", "other_job"},
			},
			expected: false,
		},
		{
			name: "no needs field",
			jobConfig: map[string]any{
				"runs-on": "ubuntu-latest",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := jobDependsOnPreActivation(tt.jobConfig)
			if result != tt.expected {
				t.Errorf("jobDependsOnPreActivation(%v) = %v, want %v", tt.jobConfig, result, tt.expected)
			}
		})
	}
}

func TestGetCustomJobsDependingOnPreActivation(t *testing.T) {
	c := &Compiler{}

	tests := []struct {
		name       string
		customJobs map[string]any
		expected   []string
	}{
		{
			name:       "nil custom jobs",
			customJobs: nil,
			expected:   nil,
		},
		{
			name:       "empty custom jobs",
			customJobs: map[string]any{},
			expected:   nil,
		},
		{
			name: "job with needs pre_activation as string",
			customJobs: map[string]any{
				"ast_grep": map[string]any{
					"needs": "pre_activation",
				},
			},
			expected: []string{"ast_grep"},
		},
		{
			name: "job with needs pre_activation in array",
			customJobs: map[string]any{
				"ast_grep": map[string]any{
					"needs": []any{"pre_activation"},
				},
			},
			expected: []string{"ast_grep"},
		},
		{
			name: "job without needs field",
			customJobs: map[string]any{
				"my_job": map[string]any{
					"runs-on": "ubuntu-latest",
				},
			},
			expected: nil,
		},
		{
			name: "job with different needs",
			customJobs: map[string]any{
				"my_job": map[string]any{
					"needs": "activation",
				},
			},
			expected: nil,
		},
		{
			name: "multiple jobs mixed",
			customJobs: map[string]any{
				"job_a": map[string]any{
					"needs": "pre_activation",
				},
				"job_b": map[string]any{
					"needs": "activation",
				},
				"job_c": map[string]any{
					"needs": []any{"pre_activation", "job_a"},
				},
			},
			expected: []string{"job_a", "job_c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.getCustomJobsDependingOnPreActivation(tt.customJobs)
			// Convert to maps for easier comparison (order doesn't matter)
			resultMap := make(map[string]bool)
			for _, r := range result {
				resultMap[r] = true
			}
			expectedMap := make(map[string]bool)
			for _, e := range tt.expected {
				expectedMap[e] = true
			}

			if len(resultMap) != len(expectedMap) {
				t.Errorf("getCustomJobsDependingOnPreActivation() returned %v, want %v", result, tt.expected)
				return
			}
			for k := range expectedMap {
				if !resultMap[k] {
					t.Errorf("getCustomJobsDependingOnPreActivation() returned %v, want %v", result, tt.expected)
					return
				}
			}
		})
	}
}
