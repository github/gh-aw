//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
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

func TestWorkflowDispatchAwContextRemovalCodemod_YAMLFormsAndLiteralText(t *testing.T) {
	t.Parallel()

	codemod := getWorkflowDispatchAwContextRemovalCodemod()
	tests := []struct {
		name            string
		content         string
		expectedComment string
	}{
		{
			name: "flow mappings and quoted key",
			content: `---
on: {workflow_dispatch: {inputs: {'aw_context': {type: string}, task: {type: string}}}}
---
`,
		},
		{
			name: "block mapping and description literal",
			content: `---
on:
  workflow_dispatch:
    inputs:
      'aw_context':
        type: string
      task:
        description: |
          aw_context: example
        type: string # keep task comment
---
`,
			expectedComment: "# keep task comment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			frontmatter, err := parser.ExtractFrontmatterFromContent(tt.content)
			require.NoError(t, err)
			updated, applied, err := codemod.Apply(tt.content, frontmatter.Frontmatter)
			require.NoError(t, err)
			require.True(t, applied)

			updatedFrontmatter, err := parser.ExtractFrontmatterFromContent(updated)
			require.NoError(t, err)
			dispatch := updatedFrontmatter.Frontmatter["on"].(map[string]any)["workflow_dispatch"].(map[string]any)
			inputs := dispatch["inputs"].(map[string]any)
			assert.NotContains(t, inputs, "aw_context")
			assert.Contains(t, inputs, "task")
			if task, ok := inputs["task"].(map[string]any); ok {
				assert.Equal(t, frontmatter.Frontmatter["on"].(map[string]any)["workflow_dispatch"].(map[string]any)["inputs"].(map[string]any)["task"].(map[string]any)["description"], task["description"])
			}
			if tt.expectedComment != "" {
				assert.Contains(t, updated, tt.expectedComment)
			}
		})
	}
}

func TestWorkflowDispatchAwContextRemovalCodemod_EmptyInputsPreserveComments(t *testing.T) {
	t.Parallel()

	codemod := getWorkflowDispatchAwContextRemovalCodemod()
	tests := []struct {
		name            string
		content         string
		expectedComment string
	}{
		{
			name: "flow mapping with only reserved input",
			content: `---
on: {workflow_dispatch: {inputs: {aw_context: {type: string}}}}
---

# Workflow
`,
		},
		{
			name: "inline input header comment",
			content: `---
on:
  workflow_dispatch:
    inputs: # keep the input header comment
      aw_context:
        type: string
---

# Workflow
`,
			expectedComment: "# keep the input header comment",
		},
		{
			name: "comment after reserved input",
			content: `---
on:
  workflow_dispatch:
    inputs:
      aw_context:
        type: string
      # retain this comment
---

# Workflow
`,
			expectedComment: "# retain this comment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			frontmatter, err := parser.ExtractFrontmatterFromContent(tt.content)
			require.NoError(t, err)
			updated, applied, err := codemod.Apply(tt.content, frontmatter.Frontmatter)
			require.NoError(t, err)
			require.True(t, applied)
			assert.Contains(t, updated, "inputs: {}")
			if tt.expectedComment != "" {
				assert.Contains(t, updated, tt.expectedComment)
			}

			updatedFrontmatter, err := parser.ExtractFrontmatterFromContent(updated)
			require.NoError(t, err)
			dispatch := updatedFrontmatter.Frontmatter["on"].(map[string]any)["workflow_dispatch"].(map[string]any)
			require.IsType(t, map[string]any{}, dispatch["inputs"])
			assert.Empty(t, dispatch["inputs"])

			workflowPath := filepath.Join(t.TempDir(), "aw-context-empty-inputs.md")
			require.NoError(t, os.WriteFile(workflowPath, []byte(updated), 0o600))
			compiler := workflow.NewCompiler()
			data, err := compiler.ParseWorkflowFile(workflowPath)
			require.NoError(t, err)
			require.NoError(t, compiler.CompileWorkflowData(data, workflowPath))
		})
	}
}
