//go:build !integration

package workflow

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildNormalizedSortedJSON(t *testing.T) {
	tests := []struct {
		name     string
		names    []string
		valueFn  func(string) string
		expected string
	}{
		{
			name:     "nil names",
			names:    nil,
			valueFn:  func(string) string { return "" },
			expected: `{}`,
		},
		{
			name:     "empty names",
			names:    []string{},
			valueFn:  func(string) string { return "" },
			expected: `{}`,
		},
		{
			name:     "normalizes name before calling value function",
			names:    []string{"my-job"},
			valueFn:  func(name string) string { return name },
			expected: `{"my_job":"my_job"}`,
		},
		{
			name:  "collapses duplicate normalized names",
			names: []string{"my-job", "my_job"},
			valueFn: func() func(string) string {
				calls := 0
				return func(name string) string {
					calls++
					return name + strconv.Itoa(calls)
				}
			}(),
			expected: `{"my_job":"my_job2"}`,
		},
		{
			name:     "sorts keys",
			names:    []string{"z-job", "a-job"},
			valueFn:  func(name string) string { return name },
			expected: `{"a_job":"a_job","z_job":"z_job"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonStr, err := buildNormalizedSortedJSON(tt.names, tt.valueFn)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, jsonStr) //nolint:testifylint // Compare serialized JSON bytes, including object-key order.
		})
	}
}

func TestBuildCustomSafeOutputJobsJSON(t *testing.T) {
	tests := []struct {
		name     string
		data     *WorkflowData
		expected string
	}{
		{
			name:     "nil safe outputs",
			data:     &WorkflowData{},
			expected: "",
		},
		{
			name:     "nil jobs map",
			data:     &WorkflowData{SafeOutputs: &SafeOutputsConfig{}},
			expected: "",
		},
		{
			name: "empty jobs map",
			data: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{Jobs: map[string]*SafeJobConfig{}},
			},
			expected: "",
		},
		{
			name: "single job with uppercase and special characters",
			data: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					Jobs: map[string]*SafeJobConfig{"My.Custom-Job": {}},
				},
			},
			expected: `{"My_Custom_Job":""}`,
		},
		{
			name: "multiple sorted jobs",
			data: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					Jobs: map[string]*SafeJobConfig{
						"z-job": {},
						"a-job": {},
					},
				},
			},
			expected: `{"a_job":"","z_job":""}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonStr := buildCustomSafeOutputJobsJSON(tt.data)
			assert.Equal(t, tt.expected, jsonStr) //nolint:testifylint // Compare serialized JSON bytes, including object-key order.
		})
	}
}
