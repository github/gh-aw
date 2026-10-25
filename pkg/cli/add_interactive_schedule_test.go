//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateSelectedSchedulePreservesOtherTriggers(t *testing.T) {
	t.Parallel()
	content := "---\non:\n  schedule: daily\n  workflow_dispatch:\n  push:\n    branches: [main]\n---\n# Report\n"
	detection := detectWorkflowScheduleInfo(content)
	updated, changed := updateSelectedSchedule(content, "weekly", detection)
	require.True(t, changed)
	assert.Contains(t, updated, "workflow_dispatch:")
	assert.Contains(t, updated, "push:")
	assert.Contains(t, updated, "branches:")
	assert.Contains(t, updated, "# Report")
	assert.Equal(t, "weekly", detectWorkflowScheduleInfo(updated).Frequency)
}

func TestUpdateSelectedScheduleRejectsUnknownSelection(t *testing.T) {
	t.Parallel()
	updated, changed := updateSelectedSchedule("---\non: daily\n---\n", "unknown", scheduleDetection{})
	assert.False(t, changed)
	assert.Empty(t, updated)
}

func TestUpdateSelectedScheduleUpdatesScalarTrigger(t *testing.T) {
	t.Parallel()
	content := "---\non: daily\n---\n# Report\n"
	updated, changed := updateSelectedSchedule(content, "weekly", detectWorkflowScheduleInfo(content))
	require.True(t, changed)
	assert.Equal(t, "weekly", detectWorkflowScheduleInfo(updated).Frequency)
	assert.Contains(t, updated, "# Report")
}

func TestClassifyScheduleFrequency(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Friendly format — direct string matches
		{name: "hourly keyword", input: "hourly", expected: "hourly"},
		{name: "every 1h", input: "every 1h", expected: "hourly"},
		{name: "every 1 hour", input: "every 1 hour", expected: "hourly"},
		{name: "every 1 hours", input: "every 1 hours", expected: "hourly"},
		{name: "every 3h", input: "every 3h", expected: "3-hourly"},
		{name: "every 3 hours", input: "every 3 hours", expected: "3-hourly"},
		{name: "daily keyword", input: "daily", expected: "daily"},
		{name: "weekly keyword", input: "weekly", expected: "weekly"},

		// Case insensitive
		{name: "DAILY upper", input: "DAILY", expected: "daily"},
		{name: "Weekly mixed", input: "Weekly", expected: "weekly"},

		// FUZZY placeholders
		{name: "fuzzy hourly/1", input: "FUZZY:HOURLY/1 * * *", expected: "hourly"},
		{name: "fuzzy hourly/3", input: "FUZZY:HOURLY/3 * * *", expected: "3-hourly"},
		{name: "fuzzy daily", input: "FUZZY:DAILY * * *", expected: "daily"},
		{name: "fuzzy daily around", input: "FUZZY:DAILY_AROUND:14:0 * * *", expected: "daily"},
		{name: "fuzzy weekly", input: "FUZZY:WEEKLY * * *", expected: "weekly"},
		{name: "fuzzy weekly with day", input: "FUZZY:WEEKLY:1 * * *", expected: "weekly"},

		// Standard cron expressions — daily pattern
		{name: "cron daily midnight", input: "0 0 * * *", expected: "daily"},
		{name: "cron daily 9am", input: "0 9 * * *", expected: "daily"},
		{name: "cron daily 14:30", input: "30 14 * * *", expected: "daily"},

		// Standard cron — weekly pattern
		{name: "cron weekly monday", input: "0 0 * * 1", expected: "weekly"},
		{name: "cron weekly friday", input: "30 9 * * 5", expected: "weekly"},

		// Standard cron — hourly patterns
		{name: "cron every 1 hour", input: "0 */1 * * *", expected: "hourly"},
		{name: "cron every 3 hours", input: "0 */3 * * *", expected: "3-hourly"},
		{name: "cron every 6 hours (custom)", input: "0 */6 * * *", expected: "custom"},

		// Monthly cron pattern
		{name: "cron monthly 1st", input: "0 0 1 * *", expected: "monthly"},
		{name: "cron monthly 15th", input: "0 9 15 * *", expected: "monthly"},

		// Custom / unrecognised
		{name: "cron every 2 days", input: "0 0 */2 * *", expected: "custom"},
		{name: "random cron", input: "15 10 * * 1-5", expected: "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := classifyScheduleFrequency(tt.input)
			assert.Equal(t, tt.expected, got, "classifyScheduleFrequency(%q)", tt.input)
		})
	}
}

