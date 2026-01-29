//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetScheduleAtToAroundCodemod(t *testing.T) {
	codemod := getScheduleAtToAroundCodemod()

	assert.Equal(t, "schedule-at-to-around-migration", codemod.ID)
	assert.Equal(t, "Migrate schedule 'at' syntax to 'around' syntax", codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "0.5.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestScheduleCodemod_DailyAt(t *testing.T) {
	codemod := getScheduleAtToAroundCodemod()

	content := `---
on:
  schedule:
    - cron: daily at 09:00
---

# Test`

	frontmatter := map[string]any{
		"on": map[string]any{
			"schedule": []any{
				map[string]any{"cron": "daily at 09:00"},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "daily around 09:00")
	assert.NotContains(t, result, "daily at 09:00")
}

func TestScheduleCodemod_WeeklyOnAt(t *testing.T) {
	codemod := getScheduleAtToAroundCodemod()

	content := `---
on:
  schedule:
    - cron: weekly on Monday at 10:00
---

# Test`

	frontmatter := map[string]any{
		"on": map[string]any{
			"schedule": []any{
				map[string]any{"cron": "weekly on Monday at 10:00"},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "weekly on Monday around 10:00")
	assert.NotContains(t, result, "weekly on Monday at 10:00")
}

func TestScheduleCodemod_MonthlyOn(t *testing.T) {
	codemod := getScheduleAtToAroundCodemod()

	content := `---
on:
  schedule:
    - cron: monthly on 1
---

# Test`

	frontmatter := map[string]any{
		"on": map[string]any{
			"schedule": []any{
				map[string]any{"cron": "monthly on 1"},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "0 0 1 * *")
	assert.Contains(t, result, "# Converted from 'monthly on 1'")
}

func TestScheduleCodemod_MonthlyOnAt(t *testing.T) {
	codemod := getScheduleAtToAroundCodemod()

	content := `---
on:
  schedule:
    - cron: monthly on 15 at 14:00
---

# Test`

	frontmatter := map[string]any{
		"on": map[string]any{
			"schedule": []any{
				map[string]any{"cron": "monthly on 15 at 14:00"},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "0 9 15 * *")
	assert.Contains(t, result, "# Converted from 'monthly on 15 at 14:00'")
}

func TestScheduleCodemod_DailyAround_NoChange(t *testing.T) {
	codemod := getScheduleAtToAroundCodemod()

	content := `---
on:
  schedule:
    - cron: daily around 09:00
---

# Test`

	frontmatter := map[string]any{
		"on": map[string]any{
			"schedule": []any{
				map[string]any{"cron": "daily around 09:00"},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestScheduleCodemod_StandardCron_NoChange(t *testing.T) {
	codemod := getScheduleAtToAroundCodemod()

	content := `---
on:
  schedule:
    - cron: "0 9 * * *"
---

# Test`

	frontmatter := map[string]any{
		"on": map[string]any{
			"schedule": []any{
				map[string]any{"cron": "0 9 * * *"},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestScheduleCodemod_PreservesIndentation(t *testing.T) {
	codemod := getScheduleAtToAroundCodemod()

	content := `---
on:
  schedule:
    - cron: daily at 09:00
---

# Test`

	frontmatter := map[string]any{
		"on": map[string]any{
			"schedule": []any{
				map[string]any{"cron": "daily at 09:00"},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "    - cron: daily around 09:00")
}

func TestScheduleCodemod_MultipleSchedules(t *testing.T) {
	codemod := getScheduleAtToAroundCodemod()

	content := `---
on:
  schedule:
    - cron: daily at 09:00
    - cron: weekly on Monday at 10:00
---

# Test`

	frontmatter := map[string]any{
		"on": map[string]any{
			"schedule": []any{
				map[string]any{"cron": "daily at 09:00"},
				map[string]any{"cron": "weekly on Monday at 10:00"},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "daily around 09:00")
	assert.Contains(t, result, "weekly on Monday around 10:00")
}

func TestScheduleCodemod_ScheduleField(t *testing.T) {
	codemod := getScheduleAtToAroundCodemod()

	content := `---
on:
  schedule:
    - schedule: daily at 09:00
---

# Test`

	frontmatter := map[string]any{
		"on": map[string]any{
			"schedule": []any{
				map[string]any{"schedule": "daily at 09:00"},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "schedule: daily around 09:00")
}

func TestScheduleCodemod_PreservesMarkdown(t *testing.T) {
	codemod := getScheduleAtToAroundCodemod()

	content := `---
on:
  schedule:
    - cron: daily at 09:00
---

# Test Workflow

Runs on a schedule.`

	frontmatter := map[string]any{
		"on": map[string]any{
			"schedule": []any{
				map[string]any{"cron": "daily at 09:00"},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "# Test Workflow")
	assert.Contains(t, result, "Runs on a schedule.")
}
