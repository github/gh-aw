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
		{"omitted", "", false},
		{"wildcard string", "'*'", false},
		{"wildcard list", "['*']", false},
		{"mixed wildcard", "[trusted/repo, '*']", false},
		{"wildcard first", "['*', trusted/repo]", false},
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
					require.ErrorContains(t, err, "requires explicit bounded fork admission")
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
    forks: ["*"]
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
	require.ErrorContains(t, compiler.CompileWorkflow(file), "requires explicit bounded fork admission")
}

func TestPullRequestTargetBoundedForkAdmissionShorthand(t *testing.T) {
	for _, tt := range []struct {
		on        string
		wantError string
	}{
		{"pull_request_target", "requires explicit bounded fork admission"},
		{"[pull_request_target, workflow_dispatch]", "expected string or object"},
		{"\n  pull_request_target:", "expected string or object"},
		{"\n  pull_request_target: null", "expected string or object"},
		{"\n  pull_request_target: {}", "requires explicit bounded fork admission"},
	} {
		t.Run(tt.on, func(t *testing.T) {
			source := "---\non: " + tt.on + "\ncheckout: false\ntools:\n  github: false\n---\nReview the contribution.\n"
			file := filepath.Join(testutil.TempDir(t, "bounded-target-shorthand"), "workflow.md")
			require.NoError(t, os.WriteFile(file, []byte(source), 0600))
			require.ErrorContains(t, NewCompiler().CompileWorkflow(file), tt.wantError)
			assert.NoFileExists(t, filepath.Join(filepath.Dir(file), "workflow.lock.yml"))
		})
	}
}

func TestHasBoundedPullRequestTargetForksRejectsMalformedValues(t *testing.T) {
	for _, forks := range []any{nil, true, 1, []any{nil}, []any{true}, []any{"trusted/repo", "*"}, map[string]any{"author-association": []any{"OWNER"}}} {
		assert.False(t, hasBoundedPullRequestTargetForks(map[string]any{
			"on": map[string]any{
				"pull_request_target": map[string]any{"forks": forks},
			},
		}), "forks: %#v", forks)
	}
}
