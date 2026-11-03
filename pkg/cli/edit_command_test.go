package cli

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEditChangesSupportAssignmentsSchedulesAndImports(t *testing.T) {
	t.Parallel()
	cmd := NewEditCommand()
	require.NoError(t, cmd.Flags().Set("set", "max-turns=20"))
	require.NoError(t, cmd.Flags().Set("schedule", "every 6h"))
	require.NoError(t, cmd.Flags().Set("add-import", "shared/common.md"))
	require.NoError(t, cmd.Flags().Set("add-skill", "shared/review"))

	changes, err := editChangesFromCommand(cmd, []string{"workflow", "model: small"})
	require.NoError(t, err)
	frontmatter := map[string]any{"on": "workflow_dispatch"}
	for _, change := range changes {
		mustApplyEditChange(t, frontmatter, change)
	}

	assert.Equal(t, "small", frontmatter["model"])
	assert.Equal(t, uint64(20), frontmatter["max-turns"])
	assert.Equal(t, []any{"shared/common.md"}, frontmatter["imports"])
	assert.Equal(t, []any{"shared/review"}, frontmatter["skills"])
	assert.Equal(t, map[string]any{"workflow_dispatch": nil, "schedule": "every 6h"}, frontmatter["on"])
}

func TestEditCommandDryRunPreservesWorkflowFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workflowPath := dir + "/workflow.md"
	original := "---\non: workflow_dispatch\n---\n# Workflow\n"
	require.NoError(t, os.WriteFile(workflowPath, []byte(original), 0o644))

	cmd := NewEditCommand()
	cmd.SetArgs([]string{workflowPath, "max-turns: 20", "--dry-run"})
	var output strings.Builder
	cmd.SetOut(&output)
	require.NoError(t, cmd.Execute())

	assert.Contains(t, output.String(), "max-turns: 20")
	content, err := os.ReadFile(workflowPath)
	require.NoError(t, err)
	assert.Equal(t, original, string(content))
}

func TestEditChangesAddImportsToObjectForm(t *testing.T) {
	t.Parallel()
	frontmatter := map[string]any{"imports": map[string]any{"aw": []any{"shared/base.md"}}}
	mustApplyEditChange(t, frontmatter, editChange{kind: "add", path: "imports", value: "shared/extra.md"})
	mustApplyEditChange(t, frontmatter, editChange{kind: "remove", path: "imports", value: "shared/base.md"})
	assert.Equal(t, []any{"shared/extra.md"}, frontmatter["imports"].(map[string]any)["aw"])
}

func TestEditConnectorImportBindingsAreExplicit(t *testing.T) {
	t.Parallel()
	for _, objectForm := range []bool{false, true} {
		t.Run(strconv.FormatBool(objectForm), func(t *testing.T) {
			frontmatter := map[string]any{"on": "workflow_dispatch", "network": "defaults"}
			if objectForm {
				frontmatter["imports"] = map[string]any{"aw": []any{}}
			}
			importObject := "{uses: connectors/example.md, with: {items: [approved-id], reader-secret: EXAMPLE_READ_TOKEN}}"
			cmd := NewEditCommand()
			require.NoError(t, cmd.Flags().Set("add-import", importObject))
			changes, err := editChangesFromCommand(cmd, []string{"workflow"})
			require.NoError(t, err)
			for _, change := range changes {
				mustApplyEditChange(t, frontmatter, change)
			}
			imports := frontmatter["imports"]
			if objectForm {
				imports = imports.(map[string]any)["aw"]
			}
			require.Equal(t, []any{map[string]any{
				"uses": "connectors/example.md",
				"with": map[string]any{"items": []any{"approved-id"}, "reader-secret": "EXAMPLE_READ_TOKEN"},
			}}, imports)
			for _, absent := range []string{"tools", "safe-outputs", "connectors", "permissions"} {
				require.NotContains(t, frontmatter, absent)
			}
			require.Equal(t, "defaults", frontmatter["network"])
			remove := NewEditCommand()
			require.NoError(t, remove.Flags().Set("remove-import", importObject))
			changes, err = editChangesFromCommand(remove, []string{"workflow"})
			require.NoError(t, err)
			for _, change := range changes {
				mustApplyEditChange(t, frontmatter, change)
			}
		})
	}
	for _, invalid := range []string{"{uses: [", "{with: {items: [approved-id]}}", "{uses: example.md, unexpected: true}"} {
		_, err := parseEditImport(invalid)
		require.ErrorContains(t, err, "invalid import object")
		require.NotContains(t, err.Error(), invalid, "diagnostics must not echo binding values")
	}
}

