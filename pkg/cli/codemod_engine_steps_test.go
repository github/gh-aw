//go:build !integration

package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEngineStepsToTopLevelCodemod_Metadata(t *testing.T) {
	t.Parallel()
	codemod := getEngineStepsToTopLevelCodemod()

	assert.Equal(t, "engine-steps-to-top-level", codemod.ID)
	assert.Equal(t, "Move engine.steps to top-level steps", codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "0.11.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

// TestEngineStepsToTopLevelCodemod_NoOp tests cases where the codemod should not apply
func TestEngineStepsToTopLevelCodemod_NoOp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		content     string
		frontmatter map[string]any
	}{
		{
			name: "no engine field",
			content: `---
on: push
---

# Test workflow`,
			frontmatter: map[string]any{
				"on": "push",
			},
		},
		{
			name: "engine is a string",
			content: `---
on: push
engine: claude
---

# Test workflow`,
			frontmatter: map[string]any{
				"on":     "push",
				"engine": "claude",
			},
		},
		{
			name: "engine object without steps",
			content: `---
on: push
engine:
  id: claude
  model: claude-3-5-sonnet
---

# Test workflow`,
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id":    "claude",
					"model": "claude-3-5-sonnet",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			codemod := getEngineStepsToTopLevelCodemod()
			result, applied, err := codemod.Apply(tt.content, tt.frontmatter)
			require.NoError(t, err)
			assert.False(t, applied, "Should not apply")
			assert.Equal(t, tt.content, result, "Content should be unchanged")
		})
	}
}

// TestEngineStepsToTopLevelCodemod_SingleStep tests moving a single engine step to top level
func TestEngineStepsToTopLevelCodemod_SingleStep(t *testing.T) {
	t.Parallel()
	codemod := getEngineStepsToTopLevelCodemod()

	content := `---
on: push
engine:
  id: codex
  steps:
    - name: Run step
      run: echo "hello"
---

# Test workflow`

	frontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id": "codex",
			"steps": []any{
				map[string]any{"name": "Run step", "run": `echo "hello"`},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied, "Should apply when engine has steps")
	assert.Contains(t, result, "steps:")
	assert.Contains(t, result, "name: Run step")
	assert.Contains(t, result, "engine:")
	assert.Contains(t, result, "id: codex")
	// engine block should not contain steps:
	assert.NotContains(t, result, "  steps:")
}

// TestEngineStepsToTopLevelCodemod_MultipleSteps tests moving multiple steps preserves order
func TestEngineStepsToTopLevelCodemod_MultipleSteps(t *testing.T) {
	t.Parallel()
	codemod := getEngineStepsToTopLevelCodemod()

	content := `---
on: push
engine:
  id: codex
  model: gpt-4o
  steps:
    - name: Step 1
      run: echo "step1"
    - name: Step 2
      run: echo "step2"
---

# Test workflow`

	frontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id":    "codex",
			"model": "gpt-4o",
			"steps": []any{
				map[string]any{"name": "Step 1", "run": `echo "step1"`},
				map[string]any{"name": "Step 2", "run": `echo "step2"`},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)

	// Steps should be at top level in order
	step1Pos := strings.Index(result, "name: Step 1")
	step2Pos := strings.Index(result, "name: Step 2")
	require.Positive(t, step1Pos, "Should contain 'name: Step 1'")
	require.Positive(t, step2Pos, "Should contain 'name: Step 2'")
	assert.Less(t, step1Pos, step2Pos, "Step 1 should appear before Step 2")

	// Engine should still have id and model
	assert.Contains(t, result, "id: codex")
	assert.Contains(t, result, "model: gpt-4o")

	// Engine should no longer have steps (check each line)
	lines := strings.Split(result, "\n")
	inEngine := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "engine:") {
			inEngine = true
		} else if inEngine && len(trimmed) > 0 && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inEngine = false
		}
		if inEngine && trimmed == "steps:" {
			t.Error("engine block should not contain 'steps:' after codemod")
		}
	}
}

