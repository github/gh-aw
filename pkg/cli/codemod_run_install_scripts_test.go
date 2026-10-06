//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRunInstallScriptsToRuntimesNodeCodemod_Metadata(t *testing.T) {
	t.Parallel()
	codemod := getRunInstallScriptsToRuntimesNodeCodemod()

	assert.Equal(t, "run-install-scripts-to-runtimes-node", codemod.ID)
	assert.Equal(t, "Move run-install-scripts under runtimes.node", codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.NotEmpty(t, codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestRunInstallScriptsToRuntimesNode_NoOp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		content     string
		frontmatter map[string]any
	}{
		{
			name: "no run-install-scripts field",
			content: `---
on: workflow_dispatch
---

# Test`,
			frontmatter: map[string]any{
				"on": "workflow_dispatch",
			},
		},
		{
			name: "only runtimes.node.run-install-scripts (already nested)",
			content: `---
on: workflow_dispatch
runtimes:
  node:
    run-install-scripts: true
---

# Test`,
			frontmatter: map[string]any{
				"on": "workflow_dispatch",
				"runtimes": map[string]any{
					"node": map[string]any{
						"run-install-scripts": true,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			codemod := getRunInstallScriptsToRuntimesNodeCodemod()
			result, applied, err := codemod.Apply(tt.content, tt.frontmatter)
			require.NoError(t, err)
			assert.False(t, applied, "Should not apply")
			assert.Equal(t, tt.content, result, "Content should be unchanged")
		})
	}
}

func TestRunInstallScriptsToRuntimesNode_AppendNewRuntimesBlock(t *testing.T) {
	t.Parallel()
	codemod := getRunInstallScriptsToRuntimesNodeCodemod()

	content := `---
on: workflow_dispatch
run-install-scripts: true
---

# Test`

	frontmatter := map[string]any{
		"on":                  "workflow_dispatch",
		"run-install-scripts": true,
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "\nrun-install-scripts: true\n")
	assert.Contains(t, result, "runtimes:")
	assert.Contains(t, result, "  node:")
	assert.Contains(t, result, "    run-install-scripts: true")
}

func TestRunInstallScriptsToRuntimesNode_FalseValue(t *testing.T) {
	t.Parallel()
	codemod := getRunInstallScriptsToRuntimesNodeCodemod()

	content := `---
on: workflow_dispatch
run-install-scripts: false
---

# Test`

	frontmatter := map[string]any{
		"on":                  "workflow_dispatch",
		"run-install-scripts": false,
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "\nrun-install-scripts: false\n")
	assert.Contains(t, result, "    run-install-scripts: false")
}

func TestRunInstallScriptsToRuntimesNode_ExistingRuntimesNoNode(t *testing.T) {
	t.Parallel()
	codemod := getRunInstallScriptsToRuntimesNodeCodemod()

	content := `---
on: workflow_dispatch
run-install-scripts: true
runtimes:
  python:
    version: "3.11"
---

# Test`

	frontmatter := map[string]any{
		"on":                  "workflow_dispatch",
		"run-install-scripts": true,
		"runtimes": map[string]any{
			"python": map[string]any{
				"version": "3.11",
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "\nrun-install-scripts: true\n")
	assert.Contains(t, result, "runtimes:")
	assert.Contains(t, result, "  node:")
	assert.Contains(t, result, "    run-install-scripts: true")
	assert.Contains(t, result, "  python:")
}

func TestRunInstallScriptsToRuntimesNode_ExistingRuntimesWithNode(t *testing.T) {
	t.Parallel()
	codemod := getRunInstallScriptsToRuntimesNodeCodemod()

	content := `---
on: workflow_dispatch
run-install-scripts: true
runtimes:
  node:
    version: "24"
---

# Test`

	frontmatter := map[string]any{
		"on":                  "workflow_dispatch",
		"run-install-scripts": true,
		"runtimes": map[string]any{
			"node": map[string]any{
				"version": "24",
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "\nrun-install-scripts: true\n")
	assert.Contains(t, result, "  node:")
	assert.Contains(t, result, "    run-install-scripts: true")
	assert.Contains(t, result, "    version: \"24\"")
}

func TestRunInstallScriptsToRuntimesNode_IdempotentBothPresent(t *testing.T) {
	t.Parallel()
	codemod := getRunInstallScriptsToRuntimesNodeCodemod()

	// Top-level AND nested both present: remove top-level, keep nested.
	content := `---
on: workflow_dispatch
run-install-scripts: true
runtimes:
  node:
    run-install-scripts: true
---

# Test`

	frontmatter := map[string]any{
		"on":                  "workflow_dispatch",
		"run-install-scripts": true,
		"runtimes": map[string]any{
			"node": map[string]any{
				"run-install-scripts": true,
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	// Top-level line should be gone
	assert.NotContains(t, result, "\nrun-install-scripts: true\n")
	// Nested line should still be there
	assert.Contains(t, result, "    run-install-scripts: true")
}

func TestRunInstallScriptsToRuntimesNode_PreservesOtherFields(t *testing.T) {
	t.Parallel()
	codemod := getRunInstallScriptsToRuntimesNodeCodemod()

	content := `---
on: workflow_dispatch
run-install-scripts: true
permissions:
  contents: read
---

# Test`

	frontmatter := map[string]any{
		"on":                  "workflow_dispatch",
		"run-install-scripts": true,
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "permissions:")
	assert.Contains(t, result, "  contents: read")
	assert.Contains(t, result, "runtimes:")
	assert.Contains(t, result, "  node:")
	assert.Contains(t, result, "    run-install-scripts: true")
}