func TestEditChangesRemoveImportsAndSkills(t *testing.T) {
	t.Parallel()
	cmd := NewEditCommand()
	require.NoError(t, cmd.Flags().Set("remove-import", "shared/base.md"))
	require.NoError(t, cmd.Flags().Set("remove-skill", "shared/base"))

	changes, err := editChangesFromCommand(cmd, []string{"workflow"})
	require.NoError(t, err)
	frontmatter := map[string]any{
		"imports": []any{"shared/base.md", "shared/extra.md"},
		"skills":  []any{"shared/base", "shared/review"},
	}
	for _, change := range changes {
		mustApplyEditChange(t, frontmatter, change)
	}

	assert.Equal(t, []any{"shared/extra.md"}, frontmatter["imports"])
	assert.Equal(t, []any{"shared/review"}, frontmatter["skills"])
}

func TestEditAssignmentParsesScheduleShorthands(t *testing.T) {
	t.Parallel()
	change, err := parseEditAssignment("on.schedule: daily on weekdays", ":")
	require.NoError(t, err)
	frontmatter := map[string]any{"on": "workflow_dispatch"}
	mustApplyEditChange(t, frontmatter, change)
	assert.Equal(t, "daily on weekdays", frontmatter["on"].(map[string]any)["schedule"])
}

func TestReplaceFrontmatterPreservesBodySeparators(t *testing.T) {
	t.Parallel()
	content := "---\non: workflow_dispatch\n---\n# Workflow\n\n---\nBody\n"
	updated, err := replaceFrontmatter(content, map[string]any{"on": "push"})
	require.NoError(t, err)
	assert.Contains(t, updated, "---\n# Workflow\n\n---\nBody\n")
}

func TestEditCommandAllowsSourceManagedWorkflow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workflowPath := dir + "/workflow.md"
	original := "---\nsource: owner/repo@v1\non: workflow_dispatch\n---\n"
	require.NoError(t, os.WriteFile(workflowPath, []byte(original), 0o644))

	cmd := NewEditCommand()
	cmd.SetArgs([]string{workflowPath, "max-turns: 20", "--dry-run"})
	var output strings.Builder
	cmd.SetOut(&output)
	require.NoError(t, cmd.Execute())

	assert.Contains(t, output.String(), "source: owner/repo@v1")
	assert.Contains(t, output.String(), "max-turns: 20")
	content, err := os.ReadFile(workflowPath)
	require.NoError(t, err)
	assert.Equal(t, original, string(content))
}

func TestEditCommandRejectsSourceChangesForSourceManagedWorkflow(t *testing.T) {
	t.Parallel()
	for name, args := range map[string][]string{
		"set":       {"--set", "source=owner/repo@v2", "--dry-run"},
		"nestedSet": {"--set", "source.ref=v2", "--dry-run"},
		"unset":     {"--unset", "source", "--dry-run"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			workflowPath := dir + "/workflow.md"
			original := "---\nsource: owner/repo@v1\non: workflow_dispatch\n---\n"
			require.NoError(t, os.WriteFile(workflowPath, []byte(original), 0o644))

			cmd := NewEditCommand()
			cmd.SetArgs(append([]string{workflowPath}, args...))
			err := cmd.Execute()
			require.ErrorContains(t, err, "cannot edit source for a source-managed workflow")

			content, readErr := os.ReadFile(workflowPath)
			require.NoError(t, readErr)
			assert.Equal(t, original, string(content))
		})
	}
}

