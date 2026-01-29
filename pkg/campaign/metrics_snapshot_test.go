//go:build !integration

package campaign

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCampaignMetricsSnapshot_JSONMarshaling verifies that required fields
// are always present in the JSON output, even when they have zero values.
// This ensures compatibility with push_repo_memory.cjs validation which
// requires tasks_total and tasks_completed to be present (not undefined).
func TestCampaignMetricsSnapshot_JSONMarshaling(t *testing.T) {
	tests := []struct {
		name           string
		snapshot       CampaignMetricsSnapshot
		expectedFields []string
		omittedFields  []string
	}{
		{
			name: "all fields populated",
			snapshot: CampaignMetricsSnapshot{
				Date:                "2025-01-05",
				CampaignID:          "test-campaign",
				TasksTotal:          100,
				TasksCompleted:      50,
				TasksInProgress:     30,
				TasksBlocked:        5,
				VelocityPerDay:      7.5,
				EstimatedCompletion: "2025-02-15",
			},
			expectedFields: []string{
				"date", "campaign_id", "tasks_total", "tasks_completed",
				"tasks_in_progress", "tasks_blocked", "velocity_per_day", "estimated_completion",
			},
			omittedFields: []string{},
		},
		{
			name: "zero values for required fields (must be present)",
			snapshot: CampaignMetricsSnapshot{
				Date:           "2025-01-05",
				CampaignID:     "test-campaign",
				TasksTotal:     0, // Zero but must be present
				TasksCompleted: 0, // Zero but must be present
			},
			expectedFields: []string{
				"date", "campaign_id", "tasks_total", "tasks_completed",
			},
			omittedFields: []string{
				"tasks_in_progress", "tasks_blocked", "velocity_per_day", "estimated_completion",
			},
		},
		{
			name: "only required fields",
			snapshot: CampaignMetricsSnapshot{
				Date:           "2025-01-05",
				CampaignID:     "test-campaign",
				TasksTotal:     10,
				TasksCompleted: 5,
			},
			expectedFields: []string{
				"date", "campaign_id", "tasks_total", "tasks_completed",
			},
			omittedFields: []string{
				"tasks_in_progress", "tasks_blocked", "velocity_per_day", "estimated_completion",
			},
		},
		{
			name: "zero velocity should be omitted",
			snapshot: CampaignMetricsSnapshot{
				Date:           "2025-01-05",
				CampaignID:     "test-campaign",
				TasksTotal:     10,
				TasksCompleted: 5,
				VelocityPerDay: 0.0, // Zero optional field should be omitted
			},
			expectedFields: []string{
				"date", "campaign_id", "tasks_total", "tasks_completed",
			},
			omittedFields: []string{
				"tasks_in_progress", "tasks_blocked", "velocity_per_day", "estimated_completion",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON
			jsonBytes, err := json.Marshal(tt.snapshot)
			require.NoError(t, err, "Failed to marshal snapshot")

			// Unmarshal to map to check presence of fields
			var result map[string]interface{}
			err = json.Unmarshal(jsonBytes, &result)
			require.NoError(t, err, "Failed to unmarshal JSON")

			// Verify expected fields are present
			for _, field := range tt.expectedFields {
				assert.Contains(t, result, field, "Expected field %q to be present in JSON", field)
			}

			// Verify omitted fields are not present
			for _, field := range tt.omittedFields {
				assert.NotContains(t, result, field, "Expected field %q to be omitted from JSON", field)
			}

			// Specifically verify required integer fields are present even when zero
			if tt.snapshot.TasksTotal == 0 {
				assert.Contains(t, result, "tasks_total", "tasks_total must be present even when zero")
			}
			if tt.snapshot.TasksCompleted == 0 {
				assert.Contains(t, result, "tasks_completed", "tasks_completed must be present even when zero")
			}
		})
	}
}

