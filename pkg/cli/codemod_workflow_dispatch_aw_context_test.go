//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowDispatchAwContextRemovalCodemod_Metadata(t *testing.T) {
	t.Parallel()

	codemod := getWorkflowDispatchAwContextRemovalCodemod()
	assert.Equal(t, "workflow-dispatch-aw-context-removal", codemod.ID)
	assert.NotEmpty(t, codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "1.0.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestWorkflowDispatchAwContextRemovalCodemod_Apply(t *testing.T) {
	t.Parallel()

	content := `---
on:
  workflow_dispatch:
    inputs:
      aw_context:
        type: string # reserved input
      task:
        type: string
engine: copilot
---

# Preserve the body.
`
	frontmatter := map[string]any{
		"on": map[string]any{
			"workflow_dispatch": map[string]any{
				"inputs": map[string]any{
					"aw_context": map[string]any{"type": "string"},
					"task":       map[string]any{"type": "string"},
				},
			},
		},
		"engine": "copilot",
	}

	codemod := getWorkflowDispatchAwContextRemovalCodemod()
	updated, applied, err := codemod.Apply(content, frontmatter)
	require.NoError(t, err)
	require.True(t, applied)
	assert.Equal(t, `---
on:
  workflow_dispatch:
    inputs:
      task:
        type: string
engine: copilot
---

# Preserve the body.
`, updated)

	updatedAgain, appliedAgain, err := codemod.Apply(updated, frontmatter)
	require.NoError(t, err)
	assert.False(t, appliedAgain)
	assert.Equal(t, updated, updatedAgain)
}

func TestWorkflowDispatchAwContextRemovalCodemod_AbsentOrOtherTriggerIsUnchanged(t *testing.T) {
	t.Parallel()

	codemod := getWorkflowDispatchAwContextRemovalCodemod()
	content := "---\non: workflow_call\n---\n"
	frontmatter := map[string]any{
		"on": map[string]any{
			"workflow_call": map[string]any{
				"inputs": map[string]any{"aw_context": map[string]any{"type": "string"}},
			},
		},
	}

	updated, applied, err := codemod.Apply(content, frontmatter)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, updated)

	updated, applied, err = codemod.Apply(content, map[string]any{"on": map[string]any{"workflow_dispatch": nil}})
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, updated)
}

func TestWorkflowDispatchAwContextRemovalCodemod_FixesStrictCompilation(t *testing.T) {
	t.Parallel()

	content := `---
on:
  workflow_dispatch:
    inputs:
      aw_context:
        type: string
strict: true
---
Use caller context.
`
	compiler := workflow.NewCompiler()
	data, err := compiler.ParseWorkflowString(content, "aw-context.md")
	require.NoError(t, err)
	err = compiler.CompileWorkflowData(data, filepath.Join(t.TempDir(), "aw-context.md"))
	require.ErrorContains(t, err, "on.workflow_dispatch.inputs.aw_context is reserved")

	path := filepath.Join(t.TempDir(), "aw-context.md")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	fixed, _, err := processWorkflowFileWithInfo(path, GetAllCodemods(), true, false)
	require.NoError(t, err)
	require.True(t, fixed)

	updatedBytes, err := os.ReadFile(path)
	require.NoError(t, err)
	updated := string(updatedBytes)
	compiler = workflow.NewCompiler()
	data, err = compiler.ParseWorkflowString(updated, "aw-context.md")
	require.NoError(t, err)
	require.NoError(t, compiler.CompileWorkflowData(data, filepath.Join(t.TempDir(), "aw-context.md")))
}