// mustApplyEditChange applies a change and asserts that it modified the frontmatter.
func mustApplyEditChange(t *testing.T, frontmatter map[string]any, change editChange) {
	t.Helper()
	applied, err := applyEditChange(frontmatter, change)
	require.NoError(t, err)
	assert.True(t, applied, "expected the change to modify the frontmatter")
}

func TestEditChangeKeepsShorthandTriggersWhenScheduleIsAbsent(t *testing.T) {
	t.Parallel()
	for name, triggers := range map[string]any{
		"string": "push",
		"list":   []any{"push", "workflow_dispatch"},
		"map":    map[string]any{"push": nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			frontmatter := map[string]any{"on": triggers}
			applied, err := applyEditChange(frontmatter, editChange{kind: "unset", path: "on.schedule"})
			require.NoError(t, err)
			assert.False(t, applied)
			assert.Equal(t, triggers, frontmatter["on"])
		})
	}
}

func TestEditChangeExpandsScheduleAndTriggerShorthands(t *testing.T) {
	t.Parallel()
	frontmatter := map[string]any{"on": "daily"}
	mustApplyEditChange(t, frontmatter, editChange{kind: "set", path: "on.schedule", value: "every 6h"})
	assert.Equal(t, map[string]any{"schedule": "every 6h", "workflow_dispatch": nil}, frontmatter["on"])

	frontmatter = map[string]any{"on": []any{"push", "workflow_dispatch"}}
	mustApplyEditChange(t, frontmatter, editChange{kind: "set", path: "on.schedule", value: "every 6h"})
	assert.Equal(t, map[string]any{"push": nil, "workflow_dispatch": nil, "schedule": "every 6h"}, frontmatter["on"])
}

func TestEditChangeRejectsUnexpandableTriggerShorthand(t *testing.T) {
	t.Parallel()
	frontmatter := map[string]any{"on": "/bot"}
	_, err := applyEditChange(frontmatter, editChange{kind: "set", path: "on.schedule", value: "every 6h"})
	require.ErrorContains(t, err, "object form")
	assert.Equal(t, "/bot", frontmatter["on"])
}

func TestEditChangesAreNoOpsWhenValuesAlreadyMatch(t *testing.T) {
	t.Parallel()
	frontmatter := map[string]any{"max-turns": 20, "imports": []any{"shared/common.md"}}
	applied, err := applyEditChange(frontmatter, editChange{kind: "set", path: "max-turns", value: 20})
	require.NoError(t, err)
	assert.False(t, applied)

	applied, err = applyEditChange(frontmatter, editChange{kind: "add", path: "imports", value: "shared/common.md"})
	require.NoError(t, err)
	assert.False(t, applied)

	applied, err = applyEditChange(frontmatter, editChange{kind: "remove", path: "imports", value: "shared/other.md"})
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, []any{"shared/common.md"}, frontmatter["imports"])
}

func TestEditCommandLeavesWorkflowUnchangedForNoOpEdits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	workflowPath := dir + "/workflow.md"
	original := "---\n# keep this comment\non: push\n---\n# Workflow\n"
	require.NoError(t, os.WriteFile(workflowPath, []byte(original), 0o644))

	cmd := NewEditCommand()
	cmd.SetArgs([]string{workflowPath, "--schedule", "off"})
	var output strings.Builder
	cmd.SetOut(&output)
	require.NoError(t, cmd.Execute())

	assert.Contains(t, output.String(), "already matches")
	content, err := os.ReadFile(workflowPath)
	require.NoError(t, err)
	assert.Equal(t, original, string(content))
}

func TestWriteFileAtomicallyReplacesContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := dir + "/workflow.md"
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))
	require.NoError(t, writeFileAtomically(path, []byte("new")))

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new", string(content))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestWriteFileAtomicallyPreservesPermissions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := dir + "/workflow.md"
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))
	require.NoError(t, writeFileAtomically(path, []byte("new")))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
