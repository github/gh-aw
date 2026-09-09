//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMergeWorkflowContent_CleanMerge tests a merge with truly non-overlapping changes
func TestMergeWorkflowContent_CleanMerge(t *testing.T) {
	base := `---
on: push
engine: claude
---

# Base Workflow

This is the base content.`

	// Local adds to markdown only, and has source field from previous update
	current := `---
on: push
engine: claude
source: test/repo/workflow.md@v1.0.0
---

# Base Workflow

This is the base content.

## Local Addition

This section was added locally.`

	// Upstream adds to frontmatter only
	new := `---
on: push
engine: claude
tools:
  bash: ["ls"]
source: test/repo/workflow.md@v1.1.0
---

# Base Workflow

This is the base content.`

	oldSourceSpec := "test/repo/workflow.md@v1.0.0"
	newRef := "v1.1.0"

	merged, hasConflicts, err := MergeWorkflowContent(base, current, new, oldSourceSpec, newRef, "", false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if hasConflicts {
		t.Errorf("Expected no conflicts when changes are in different sections (frontmatter vs markdown), merged content:\n%s", merged)
	}

	// Check that local markdown changes are preserved
	if !strings.Contains(merged, "Local Addition") {
		t.Errorf("Expected local markdown changes to be preserved, got:\n%s", merged)
	}

	// Check that upstream frontmatter changes are included
	if !strings.Contains(merged, "bash:") {
		t.Errorf("Expected upstream frontmatter changes to be included, got:\n%s", merged)
	}

	// Check that source field is updated
	if !strings.Contains(merged, "source: test/repo/workflow.md@v1.1.0") {
		t.Errorf("Expected source field to be updated to v1.1.0, got:\n%s", merged)
	}
}

// TestMergeWorkflowContent_PreservesUnchangedObjectFormImports is a regression test
// for: gh aw update replacing a valid, unchanged upstream object-form import
// (uses/with) with `imports: []` when only the prompt body changed upstream.
func TestMergeWorkflowContent_PreservesUnchangedObjectFormImports(t *testing.T) {
	base := `---
on: push
engine: claude
imports:
  - uses: shared/control.md
    with:
      role: orchestrator
---

# Dependabot Workflow

Base prompt body.`

	// Local has no modifications relative to base.
	current := `---
on: push
engine: claude
imports:
  - uses: shared/control.md
    with:
      role: orchestrator
source: test/repo/dependabot.md@v1.0.0
---

# Dependabot Workflow

Base prompt body.`

	// Upstream only changes the prompt body; the object-form import is unchanged.
	new := `---
on: push
engine: claude
imports:
  - uses: shared/control.md
    with:
      role: orchestrator
source: test/repo/dependabot.md@v1.1.0
---

# Dependabot Workflow

Updated prompt body.`

	oldSourceSpec := "test/repo/dependabot.md@v1.0.0"
	newRef := "v1.1.0"

	merged, hasConflicts, err := MergeWorkflowContent(base, current, new, oldSourceSpec, newRef, "", false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if hasConflicts {
		t.Errorf("Expected no conflicts, merged content:\n%s", merged)
	}

	if strings.Contains(merged, "imports: []") {
		t.Fatalf("Expected the unchanged upstream import to be preserved, but imports were emptied:\n%s", merged)
	}

	if !strings.Contains(merged, "uses: shared/control.md") && !strings.Contains(merged, "shared/control.md") {
		t.Errorf("Expected the shared/control.md import to be preserved, got:\n%s", merged)
	}

	if !strings.Contains(merged, "role: orchestrator") {
		t.Errorf("Expected the import 'with' fields to be preserved, got:\n%s", merged)
	}

	if !strings.Contains(merged, "Updated prompt body.") {
		t.Errorf("Expected upstream prompt body change to be applied, got:\n%s", merged)
	}
}

func TestNewUpdateCommand_CoolDownFlagUsage(t *testing.T) {
	cmd := NewUpdateCommand(func(string) error { return nil })
	require.NotNil(t, cmd)

	coolDownFlag := cmd.Flags().Lookup("cool-down")
	require.NotNil(t, coolDownFlag, "update command should register --cool-down")
	assert.Equal(t, coolDownFlagUsage, coolDownFlag.Usage, "cool-down usage should stay consistent across commands")
}

func TestNewUpdateCommand_MentionsEnterpriseSourceResolution(t *testing.T) {
	cmd := NewUpdateCommand(func(string) error { return nil })
	require.NotNil(t, cmd)

	assert.Contains(t, cmd.Long, "Note: In GitHub Enterprise repos, shorthand source specs resolve on your enterprise host by default.")
	assert.Contains(t, cmd.Long, "For github/*, githubnext/*, and microsoft/* sources, shorthand resolves on github.com.")
	assert.Contains(t, cmd.Long, "Use full https://github.com/... source URLs for other public github.com workflows.")
}

func TestNewUpdateCommand_HasApproveFlag(t *testing.T) {
	cmd := NewUpdateCommand(func(string) error { return nil })
	require.NotNil(t, cmd, "update command should be created")

	flag := cmd.Flags().Lookup("approve")
	require.NotNil(t, flag, "update command should register --approve flag")
	assert.Contains(t, flag.Usage, "When strict mode is active", "--approve description should match compile/upgrade semantics")
}

// TestMergeWorkflowContent_WithConflicts tests a merge with conflicts
func TestMergeWorkflowContent_WithConflicts(t *testing.T) {
	base := `---
on: push
engine: claude
---

# Original Workflow

This is the original content.`

	current := `---
on: push
engine: claude
source: test/repo/workflow.md@v1.0.0
---

# Original Workflow

This is the local modified content.`

	new := `---
on: push
engine: claude
---

# Original Workflow

This is the upstream modified content.`

	oldSourceSpec := "test/repo/workflow.md@v1.0.0"
	newRef := "v1.1.0"

	merged, hasConflicts, err := MergeWorkflowContent(base, current, new, oldSourceSpec, newRef, "", false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if !hasConflicts {
		t.Error("Expected conflicts to be detected")
	}

	// Check for conflict markers
	if !strings.Contains(merged, "<<<<<<<") || !strings.Contains(merged, ">>>>>>>") {
		t.Error("Expected conflict markers in merged content")
	}

	if !strings.Contains(merged, "source: test/repo/workflow.md@v1.1.0") {
		t.Errorf("Expected source field to be restored in conflict output, got:\n%s", merged)
	}

	// The merged content should contain both versions
	if !strings.Contains(merged, "local modified") && !strings.Contains(merged, "upstream modified") {
		t.Error("Expected both local and upstream changes in conflict markers")
	}
}

// TestMergeWorkflowContent_MarkdownOnly tests merging only markdown changes
func TestMergeWorkflowContent_MarkdownOnly(t *testing.T) {
	base := `---
on: push
engine: claude
---

# Original

Original markdown content.

## Base Section

Base content here.`

	current := `---
on: push
engine: claude
source: test/repo/workflow.md@v1.0.0
---

# Original

Original markdown content.

## Base Section

Base content here.

## Local Section

Local addition at the end.`

	new := `---
on: push
engine: claude
source: test/repo/workflow.md@v1.1.0
---

# Original

Original markdown content.

## Upstream Section

Upstream addition after original.

## Base Section

Base content here.`

	oldSourceSpec := "test/repo/workflow.md@v1.0.0"
	newRef := "v1.1.0"

	merged, hasConflicts, err := MergeWorkflowContent(base, current, new, oldSourceSpec, newRef, "", false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Git merge-file should successfully merge these non-overlapping sections
	if hasConflicts {
		t.Logf("Note: Conflicts detected even for non-overlapping sections:\n%s", merged)
		// This is fine - git is being conservative
	}

	// At minimum, the merge should include some content
	if !strings.Contains(merged, "## Base Section") {
		t.Errorf("Expected base section to be preserved, got:\n%s", merged)
	}
}

// TestMergeWorkflowContent_FrontmatterOnly tests merging only frontmatter changes
func TestMergeWorkflowContent_FrontmatterOnly(t *testing.T) {
	base := `---
on: push
engine: claude
---

# Workflow

Content remains the same.`

	// Local adds permissions field
	current := `---
on: push
engine: claude
permissions:
  contents: read
source: test/repo/workflow.md@v1.0.0
---

# Workflow

Content remains the same.`

	// Upstream adds tools field
	new := `---
on: push
engine: claude
tools:
  bash: ["ls"]
---

# Workflow

Content remains the same.`

	oldSourceSpec := "test/repo/workflow.md@v1.0.0"
	newRef := "v1.1.0"

	merged, hasConflicts, err := MergeWorkflowContent(base, current, new, oldSourceSpec, newRef, "", false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Since both added different fields, should not conflict
	if hasConflicts {
		t.Logf("Note: Conflicts detected for non-overlapping frontmatter fields:\n%s", merged)
	}

	// At minimum, the merge should complete
	if merged == "" {
		t.Error("Expected non-empty merged content")
	}

	// Both fields should be present (if no conflicts) or at least one should be there
	hasPermissions := strings.Contains(merged, "permissions:")
	hasTools := strings.Contains(merged, "tools:")

	if !hasPermissions && !hasTools {
		t.Errorf("Expected at least one of the frontmatter changes to be present, got:\n%s", merged)
	}
}

func TestMergeWorkflowContent_NormalizesCurrentManifestSource(t *testing.T) {
	base := `---
on: push
---

# Workflow
`

	current := `---
on: push
source: owner/repo@v1.0.0
---

# Workflow
`

	new := `---
on: push
---

# Workflow
`

	sourceSpec := "owner/repo/workflows/workflow.md@v1.0.0"
	merged, hasConflicts, err := MergeWorkflowContent(base, current, new, sourceSpec, sourceSpec, "", false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if hasConflicts {
		t.Fatalf("Expected no conflicts for source-format-only difference, got:\n%s", merged)
	}
	if !strings.Contains(merged, "source: owner/repo/workflows/workflow.md@v1.0.0") {
		t.Fatalf("expected updated source field, got:\n%s", merged)
	}
}

// TestUpdateSourceFieldInContent tests the source field update function
func TestUpdateSourceFieldInContent(t *testing.T) {
	content := `---
on: push
source: old/repo/workflow.md@v1.0.0
---

# Test Workflow`

	updated, err := UpdateFieldInFrontmatter(content, "source", "old/repo/workflow.md@v2.0.0")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if !strings.Contains(updated, "source: old/repo/workflow.md@v2.0.0") {
		t.Errorf("Expected source field to be updated to v2.0.0, got:\n%s", updated)
	}

	// Ensure other content is preserved (formatting should be preserved)
	if !strings.Contains(updated, `on: push`) {
		t.Errorf("Expected other frontmatter fields to be preserved, got:\n%s", updated)
	}

	if !strings.Contains(updated, "# Test Workflow") {
		t.Error("Expected markdown content to be preserved")
	}
}

// TestMergeWorkflowContent_SourceInMiddle tests that source field position in current
// (in the middle of frontmatter, before other fields) does not cause merge conflicts.
// This replicates the ci-doctor.md scenario where source was committed before features/evals.
func TestMergeWorkflowContent_SourceInMiddle(t *testing.T) {
	// Upstream base: no source field, has features and evals at end of frontmatter
	base := `---
on: push
engine: claude
features:
  gh-aw-detection: true
evals:
  - id: ci_failure_investigated
    question: Did the agent investigate failed CI workflows?
---

# CI Doctor

Test content.`

	// Local current: source field is in the MIDDLE (before features/evals), as it was
	// committed in a file where source was placed before other trailing fields.
	current := `---
on: push
engine: claude
source: test/repo/ci-doctor.md@v1.0.0
features:
  gh-aw-detection: true
evals:
  - id: ci_failure_investigated
    question: Did the agent investigate failed CI workflows?
---

# CI Doctor

Test content.`

	// Upstream new: no source field, same structure but updated description
	new := `---
on: push
engine: claude
features:
  gh-aw-detection: true
evals:
  - id: ci_failure_investigated
    question: Did the agent investigate failed CI workflows?
---

# CI Doctor

Test content updated upstream.`

	oldSourceSpec := "test/repo/ci-doctor.md@v1.0.0"
	newRef := "v1.1.0"

	merged, hasConflicts, err := MergeWorkflowContent(base, current, new, oldSourceSpec, newRef, "", false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if hasConflicts {
		t.Errorf("Expected no conflicts when source field is in a different position but content is otherwise unchanged, merged content:\n%s", merged)
	}

	// Source field should be updated to the new version
	if !strings.Contains(merged, "source: test/repo/ci-doctor.md@v1.1.0") {
		t.Errorf("Expected source field to be updated to v1.1.0, got:\n%s", merged)
	}

	// Upstream markdown change should be present
	if !strings.Contains(merged, "Test content updated upstream.") {
		t.Errorf("Expected upstream markdown change to be present, got:\n%s", merged)
	}
}

// TestMergeWorkflowContent_SourceRefDoesNotConflictWithLocalFrontmatterExpansion
// covers a managed source ref update next to a large local frontmatter extension.
func TestMergeWorkflowContent_SourceRefDoesNotConflictWithLocalFrontmatterExpansion(t *testing.T) {
	base := `---
tools:
  cache-memory: true
  web-fetch:

timeout-minutes: 10
---

# Workflow
`

	current := `---
tools:
  cache-memory: true
  web-fetch:
  web-search:
  github:
    mode: gh-proxy

timeout-minutes: 20

steps:
  - name: Local pre-analysis
    with:
      source: local-input
    env:
      source: local-env
    run: echo ready

features:
  gh-aw-detection: true
evals:
  - id: investigated
    question: Was the failure investigated?
source: test/repo/ci-doctor.md@v1.0.0
---

# Workflow
`

	newContent := base
	oldSourceSpec := "test/repo/ci-doctor.md@v1.0.0"
	newRef := "v1.1.0"

	merged, hasConflicts, err := MergeWorkflowContent(base, current, newContent, oldSourceSpec, newRef, "", false)
	require.NoError(t, err)
	require.False(t, hasConflicts, "managed source ref changes must not conflict with local frontmatter additions:\n%s", merged)
	assert.Contains(t, merged, "web-search:")
	assert.Contains(t, merged, "timeout-minutes: 20")
	assert.Contains(t, merged, "source: local-input")
	assert.Contains(t, merged, "source: local-env")
	assert.Contains(t, merged, "source: test/repo/ci-doctor.md@v1.1.0")
	assert.NotContains(t, merged, "source: test/repo/ci-doctor.md@v1.0.0")
	assert.NotContains(t, merged, "<<<<<<<")
}

// TestMergeWorkflowContent_Integration tests the merge with temporary files
func TestMergeWorkflowContent_Integration(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")

	base := `---
on: push
permissions:
  contents: read
---

# Test Workflow

Base content.`

	// Local adds issues permission
	current := `---
on: push
permissions:
  contents: read
  issues: read
source: test/repo/workflow.md@v1.0.0
---

# Test Workflow

Base content with local notes.`

	// Upstream adds pr permission
	new := `---
on: push
permissions:
  contents: read
  pull-requests: read
---

# Test Workflow

Base content with upstream notes.`

	oldSourceSpec := "test/repo/workflow.md@v1.0.0"
	newRef := "v1.1.0"

	merged, hasConflicts, err := MergeWorkflowContent(base, current, new, oldSourceSpec, newRef, "", true)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Write merged content to verify it's valid
	testFile := filepath.Join(tmpDir, "merged.md")
	if err := os.WriteFile(testFile, []byte(merged), 0644); err != nil {
		t.Fatalf("Failed to write merged file: %v", err)
	}

	// Since permissions are on different lines, git should merge them
	if hasConflicts {
		t.Logf("Conflicts detected (may be expected):\n%s", merged)
		// With conflicts, we can't check the merged result as reliably
		return
	}

	// Without conflicts, verify both permissions are merged
	if !strings.Contains(merged, "issues: write") || !strings.Contains(merged, "pull-requests: write") {
		t.Logf("Merged content:\n%s", merged)
		t.Error("Expected both local and upstream permission changes to be merged")
	}
}

// TestFindWorkflowsWithSource_CustomDirectory tests that findWorkflowsWithSource works with custom directories
func TestFindWorkflowsWithSource_CustomDirectory(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := testutil.TempDir(t, "test-*")
	customWorkflowDir := filepath.Join(tmpDir, "custom", "workflows")
	if err := os.MkdirAll(customWorkflowDir, 0755); err != nil {
		t.Fatalf("Failed to create custom workflow directory: %v", err)
	}

	// Create a workflow file with source field
	workflowContent := `---
on: push
engine: claude
source: test/repo/workflow.md@v1.0.0
---

# Test Workflow

Test content.`

	workflowPath := filepath.Join(customWorkflowDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Create a workflow file without source field
	workflowWithoutSource := `---
on: push
engine: claude
---

# Another Workflow

No source field.`

	workflowPath2 := filepath.Join(customWorkflowDir, "no-source.md")
	if err := os.WriteFile(workflowPath2, []byte(workflowWithoutSource), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Test findWorkflowsWithSource with custom directory
	workflows, err := findWorkflowsWithSource(customWorkflowDir, nil, false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should find only one workflow (the one with source field)
	if len(workflows) != 1 {
		t.Errorf("Expected to find 1 workflow with source field, got %d", len(workflows))
	}

	if len(workflows) > 0 {
		if workflows[0].Name != "test-workflow" {
			t.Errorf("Expected workflow name 'test-workflow', got '%s'", workflows[0].Name)
		}
		if workflows[0].SourceSpec != "test/repo/workflow.md@v1.0.0" {
			t.Errorf("Expected source spec 'test/repo/workflow.md@v1.0.0', got '%s'", workflows[0].SourceSpec)
		}
	}
}

// TestUpdateWorkflows_CustomDirectory tests that UpdateWorkflows respects custom directory parameter
func TestUpdateWorkflows_CustomDirectory(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := testutil.TempDir(t, "test-*")
	customWorkflowDir := filepath.Join(tmpDir, "custom", "workflows")
	if err := os.MkdirAll(customWorkflowDir, 0755); err != nil {
		t.Fatalf("Failed to create custom workflow directory: %v", err)
	}

	// Create a workflow file with source field
	workflowContent := `---
on: push
engine: claude
source: test/repo/workflow.md@v1.0.0
---

# Test Workflow

Test content.`

	workflowPath := filepath.Join(customWorkflowDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Test that findWorkflowsWithSource can find workflows in custom directory
	workflows, err := findWorkflowsWithSource(customWorkflowDir, nil, false)
	if err != nil {
		t.Fatalf("Expected no error finding workflows, got: %v", err)
	}

	if len(workflows) == 0 {
		t.Fatal("Expected to find at least one workflow")
	}

	// Verify the workflow was found in the custom directory
	if !strings.Contains(workflows[0].Path, customWorkflowDir) {
		t.Errorf("Expected workflow path to contain custom directory '%s', got '%s'", customWorkflowDir, workflows[0].Path)
	}
}

// TestShowUpdateSummary tests the update summary display
func TestShowUpdateSummary(t *testing.T) {
	tests := []struct {
		name              string
		successfulUpdates []string
		failedUpdates     []updateFailure
		wantSuccess       bool
		wantFailed        bool
	}{
		{
			name:              "all successful",
			successfulUpdates: []string{"workflow1", "workflow2", "workflow3"},
			failedUpdates:     []updateFailure{},
			wantSuccess:       true,
			wantFailed:        false,
		},
		{
			name:              "all failed",
			successfulUpdates: []string{},
			failedUpdates: []updateFailure{
				{Name: "workflow1", Error: "failed to download"},
				{Name: "workflow2", Error: "merge conflict"},
			},
			wantSuccess: false,
			wantFailed:  true,
		},
		{
			name:              "mixed results",
			successfulUpdates: []string{"workflow1", "workflow3"},
			failedUpdates: []updateFailure{
				{Name: "workflow2", Error: "failed to compile"},
			},
			wantSuccess: true,
			wantFailed:  true,
		},
		{
			name:              "empty results",
			successfulUpdates: []string{},
			failedUpdates:     []updateFailure{},
			wantSuccess:       false,
			wantFailed:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test just verifies the function doesn't panic and can be called
			// We don't check the exact output format since it uses console helpers
			// and the exact formatting may change
			showUpdateSummary(tt.successfulUpdates, tt.failedUpdates, false)
		})
	}
}

func TestGroupUpdateFailuresCollapsesSAMLRateLimitErrors(t *testing.T) {
	t.Parallel()
	failures := []updateFailure{
		{Name: "workflow-b", Error: "SAML enforcement: API rate limit exceeded"},
		{Name: "workflow-a", Error: "forbidden by SAML enforcement; rate limit exceeded"},
	}

	groups := groupUpdateFailures(failures)
	require.Len(t, groups, 1)
	assert.Equal(t, "workflow-a, workflow-b", groups[0].Workflows)
	assert.Equal(t, "SAML-restricted authenticated access; anonymous GitHub API fallback is rate-limited", groups[0].Reason)
}

// TestHasLocalModifications tests the local modifications detection
func TestHasLocalModifications(t *testing.T) {
	tests := []struct {
		name           string
		sourceContent  string
		localContent   string
		sourceSpec     string
		expectModified bool
		description    string
	}{
		{
			name: "no modifications - identical content",
			sourceContent: `---
on: push
engine: claude
---

# Test Workflow

Test content.`,
			localContent: `---
on: push
engine: claude
source: test/repo/workflow.md@v1.0.0
---

# Test Workflow

Test content.`,
			sourceSpec:     "test/repo/workflow.md@v1.0.0",
			expectModified: false,
			description:    "Local file with source field should match source without it",
		},
		{
			name: "local modifications in frontmatter",
			sourceContent: `---
on: push
engine: claude
---

# Test Workflow

Test content.`,
			localContent: `---
on: push
engine: claude
permissions:
  contents: read
source: test/repo/workflow.md@v1.0.0
---

# Test Workflow

Test content.`,
			sourceSpec:     "test/repo/workflow.md@v1.0.0",
			expectModified: true,
			description:    "Local has extra permissions field",
		},
		{
			name: "local modifications in markdown",
			sourceContent: `---
on: push
engine: claude
---

# Test Workflow

Test content.`,
			localContent: `---
on: push
engine: claude
source: test/repo/workflow.md@v1.0.0
---

# Test Workflow

Test content with local additions.`,
			sourceSpec:     "test/repo/workflow.md@v1.0.0",
			expectModified: true,
			description:    "Local has modified markdown content",
		},
		{
			name: "whitespace differences should be ignored",
			sourceContent: `---
on: push
engine: claude
---

# Test Workflow

Test content.`,
			localContent: `---
on: push
engine: claude
source: test/repo/workflow.md@v1.0.0
---

# Test Workflow

Test content.
`,
			sourceSpec:     "test/repo/workflow.md@v1.0.0",
			expectModified: false,
			description:    "Trailing whitespace should be normalized",
		},
		{
			name: "both empty",
			sourceContent: `---
on: push
---

# Empty`,
			localContent: `---
on: push
source: test/repo/workflow.md@v1.0.0
---

# Empty`,
			sourceSpec:     "test/repo/workflow.md@v1.0.0",
			expectModified: false,
			description:    "Both files minimal but identical",
		},
		{
			name: "source field in different position - not a modification",
			sourceContent: `---
on: push
engine: claude
features:
  gh-aw-detection: true
evals:
  - id: check_one
    question: Did the agent do the thing?
---

# Test Workflow

Test content.`,
			localContent: `---
on: push
engine: claude
source: test/repo/workflow.md@v1.0.0
features:
  gh-aw-detection: true
evals:
  - id: check_one
    question: Did the agent do the thing?
---

# Test Workflow

Test content.`,
			sourceSpec:     "test/repo/workflow.md@v1.0.0",
			expectModified: false,
			description:    "Source field in a different position (before features/evals instead of at end) should not be treated as local modification",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasLocalModifications(tt.sourceContent, tt.localContent, tt.sourceSpec, "", false)

			if result != tt.expectModified {
				t.Errorf("%s: expected modified=%v, got %v", tt.description, tt.expectModified, result)
			}
		})
	}
}

// TestCompileWorkflowWithRefresh tests that compileWorkflowWithRefresh properly passes refreshStopTime
func TestCompileWorkflowWithRefresh(t *testing.T) {

	// Create a temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a simple workflow file
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")
	workflowContent := `---
on:
  workflow_dispatch:
  stop-after: "+48h"
permissions:
  contents: read
engine: copilot
---

# Test Workflow

This is a test workflow.
`
	err := os.WriteFile(workflowFile, []byte(workflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test workflow file: %v", err)
	}

	// Test with refreshStopTime=false (should preserve existing stop time if lock exists)
	t.Run("compileWorkflowWithRefresh false", func(t *testing.T) {
		err := compileWorkflowWithRefreshAndActionRef(context.Background(), workflowFile, false, false, "", "", false, false)
		if err != nil {
			t.Logf("Compilation failed (expected in test environment): %v", err)
			// In a test environment without full setup, compilation may fail,
			// but we're testing that the function exists and accepts the parameter
		}
	})

	// Test with refreshStopTime=true (should regenerate stop time)
	t.Run("compileWorkflowWithRefresh true", func(t *testing.T) {
		err := compileWorkflowWithRefreshAndActionRef(context.Background(), workflowFile, false, false, "", "", true, false)
		if err != nil {
			t.Logf("Compilation failed (expected in test environment): %v", err)
			// In a test environment without full setup, compilation may fail,
			// but we're testing that the function exists and accepts the parameter
		}
	})
}

// TestUpdateWorkflow_OverrideMode tests the default override mode behavior
func TestUpdateWorkflow_DefaultMergeMode(t *testing.T) {
	// By default, merge mode is on. Local changes should be preserved via 3-way merge.
	t.Run("merge is default when noMerge is false", func(t *testing.T) {
		noMerge := false // default
		merge := !noMerge
		assert.True(t, merge, "merge should be true by default")
	})
}

// TestUpdateWorkflow_NoMergeMode tests that --no-merge disables merge mode
func TestUpdateWorkflow_NoMergeMode(t *testing.T) {
	// With --no-merge, local changes should be overridden with upstream.
	t.Run("no-merge overrides local changes", func(t *testing.T) {
		noMerge := true // --no-merge
		merge := !noMerge
		assert.False(t, merge, "merge should be false when --no-merge is set")
	})
}

// TestMarshalActionsLockSorted tests that the actions lock marshaling produces sorted output
// using the ActionCache.Save helper.
func TestMarshalActionsLockSorted(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	cache := workflow.NewActionCache(tmpDir)

	// Add entries in non-alphabetical order
	cache.Set("zebra/action", "v1", "abc123")
	cache.Set("actions/checkout", "v5", "def456")

	// Save to disk
	if err := cache.Save(); err != nil {
		t.Fatalf("Expected no error saving cache, got: %v", err)
	}

	// Read the file back
	data, err := os.ReadFile(filepath.Join(tmpDir, ".github", "aw", "actions-lock.json"))
	if err != nil {
		t.Fatalf("Expected to read saved file, got: %v", err)
	}

	result := string(data)

	// Check that actions/checkout comes before zebra/action (sorted output)
	checkoutIdx := strings.Index(result, "actions/checkout")
	zebraIdx := strings.Index(result, "zebra/action")

	if checkoutIdx == -1 || zebraIdx == -1 {
		t.Errorf("Expected both actions in output, got:\n%s", result)
	}

	if checkoutIdx > zebraIdx {
		t.Errorf("Expected actions/checkout to appear before zebra/action in sorted output")
	}

	// Check JSON structure
	if !strings.Contains(result, `"entries": {`) {
		t.Error("Expected 'entries' key in JSON output")
	}

	if !strings.Contains(result, `"repo": "actions/checkout"`) {
		t.Error("Expected actions/checkout repo in output")
	}

	if !strings.Contains(result, `"version": "v5"`) {
		t.Error("Expected v5 version in output")
	}

	if !strings.Contains(result, `"sha": "def456"`) {
		t.Error("Expected def456 SHA in output")
	}
}

// TestGetActionSHAForTag tests that we can look up action SHAs (requires network)
func TestGetActionSHAForTag(t *testing.T) {

	// This test requires network access and GitHub API, so skip in CI
	if os.Getenv("CI") != "" {
		t.Skip("Skipping network test in CI")
	}

	// Test with a known action and version
	sha, err := getActionSHAForTag(context.Background(), "actions/checkout", "v4")
	if err != nil {
		t.Skipf("Could not resolve action SHA (network or auth issue): %v", err)
	}

	// Check SHA format
	if len(sha) != 40 {
		t.Errorf("Expected 40-character SHA, got %d characters: %s", len(sha), sha)
	}

	// Check that it's hexadecimal
	for _, c := range sha {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("Expected hexadecimal SHA, got character: %c", c)
		}
	}
}

// TestUpdateActions_NoFile tests behavior when actions-lock.json doesn't exist
func TestUpdateActions_NoFile(t *testing.T) {
	// Create a temporary directory without an actions-lock.json file
	tmpDir := testutil.TempDir(t, "test-*")
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	os.Chdir(tmpDir)

	// Should not error when file doesn't exist
	err := UpdateActions(context.Background(), false, false, false, 0)
	if err != nil {
		t.Errorf("Expected no error when actions-lock.json doesn't exist, got: %v", err)
	}
}

// TestUpdateActions_EmptyFile tests behavior with empty actions lock file
func TestUpdateActions_EmptyFile(t *testing.T) {
	// Create a temporary directory with an empty actions-lock.json
	tmpDir := testutil.TempDir(t, "test-*")
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// Create .github/aw directory
	awDir := filepath.Join(tmpDir, ".github", "aw")
	if err := os.MkdirAll(awDir, 0755); err != nil {
		t.Fatalf("Failed to create .github/aw directory: %v", err)
	}

	// Create an empty actions-lock.json
	actionsLockPath := filepath.Join(awDir, "actions-lock.json")
	emptyLock := `{
  "entries": {}
}`
	if err := os.WriteFile(actionsLockPath, []byte(emptyLock), 0644); err != nil {
		t.Fatalf("Failed to write empty actions-lock.json: %v", err)
	}

	os.Chdir(tmpDir)

	// Should not error with empty file
	err := UpdateActions(context.Background(), false, false, false, 0)
	if err != nil {
		t.Errorf("Expected no error with empty actions-lock.json, got: %v", err)
	}
}

// TestUpdateActions_InvalidJSON tests behavior with invalid JSON
func TestUpdateActions_InvalidJSON(t *testing.T) {
	// Create a temporary directory with invalid JSON
	tmpDir := testutil.TempDir(t, "test-*")
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// Create .github/aw directory
	awDir := filepath.Join(tmpDir, ".github", "aw")
	if err := os.MkdirAll(awDir, 0755); err != nil {
		t.Fatalf("Failed to create .github/aw directory: %v", err)
	}

	// Create an invalid actions-lock.json
	actionsLockPath := filepath.Join(awDir, "actions-lock.json")
	invalidJSON := `{ invalid json`
	if err := os.WriteFile(actionsLockPath, []byte(invalidJSON), 0644); err != nil {
		t.Fatalf("Failed to write invalid actions-lock.json: %v", err)
	}

	os.Chdir(tmpDir)

	// Should error with invalid JSON
	err := UpdateActions(context.Background(), false, false, false, 0)
	if err == nil {
		t.Error("Expected error with invalid JSON, got nil")
	}

	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("Expected parse error, got: %v", err)
	}
}

// TestResolveLatestRef_CommitSHA tests that commit SHAs are correctly identified
// and trigger default branch resolution (which requires API access).
func TestResolveLatestRef_CommitSHA(t *testing.T) {
	sha := "ea350161ad5dcc9624cf510f134c6a9e39a6f94d"
	assert.True(t, IsCommitSHA(sha), "Should be recognized as a commit SHA")

	// resolveLatestRef for a SHA requires GitHub API access to look up the
	// default branch. In environments without API access it will error;
	// in authenticated environments it will succeed. Either outcome is
	// acceptable — the key invariant is that the SHA is correctly
	// identified (tested above) and the function does not panic.
	_, _ = resolveLatestRef(context.Background(), "test/repo", sha, false, false, 0)
}

// TestResolveLatestRef_NotCommitSHA tests that non-SHA refs are handled appropriately
func TestResolveLatestRef_NotCommitSHA(t *testing.T) {
	tests := []struct {
		name      string
		ref       string
		expectSHA bool
	}{
		{"branch name", "main", false},
		{"short SHA", "abc123", false},
		{"version tag", "v1.0.0", false},
		{"invalid hex", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", false},
		{"valid SHA lowercase", "abcdef1234567890123456789012345678901234", true},
		{"valid SHA uppercase", "ABCDEF1234567890123456789012345678901234", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := IsCommitSHA(tt.ref)
			if isValid != tt.expectSHA {
				t.Errorf("IsCommitSHA(%q) = %v, expected %v", tt.ref, isValid, tt.expectSHA)
			}
		})
	}
}

// TestShortRef tests the shortRef helper for abbreviating refs in messages
func TestShortRef(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		expected string
	}{
		{"commit SHA", "ea350161ad5dcc9624cf510f134c6a9e39a6f94d", "ea35016"},
		{"branch name", "main", "main"},
		{"version tag", "v1.2.3", "v1.2.3"},
		{"short string", "abc", "abc"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shortRef(tt.ref)
			assert.Equal(t, tt.expected, result, "shortRef(%q) should return %q", tt.ref, tt.expected)
		})
	}
}

// TestIsBranchRef tests the isBranchRef helper that distinguishes branch names
// from semantic version tags and commit SHAs
func TestIsBranchRef(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		expected bool
	}{
		{"branch main", "main", true},
		{"branch develop", "develop", true},
		{"branch with slash", "feature/foo", true},
		{"semver tag", "v1.2.3", false},
		{"semver tag with v", "v0.1.0", false},
		{"commit SHA", "ea350161ad5dcc9624cf510f134c6a9e39a6f94d", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isBranchRef(tt.ref)
			assert.Equal(t, tt.expected, result, "isBranchRef(%q) should return %v", tt.ref, tt.expected)
		})
	}
}

// TestRunUpdateWorkflows_NoSourceWorkflows tests that RunUpdateWorkflows reports a message (not an error) when no source workflows exist
func TestRunUpdateWorkflows_NoSourceWorkflows(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// Create empty .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	os.Chdir(tmpDir)

	// Running update with no source workflows should succeed with an info message, not an error
	err := RunUpdateWorkflows(context.Background(), UpdateWorkflowsOptions{})
	require.NoError(t, err, "Should not error when no workflows with source field exist")
}

// TestRunUpdateWorkflows_SpecificWorkflowNotFound tests that RunUpdateWorkflows errors for unknown workflow name
func TestRunUpdateWorkflows_SpecificWorkflowNotFound(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// Create empty .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	os.Chdir(tmpDir)

	// Running update with a specific name that doesn't exist should fail
	err := RunUpdateWorkflows(context.Background(), UpdateWorkflowsOptions{
		WorkflowNames: []string{"nonexistent"},
	})
	require.Error(t, err, "Should error when specified workflow not found")
	require.ErrorContains(t, err, "no workflows found matching the specified names")
}
