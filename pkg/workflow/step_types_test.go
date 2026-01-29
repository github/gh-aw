//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowStep_IsUsesStep(t *testing.T) {
	tests := []struct {
		name string
		step *WorkflowStep
		want bool
	}{
		{
			name: "step with uses field",
			step: &WorkflowStep{Uses: "actions/checkout@v4"},
			want: true,
		},
		{
			name: "step with run field only",
			step: &WorkflowStep{Run: "echo hello"},
			want: false,
		},
		{
			name: "empty step",
			step: &WorkflowStep{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.step.IsUsesStep()
			assert.Equal(t, tt.want, got, "IsUsesStep should return correct value for %s", tt.name)
		})
	}
}

func TestWorkflowStep_IsRunStep(t *testing.T) {
	tests := []struct {
		name string
		step *WorkflowStep
		want bool
	}{
		{
			name: "step with run field",
			step: &WorkflowStep{Run: "echo hello"},
			want: true,
		},
		{
			name: "step with uses field only",
			step: &WorkflowStep{Uses: "actions/checkout@v4"},
			want: false,
		},
		{
			name: "empty step",
			step: &WorkflowStep{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.step.IsRunStep()
			assert.Equal(t, tt.want, got, "IsRunStep should return correct value for %s", tt.name)
		})
	}
}

func TestWorkflowStep_ToMap(t *testing.T) {
	tests := []struct {
		name string
		step *WorkflowStep
		want map[string]any
	}{
		{
			name: "complete step with uses",
			step: &WorkflowStep{
				Name: "Checkout code",
				ID:   "checkout",
				Uses: "actions/checkout@v4",
				With: map[string]any{"fetch-depth": "0"},
			},
			want: map[string]any{
				"name": "Checkout code",
				"id":   "checkout",
				"uses": "actions/checkout@v4",
				"with": map[string]any{"fetch-depth": "0"},
			},
		},
		{
			name: "step with run",
			step: &WorkflowStep{
				Name:  "Run tests",
				Run:   "npm test",
				Shell: "bash",
				Env:   map[string]string{"NODE_ENV": "test"},
			},
			want: map[string]any{
				"name":  "Run tests",
				"run":   "npm test",
				"shell": "bash",
				"env":   map[string]string{"NODE_ENV": "test"},
			},
		},
		{
			name: "step with all fields",
			step: &WorkflowStep{
				Name:             "Complex step",
				ID:               "complex",
				If:               "success()",
				Uses:             "some/action@v1",
				WorkingDirectory: "/path/to/dir",
				With:             map[string]any{"key": "value"},
				Env:              map[string]string{"VAR": "val"},
				ContinueOnError:  true,
				TimeoutMinutes:   10,
			},
			want: map[string]any{
				"name":              "Complex step",
				"id":                "complex",
				"if":                "success()",
				"uses":              "some/action@v1",
				"working-directory": "/path/to/dir",
				"with":              map[string]any{"key": "value"},
				"env":               map[string]string{"VAR": "val"},
				"continue-on-error": true,
				"timeout-minutes":   10,
			},
		},
		{
			name: "minimal step",
			step: &WorkflowStep{
				Uses: "actions/checkout@v4",
			},
			want: map[string]any{
				"uses": "actions/checkout@v4",
			},
		},
		{
			name: "step with string continue-on-error",
			step: &WorkflowStep{
				Name:            "Test step",
				Run:             "npm test",
				ContinueOnError: "false",
			},
			want: map[string]any{
				"name":              "Test step",
				"run":               "npm test",
				"continue-on-error": "false",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.step.ToMap()
			assert.Len(t, got, len(tt.want), "ToMap should return map with correct length for %s", tt.name)
			for key, wantVal := range tt.want {
				gotVal, ok := got[key]
				assert.True(t, ok, "ToMap should include key %q for %s", key, tt.name)
				if ok {
					// Compare values - for maps, do a deep comparison
					assert.True(t, compareStepValues(gotVal, wantVal), "ToMap[%q] should equal expected value for %s", key, tt.name)
				}
			}
		})
	}
}

