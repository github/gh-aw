//go:build !integration

package workflow

import (
	"fmt"
	"strings"
	"testing"
)

// TestScheduleWorkflowDispatchAutomatic verifies that workflow_dispatch is automatically
// added to workflows with schedule triggers in object format
func TestScheduleWorkflowDispatchAutomatic(t *testing.T) {
	tests := []struct {
		name                   string
		frontmatter            map[string]any
		expectedCron           string
		expectWorkflowDispatch bool
	}{
		{
			name: "schedule array format - should add workflow_dispatch",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": []any{
						map[string]any{
							"cron": "0 2 * * *",
						},
					},
				},
			},
			expectedCron:           "0 2 * * *",
			expectWorkflowDispatch: true,
		},
		{
			name: "schedule string format - should add workflow_dispatch",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": "0 2 * * *",
				},
			},
			expectedCron:           "0 2 * * *",
			expectWorkflowDispatch: true,
		},
		{
			name: "schedule with existing workflow_dispatch - should keep it",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": []any{
						map[string]any{
							"cron": "0 2 * * *",
						},
					},
					"workflow_dispatch": map[string]any{
						"inputs": map[string]any{
							"test": map[string]any{
								"description": "test input",
							},
						},
					},
				},
			},
			expectedCron:           "0 2 * * *",
			expectWorkflowDispatch: true,
		},
		{
			name: "multiple schedules - should add workflow_dispatch",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": []any{
						map[string]any{
							"cron": "0 2 * * *",
						},
						map[string]any{
							"cron": "weekly on friday",
						},
					},
				},
			},
			expectedCron:           "0 2 * * *",
			expectWorkflowDispatch: true,
		},
		{
			name: "schedule with other triggers - should add workflow_dispatch",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"branches": []any{"main"},
					},
					"schedule": []any{
						map[string]any{
							"cron": "0 9 * * 1",
						},
					},
				},
			},
			expectedCron:           "0 9 * * 1",
			expectWorkflowDispatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			// Set workflow identifier for fuzzy schedule scattering
			compiler.SetWorkflowIdentifier("test-workflow.md")

			err := compiler.preprocessScheduleFields(tt.frontmatter, "", "")
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Check that "on" was converted to a map
			onValue, exists := tt.frontmatter["on"]
			if !exists {
				t.Error("expected 'on' field to exist")
				return
			}

			onMap, ok := onValue.(map[string]any)
			if !ok {
				t.Errorf("expected 'on' to be a map, got %T", onValue)
				return
			}

			// Check schedule field exists
			scheduleValue, hasSchedule := onMap["schedule"]
			if !hasSchedule {
				t.Error("expected 'schedule' field in 'on' map")
				return
			}

			// Check the cron expression
			scheduleArray, ok := scheduleValue.([]any)
			if !ok {
				t.Errorf("expected schedule to be array, got %T", scheduleValue)
				return
			}

			if len(scheduleArray) == 0 {
				t.Error("expected at least one schedule item")
				return
			}

			firstSchedule, ok := scheduleArray[0].(map[string]any)
			if !ok {
				t.Errorf("expected first schedule to be map, got %T", scheduleArray[0])
				return
			}

			actualCron, ok := firstSchedule["cron"].(string)
			if !ok {
				t.Errorf("expected cron to be string, got %T", firstSchedule["cron"])
				return
			}

			if tt.expectedCron != "" && actualCron != tt.expectedCron {
				t.Errorf("expected cron '%s', got '%s'", tt.expectedCron, actualCron)
			}

			// Check workflow_dispatch field
			if tt.expectWorkflowDispatch {
				if _, hasWorkflowDispatch := onMap["workflow_dispatch"]; !hasWorkflowDispatch {
					t.Error("expected 'workflow_dispatch' field in 'on' map but it was not added")
					return
				}
			}
		})
	}
}