// TestCampaignMetricsSnapshot_PushRepoMemoryValidation tests that the JSON
// output is compatible with push_repo_memory.cjs validation rules.
func TestCampaignMetricsSnapshot_PushRepoMemoryValidation(t *testing.T) {
	// This test simulates what push_repo_memory.cjs expects:
	// - campaign_id: required, non-empty string
	// - date: required, non-empty string in YYYY-MM-DD format
	// - tasks_total: required, must be present (not undefined), non-negative integer
	// - tasks_completed: required, must be present (not undefined), non-negative integer

	snapshot := CampaignMetricsSnapshot{
		Date:           "2025-01-05",
		CampaignID:     "file-size-reduction-project71",
		TasksTotal:     0,
		TasksCompleted: 0,
	}

	jsonBytes, err := json.Marshal(snapshot)
	require.NoError(t, err, "Failed to marshal snapshot")

	// Unmarshal to verify structure
	var result map[string]interface{}
	err = json.Unmarshal(jsonBytes, &result)
	require.NoError(t, err, "Failed to unmarshal JSON")

	// Validate required fields (matching push_repo_memory.cjs validation)
	assert.Contains(t, result, "campaign_id", "campaign_id must be present")
	assert.IsType(t, "", result["campaign_id"], "campaign_id must be a string")
	assert.NotEmpty(t, result["campaign_id"], "campaign_id must not be empty")

	assert.Contains(t, result, "date", "date must be present")
	assert.IsType(t, "", result["date"], "date must be a string")
	assert.NotEmpty(t, result["date"], "date must not be empty")

	assert.Contains(t, result, "tasks_total", "tasks_total must be present (not undefined)")
	assert.IsType(t, float64(0), result["tasks_total"], "tasks_total must be a number")
	assert.GreaterOrEqual(t, result["tasks_total"].(float64), 0.0, "tasks_total must be non-negative")

	assert.Contains(t, result, "tasks_completed", "tasks_completed must be present (not undefined)")
	assert.IsType(t, float64(0), result["tasks_completed"], "tasks_completed must be a number")
	assert.GreaterOrEqual(t, result["tasks_completed"].(float64), 0.0, "tasks_completed must be non-negative")
}

// TestCampaignMetricsSnapshot_RealWorldScenarios tests common real-world cases
// that might trigger the push_repo_memory validation error.
func TestCampaignMetricsSnapshot_RealWorldScenarios(t *testing.T) {
	tests := []struct {
		name        string
		snapshot    CampaignMetricsSnapshot
		description string
	}{
		{
			name: "new campaign with no tasks yet",
			snapshot: CampaignMetricsSnapshot{
				Date:           "2025-01-05",
				CampaignID:     "file-size-reduction-project71",
				TasksTotal:     0,
				TasksCompleted: 0,
			},
			description: "First run of orchestrator before any tasks discovered",
		},
		{
			name: "campaign just started",
			snapshot: CampaignMetricsSnapshot{
				Date:           "2025-01-05",
				CampaignID:     "file-size-reduction-project71",
				TasksTotal:     10,
				TasksCompleted: 0,
			},
			description: "Tasks discovered but none completed yet",
		},
		{
			name: "campaign in progress",
			snapshot: CampaignMetricsSnapshot{
				Date:                "2025-01-05",
				CampaignID:          "file-size-reduction-project71",
				TasksTotal:          100,
				TasksCompleted:      25,
				TasksInProgress:     15,
				TasksBlocked:        2,
				VelocityPerDay:      3.5,
				EstimatedCompletion: "2025-02-15",
			},
			description: "Active campaign with full metrics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBytes, err := json.Marshal(tt.snapshot)
			require.NoError(t, err, "Failed to marshal snapshot: %s", tt.description)

			var result map[string]interface{}
			err = json.Unmarshal(jsonBytes, &result)
			require.NoError(t, err, "Failed to unmarshal JSON: %s", tt.description)

			// Verify all required fields are present
			assert.Contains(t, result, "campaign_id")
			assert.Contains(t, result, "date")
			assert.Contains(t, result, "tasks_total")
			assert.Contains(t, result, "tasks_completed")
		})
	}
}