func TestDetectWorkflowScheduleInfo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		content            string
		wantRawExpr        string
		wantFrequency      string
		wantIsUpdatable    bool
		wantIsMultiTrigger bool
		wantIsOnMap        bool
	}{
		{
			name: "simple on: daily",
			content: `---
on: daily
engine: copilot
---
# Daily report
`,
			wantRawExpr:     "daily",
			wantFrequency:   "daily",
			wantIsUpdatable: true,
		},
		{
			name: "simple on: weekly",
			content: `---
on: weekly
engine: copilot
---
`,
			wantRawExpr:     "weekly",
			wantFrequency:   "weekly",
			wantIsUpdatable: true,
		},
		{
			name: "simple on: hourly",
			content: `---
on: hourly
engine: copilot
---
`,
			wantRawExpr:     "hourly",
			wantFrequency:   "hourly",
			wantIsUpdatable: true,
		},
		{
			name: "simple on: every 3h",
			content: `---
on: every 3h
engine: copilot
---
`,
			wantRawExpr:     "every 3h",
			wantFrequency:   "3-hourly",
			wantIsUpdatable: true,
		},
		{
			name: "on: with custom cron",
			content: `---
on: "0 14 * * 1-5"
engine: copilot
---
`,
			wantRawExpr:     "0 14 * * 1-5",
			wantFrequency:   "custom",
			wantIsUpdatable: true,
		},
		{
			name: "on: map with multiple cron entries (not updatable)",
			content: `---
on:
  schedule:
    - cron: "0 9 * * 1"  # Monday 9AM
    - cron: "0 17 * * 5" # Friday 5PM
  workflow_dispatch:
engine: copilot
---
`,
			wantRawExpr:     "",
			wantFrequency:   "",
			wantIsUpdatable: false,
		},
		{
			name: "on: map with schedule array and workflow_dispatch",
			content: `---
on:
  schedule:
    - cron: daily
  workflow_dispatch:
engine: copilot
---
`,
			wantRawExpr:     "daily",
			wantFrequency:   "daily",
			wantIsUpdatable: true,
			wantIsOnMap:     true,
		},
		{
			name: "on: map with schedule string shorthand",
			content: `---
on:
  schedule: weekly
  workflow_dispatch:
engine: copilot
---
`,
			wantRawExpr:     "weekly",
			wantFrequency:   "weekly",
			wantIsUpdatable: true,
			wantIsOnMap:     true,
		},
		{
			name: "on: map with schedule and slash_command (multi-trigger, updatable)",
			content: `---
on:
  schedule: daily
  workflow_dispatch:
  slash_command:
    name: repo-assist
  reaction: "eyes"
engine: copilot
---
`,
			wantRawExpr:        "daily",
			wantFrequency:      "daily",
			wantIsUpdatable:    true,
			wantIsMultiTrigger: true,
			wantIsOnMap:        true,
		},
		{
			name: "on: map with schedule and push (multi-trigger, updatable)",
			content: `---
on:
  schedule:
    - cron: daily
  push:
    branches: [main]
engine: copilot
---
`,
			wantRawExpr:        "daily",
			wantFrequency:      "daily",
			wantIsUpdatable:    true,
			wantIsMultiTrigger: true,
			wantIsOnMap:        true,
		},
		{
			name: "on: workflow_dispatch only (not a schedule)",
			content: `---
on:
  workflow_dispatch:
engine: copilot
---
`,
			wantRawExpr:     "",
			wantFrequency:   "",
			wantIsUpdatable: false,
		},
		{
			name: "no frontmatter",
			content: `# Just markdown
No frontmatter here.
`,
			wantRawExpr:     "",
			wantFrequency:   "",
			wantIsUpdatable: false,
		},
		{
			name: "on: non-schedule string (push event)",
			content: `---
on: push
engine: copilot
---
`,
			wantRawExpr:     "",
			wantFrequency:   "",
			wantIsUpdatable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			detection := detectWorkflowScheduleInfo(tt.content)
			assert.Equal(t, tt.wantRawExpr, detection.RawExpr, "raw expression")
			assert.Equal(t, tt.wantFrequency, detection.Frequency, "frequency")
			assert.Equal(t, tt.wantIsUpdatable, detection.IsUpdatable, "is updatable")
			assert.Equal(t, tt.wantIsMultiTrigger, detection.IsMultiTrigger, "is multi-trigger")
			assert.Equal(t, tt.wantIsOnMap, detection.IsOnMap, "is on map")
		})
	}
}