func TestSchedulePreprocessingShorthandOnString(t *testing.T) {
	tests := []struct {
		name                   string
		frontmatter            map[string]any
		checkScattered         bool // Check if fuzzy was scattered to valid cron
		expectedCron           string
		expectedError          bool
		errorSubstring         string
		expectWorkflowDispatch bool
	}{
		{
			name: "on: daily",
			frontmatter: map[string]any{
				"on": "daily",
			},
			checkScattered:         true, // Fuzzy schedule, should be scattered
			expectWorkflowDispatch: true,
		},
		{
			name: "on: weekly",
			frontmatter: map[string]any{
				"on": "weekly",
			},
			checkScattered:         true, // Fuzzy schedule, should be scattered
			expectWorkflowDispatch: true,
		},
		{
			name: "on: daily at 14:00",
			frontmatter: map[string]any{
				"on": "daily at 14:00",
			},
			expectedError:  true, // Now rejected
			errorSubstring: "'daily at <time>' syntax is not supported",
		},
		{
			name: "on: weekly on monday",
			frontmatter: map[string]any{
				"on": "weekly on monday",
			},
			checkScattered:         true, // Fuzzy schedule, should be scattered
			expectWorkflowDispatch: true,
		},
		{
			name: "on: every 10 minutes",
			frontmatter: map[string]any{
				"on": "every 10 minutes",
			},
			expectedCron:           "*/10 * * * *",
			expectWorkflowDispatch: true,
		},
		{
			name: "on: 0 9 * * 1 (cron expression)",
			frontmatter: map[string]any{
				"on": "0 9 * * 1",
			},
			expectedCron:           "0 9 * * 1",
			expectWorkflowDispatch: true,
		},
		{
			name: "on: push (not a schedule)",
			frontmatter: map[string]any{
				"on": "push",
			},
			expectedCron:           "",
			expectWorkflowDispatch: false,
		},
		{
			name: "on: invalid schedule",
			frontmatter: map[string]any{
				"on": "invalid schedule format",
			},
			expectedCron:           "",
			expectWorkflowDispatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			// Set workflow identifier for fuzzy schedule scattering
			// (required for all schedule tests to avoid fuzzy schedule errors)
			compiler.SetWorkflowIdentifier("test-workflow.md")

			err := compiler.preprocessScheduleFields(tt.frontmatter, "", "")

			if tt.expectedError {
				if err == nil {
					t.Errorf("expected error containing '%s', got nil", tt.errorSubstring)
					return
				}
				if !strings.Contains(err.Error(), tt.errorSubstring) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errorSubstring, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// If expectWorkflowDispatch is false, "on" should still be a string
			if !tt.expectWorkflowDispatch {
				onValue, exists := tt.frontmatter["on"]
				if !exists {
					t.Error("expected 'on' field to exist")
					return
				}
				if _, ok := onValue.(string); !ok {
					t.Errorf("expected 'on' to remain a string for non-schedule value")
				}
				return
			}

			// Check that "on" was converted to a map with schedule and workflow_dispatch
			onValue, exists := tt.frontmatter["on"]
			if !exists {
				t.Error("expected 'on' field to exist")
				return
			}

			onMap, ok := onValue.(map[string]any)
			if !ok {
				t.Errorf("expected 'on' to be converted to map, got %T", onValue)
				return
			}

			// Check schedule field exists
			scheduleValue, hasSchedule := onMap["schedule"]
			if !hasSchedule {
				t.Error("expected 'schedule' field in 'on' map")
				return
			}

			// Check workflow_dispatch field exists
			if _, hasWorkflowDispatch := onMap["workflow_dispatch"]; !hasWorkflowDispatch {
				t.Error("expected 'workflow_dispatch' field in 'on' map")
				return
			}

			// Check the cron expression
			scheduleArray, ok := scheduleValue.([]any)
			if !ok {
				t.Errorf("expected schedule to be array, got %T", scheduleValue)
				return
			}

			if len(scheduleArray) == 0 {
				t.Error("expected at least one schedule item")
				return
			}

			firstSchedule, ok := scheduleArray[0].(map[string]any)
			if !ok {
				t.Errorf("expected first schedule to be map, got %T", scheduleArray[0])
				return
			}

			actualCron, ok := firstSchedule["cron"].(string)
			if !ok {
				t.Errorf("expected cron to be string, got %T", firstSchedule["cron"])
				return
			}

			if tt.checkScattered {
				// Should be scattered to a valid cron (not fuzzy)
				if strings.HasPrefix(actualCron, "FUZZY:") {
					t.Errorf("expected scattered cron, got fuzzy: %s", actualCron)
				}
				// Verify it's a valid cron expression
				fields := strings.Fields(actualCron)
				if len(fields) != 5 {
					t.Errorf("expected 5 fields in cron expression, got %d: %s", len(fields), actualCron)
				}
				t.Logf("Successfully scattered schedule to: %s", actualCron)
			} else if tt.expectedCron != "" {
				if actualCron != tt.expectedCron {
					t.Errorf("expected cron '%s', got '%s'", tt.expectedCron, actualCron)
				}
			}
		})
	}
}

