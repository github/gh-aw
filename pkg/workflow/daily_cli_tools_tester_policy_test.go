package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDailyCLIToolsTesterCompileEdgeCasePolicy(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "daily-cli-tools-tester.md"))
	require.NoError(t, err)
	prompt := string(source)
	assert.Contains(t, prompt, "3 representative trusted repository workflows with `dry_run: true` and `strict: true`")

	_, phase, found := strings.Cut(prompt, "### 4.6 Edge Cases for Compile\n")
	require.True(t, found)
	phase, _, found = strings.Cut(phase, "### 4.7 Document Compile Test Results")
	require.True(t, found)
	assert.Contains(t, phase, "Malformed YAML frontmatter, invalid tool configurations, and missing imports")
	assert.Contains(t, phase, "**out of scope**")
	assert.Contains(t, phase, "Do not create fixtures, modify selected workflows to induce errors, or compile downloaded content.")
	assert.Contains(t, phase, "Record these three skipped edge cases as **not tested (expected exclusion)** with the trusted-target restriction in the findings issue or final `noop`.")
	assert.Contains(t, phase, "These exclusions alone do not require `report_incomplete`.")
	assert.NotContains(t, phase, "Use `report_incomplete` to record these three skipped edge cases")
	assert.Contains(t, phase, "Do not count them as passing tests or claim complete compile error-handling coverage.")
	assert.NotContains(t, phase, "Create a test workflow")
	assert.NotContains(t, phase, "Attempt to compile")
}
