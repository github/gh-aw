//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestDocsAgenticsCheckoutAvoidsDefaultBranchAPI(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	if err != nil {
		t.Skipf("Skipping test: not in a git repository: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "docs.yml"))
	require.NoError(t, err)

	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string
				With map[string]any
			}
		}
	}
	require.NoError(t, yaml.Unmarshal(content, &workflow))

	for _, step := range workflow.Jobs["build"].Steps {
		if step.Name == "Checkout agentics workflows" {
			// An explicit branch skips checkout's token-authenticated REST lookup.
			require.Equal(t, "main", step.With["ref"])
			return
		}
	}
	t.Fatal("agentics checkout step not found")
}

func TestVisualRegressionCheckerAgenticsDependency(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	require.NoError(t, err)

	type step struct {
		Name string
		Uses string
		With map[string]any
		Env  map[string]string
	}

	for _, extension := range []string{".md", ".lock.yml"} {
		t.Run(extension, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "visual-regression-checker"+extension))
			require.NoError(t, err)
			if extension == ".md" {
				parts := strings.SplitN(string(content), "---", 3)
				require.Len(t, parts, 3)
				content = []byte(parts[1])
			}

			var workflow struct {
				Steps []step
				Jobs  map[string]struct {
					Steps []step
				}
			}
			require.NoError(t, yaml.Unmarshal(content, &workflow))
			steps := workflow.Steps
			if extension == ".lock.yml" {
				steps = workflow.Jobs["agent"].Steps
			}
			require.NotEmpty(t, steps)

			checkoutIndex := -1
			for i, s := range steps {
				if s.Name == "Checkout agentics workflows" {
					require.Equal(t, -1, checkoutIndex, "agentics checkout must not be duplicated")
					checkoutIndex = i
					require.True(t, strings.HasPrefix(s.Uses, "actions/checkout@"))
					require.Equal(t, "githubnext/agentics", s.With["repository"])
					require.Equal(t, "main", s.With["ref"])
					require.Equal(t, ".agentics", s.With["path"])
					require.Equal(t, "workflows", s.With["sparse-checkout"])
					require.Equal(t, false, s.With["persist-credentials"])
				}
			}
			require.NotEqual(t, -1, checkoutIndex, "agentics checkout step not found")

			for _, name := range []string{"Build documentation"} {
				found := false
				for i, s := range steps {
					if s.Name == name {
						require.Less(t, checkoutIndex, i, "agentics checkout must precede %s", name)
						require.Equal(t, "${{ github.workspace }}/.agentics/workflows", s.Env["AGENTICS_WORKFLOWS_DIR"], name)
						found = true
					}
				}
				require.True(t, found, "%s step not found", name)
			}
		})
	}
}