// TestEngineStepsToTopLevelCodemod_UsesStep tests a step that uses an action (not run:)
func TestEngineStepsToTopLevelCodemod_UsesStep(t *testing.T) {
	t.Parallel()
	codemod := getEngineStepsToTopLevelCodemod()

	content := `---
on: push
engine:
  id: codex
  steps:
    - name: Run AI Inference
      uses: actions/ai-inference@v1
      with:
        prompt-file: ${{ env.GH_AW_PROMPT }}
        model: gpt-4o-mini
---

# Test workflow`

	frontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id": "codex",
			"steps": []any{
				map[string]any{
					"name": "Run AI Inference",
					"uses": "actions/ai-inference@v1",
					"with": map[string]any{
						"prompt-file": "${{ env.GH_AW_PROMPT }}",
						"model":       "gpt-4o-mini",
					},
				},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "name: Run AI Inference")
	assert.Contains(t, result, "uses: actions/ai-inference@v1")
	assert.Contains(t, result, "prompt-file:")
	assert.Contains(t, result, "model: gpt-4o-mini")
	// Should be at top level, not inside engine
	assert.NotContains(t, result, "  steps:")
}

// TestEngineStepsToTopLevelCodemod_EngineFieldsAfterSteps tests that engine fields after steps
// are preserved correctly in the engine block
func TestEngineStepsToTopLevelCodemod_EngineFieldsAfterSteps(t *testing.T) {
	t.Parallel()
	codemod := getEngineStepsToTopLevelCodemod()

	content := `---
on: push
engine:
  id: codex
  steps:
    - name: Prep
      run: echo "prep"
  model: gpt-4o
---

# Test workflow`

	frontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id":    "codex",
			"model": "gpt-4o",
			"steps": []any{
				map[string]any{"name": "Prep", "run": `echo "prep"`},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	// Engine fields should still be present
	assert.Contains(t, result, "id: codex")
	assert.Contains(t, result, "model: gpt-4o")
	// Step should be at top level
	assert.Contains(t, result, "name: Prep")
}

// TestEngineStepsToTopLevelCodemod_MergeWithExistingSteps tests appending engine steps
// after existing top-level steps
func TestEngineStepsToTopLevelCodemod_MergeWithExistingSteps(t *testing.T) {
	t.Parallel()
	codemod := getEngineStepsToTopLevelCodemod()

	content := `---
on: push
engine:
  id: codex
  steps:
    - name: Engine Step
      run: echo "engine"
steps:
  - name: Existing Step
    run: echo "existing"
---

# Test workflow`

	frontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id": "codex",
			"steps": []any{
				map[string]any{"name": "Engine Step", "run": `echo "engine"`},
			},
		},
		"steps": []any{
			map[string]any{"name": "Existing Step", "run": `echo "existing"`},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)

	// Both steps should be present
	assert.Contains(t, result, "name: Engine Step")
	assert.Contains(t, result, "name: Existing Step")

	// Should have only one top-level "steps:" header
	stepsCount := strings.Count(result, "\nsteps:\n")
	assert.Equal(t, 1, stepsCount, "Should have exactly one top-level 'steps:' header")

	// Engine block should not have steps
	assert.NotContains(t, result, "  steps:")

	// Existing step should come before the engine step (engine steps are appended)
	existingPos := strings.Index(result, "name: Existing Step")
	enginePos := strings.Index(result, "name: Engine Step")
	require.Positive(t, existingPos)
	require.Positive(t, enginePos)
	assert.Less(t, existingPos, enginePos, "Existing step should come before appended engine step")
}

// TestEngineStepsToTopLevelCodemod_NoMarkdownBody tests a workflow without a body section
func TestEngineStepsToTopLevelCodemod_NoMarkdownBody(t *testing.T) {
	t.Parallel()
	codemod := getEngineStepsToTopLevelCodemod()

	content := `---
on: push
engine:
  id: claude
  steps:
    - name: Setup
      run: echo "setup"
---`

	frontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id": "claude",
			"steps": []any{
				map[string]any{"name": "Setup", "run": "echo \"setup\""},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "steps:")
	assert.Contains(t, result, "name: Setup")
	assert.NotContains(t, result, "  steps:")
}

