//go:build !integration

package cli

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

func TestMergeWorkflowContent_PreservesFrontmatterComments(t *testing.T) {
	base := `---
# Keep the authored field order.
engine: copilot
on:
  workflow_dispatch:
  bots: [original] # locally customized
permissions: {contents: read}

# Pin shared behavior.
imports:
  - uses: other/repo/shared.md@abc123 # keep this explanation
    with:
      mode: original
---

# Workflow

Original upstream instructions.
`
	for _, tt := range []struct {
		name       string
		importPath string
		resolved   string
	}{
		{"pinned", "other/repo/shared.md@abc123", "other/repo/shared.md@abc123"},
		{"relative", "shared.md", "owner/repo/workflows/shared.md@v2"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			original := strings.Replace(base, "other/repo/shared.md@abc123", tt.importPath, 1)
			current := strings.Replace(original, "bots: [original]", "bots: [local]", 1)
			upstream := strings.Replace(original, "Original upstream instructions.", "Updated upstream instructions.", 1)
			expected := strings.Replace(current, "Original upstream instructions.", "Updated upstream instructions.", 1)
			expected = strings.Replace(expected, tt.importPath, tt.resolved, 1)
			expected = strings.Replace(expected, "\n---\n\n# Workflow", "\nsource: owner/repo/workflows/test.md@v2\n---\n\n# Workflow", 1)

			merged, conflicts, err := MergeWorkflowContent(original, current, upstream,
				"owner/repo/workflows/test.md@v1", "v2", "", false)
			require.NoError(t, err)
			require.False(t, conflicts, "merge should be conflict-free:\n%s", merged)
			require.Equal(t, strings.TrimSuffix(expected, "\n"), merged)
		})
	}
}

func TestMergeWorkflowContent_PreservesUpstreamCommentsAfterLegacyFormatting(t *testing.T) {
	base := "---\n# Shared behavior\nimports:\n  - owner/repo/shared.md@v1 # pinned\n---\n\n# Workflow\n"
	current := "---\nimports:\n- owner/repo/shared.md@v1\n---\n\n# Customized Workflow\n"
	upstream := strings.Replace(base, "@v1", "@v2", 1)
	expected := strings.Replace(upstream, "\n---\n\n", "\nsource: owner/repo/workflows/test.md@v2\n---\n\n", 1)
	expected = strings.Replace(expected, "# Workflow", "# Customized Workflow", 1)

	merged, conflicts, err := MergeWorkflowContent(base, current, upstream,
		"owner/repo/workflows/test.md@v1", "v2", "", false)
	require.NoError(t, err)
	require.False(t, conflicts, "legacy formatting must not conflict:\n%s", merged)
	require.Equal(t, strings.TrimSuffix(expected, "\n"), merged)
}

func TestMergeWorkflowContent_PreservesUpstreamFormattingWhenFrontmatterMatches(t *testing.T) {
	base := "---\nengine: copilot\nimports:\n  - owner/repo/shared.md@v1\n---\n\n# Workflow\nOriginal instructions.\n\nShared ending.\n"
	current := strings.Replace(base, "Original instructions.", "Local instructions.", 1)
	upstream := strings.Replace(base, "imports:\n", "# Added upstream note\nimports:\n", 1)
	upstream = strings.Replace(upstream, "Shared ending.", "Updated ending.", 1)
	expected := strings.Replace(upstream, "Original instructions.", "Local instructions.", 1)
	expected = strings.Replace(expected, "\n---\n\n", "\nsource: owner/repo/workflows/test.md@v2\n---\n\n", 1)

	merged, conflicts, err := MergeWorkflowContent(base, current, upstream,
		"owner/repo/workflows/test.md@v1", "v2", "", false)
	require.NoError(t, err)
	require.False(t, conflicts, "merge should preserve upstream comments:\n%s", merged)
	require.Equal(t, strings.TrimSuffix(expected, "\n"), merged)
}

func TestMergeWorkflowContent_LegacyFormattingWithLocalFrontmatterEdit(t *testing.T) {
	base := "---\nengine: copilot\nname: original\nimports:\n  - owner/repo/shared.md@v1\n---\n\n# Workflow\n"
	parsed, err := parser.ExtractFrontmatterFromContent(base)
	require.NoError(t, err)
	current, err := reconstructWorkflowFileFromMap(parsed.Frontmatter, parsed.Markdown)
	require.NoError(t, err)
	current = strings.Replace(current, "name: original", "name: local", 1)
	upstream := strings.Replace(base, "@v1", "@v2", 1)
	upstream = strings.Replace(upstream, "engine: copilot", "# Upstream note\nimports:\n  - owner/repo/shared.md@v2 # pinned upstream\nengine: \"copilot\"", 1)
	upstream = strings.Replace(upstream, "imports:\n  - owner/repo/shared.md@v2\n", "", 1)
	upstream = strings.Replace(upstream, "name: original", "# Keep upstream name metadata\nname: original", 1)
	expected := strings.Replace(upstream, "name: original", "name: local", 1)
	expected = strings.Replace(expected, "\n---\n\n", "\nsource: owner/repo/workflows/test.md@v2\n---\n\n", 1)

	merged, conflicts, err := MergeWorkflowContent(base, current, upstream,
		"owner/repo/workflows/test.md@v1", "v2", "", false)
	require.NoError(t, err)
	require.False(t, conflicts, "legacy formatting must not conflict with independent edits:\n%s", merged)
	require.Equal(t, strings.TrimSuffix(expected, "\n"), merged)
}