func TestSchedulePreprocessing(t *testing.T) {
	tests := []struct {
		name           string
		frontmatter    map[string]any
		expectedCron   string
		expectedError  bool
		errorSubstring string
	}{
		{
			name: "daily schedule",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": []any{
						map[string]any{
							"cron": "0 2 * * *",
						},
					},
				},
			},
			expectedCron: "0 2 * * *",
		},
		{
			name: "weekly schedule",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": []any{
						map[string]any{
							"cron": "30 6 * * 1",
						},
					},
				},
			},
			expectedCron: "30 6 * * 1",
		},
		{
			name: "interval schedule",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": []any{
						map[string]any{
							"cron": "every 10 minutes",
						},
					},
				},
			},
			expectedCron: "*/10 * * * *",
		},
		{
			name: "existing cron expression unchanged",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": []any{
						map[string]any{
							"cron": "0 9 * * 1",
						},
					},
				},
			},
			expectedCron: "0 9 * * 1",
		},
		{
			name: "multiple schedules",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": []any{
						map[string]any{
							"cron": "0 2 * * *",
						},
						map[string]any{
							"cron": "0 17 * * 5",
						},
					},
				},
			},
			expectedCron: "0 2 * * *", // First one
		},
		{
			name: "invalid schedule format",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": []any{
						map[string]any{
							"cron": "invalid schedule format",
						},
					},
				},
			},
			expectedError:  true,
			errorSubstring: "invalid schedule expression",
		},
		// New tests for shorthand string format
		{
			name: "shorthand string format - daily",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": "0 2 * * *",
				},
			},
			expectedCron: "0 2 * * *",
		},
		{
			name: "shorthand string format - weekly",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": "30 6 * * 1",
				},
			},
			expectedCron: "30 6 * * 1",
		},
		{
			name: "shorthand string format - interval",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": "every 10 minutes",
				},
			},
			expectedCron: "*/10 * * * *",
		},
		{
			name: "shorthand string format - existing cron",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": "0 9 * * 1",
				},
			},
			expectedCron: "0 9 * * 1",
		},
		{
			name: "shorthand string format - invalid",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": "invalid format",
				},
			},
			expectedError:  true,
			errorSubstring: "invalid schedule expression",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			err := compiler.preprocessScheduleFields(tt.frontmatter, "", "")

			if tt.expectedError {
				if err == nil {
					t.Errorf("expected error containing '%s', got nil", tt.errorSubstring)
					return
				}
				if !strings.Contains(err.Error(), tt.errorSubstring) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errorSubstring, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Check that the cron expression was updated
			onMap := tt.frontmatter["on"].(map[string]any)
			scheduleArray := onMap["schedule"].([]any)
			firstSchedule := scheduleArray[0].(map[string]any)
			actualCron := firstSchedule["cron"].(string)

			if actualCron != tt.expectedCron {
				t.Errorf("expected cron '%s', got '%s'", tt.expectedCron, actualCron)
			}
		})
	}
}

func TestScheduleFriendlyComments(t *testing.T) {
	// Create a test frontmatter with a fuzzy schedule that will have a friendly format
	frontmatter := map[string]any{
		"on": map[string]any{
			"schedule": []any{
				map[string]any{
					"cron": "daily around 14:00",
				},
			},
		},
	}

	compiler := NewCompiler()
	compiler.SetWorkflowIdentifier("test-workflow.md")

	// Preprocess to convert and store friendly formats
	err := compiler.preprocessScheduleFields(frontmatter, "", "")
	if err != nil {
		t.Fatalf("preprocessing failed: %v", err)
	}

	// Create test YAML output
	yamlStr := `"on":
  schedule:
  - cron: "30 13 * * *"
  workflow_dispatch:`

	// Add friendly comments
	result := compiler.addFriendlyScheduleComments(yamlStr, frontmatter)

	// Check that the comment was added
	if !strings.Contains(result, "# Friendly format: daily around 14:00") {
		t.Errorf("expected friendly format comment to be added, got:\n%s", result)
	}

	// Check that the cron expression is still there
	if !strings.Contains(result, `cron: "30 13 * * *"`) {
		t.Errorf("expected cron expression to remain, got:\n%s", result)
	}
}