func TestMapToStep(t *testing.T) {
	tests := []struct {
		name    string
		stepMap map[string]any
		want    *WorkflowStep
		wantErr bool
	}{
		{
			name: "complete step with uses",
			stepMap: map[string]any{
				"name": "Checkout code",
				"id":   "checkout",
				"uses": "actions/checkout@v4",
				"with": map[string]any{"fetch-depth": "0"},
			},
			want: &WorkflowStep{
				Name: "Checkout code",
				ID:   "checkout",
				Uses: "actions/checkout@v4",
				With: map[string]any{"fetch-depth": "0"},
			},
			wantErr: false,
		},
		{
			name: "step with run",
			stepMap: map[string]any{
				"name":  "Run tests",
				"run":   "npm test",
				"shell": "bash",
				"env":   map[string]any{"NODE_ENV": "test"},
			},
			want: &WorkflowStep{
				Name:  "Run tests",
				Run:   "npm test",
				Shell: "bash",
				Env:   map[string]string{"NODE_ENV": "test"},
			},
			wantErr: false,
		},
		{
			name: "step with all fields",
			stepMap: map[string]any{
				"name":              "Complex step",
				"id":                "complex",
				"if":                "success()",
				"uses":              "some/action@v1",
				"working-directory": "/path/to/dir",
				"with":              map[string]any{"key": "value"},
				"env":               map[string]any{"VAR": "val"},
				"continue-on-error": true,
				"timeout-minutes":   10,
			},
			want: &WorkflowStep{
				Name:             "Complex step",
				ID:               "complex",
				If:               "success()",
				Uses:             "some/action@v1",
				WorkingDirectory: "/path/to/dir",
				With:             map[string]any{"key": "value"},
				Env:              map[string]string{"VAR": "val"},
				ContinueOnError:  true,
				TimeoutMinutes:   10,
			},
			wantErr: false,
		},
		{
			name:    "nil step map",
			stepMap: nil,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "empty step map",
			stepMap: map[string]any{},
			want:    &WorkflowStep{},
			wantErr: false,
		},
		{
			name: "step with string continue-on-error",
			stepMap: map[string]any{
				"name":              "Test step",
				"run":               "npm test",
				"continue-on-error": "false",
			},
			want: &WorkflowStep{
				Name:            "Test step",
				Run:             "npm test",
				ContinueOnError: "false",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MapToStep(tt.stepMap)
			if tt.wantErr {
				require.Error(t, err, "MapToStep should return error for %s", tt.name)
				return
			}
			require.NoError(t, err, "MapToStep should succeed for %s", tt.name)
			assert.True(t, compareSteps(got, tt.want), "MapToStep should return correct step for %s", tt.name)
		})
	}
}

func TestWorkflowStep_Clone(t *testing.T) {
	original := &WorkflowStep{
		Name:             "Original step",
		ID:               "original",
		If:               "success()",
		Uses:             "some/action@v1",
		Run:              "echo test",
		WorkingDirectory: "/test",
		Shell:            "bash",
		With:             map[string]any{"key": "value", "nested": map[string]any{"inner": "val"}},
		Env:              map[string]string{"VAR1": "val1", "VAR2": "val2"},
		ContinueOnError:  true,
		TimeoutMinutes:   15,
	}

	clone := original.Clone()

	// Verify clone is equal to original
	assert.True(t, compareSteps(clone, original), "Clone should create equal step")

	// Verify clone is independent (modifying clone doesn't affect original)
	clone.Name = "Modified"
	assert.NotEqual(t, "Modified", original.Name, "Clone should be independent - modifying clone Name should not affect original")

	clone.With["new-key"] = "new-value"
	_, exists := original.With["new-key"]
	assert.False(t, exists, "Clone should deep copy With map - modifying clone should not affect original")

	clone.Env["NEW_VAR"] = "new-val"
	_, exists = original.Env["NEW_VAR"]
	assert.False(t, exists, "Clone should deep copy Env map - modifying clone should not affect original")
}