func TestUpdateScheduleInOnBlock(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		content     string
		newExpr     string
		wantErr     bool
		wantContain []string // substrings that must appear in the result
		wantAbsent  []string // substrings that must NOT appear in the result
	}{
		{
			name: "updates scalar schedule, preserves workflow_dispatch",
			content: `---
on:
  schedule: daily
  workflow_dispatch:
engine: copilot
---
# My workflow
`,
			newExpr:     "weekly",
			wantContain: []string{"schedule: weekly", "workflow_dispatch:", "engine: copilot"},
			wantAbsent:  []string{"schedule: daily"},
		},
		{
			name: "updates cron-list schedule to scalar, preserves workflow_dispatch",
			content: `---
on:
  schedule:
    - cron: daily
  workflow_dispatch:
engine: copilot
---
`,
			newExpr:     "weekly",
			wantContain: []string{"schedule: weekly", "workflow_dispatch:"},
			wantAbsent:  []string{"- cron:", "schedule:\n"},
		},
		{
			name: "updates schedule in multi-trigger on: block, preserves push trigger",
			content: `---
on:
  schedule:
    - cron: "0 9 * * *"
  push:
    branches: [main]
engine: copilot
---
`,
			newExpr:     "every 3h",
			wantContain: []string{"schedule: every 3h", "push:", "branches: [main]"},
			wantAbsent:  []string{"0 9 * * *", "- cron:"},
		},
		{
			name: "updates schedule with slash_command trigger preserved",
			content: `---
on:
  schedule: daily
  workflow_dispatch:
  slash_command:
    name: repo-assist
engine: copilot
---
`,
			newExpr:     "weekly",
			wantContain: []string{"schedule: weekly", "workflow_dispatch:", "slash_command:", "name: repo-assist"},
			wantAbsent:  []string{"schedule: daily"},
		},
		{
			name: "returns error when no schedule key inside on: block",
			content: `---
on:
  workflow_dispatch:
engine: copilot
---
`,
			newExpr: "daily",
			wantErr: true,
		},
		{
			name: "returns error when on: is a scalar (no block)",
			content: `---
on: daily
engine: copilot
---
`,
			newExpr: "weekly",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := UpdateScheduleInOnBlock(tt.content, tt.newExpr)
			if tt.wantErr {
				assert.Error(t, err, "expected an error")
				return
			}
			require.NoError(t, err, "unexpected error")
			for _, want := range tt.wantContain {
				assert.Contains(t, result, want, "result should contain %q", want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, result, absent, "result should not contain %q", absent)
			}
		})
	}
}

func TestBuildScheduleOptions(t *testing.T) {
	t.Parallel()
	t.Run("custom schedule puts custom first", func(t *testing.T) {
		t.Parallel()
		opts := buildScheduleOptions("15 10 * * 1-5", "custom")
		require.NotEmpty(t, opts, "options should not be empty")
		assert.Equal(t, "custom", opts[0].Value, "custom should be first option")
	})

	t.Run("daily schedule puts daily first", func(t *testing.T) {
		t.Parallel()
		opts := buildScheduleOptions("daily", "daily")
		require.NotEmpty(t, opts, "options should not be empty")
		assert.Equal(t, "daily", opts[0].Value, "daily should be first option")
	})

	t.Run("hourly schedule puts hourly first", func(t *testing.T) {
		t.Parallel()
		opts := buildScheduleOptions("every 1h", "hourly")
		require.NotEmpty(t, opts, "options should not be empty")
		assert.Equal(t, "hourly", opts[0].Value, "hourly should be first option")
	})

	t.Run("weekly schedule puts weekly first", func(t *testing.T) {
		t.Parallel()
		opts := buildScheduleOptions("weekly", "weekly")
		require.NotEmpty(t, opts, "options should not be empty")
		assert.Equal(t, "weekly", opts[0].Value, "weekly should be first option")
	})

	t.Run("3-hourly schedule puts 3-hourly first", func(t *testing.T) {
		t.Parallel()
		opts := buildScheduleOptions("every 3h", "3-hourly")
		require.NotEmpty(t, opts, "options should not be empty")
		assert.Equal(t, "3-hourly", opts[0].Value, "3-hourly should be first option")
	})

	t.Run("monthly schedule puts monthly first", func(t *testing.T) {
		t.Parallel()
		opts := buildScheduleOptions("0 0 1 * *", "monthly")
		require.NotEmpty(t, opts, "options should not be empty")
		assert.Equal(t, "monthly", opts[0].Value, "monthly should be first option")
	})

	t.Run("includes all standard frequencies", func(t *testing.T) {
		t.Parallel()
		opts := buildScheduleOptions("daily", "daily")
		require.NotEmpty(t, opts, "options should not be empty")
		values := make(map[string]struct{})
		for _, o := range opts {
			values[o.Value] = struct{}{}
		}
		assert.Contains(t, values, "hourly", "hourly should be included")
		assert.Contains(t, values, "3-hourly", "3-hourly should be included")
		assert.Contains(t, values, "daily", "daily should be included")
		assert.Contains(t, values, "weekly", "weekly should be included")
		assert.Contains(t, values, "monthly", "monthly should be included")
	})

	t.Run("custom schedule has no duplicate custom option", func(t *testing.T) {
		t.Parallel()
		opts := buildScheduleOptions("15 10 * * 1-5", "custom")
		count := 0
		for _, o := range opts {
			if o.Value == "custom" {
				count++
			}
		}
		assert.Equal(t, 1, count, "custom should appear exactly once")
	})

	t.Run("marks current frequency in label", func(t *testing.T) {
		t.Parallel()
		opts := buildScheduleOptions("daily", "daily")
		for _, o := range opts {
			if o.Value == "daily" {
				assert.Contains(t, o.Key, "(current)", "current frequency label should contain '(current)'")
			}
		}
	})
}
