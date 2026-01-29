//go:build !integration

package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTimeoutMinutesCodemod(t *testing.T) {
	codemod := getTimeoutMinutesCodemod()

	// Verify codemod metadata
	assert.Equal(t, "timeout-minutes-migration", codemod.ID, "Codemod ID should match")
	assert.Equal(t, "Migrate timeout_minutes to timeout-minutes", codemod.Name, "Codemod name should match")
	assert.NotEmpty(t, codemod.Description, "Codemod should have a description")
	assert.Equal(t, "0.1.0", codemod.IntroducedIn, "Codemod version should match")
	require.NotNil(t, codemod.Apply, "Codemod should have an Apply function")
}

func TestTimeoutMinutesCodemod_BasicMigration(t *testing.T) {
	codemod := getTimeoutMinutesCodemod()

	content := `---
on: workflow_dispatch
timeout_minutes: 30
permissions:
  contents: read
---

# Test Workflow`

	frontmatter := map[string]any{
		"on":              "workflow_dispatch",
		"timeout_minutes": 30,
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Codemod should report changes")
	assert.Contains(t, result, "timeout-minutes: 30", "Result should contain new field name")
	assert.NotContains(t, result, "timeout_minutes:", "Result should not contain old field name")
}

func TestTimeoutMinutesCodemod_PreservesIndentation(t *testing.T) {
	codemod := getTimeoutMinutesCodemod()

	content := `---
on: workflow_dispatch
timeout_minutes: 45
---

# Test`

	frontmatter := map[string]any{
		"on":              "workflow_dispatch",
		"timeout_minutes": 45,
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Codemod should report changes")
	assert.Contains(t, result, "timeout-minutes: 45", "Result should contain migrated field")
}

func TestTimeoutMinutesCodemod_PreservesComments(t *testing.T) {
	codemod := getTimeoutMinutesCodemod()

	content := `---
on: workflow_dispatch
timeout_minutes: 30  # 30 minutes should be enough
permissions:
  contents: read
---

# Test Workflow`

	frontmatter := map[string]any{
		"on":              "workflow_dispatch",
		"timeout_minutes": 30,
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Codemod should report changes")
	assert.Contains(t, result, "timeout-minutes: 30  # 30 minutes should be enough", "Result should preserve inline comment")
	assert.NotContains(t, result, "timeout_minutes:", "Result should not contain old field name")
}

func TestTimeoutMinutesCodemod_NoFieldPresent(t *testing.T) {
	codemod := getTimeoutMinutesCodemod()

	content := `---
on: workflow_dispatch
timeout-minutes: 30
permissions:
  contents: read
---

# Test Workflow`

	frontmatter := map[string]any{
		"on":              "workflow_dispatch",
		"timeout-minutes": 30,
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.False(t, applied, "Codemod should not report changes when field is not present")
	assert.Equal(t, content, result, "Content should remain unchanged")
}

func TestTimeoutMinutesCodemod_PreservesMarkdownBody(t *testing.T) {
	codemod := getTimeoutMinutesCodemod()

	content := `---
on: workflow_dispatch
timeout_minutes: 60
---

# Test Workflow

This is a test workflow with:
- Multiple lines
- Markdown formatting
- Code blocks

` + "```yaml" + `
key: value
` + "```"

	frontmatter := map[string]any{
		"on":              "workflow_dispatch",
		"timeout_minutes": 60,
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Codemod should report changes")
	assert.Contains(t, result, "timeout-minutes: 60", "Result should contain new field name")
	assert.Contains(t, result, "# Test Workflow", "Result should preserve markdown body")
	assert.Contains(t, result, "- Multiple lines", "Result should preserve markdown content")
	assert.Contains(t, result, "```yaml", "Result should preserve code blocks")
}

func TestTimeoutMinutesCodemod_DifferentValues(t *testing.T) {
	tests := []struct {
		name  string
		value int
	}{
		{"small timeout", 5},
		{"medium timeout", 30},
		{"large timeout", 120},
		{"very large timeout", 360},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codemod := getTimeoutMinutesCodemod()

			content := fmt.Sprintf(`---
on: workflow_dispatch
timeout_minutes: %d
---

# Test`, tt.value)

			frontmatter := map[string]any{
				"on":              "workflow_dispatch",
				"timeout_minutes": tt.value,
			}

			result, applied, err := codemod.Apply(content, frontmatter)

			require.NoError(t, err, "Apply should not return an error")
			assert.True(t, applied, "Codemod should report changes")
			assert.Contains(t, result, fmt.Sprintf("timeout-minutes: %d", tt.value), "Result should contain new field with correct value")
			assert.NotContains(t, result, "timeout_minutes:", "Result should not contain old field name")
		})
	}
}

func TestTimeoutMinutesCodemod_OnlyReplacesExactMatch(t *testing.T) {
	codemod := getTimeoutMinutesCodemod()

	content := `---
on: workflow_dispatch
timeout_minutes: 30
custom_timeout_minutes: 60
---

# Test`

	frontmatter := map[string]any{
		"on":                     "workflow_dispatch",
		"timeout_minutes":        30,
		"custom_timeout_minutes": 60,
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Codemod should report changes")

	lines := strings.Split(result, "\n")
	foundTimeoutMinutes := false
	foundCustomTimeoutMinutes := false

	for _, line := range lines {
		if strings.Contains(line, "timeout-minutes: 30") {
			foundTimeoutMinutes = true
		}
		if strings.Contains(line, "custom_timeout_minutes: 60") {
			foundCustomTimeoutMinutes = true
		}
	}

	assert.True(t, foundTimeoutMinutes, "Should replace timeout_minutes")
	assert.True(t, foundCustomTimeoutMinutes, "Should not replace custom_timeout_minutes")
}
