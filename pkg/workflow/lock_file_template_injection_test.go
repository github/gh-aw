//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findGitHubEventRunExpressions parses a compiled workflow and returns github.event
// expressions that reach an executable run: block. Expressions assigned to env: are
// deliberately excluded because the shell receives their values as data.
func findGitHubEventRunExpressions(lockContent []byte) ([]string, error) {
	var workflow map[string]any
	if err := yaml.Unmarshal(lockContent, &workflow); err != nil {
		return nil, err
	}

	var violations []string
	for _, runContent := range extractRunBlocks(workflow) {
		executableContent := executableShellContent(runContent)
		for _, expression := range InlineExpressionPattern.FindAllString(executableContent, -1) {
			if strings.Contains(expression, "github.event.") {
				violations = append(violations, expression)
			}
		}
	}
	return violations, nil
}

// TestCompiledLockFiles_NoGitHubEventExpressionsInRunScripts is a deterministic
// template-injection guard. It parses each compiled workflow and inspects only run:
// values, avoiding false positives from github.event expressions in env: assignments.
func TestCompiledLockFiles_NoGitHubEventExpressionsInRunScripts(t *testing.T) {
	lockFiles, err := filepath.Glob(filepath.Join(workflowsDir, "*.lock.yml"))
	require.NoError(t, err, "should glob .lock.yml workflow files")
	require.NotEmpty(t, lockFiles, "should find at least one compiled .lock.yml file")

	for _, lockFile := range lockFiles {
		lockContent, err := os.ReadFile(lockFile)
		require.NoError(t, err, "should read %s", lockFile)

		violations, err := findGitHubEventRunExpressions(lockContent)
		require.NoError(t, err, "should parse %s as YAML", lockFile)
		assert.Empty(t, violations, "%s directly interpolates github.event data in a run: script", filepath.Base(lockFile))
	}
}

func TestFindGitHubEventRunExpressions(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected []string
	}{
		{
			name: "expressions outside run are ignored",
			yaml: `
jobs:
  test:
    if: github.event.issue.state == 'open'
    env:
      TITLE: ${{ github.event.issue.title }}
    steps:
      - env:
          BODY: ${{ github.event.issue.body }}
        run: echo "$BODY"
`,
		},
		{
			name: "direct run interpolation is reported",
			yaml: `
jobs:
  test:
    steps:
      - run: echo "${{ github.event.issue.title }}"
`,
			expected: []string{"${{ github.event.issue.title }}"},
		},
		{
			name: "comments and heredocs are ignored",
			yaml: `
jobs:
  test:
    steps:
      - run: |
          # ${{ github.event.issue.title }}
          cat << 'EOF' > config.txt
          title=${{ github.event.issue.title }}
          EOF
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violations, err := findGitHubEventRunExpressions([]byte(tt.yaml))
			require.NoError(t, err)
			if len(tt.expected) == 0 {
				assert.Empty(t, violations)
				return
			}
			assert.Equal(t, tt.expected, violations)
		})
	}
}