func TestFuzzyScheduleScattering(t *testing.T) {
	tests := []struct {
		name               string
		frontmatter        map[string]any
		workflowIdentifier string
		checkScattered     bool // If true, verify the result is scattered (not fuzzy)
		expectError        bool // If true, expect an error (fuzzy without identifier)
		errorSubstring     string
	}{
		{
			name: "fuzzy daily schedule with identifier",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": []any{
						map[string]any{
							"cron": "daily",
						},
					},
				},
			},
			workflowIdentifier: "workflow-a.md",
			checkScattered:     true,
			expectError:        false,
		},
		{
			name: "fuzzy daily schedule without identifier",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": []any{
						map[string]any{
							"cron": "daily",
						},
					},
				},
			},
			workflowIdentifier: "", // No identifier, should error
			checkScattered:     false,
			expectError:        true,
			errorSubstring:     "fuzzy cron expression",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			if tt.workflowIdentifier != "" {
				compiler.SetWorkflowIdentifier(tt.workflowIdentifier)
			}

			err := compiler.preprocessScheduleFields(tt.frontmatter, "", "")

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing '%s', got nil", tt.errorSubstring)
					return
				}
				if !strings.Contains(err.Error(), tt.errorSubstring) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errorSubstring, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Check that the cron expression was updated
			onMap := tt.frontmatter["on"].(map[string]any)
			scheduleArray := onMap["schedule"].([]any)
			firstSchedule := scheduleArray[0].(map[string]any)
			actualCron := firstSchedule["cron"].(string)

			if tt.checkScattered {
				// Should be scattered (not fuzzy)
				if strings.HasPrefix(actualCron, "FUZZY:") {
					t.Errorf("expected scattered schedule, got fuzzy: %s", actualCron)
				}
				// Should be a valid daily cron
				fields := strings.Fields(actualCron)
				if len(fields) != 5 {
					t.Errorf("expected 5 fields in cron, got %d: %s", len(fields), actualCron)
				}
			}
		})
	}
}

func TestFuzzyScheduleScatteringDeterministic(t *testing.T) {
	// Test that scattering is deterministic - same workflow ID produces same result
	workflows := []string{"workflow-a.md", "workflow-b.md", "workflow-c.md", "workflow-a.md"}

	results := make([]string, len(workflows))
	for i, wf := range workflows {
		frontmatter := map[string]any{
			"on": map[string]any{
				"schedule": []any{
					map[string]any{
						"cron": "daily",
					},
				},
			},
		}

		compiler := NewCompiler()
		compiler.SetWorkflowIdentifier(wf)

		err := compiler.preprocessScheduleFields(frontmatter, "", "")
		if err != nil {
			t.Fatalf("unexpected error for workflow %s: %v", wf, err)
		}

		onMap := frontmatter["on"].(map[string]any)
		scheduleArray := onMap["schedule"].([]any)
		firstSchedule := scheduleArray[0].(map[string]any)
		results[i] = firstSchedule["cron"].(string)
	}

	// workflow-a.md should produce the same result both times
	if results[0] != results[3] {
		t.Errorf("Scattering not deterministic: workflow-a.md produced %s and %s", results[0], results[3])
	}

	// Different workflows should produce different results (with high probability)
	if results[0] == results[1] && results[1] == results[2] {
		t.Errorf("Scattering produced identical results for all workflows: %s", results[0])
	}
}

func TestSchedulePreprocessingWithFuzzyDaily(t *testing.T) {
	// Test various fuzzy daily schedule formats
	tests := []struct {
		name          string
		frontmatter   map[string]any
		checkScatter  bool
		expectError   bool
		errorContains string
	}{
		{
			name: "fuzzy daily - shorthand string",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": "daily",
				},
			},
			checkScatter: true,
		},
		{
			name: "fuzzy daily - array format",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": []any{
						map[string]any{
							"cron": "daily",
						},
					},
				},
			},
			checkScatter: true,
		},
		{
			name: "fuzzy daily at specific time",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": "daily at 14:30",
				},
			},
			expectError:   true, // Now rejected
			errorContains: "'daily at <time>' syntax is not supported",
		},
		{
			name: "fuzzy daily around specific time",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": "daily around 14:30",
				},
			},
			checkScatter: true, // This uses "around", so it should be scattered
		},
		{
			name: "fuzzy daily with multiple schedules",
			frontmatter: map[string]any{
				"on": map[string]any{
					"schedule": []any{
						map[string]any{
							"cron": "daily",
						},
						map[string]any{
							"cron": "weekly on monday",
						},
					},
				},
			},
			checkScatter: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.SetWorkflowIdentifier("test-workflow.md")

			err := compiler.preprocessScheduleFields(tt.frontmatter, "", "")

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing '%s', got nil", tt.errorContains)
					return
				}
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errorContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Extract the cron expression
			onMap := tt.frontmatter["on"].(map[string]any)

			var actualCron string
			switch schedule := onMap["schedule"].(type) {
			case []any:
				firstSchedule := schedule[0].(map[string]any)
				actualCron = firstSchedule["cron"].(string)
			case map[string]any:
				actualCron = schedule["cron"].(string)
			default:
				t.Fatalf("unexpected schedule type: %T", schedule)
			}

			// Verify it's not in fuzzy format anymore (should be scattered)
			if strings.HasPrefix(actualCron, "FUZZY:") {
				t.Errorf("schedule should have been scattered, still in fuzzy format: %s", actualCron)
			}

			// Verify it's a valid cron expression
			fields := strings.Fields(actualCron)
			if len(fields) != 5 {
				t.Errorf("expected 5 fields in cron expression, got %d: %s", len(fields), actualCron)
			}

			if tt.checkScatter {
				// For scattered daily schedules, verify it's a daily pattern
				if fields[2] != "*" || fields[3] != "*" || fields[4] != "*" {
					t.Errorf("expected daily pattern (minute hour * * *), got: %s", actualCron)
				}
				t.Logf("Successfully scattered fuzzy daily schedule to: %s", actualCron)
			}
		})
	}
}

