//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseNoOpConfig(t *testing.T) {
	tests := []struct {
		name           string
		outputMap      map[string]any
		expectedNil    bool
		expectedMax    int
		expectedReport bool
	}{
		{
			name:           "noop not present",
			outputMap:      map[string]any{},
			expectedNil:    true,
			expectedMax:    0,
			expectedReport: false,
		},
		{
			name: "noop explicitly disabled with false",
			outputMap: map[string]any{
				"noop": false,
			},
			expectedNil:    true,
			expectedMax:    0,
			expectedReport: false,
		},
		{
			name: "noop enabled with nil value",
			outputMap: map[string]any{
				"noop": nil,
			},
			expectedNil:    false,
			expectedMax:    1,
			expectedReport: true,
		},
		{
			name: "noop with empty config object",
			outputMap: map[string]any{
				"noop": map[string]any{},
			},
			expectedNil:    false,
			expectedMax:    1,
			expectedReport: true,
		},
		{
			name: "noop with max specified",
			outputMap: map[string]any{
				"noop": map[string]any{
					"max": 5,
				},
			},
			expectedNil:    false,
			expectedMax:    5,
			expectedReport: true,
		},
		{
			name: "noop with report-as-issue set to true",
			outputMap: map[string]any{
				"noop": map[string]any{
					"report-as-issue": true,
				},
			},
			expectedNil:    false,
			expectedMax:    1,
			expectedReport: true,
		},
		{
			name: "noop with report-as-issue set to false",
			outputMap: map[string]any{
				"noop": map[string]any{
					"report-as-issue": false,
				},
			},
			expectedNil:    false,
			expectedMax:    1,
			expectedReport: false,
		},
		{
			name: "noop with max and report-as-issue",
			outputMap: map[string]any{
				"noop": map[string]any{
					"max":             3,
					"report-as-issue": false,
				},
			},
			expectedNil:    false,
			expectedMax:    3,
			expectedReport: false,
		},
		{
			name: "noop with report-as-issue not specified defaults to true",
			outputMap: map[string]any{
				"noop": map[string]any{
					"max": 2,
				},
			},
			expectedNil:    false,
			expectedMax:    2,
			expectedReport: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := &Compiler{}
			result := compiler.parseNoOpConfig(tt.outputMap)

			if tt.expectedNil {
				assert.Nil(t, result, "Expected nil NoOpConfig")
			} else {
				assert.NotNil(t, result, "Expected non-nil NoOpConfig")
				assert.Equal(t, tt.expectedMax, result.Max, "Max value mismatch")
				assert.Equal(t, tt.expectedReport, result.ReportAsIssue, "ReportAsIssue value mismatch")
			}
		})
	}
}
