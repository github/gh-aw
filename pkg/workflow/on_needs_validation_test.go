//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateOnNeedsTargets(t *testing.T) {
	t.Run("valid on.needs target", func(t *testing.T) {
		data := &WorkflowData{
			Jobs: map[string]any{
				"secrets_fetcher": map[string]any{
					"runs-on": "ubuntu-latest",
				},
			},
			OnNeeds: []string{"secrets_fetcher"},
		}

		require.NoError(t, validateOnNeedsTargets(data), "expected on.needs validation to pass")
	})

	t.Run("built-in target rejected", func(t *testing.T) {
		data := &WorkflowData{
			Jobs:    map[string]any{"secrets_fetcher": map[string]any{}},
			OnNeeds: []string{"activation"},
		}

		err := validateOnNeedsTargets(data)
		require.Error(t, err, "expected on.needs validation error")
		require.ErrorContains(t, err, `built-in job "activation"`, "error should explain invalid built-in target")
	})

	t.Run("target depending on activation rejected", func(t *testing.T) {
		data := &WorkflowData{
			Jobs: map[string]any{
				"secrets_fetcher": map[string]any{
					"needs": "activation",
				},
			},
			OnNeeds: []string{"secrets_fetcher"},
		}

		err := validateOnNeedsTargets(data)
		require.Error(t, err, "expected on.needs validation error")
		require.ErrorContains(t, err, "cannot depend on activation/pre_activation", "error should explain cyclic dependency risk")
	})
}

func TestValidateOnNeedsDependencyChains(t *testing.T) {
	c := NewCompiler()

	t.Run("rejects chain where transitive dependency may get implicit activation need", func(t *testing.T) {
		data := &WorkflowData{
			Jobs: map[string]any{
				"secrets_fetcher": map[string]any{
					"needs": []any{"bootstrap"},
				},
				"bootstrap": map[string]any{
					"runs-on": "ubuntu-latest",
				},
			},
			OnNeeds: []string{"secrets_fetcher"},
		}

		err := c.validateOnNeeds(data)
		require.Error(t, err, "expected transitive chain validation error")
		require.ErrorContains(t, err, `depends on "bootstrap"`, "error should identify problematic transitive dependency")
		require.ErrorContains(t, err, "implicit needs: activation", "error should explain cycle-prone implicit activation dependency")
	})

	t.Run("allows chain when transitive dependency is explicitly in on.needs", func(t *testing.T) {
		data := &WorkflowData{
			Jobs: map[string]any{
				"secrets_fetcher": map[string]any{
					"needs": []any{"bootstrap"},
				},
				"bootstrap": map[string]any{
					"runs-on": "ubuntu-latest",
				},
			},
			OnNeeds: []string{"secrets_fetcher", "bootstrap"},
		}

		require.NoError(t, c.validateOnNeeds(data), "expected transitive chain to be valid when all dependencies are in on.needs")
	})
}

func TestValidateOnGitHubAppNeedsExpressions(t *testing.T) {
	c := NewCompiler()

	t.Run("allows on.needs expression in on.github-app", func(t *testing.T) {
		data := &WorkflowData{
			Jobs: map[string]any{
				"secrets_fetcher": map[string]any{
					"runs-on": "ubuntu-latest",
				},
			},
			OnNeeds: []string{"secrets_fetcher"},
			ActivationGitHubApp: &GitHubAppConfig{
				AppID:      "${{ needs.secrets_fetcher.outputs.app_id }}",
				PrivateKey: "${{ needs.secrets_fetcher.outputs.private_key }}",
			},
		}

		require.NoError(t, c.validateOnNeeds(data), "expected on.github-app needs expression to validate")
	})

	t.Run("rejects unknown needs expression in on.github-app", func(t *testing.T) {
		data := &WorkflowData{
			Jobs: map[string]any{
				"secrets_fetcher": map[string]any{
					"runs-on": "ubuntu-latest",
				},
			},
			ActivationGitHubApp: &GitHubAppConfig{
				AppID:      "${{ needs.missing_job.outputs.app_id }}",
				PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
			},
		}

		err := c.validateOnNeeds(data)
		require.Error(t, err, "expected on.github-app validation error")
		require.ErrorContains(t, err, `unknown job "missing_job"`, "error should identify unknown needs job")
	})

	t.Run("error field label uses client-id", func(t *testing.T) {
		data := &WorkflowData{
			Jobs: map[string]any{
				"secrets_fetcher": map[string]any{
					"runs-on": "ubuntu-latest",
				},
			},
			ActivationGitHubApp: &GitHubAppConfig{
				AppID:      "${{ needs.secrets_fetcher.outputs.app_id }}",
				PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
			},
		}

		err := c.validateOnNeeds(data)
		require.Error(t, err, "expected on.github-app validation error")
		require.ErrorContains(t, err, "on.github-app.client-id", "error field should use yaml key client-id")
	})
}