func TestWorkflowStep_ToYAML(t *testing.T) {
	tests := []struct {
		name    string
		step    *WorkflowStep
		wantErr bool
	}{
		{
			name: "simple step",
			step: &WorkflowStep{
				Name: "Test step",
				Uses: "actions/checkout@v4",
			},
			wantErr: false,
		},
		{
			name: "step with complex fields",
			step: &WorkflowStep{
				Name: "Complex step",
				Uses: "some/action@v1",
				With: map[string]any{
					"string-field": "value",
					"int-field":    42,
					"bool-field":   true,
				},
				Env: map[string]string{
					"VAR": "value",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.step.ToYAML()
			if tt.wantErr {
				require.Error(t, err, "ToYAML should return error for %s", tt.name)
				return
			}
			require.NoError(t, err, "ToYAML should succeed for %s", tt.name)
			assert.NotEmpty(t, got, "ToYAML should return non-empty string for %s", tt.name)
		})
	}
}

func TestMapToStep_RoundTrip(t *testing.T) {
	// Test that converting map -> step -> map produces the same result
	// Note: env field converts from map[string]any to map[string]string
	originalMap := map[string]any{
		"name": "Test step",
		"id":   "test",
		"uses": "actions/checkout@v4",
		"with": map[string]any{"fetch-depth": "0"},
		"env":  map[string]any{"NODE_ENV": "test"},
	}

	step, err := MapToStep(originalMap)
	require.NoError(t, err, "MapToStep should succeed for round-trip test")

	resultMap := step.ToMap()

	// Compare maps
	assert.Len(t, resultMap, len(originalMap), "Round trip should preserve map size")

	for key, originalVal := range originalMap {
		resultVal, ok := resultMap[key]
		assert.True(t, ok, "Round trip should preserve key %q", key)
		if !ok {
			continue
		}
		// Special handling for env field which converts from map[string]any to map[string]string
		if key == "env" {
			origEnv, origOK := originalVal.(map[string]any)
			resultEnv, resultOK := resultVal.(map[string]string)
			if origOK && resultOK {
				assert.Len(t, resultEnv, len(origEnv), "Round trip should preserve env map size")
				for k, v := range origEnv {
					if vStr, ok := v.(string); ok {
						assert.Equal(t, vStr, resultEnv[k], "Round trip should preserve env[%q]", k)
					}
				}
				continue
			}
		}
		assert.True(t, compareStepValues(resultVal, originalVal), "Round trip should preserve value for key %q", key)
	}
}

// Helper function to compare two values (handles nested maps)
func compareStepValues(a, b any) bool {
	switch aVal := a.(type) {
	case map[string]any:
		bMap, ok := b.(map[string]any)
		if !ok {
			return false
		}
		if len(aVal) != len(bMap) {
			return false
		}
		for k, v := range aVal {
			bv, ok := bMap[k]
			if !ok || !compareStepValues(v, bv) {
				return false
			}
		}
		return true
	case map[string]string:
		bMap, ok := b.(map[string]string)
		if !ok {
			return false
		}
		if len(aVal) != len(bMap) {
			return false
		}
		for k, v := range aVal {
			if bMap[k] != v {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

// Helper function to compare two WorkflowStep structs
func compareSteps(a, b *WorkflowStep) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	if a.Name != b.Name || a.ID != b.ID || a.If != b.If ||
		a.Uses != b.Uses || a.Run != b.Run ||
		a.WorkingDirectory != b.WorkingDirectory || a.Shell != b.Shell ||
		a.TimeoutMinutes != b.TimeoutMinutes {
		return false
	}

	// Compare ContinueOnError (can be any type)
	if !compareStepValues(a.ContinueOnError, b.ContinueOnError) {
		return false
	}

	// Compare With maps
	if !compareStepValues(a.With, b.With) {
		return false
	}

	// Compare Env maps
	if !compareStepValues(a.Env, b.Env) {
		return false
	}

	return true
}

func TestSliceToSteps(t *testing.T) {
	tests := []struct {
		name    string
		input   []any
		want    []*WorkflowStep
		wantErr bool
	}{
		{
			name:  "nil slice",
			input: nil,
			want:  nil,
		},
		{
			name:  "empty slice",
			input: []any{},
			want:  []*WorkflowStep{},
		},
		{
			name: "single uses step",
			input: []any{
				map[string]any{
					"name": "Checkout",
					"uses": "actions/checkout@v4",
				},
			},
			want: []*WorkflowStep{
				{
					Name: "Checkout",
					Uses: "actions/checkout@v4",
				},
			},
		},
		{
			name: "multiple mixed steps",
			input: []any{
				map[string]any{
					"name": "Checkout",
					"uses": "actions/checkout@v4",
					"with": map[string]any{"fetch-depth": "0"},
				},
				map[string]any{
					"name": "Run tests",
					"run":  "npm test",
					"env":  map[string]any{"NODE_ENV": "test"},
				},
			},
			want: []*WorkflowStep{
				{
					Name: "Checkout",
					Uses: "actions/checkout@v4",
					With: map[string]any{"fetch-depth": "0"},
				},
				{
					Name: "Run tests",
					Run:  "npm test",
					Env:  map[string]string{"NODE_ENV": "test"},
				},
			},
		},
		{
			name: "invalid step type",
			input: []any{
				"not a map",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SliceToSteps(tt.input)
			if tt.wantErr {
				require.Error(t, err, "SliceToSteps should return error for %s", tt.name)
				return
			}
			require.NoError(t, err, "SliceToSteps should succeed for %s", tt.name)
			assert.Len(t, got, len(tt.want), "SliceToSteps should return correct number of steps for %s", tt.name)
			for i := range got {
				assert.True(t, compareSteps(got[i], tt.want[i]), "SliceToSteps step %d should match expected for %s", i, tt.name)
			}
		})
	}
}

func TestStepsToSlice(t *testing.T) {
	tests := []struct {
		name  string
		input []*WorkflowStep
		want  []any
	}{
		{
			name:  "nil slice",
			input: nil,
			want:  nil,
		},
		{
			name:  "empty slice",
			input: []*WorkflowStep{},
			want:  []any{},
		},
		{
			name: "single uses step",
			input: []*WorkflowStep{
				{
					Name: "Checkout",
					Uses: "actions/checkout@v4",
				},
			},
			want: []any{
				map[string]any{
					"name": "Checkout",
					"uses": "actions/checkout@v4",
				},
			},
		},
		{
			name: "multiple mixed steps",
			input: []*WorkflowStep{
				{
					Name: "Checkout",
					Uses: "actions/checkout@v4",
					With: map[string]any{"fetch-depth": "0"},
				},
				{
					Name: "Run tests",
					Run:  "npm test",
					Env:  map[string]string{"NODE_ENV": "test"},
				},
			},
			want: []any{
				map[string]any{
					"name": "Checkout",
					"uses": "actions/checkout@v4",
					"with": map[string]any{"fetch-depth": "0"},
				},
				map[string]any{
					"name": "Run tests",
					"run":  "npm test",
					"env":  map[string]string{"NODE_ENV": "test"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StepsToSlice(tt.input)
			assert.Len(t, got, len(tt.want), "StepsToSlice should return correct number of items for %s", tt.name)
			for i := range got {
				gotMap, ok := got[i].(map[string]any)
				assert.True(t, ok, "StepsToSlice item %d should be a map for %s", i, tt.name)
				if !ok {
					continue
				}
				wantMap, ok := tt.want[i].(map[string]any)
				assert.True(t, ok, "Test data item %d should be a map for %s", i, tt.name)
				if !ok {
					continue
				}
				assert.Len(t, gotMap, len(wantMap), "StepsToSlice item %d should have correct number of fields for %s", i, tt.name)
				for key, wantVal := range wantMap {
					gotVal, exists := gotMap[key]
					assert.True(t, exists, "StepsToSlice item %d should include key %q for %s", i, key, tt.name)
					if exists {
						assert.True(t, compareStepValues(gotVal, wantVal), "StepsToSlice item %d key %q should match expected value for %s", i, key, tt.name)
					}
				}
			}
		})
	}
}

func TestSliceToSteps_RoundTrip(t *testing.T) {
	// Test that converting []any -> []*WorkflowStep -> []any produces equivalent result
	originalSlice := []any{
		map[string]any{
			"name": "Checkout",
			"uses": "actions/checkout@v4",
			"with": map[string]any{"fetch-depth": "0"},
		},
		map[string]any{
			"name": "Run tests",
			"run":  "npm test",
			"env":  map[string]any{"NODE_ENV": "test"},
		},
	}

	// Convert to typed steps
	steps, err := SliceToSteps(originalSlice)
	require.NoError(t, err, "SliceToSteps should succeed for round-trip test")

	// Convert back to slice
	resultSlice := StepsToSlice(steps)

	// Compare
	assert.Len(t, resultSlice, len(originalSlice), "Round trip should preserve slice size")

	for i := range originalSlice {
		origMap, _ := originalSlice[i].(map[string]any)
		resultMap, _ := resultSlice[i].(map[string]any)

		// Check all keys from original exist in result
		for key, origVal := range origMap {
			resultVal, exists := resultMap[key]
			assert.True(t, exists, "Round trip should preserve key %q in step %d", key, i)
			if !exists {
				continue
			}
			// Special handling for env field which converts map[string]any to map[string]string
			if key == "env" {
				origEnv, _ := origVal.(map[string]any)
				resultEnv, _ := resultVal.(map[string]string)
				for k, v := range origEnv {
					if vStr, ok := v.(string); ok {
						assert.Equal(t, vStr, resultEnv[k], "Round trip should preserve env[%q] in step %d", k, i)
					}
				}
			} else {
				assert.True(t, compareStepValues(resultVal, origVal), "Round trip should preserve value for key %q in step %d", key, i)
			}
		}
	}
}

func TestMapToStep_InvalidTypes(t *testing.T) {
	tests := []struct {
		name        string
		stepMap     map[string]any
		shouldParse bool
		description string
	}{
		{
			name: "timeout-minutes as string",
			stepMap: map[string]any{
				"name":            "Test",
				"run":             "echo test",
				"timeout-minutes": "not-a-number",
			},
			shouldParse: true, // Currently ignores invalid types
			description: "should handle non-integer timeout-minutes gracefully",
		},
		{
			name: "continue-on-error as string expression",
			stepMap: map[string]any{
				"name":              "Test",
				"run":               "echo test",
				"continue-on-error": "${{ github.event_name == 'push' }}",
			},
			shouldParse: true, // Should preserve string expressions
			description: "should preserve string expressions for continue-on-error",
		},
		{
			name: "env with non-string values",
			stepMap: map[string]any{
				"name": "Test",
				"run":  "echo test",
				"env": map[string]any{
					"STRING_VAR": "value",
					"INT_VAR":    42,
					"BOOL_VAR":   true,
				},
			},
			shouldParse: true, // Should handle type conversion
			description: "should handle non-string env values",
		},
		{
			name: "with as non-map",
			stepMap: map[string]any{
				"name": "Test",
				"uses": "actions/checkout@v4",
				"with": "not-a-map",
			},
			shouldParse: true, // Currently ignores invalid types
			description: "should ignore invalid with field type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step, err := MapToStep(tt.stepMap)
			if tt.shouldParse {
				require.NoError(t, err, "MapToStep should handle %s", tt.description)
				require.NotNil(t, step, "MapToStep should return non-nil step for %s", tt.description)
			} else {
				require.Error(t, err, "MapToStep should return error for %s", tt.description)
			}
		})
	}
}

func TestClone_DeepNestedMaps(t *testing.T) {
	original := &WorkflowStep{
		Name: "Test",
		Uses: "some/action@v1",
		With: map[string]any{
			"level1": map[string]any{
				"level2": map[string]any{
					"level3": "value",
				},
			},
			"simple": "value",
		},
	}

	clone := original.Clone()

	// Verify original values are preserved in clone
	require.NotNil(t, clone.With, "Clone should preserve With map")
	level1, ok := clone.With["level1"].(map[string]any)
	require.True(t, ok, "Clone should preserve nested map structure")
	level2, ok := level1["level2"].(map[string]any)
	require.True(t, ok, "Clone should preserve deeply nested map structure")
	assert.Equal(t, "value", level2["level3"], "Clone should preserve deeply nested value")

	// Modify deeply nested value in clone
	level2["level3"] = "modified"

	// Verify original is unchanged (deep copy validation)
	origLevel1, ok := original.With["level1"].(map[string]any)
	require.True(t, ok, "Original should still have nested map")
	origLevel2, ok := origLevel1["level2"].(map[string]any)
	require.True(t, ok, "Original should still have deeply nested map")
	// Note: Current implementation does shallow copy, so this test documents existing behavior
	// In a true deep copy, this would be "value" instead of "modified"
	assert.Equal(t, "modified", origLevel2["level3"], "Current Clone implementation does shallow copy of nested maps")
}

func TestToYAML_ErrorHandling(t *testing.T) {
	tests := []struct {
		name        string
		step        *WorkflowStep
		wantErr     bool
		description string
	}{
		{
			name: "step with complex nested maps",
			step: &WorkflowStep{
				Name: "Complex step",
				Uses: "some/action@v1",
				With: map[string]any{
					"nested": map[string]any{
						"deep": map[string]any{
							"value": "test",
						},
					},
				},
			},
			wantErr:     false,
			description: "should handle complex nested structures",
		},
		{
			name: "step with special characters",
			step: &WorkflowStep{
				Name: "Test: Special chars!",
				Run:  "echo 'hello' && echo \"world\"",
			},
			wantErr:     false,
			description: "should handle special characters in strings",
		},
		{
			name: "step with multiline run",
			step: &WorkflowStep{
				Name: "Multiline",
				Run:  "line1\nline2\nline3",
			},
			wantErr:     false,
			description: "should handle multiline strings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml, err := tt.step.ToYAML()
			if tt.wantErr {
				require.Error(t, err, "ToYAML should return error for %s", tt.description)
			} else {
				require.NoError(t, err, "ToYAML should succeed for %s", tt.description)
				assert.NotEmpty(t, yaml, "ToYAML should return non-empty YAML for %s", tt.description)
			}
		})
	}
}

func TestSliceToSteps_MixedValidInvalid(t *testing.T) {
	tests := []struct {
		name        string
		input       []any
		wantErr     bool
		description string
	}{
		{
			name: "invalid step type in middle",
			input: []any{
				map[string]any{"name": "Step 1", "run": "echo 1"},
				"not a map",
				map[string]any{"name": "Step 3", "run": "echo 3"},
			},
			wantErr:     true,
			description: "should fail on first invalid step type",
		},
		{
			name: "all valid steps",
			input: []any{
				map[string]any{"name": "Step 1", "run": "echo 1"},
				map[string]any{"name": "Step 2", "uses": "actions/checkout@v4"},
				map[string]any{"name": "Step 3", "run": "echo 3"},
			},
			wantErr:     false,
			description: "should succeed with all valid steps",
		},
		{
			name: "empty map as step",
			input: []any{
				map[string]any{},
			},
			wantErr:     false,
			description: "should handle empty step map",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps, err := SliceToSteps(tt.input)
			if tt.wantErr {
				require.Error(t, err, "SliceToSteps should return error for %s", tt.description)
			} else {
				require.NoError(t, err, "SliceToSteps should succeed for %s", tt.description)
				assert.Len(t, steps, len(tt.input), "SliceToSteps should return correct number of steps for %s", tt.description)
			}
		})
	}
}