// TestEngineStepsToTopLevelCodemod_Idempotent tests that applying the codemod twice
// (simulated by running on output with updated frontmatter) does not change the content
func TestEngineStepsToTopLevelCodemod_Idempotent(t *testing.T) {
	t.Parallel()
	codemod := getEngineStepsToTopLevelCodemod()

	// After codemod is applied, engine no longer has steps in frontmatter
	alreadyMigratedContent := `---
on: push
engine:
  id: codex
steps:
  - name: Run step
    run: echo "hello"
---

# Test workflow`

	// Frontmatter reflects the already-migrated state (no engine.steps)
	alreadyMigratedFrontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id": "codex",
		},
		"steps": []any{
			map[string]any{"name": "Run step", "run": `echo "hello"`},
		},
	}

	result, applied, err := codemod.Apply(alreadyMigratedContent, alreadyMigratedFrontmatter)

	require.NoError(t, err)
	assert.False(t, applied, "Should not apply again when engine.steps is already gone")
	assert.Equal(t, alreadyMigratedContent, result)
}

// TestEngineStepsToTopLevelCodemod_StepsBeforeEngine tests when top-level steps field
// comes before the engine field in the YAML
func TestEngineStepsToTopLevelCodemod_StepsBeforeEngine(t *testing.T) {
	t.Parallel()
	codemod := getEngineStepsToTopLevelCodemod()

	content := `---
on: push
steps:
  - name: First Step
    run: echo "first"
engine:
  id: codex
  steps:
    - name: Engine Step
      run: echo "engine"
---

# Test workflow`

	frontmatter := map[string]any{
		"on": "push",
		"steps": []any{
			map[string]any{"name": "First Step", "run": `echo "first"`},
		},
		"engine": map[string]any{
			"id": "codex",
			"steps": []any{
				map[string]any{"name": "Engine Step", "run": `echo "engine"`},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)

	// Both steps should be present
	assert.Contains(t, result, "name: First Step")
	assert.Contains(t, result, "name: Engine Step")

	// Only one top-level steps: header
	stepsCount := strings.Count(result, "\nsteps:\n")
	assert.Equal(t, 1, stepsCount, "Should have exactly one top-level 'steps:' header")

	// Engine should not have steps
	assert.NotContains(t, result, "  steps:")
}

// TestEngineStepsToTopLevelCodemod_PreservesMarkdownBody tests that the markdown body
// is preserved after the frontmatter when applying the codemod
func TestEngineStepsToTopLevelCodemod_PreservesMarkdownBody(t *testing.T) {
	t.Parallel()
	codemod := getEngineStepsToTopLevelCodemod()

	content := `---
on: push
engine:
  id: claude
  steps:
    - name: Install deps
      run: npm install
---

# My Workflow

This workflow does something useful.

## Instructions

Follow these steps carefully.`

	frontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id": "claude",
			"steps": []any{
				map[string]any{"name": "Install deps", "run": "npm install"},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)

	// Markdown body should be preserved
	assert.Contains(t, result, "# My Workflow")
	assert.Contains(t, result, "This workflow does something useful.")
	assert.Contains(t, result, "## Instructions")
	assert.Contains(t, result, "Follow these steps carefully.")

	// Frontmatter changes should also be present
	assert.Contains(t, result, "name: Install deps")
}

// TestEngineStepsToTopLevelCodemod_TableDriven is a comprehensive table-driven test
func TestEngineStepsToTopLevelCodemod_TableDriven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		content      string
		frontmatter  map[string]any
		wantApplied  bool
		wantContains []string
		wantAbsent   []string
	}{
		{
			name: "claude engine with single run step",
			content: `---
on: issues
engine:
  id: claude
  steps:
    - name: Checkout
      uses: actions/checkout@v4
---

Do something`,
			frontmatter: map[string]any{
				"on": "issues",
				"engine": map[string]any{
					"id": "claude",
					"steps": []any{
						map[string]any{"name": "Checkout", "uses": "actions/checkout@v4"},
					},
				},
			},
			wantApplied:  true,
			wantContains: []string{"name: Checkout", "uses: actions/checkout@v4", "steps:\n"},
			wantAbsent:   []string{"  steps:"},
		},
		{
			name: "engine with env and steps - env preserved",
			content: `---
on: push
engine:
  id: codex
  env:
    MY_VAR: value
  steps:
    - name: Test
      run: echo test
---`,
			frontmatter: map[string]any{
				"on": "push",
				"engine": map[string]any{
					"id": "codex",
					"env": map[string]any{
						"MY_VAR": "value",
					},
					"steps": []any{
						map[string]any{"name": "Test", "run": "echo test"},
					},
				},
			},
			wantApplied:  true,
			wantContains: []string{"env:", "MY_VAR: value", "name: Test"},
			wantAbsent:   []string{"  steps:"},
		},
		{
			name: "copilot engine with steps",
			content: `---
on: pull_request
engine:
  id: copilot
  steps:
    - name: Setup Node
      uses: actions/setup-node@v4
      with:
        node-version: "24"
---

Review this PR`,
			frontmatter: map[string]any{
				"on": "pull_request",
				"engine": map[string]any{
					"id": "copilot",
					"steps": []any{
						map[string]any{
							"name": "Setup Node",
							"uses": "actions/setup-node@v4",
							"with": map[string]any{"node-version": "24"},
						},
					},
				},
			},
			wantApplied:  true,
			wantContains: []string{"name: Setup Node", "uses: actions/setup-node@v4"},
			wantAbsent:   []string{"  steps:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			codemod := getEngineStepsToTopLevelCodemod()
			result, applied, err := codemod.Apply(tt.content, tt.frontmatter)

			require.NoError(t, err, "Should not return error")
			assert.Equal(t, tt.wantApplied, applied, "Applied state mismatch")

			for _, want := range tt.wantContains {
				assert.Contains(t, result, want, "Should contain %q", want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, result, absent, "Should not contain %q", absent)
			}
		})
	}
}

