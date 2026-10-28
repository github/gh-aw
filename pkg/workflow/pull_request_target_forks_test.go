//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullRequestTargetBoundedForkAdmission(t *testing.T) {
	tests := []struct {
		name   string
		forks  string
		allows bool
	}{
		{"omitted", "", true},
		{"wildcard string", "'*'", true},
		{"wildcard list", "['*']", true},
		{"mixed wildcard", "[trusted/repo, '*']", true},
		{"wildcard first", "['*', trusted/repo]", true},
		{"empty list", "[]", true},
		{"exact string", "trusted/repo", true},
		{"owner string", "'trusted/*'", true},
		{"mixed bounded", "[trusted/repo, 'partner/*']", true},
		{"literal punctuation", "[trusted/repo.name-1_2]", true},
		{"empty string", "''", false},
		{"wildcard owner", "'*/repo'", false},
		{"partial owner", "'trust*/*'", false},
		{"partial repo", "'trusted/repo*'", false},
		{"recursive wildcard", "'trusted/**'", false},
		{"expression", "'${{ github.repository }}'", false},
		{"whitespace", "' trusted/repo '", false},
		{"missing repo", "'trusted/'", false},
	}
	for _, tt := range tests {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/strict=%t", tt.name, strict), func(t *testing.T) {
				forks := ""
				if tt.forks != "" {
					forks = "    forks: " + tt.forks + "\n"
				}
				source := fmt.Sprintf(`---
strict: %t
on:
  pull_request_target:
    types: [labeled]
    acknowledge-risk: true
%s  workflow_dispatch:
checkout: false
tools:
  github: false
---
Review the contribution.
`, strict, forks)
				file := filepath.Join(testutil.TempDir(t, "bounded-target-forks"), "workflow.md")
				require.NoError(t, os.WriteFile(file, []byte(source), 0600))
				err := NewCompiler().CompileWorkflow(file)
				if strict && !tt.allows {
					require.ErrorContains(t, err, "requires supported fork patterns")
					assert.Contains(t, err.Error(), "acknowledge-risk do not bypass")
					assert.NoFileExists(t, filepath.Join(filepath.Dir(file), "workflow.lock.yml"))
				} else {
					require.NoError(t, err)
				}
			})
		}
	}
}

func TestPullRequestTargetBoundedForkAdmissionCLIOverride(t *testing.T) {
	source := `---
strict: false
on:
  pull_request_target:
    acknowledge-risk: true
    forks: ["trusted/repo*"]
checkout:
  ref: ${{ github.event.pull_request.base.sha }}
tools:
  github: false
---
Review the contribution.
`
	file := filepath.Join(testutil.TempDir(t, "bounded-target-cli"), "workflow.md")
	require.NoError(t, os.WriteFile(file, []byte(source), 0600))
	compiler := NewCompiler()
	compiler.SetStrictMode(true)
	require.ErrorContains(t, compiler.CompileWorkflow(file), "requires supported fork patterns")
}

func TestPullRequestTargetBoundedForkAdmissionShorthand(t *testing.T) {
	for _, tt := range []struct {
		on        string
		wantError string
	}{
		{"pull_request_target", ""},
		{"[pull_request_target, workflow_dispatch]", "expected string or object"},
		{"\n  pull_request_target:", "expected string or object"},
		{"\n  pull_request_target: null", "expected string or object"},
		{"\n  pull_request_target: {}", ""},
	} {
		t.Run(tt.on, func(t *testing.T) {
			source := "---\non: " + tt.on + "\ncheckout: false\ntools:\n  github: false\n---\nReview the contribution.\n"
			file := filepath.Join(testutil.TempDir(t, "bounded-target-shorthand"), "workflow.md")
			require.NoError(t, os.WriteFile(file, []byte(source), 0600))
			err := NewCompiler().CompileWorkflow(file)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				assert.NoFileExists(t, filepath.Join(filepath.Dir(file), "workflow.lock.yml"))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestHasSupportedPullRequestTargetForksRejectsMalformedValues(t *testing.T) {
	for _, forks := range []any{nil, true, 1, []any{nil}, []any{true}, []any{"*", "trusted/repo*"}, map[string]any{"author-association": []any{"OWNER"}}} {
		assert.False(t, hasSupportedPullRequestTargetForks(map[string]any{
			"on": map[string]any{
				"pull_request_target": map[string]any{"forks": forks},
			},
		}), "forks: %#v", forks)
	}
}

func TestPullRequestTargetWildcardWarning(t *testing.T) {
	for _, forks := range []any{"*", []any{"*"}, []any{"trusted/repo", "*"}, []any{"*", "trusted/*"}} {
		for _, strict := range []bool{false, true} {
			compiler := NewCompiler()
			frontmatter := map[string]any{
				"strict": strict,
				"on": map[string]any{
					"pull_request_target": map[string]any{
						"forks":            forks,
						"acknowledge-risk": true,
					},
				},
			}
			data := &WorkflowData{
				On:                         "on:\n  pull_request_target:\n    types: [labeled]",
				RawFrontmatter:             frontmatter,
				CheckoutExplicitlyDisabled: true,
				Roles:                      []string{"admin", "maintainer", "write"},
			}
			output := testutil.CaptureStderr(t, func() {
				require.NoError(t, compiler.validatePullRequestTargetTrigger(data, "workflow.md"))
			})
			if strict {
				assert.Equal(t, 1, compiler.GetWarningCount(), "forks: %#v", forks)
				assert.Contains(t, output, "Prefer restricting forks")
				assert.Contains(t, output, "acknowledge-risk does not suppress")
			} else {
				assert.Zero(t, compiler.GetWarningCount())
				assert.Empty(t, output)
			}
			compiler.ResetWarningCount()
			data.CheckoutExplicitlyDisabled = false
			err := compiler.validatePullRequestTargetTrigger(data, "workflow.md")
			if strict {
				require.ErrorContains(t, err, "checkout enabled is extremely insecure")
			} else {
				require.NoError(t, err)
			}
			data.CheckoutExplicitlyDisabled = true
			data.Roles = []string{"all"}
			err = compiler.validatePullRequestTargetTrigger(data, "workflow.md")
			if strict {
				require.ErrorContains(t, err, "on.roles containing 'all' or 'read'")
			} else {
				require.NoError(t, err)
			}
		}
	}
}