func TestSchedulePreprocessingDailyVariations(t *testing.T) {
	// Test that "daily" produces a valid scattered schedule
	compiler := NewCompiler()
	compiler.SetWorkflowIdentifier("daily-variation-test.md")

	frontmatter := map[string]any{
		"on": map[string]any{
			"schedule": "daily",
		},
	}

	err := compiler.preprocessScheduleFields(frontmatter, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Extract and verify the scattered schedule
	onMap := frontmatter["on"].(map[string]any)

	// The schedule will be converted to array format during preprocessing
	var cronExpr string
	switch schedule := onMap["schedule"].(type) {
	case []any:
		firstSchedule := schedule[0].(map[string]any)
		cronExpr = firstSchedule["cron"].(string)
	case map[string]any:
		cronExpr = schedule["cron"].(string)
	default:
		t.Fatalf("unexpected schedule type: %T", schedule)
	}

	// Verify it's a valid daily cron expression
	fields := strings.Fields(cronExpr)
	if len(fields) != 5 {
		t.Fatalf("expected 5 fields in cron expression, got %d: %s", len(fields), cronExpr)
	}

	// Parse hour and minute to ensure they're valid
	var minute, hour int
	if _, err := fmt.Sscanf(fields[0], "%d", &minute); err != nil {
		t.Errorf("invalid minute field: %s", fields[0])
	}
	if _, err := fmt.Sscanf(fields[1], "%d", &hour); err != nil {
		t.Errorf("invalid hour field: %s", fields[1])
	}

	// Verify ranges
	if minute < 0 || minute > 59 {
		t.Errorf("minute should be 0-59, got: %d", minute)
	}
	if hour < 0 || hour > 23 {
		t.Errorf("hour should be 0-23, got: %d", hour)
	}

	// Verify daily pattern
	if fields[2] != "*" || fields[3] != "*" || fields[4] != "*" {
		t.Errorf("expected daily pattern (minute hour * * *), got: %s", cronExpr)
	}

	t.Logf("Successfully compiled 'daily' to valid cron: %s", cronExpr)
}

func TestSlashCommandShorthand(t *testing.T) {
	tests := []struct {
		name                  string
		frontmatter           map[string]any
		expectedCommand       string
		expectWorkflowDispath bool
		expectedError         bool
		errorSubstring        string
	}{
		{
			name: "on: /command",
			frontmatter: map[string]any{
				"on": "/my-bot",
			},
			expectedCommand:       "my-bot",
			expectWorkflowDispath: true,
		},
		{
			name: "on: /another-command",
			frontmatter: map[string]any{
				"on": "/code-review",
			},
			expectedCommand:       "code-review",
			expectWorkflowDispath: true,
		},
		{
			name: "on: / (empty command)",
			frontmatter: map[string]any{
				"on": "/",
			},
			expectedError:  true,
			errorSubstring: "slash command shorthand cannot be empty after '/'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.SetWorkflowIdentifier("test-workflow.md")

			err := compiler.preprocessScheduleFields(tt.frontmatter, "", "")

			if tt.expectedError {
				if err == nil {
					t.Errorf("expected error containing '%s', got nil", tt.errorSubstring)
					return
				}
				if !strings.Contains(err.Error(), tt.errorSubstring) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errorSubstring, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Check that "on" was converted to a map with slash_command and workflow_dispatch
			onValue, exists := tt.frontmatter["on"]
			if !exists {
				t.Error("expected 'on' field to exist")
				return
			}

			onMap, ok := onValue.(map[string]any)
			if !ok {
				t.Errorf("expected 'on' to be converted to map, got %T", onValue)
				return
			}

			// Check slash_command field exists and has correct value
			slashCommandValue, hasSlashCommand := onMap["slash_command"]
			if !hasSlashCommand {
				t.Error("expected 'slash_command' field in 'on' map")
				return
			}

			slashCommandStr, ok := slashCommandValue.(string)
			if !ok {
				t.Errorf("expected slash_command to be string, got %T", slashCommandValue)
				return
			}

			if slashCommandStr != tt.expectedCommand {
				t.Errorf("expected slash_command '%s', got '%s'", tt.expectedCommand, slashCommandStr)
			}

			// Check workflow_dispatch field exists
			if _, hasWorkflowDispatch := onMap["workflow_dispatch"]; !hasWorkflowDispatch {
				t.Error("expected 'workflow_dispatch' field in 'on' map")
				return
			}

			// Ensure there are no extra fields (should only have slash_command and workflow_dispatch)
			if len(onMap) != 2 {
				t.Errorf("expected exactly 2 fields in 'on' map, got %d: %v", len(onMap), onMap)
			}
		})
	}
}

// TestFuzzyScheduleScatteringWithRepositorySlug verifies that the repository slug (org/repo)
// is properly included in the hash computation for schedule scattering across an organization
func TestFuzzyScheduleScatteringWithRepositorySlug(t *testing.T) {
	tests := []struct {
		name               string
		workflowIdentifier string
		repositorySlug     string
		expectedSeedFormat string // Format used to verify seed construction
	}{
		{
			name:               "with repository slug",
			workflowIdentifier: "test-workflow.md",
			repositorySlug:     "github/gh-aw",
			expectedSeedFormat: "github/gh-aw/test-workflow.md",
		},
		{
			name:               "with different org, same workflow name",
			workflowIdentifier: "test-workflow.md",
			repositorySlug:     "otherorg/gh-aw",
			expectedSeedFormat: "otherorg/gh-aw/test-workflow.md",
		},
		{
			name:               "with different repo, same workflow name",
			workflowIdentifier: "test-workflow.md",
			repositorySlug:     "githubnext/other-repo",
			expectedSeedFormat: "githubnext/other-repo/test-workflow.md",
		},
		{
			name:               "without repository slug",
			workflowIdentifier: "test-workflow.md",
			repositorySlug:     "",
			expectedSeedFormat: "test-workflow.md",
		},
	}

	results := make(map[string]string)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of frontmatter for this test
			testFrontmatter := map[string]any{
				"on": map[string]any{
					"schedule": []any{
						map[string]any{
							"cron": "daily",
						},
					},
				},
			}

			compiler := NewCompiler()
			compiler.SetWorkflowIdentifier(tt.workflowIdentifier)
			if tt.repositorySlug != "" {
				compiler.SetRepositorySlug(tt.repositorySlug)
			}

			err := compiler.preprocessScheduleFields(testFrontmatter, "", "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			onMap := testFrontmatter["on"].(map[string]any)
			scheduleArray := onMap["schedule"].([]any)
			firstSchedule := scheduleArray[0].(map[string]any)
			actualCron := firstSchedule["cron"].(string)

			// Should be scattered (not fuzzy)
			if strings.HasPrefix(actualCron, "FUZZY:") {
				t.Errorf("expected scattered schedule, got fuzzy: %s", actualCron)
			}

			// Should be a valid daily cron
			fields := strings.Fields(actualCron)
			if len(fields) != 5 {
				t.Errorf("expected 5 fields in cron, got %d: %s", len(fields), actualCron)
			}

			// Store result for comparison
			results[tt.expectedSeedFormat] = actualCron
		})
	}

	// Verify that different org/repo combinations produce different schedules
	// for the same workflow name
	sameWorkflowDifferentOrg := results["github/gh-aw/test-workflow.md"]
	sameWorkflowOtherOrg := results["otherorg/gh-aw/test-workflow.md"]
	sameWorkflowOtherRepo := results["githubnext/other-repo/test-workflow.md"]
	workflowWithoutSlug := results["test-workflow.md"]

	// All should be different
	if sameWorkflowDifferentOrg == sameWorkflowOtherOrg {
		t.Errorf("Expected different schedules for different orgs, got same: %s", sameWorkflowDifferentOrg)
	}

	if sameWorkflowDifferentOrg == sameWorkflowOtherRepo {
		t.Errorf("Expected different schedules for different repos, got same: %s", sameWorkflowDifferentOrg)
	}

	if sameWorkflowOtherOrg == sameWorkflowOtherRepo {
		t.Errorf("Expected different schedules for different org/repo combinations, got same: %s", sameWorkflowOtherOrg)
	}

	// Workflow without slug should likely be different from one with slug
	// (This is a probabilistic check - they could theoretically collide)
	if sameWorkflowDifferentOrg == workflowWithoutSlug && sameWorkflowOtherOrg == workflowWithoutSlug && sameWorkflowOtherRepo == workflowWithoutSlug {
		t.Logf("Warning: All schedules with slug matched schedule without slug: %s (probabilistic collision)", workflowWithoutSlug)
	}

	t.Logf("Schedule results:")
	for seed, cron := range results {
		t.Logf("  Seed: %s -> Cron: %s", seed, cron)
	}
}