// TestEngineStepsToTopLevelCodemod_EmptyEngineBlockRemoved tests that a dangling
// engine: block (containing only steps) is removed after migration
func TestEngineStepsToTopLevelCodemod_EmptyEngineBlockRemoved(t *testing.T) {
	t.Parallel()
	codemod := getEngineStepsToTopLevelCodemod()

	content := `---
on: push
engine:
  steps:
    - name: Only step
      run: echo "only"
---

# Test workflow`

	frontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"steps": []any{
				map[string]any{"name": "Only step", "run": `echo "only"`},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)

	// Step should be at top level
	assert.Contains(t, result, "name: Only step")

	// The empty engine block should be removed (it only contained steps)
	assert.NotContains(t, result, "engine:")
}

// TestEngineStepsToTopLevelCodemod_NonSequenceTopLevelSteps tests that when
// top-level steps exists but is not a sequence, a fresh steps block is inserted
func TestEngineStepsToTopLevelCodemod_NonSequenceTopLevelSteps(t *testing.T) {
	t.Parallel()
	codemod := getEngineStepsToTopLevelCodemod()

	content := `---
on: push
engine:
  id: codex
  steps:
    - name: Engine Step
      run: echo "engine"
steps: invalid-scalar
---

# Test workflow`

	frontmatter := map[string]any{
		"on": "push",
		"engine": map[string]any{
			"id": "codex",
			"steps": []any{
				map[string]any{"name": "Engine Step", "run": `echo "engine"`},
			},
		},
		// steps is a scalar, not a slice
		"steps": "invalid-scalar",
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)

	// Engine step should be present as a new top-level block
	assert.Contains(t, result, "name: Engine Step")
	// Engine block should not have steps any more
	assert.NotContains(t, result, "  steps:")
}