func TestValidatePromptNeedsAvailableBeforeActivation(t *testing.T) {
	t.Run("rejects runtime-import reference to explicit-needs job not in activation needs", func(t *testing.T) {
		tmpDir := t.TempDir()
		workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
		promptsDir := filepath.Join(tmpDir, ".github", "prompts")
		require.NoError(t, os.MkdirAll(workflowsDir, 0755))
		require.NoError(t, os.MkdirAll(promptsDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(promptsDir, "runtime.md"), []byte("Value: ${{ needs.select.outputs.value }}"), 0600))

		c := NewCompiler()
		c.markdownPath = filepath.Join(workflowsDir, "workflow.md")
		data := &WorkflowData{
			MarkdownContent: "{{#runtime-import .github/prompts/runtime.md}}",
			Jobs: map[string]any{
				"select": map[string]any{
					"needs": "other",
				},
			},
		}

		err := c.validateOnNeeds(data)
		require.Error(t, err)
		require.ErrorContains(t, err, "prompt references needs.select.*")
		require.ErrorContains(t, err, "not available before activation")
	})

	t.Run("allows runtime-import reference to explicit-needs job in on.needs", func(t *testing.T) {
		tmpDir := t.TempDir()
		workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
		promptsDir := filepath.Join(tmpDir, ".github", "prompts")
		require.NoError(t, os.MkdirAll(workflowsDir, 0755))
		require.NoError(t, os.MkdirAll(promptsDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(promptsDir, "runtime.md"), []byte("Value: ${{ needs.select.outputs.value }}"), 0600))

		c := NewCompiler()
		c.markdownPath = filepath.Join(workflowsDir, "workflow.md")
		data := &WorkflowData{
			MarkdownContent: "{{#runtime-import .github/prompts/runtime.md}}",
			OnNeeds:         []string{"select", "other"},
			Jobs: map[string]any{
				"select": map[string]any{
					"needs": "other",
				},
				"other": map[string]any{},
			},
		}

		require.NoError(t, c.validateOnNeeds(data))
	})

	t.Run("allows no-explicit-needs runtime-import reference because activation auto-adds it", func(t *testing.T) {
		tmpDir := t.TempDir()
		workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
		promptsDir := filepath.Join(tmpDir, ".github", "prompts")
		require.NoError(t, os.MkdirAll(workflowsDir, 0755))
		require.NoError(t, os.MkdirAll(promptsDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(promptsDir, "runtime.md"), []byte("Value: ${{ needs.select.outputs.value }}"), 0600))

		c := NewCompiler()
		c.markdownPath = filepath.Join(workflowsDir, "workflow.md")
		data := &WorkflowData{
			MarkdownContent: "{{#runtime-import .github/prompts/runtime.md}}",
			Jobs: map[string]any{
				"select": map[string]any{},
			},
		}

		require.NoError(t, c.validateOnNeeds(data))
	})

	t.Run("allows no-explicit-needs reference from frontmatter import only", func(t *testing.T) {
		tmpDir := t.TempDir()
		workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
		sharedDir := filepath.Join(workflowsDir, "shared")
		require.NoError(t, os.MkdirAll(sharedDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "runtime.md"), []byte("Value: ${{ needs.select.outputs.value }}"), 0600))

		c := NewCompiler()
		c.markdownPath = filepath.Join(workflowsDir, "workflow.md")
		data := &WorkflowData{
			ImportPaths: []string{".github/workflows/shared/runtime.md"},
			Jobs: map[string]any{
				"select": map[string]any{},
			},
		}

		require.NoError(t, c.validateOnNeeds(data))
	})
}