// TestFuzzyScheduleScatteringAcrossOrganization verifies that workflows with the same name
// in different repositories get different scattered schedules
func TestFuzzyScheduleScatteringAcrossOrganization(t *testing.T) {
	// Simulate multiple repositories in an organization with same workflow name
	repositories := []struct {
		slug         string
		workflowName string
	}{
		{"githubnext/repo-1", "ci.md"},
		{"githubnext/repo-2", "ci.md"},
		{"githubnext/repo-3", "ci.md"},
		{"other-org/repo-1", "ci.md"},
	}

	results := make(map[string]string)

	for _, repo := range repositories {
		frontmatter := map[string]any{
			"on": map[string]any{
				"schedule": []any{
					map[string]any{
						"cron": "daily",
					},
				},
			},
		}

		compiler := NewCompiler()
		compiler.SetRepositorySlug(repo.slug)
		compiler.SetWorkflowIdentifier(repo.workflowName)

		err := compiler.preprocessScheduleFields(frontmatter, "", "")
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", repo.slug, err)
		}

		onMap := frontmatter["on"].(map[string]any)
		scheduleArray := onMap["schedule"].([]any)
		firstSchedule := scheduleArray[0].(map[string]any)
		actualCron := firstSchedule["cron"].(string)

		results[repo.slug] = actualCron
		t.Logf("Repository %s: %s", repo.slug, actualCron)
	}

	// Verify that all schedules are different (high probability with good hash distribution)
	uniqueSchedules := make(map[string]bool)
	for _, cron := range results {
		uniqueSchedules[cron] = true
	}

	// With 4 repositories and good hash distribution, we should get 4 unique schedules
	// (or at least 3, allowing for small collision probability)
	if len(uniqueSchedules) < 3 {
		t.Errorf("Expected at least 3 unique schedules for 4 repositories, got %d unique schedules", len(uniqueSchedules))
		t.Logf("Schedules: %v", results)
	}

	// Verify that same org different repos get different schedules
	repo1Schedule := results["githubnext/repo-1"]
	repo2Schedule := results["githubnext/repo-2"]
	repo3Schedule := results["githubnext/repo-3"]

	sameOrg := 0
	if repo1Schedule == repo2Schedule {
		sameOrg++
	}
	if repo1Schedule == repo3Schedule {
		sameOrg++
	}
	if repo2Schedule == repo3Schedule {
		sameOrg++
	}

	// Allow at most 1 collision among 3 repos in same org
	if sameOrg > 1 {
		t.Errorf("Too many schedule collisions within same org: %d collisions among 3 repos", sameOrg)
	}
}

// TestFuzzyScheduleScatteringWarningWithoutRepoSlug verifies that a warning is shown
// when fuzzy schedule scattering occurs without repository slug
func TestFuzzyScheduleScatteringWarningWithoutRepoSlug(t *testing.T) {
	frontmatter := map[string]any{
		"on": map[string]any{
			"schedule": []any{
				map[string]any{
					"cron": "daily",
				},
			},
		},
	}

	compiler := NewCompiler()
	compiler.SetWorkflowIdentifier("test-workflow.md")
	// Explicitly NOT setting repository slug

	// Get initial warning count
	initialWarnings := compiler.GetWarningCount()

	err := compiler.preprocessScheduleFields(frontmatter, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify schedule was still scattered
	onMap := frontmatter["on"].(map[string]any)
	scheduleArray := onMap["schedule"].([]any)
	firstSchedule := scheduleArray[0].(map[string]any)
	actualCron := firstSchedule["cron"].(string)

	if strings.HasPrefix(actualCron, "FUZZY:") {
		t.Errorf("expected scattered schedule, got fuzzy: %s", actualCron)
	}

	// Verify warning was added
	finalWarnings := compiler.GetWarningCount()
	if finalWarnings <= initialWarnings {
		t.Errorf("expected warning count to increase, got initial=%d, final=%d", initialWarnings, finalWarnings)
	}

	// Verify warning message was added
	warnings := compiler.GetScheduleWarnings()
	foundWarning := false
	for _, warning := range warnings {
		if strings.Contains(warning, "repository context") && strings.Contains(warning, "Fuzzy schedule scattering") {
			foundWarning = true
			break
		}
	}

	if !foundWarning {
		t.Errorf("expected warning about missing repository context, got warnings: %v", warnings)
	}
}

// TestFriendlyFormatDeterminism verifies that friendly format comments are generated
// deterministically and don't carry over between compilations
func TestFriendlyFormatDeterminism(t *testing.T) {
	// Create a compiler instance
	compiler := NewCompiler()
	compiler.SetWorkflowIdentifier("test-workflow.md")

	// Frontmatter with daily schedule
	frontmatter1 := map[string]any{
		"on": map[string]any{
			"schedule": "daily",
		},
	}

	// First compilation - preprocess schedule
	err := compiler.preprocessScheduleFields(frontmatter1, "test-workflow-1.md", "")
	if err != nil {
		t.Fatalf("failed to preprocess schedule: %v", err)
	}

	// Verify friendly format was stored
	if len(compiler.scheduleFriendlyFormats) == 0 {
		t.Fatalf("expected friendly formats to be stored after preprocessing")
	}

	firstFormat := compiler.scheduleFriendlyFormats[0]
	if !strings.Contains(firstFormat, "daily") {
		t.Errorf("expected friendly format to contain 'daily', got: %s", firstFormat)
	}

	// Generate YAML for first compilation
	yamlStr1 := `"on":
  schedule:
  - cron: "30 13 * * *"
  workflow_dispatch:`

	result1 := compiler.addFriendlyScheduleComments(yamlStr1, frontmatter1)
	if !strings.Contains(result1, "# Friendly format: daily (scattered)") {
		t.Errorf("expected friendly format comment in first compilation, got:\n%s", result1)
	}

	// Second compilation with different frontmatter (weekly schedule)
	// Reset compiler state as would happen in a new compilation
	compiler.scheduleFriendlyFormats = nil

	frontmatter2 := map[string]any{
		"on": map[string]any{
			"schedule": "weekly",
		},
	}

	err = compiler.preprocessScheduleFields(frontmatter2, "test-workflow-2.md", "")
	if err != nil {
		t.Fatalf("failed to preprocess schedule: %v", err)
	}

	// Verify friendly format was replaced, not appended
	if len(compiler.scheduleFriendlyFormats) == 0 {
		t.Fatalf("expected friendly formats to be stored after second preprocessing")
	}

	secondFormat := compiler.scheduleFriendlyFormats[0]
	if !strings.Contains(secondFormat, "weekly") {
		t.Errorf("expected friendly format to contain 'weekly', got: %s", secondFormat)
	}

	// Verify the old format doesn't leak into the second compilation
	yamlStr2 := `"on":
  schedule:
  - cron: "15 9 * * 1"
  workflow_dispatch:`

	result2 := compiler.addFriendlyScheduleComments(yamlStr2, frontmatter2)
	if !strings.Contains(result2, "# Friendly format: weekly (scattered)") {
		t.Errorf("expected 'weekly' friendly format comment in second compilation, got:\n%s", result2)
	}
	if strings.Contains(result2, "# Friendly format: daily") {
		t.Errorf("unexpected 'daily' format leaking into second compilation, got:\n%s", result2)
	}

	// Third compilation - compile the first workflow again
	// This ensures the format is deterministic across repeated compilations
	compiler.scheduleFriendlyFormats = nil

	// Create a fresh frontmatter map (important: don't reuse the modified one)
	frontmatter3 := map[string]any{
		"on": map[string]any{
			"schedule": "daily",
		},
	}

	err = compiler.preprocessScheduleFields(frontmatter3, "test-workflow-1.md", "")
	if err != nil {
		t.Fatalf("failed to preprocess schedule in third compilation: %v", err)
	}

	result3 := compiler.addFriendlyScheduleComments(yamlStr1, frontmatter3)
	if !strings.Contains(result3, "# Friendly format: daily (scattered)") {
		t.Errorf("expected 'daily' friendly format comment in third compilation, got:\n%s", result3)
	}

	// Verify the results are identical for the same workflow
	if result1 != result3 {
		t.Errorf("expected identical results for same workflow, got:\n===First===\n%s\n===Third===\n%s", result1, result3)
	}
}
