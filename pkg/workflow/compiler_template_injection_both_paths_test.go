//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateTemplateInjection_NoEmitPreservesDiagnostics(t *testing.T) {
	t.Parallel()
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing=%t", existing), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "test.invalid.yml")
			if existing {
				require.NoError(t, os.WriteFile(path, []byte("original"), 0o644))
			}
			compiler := NewCompiler(WithNoEmit(true))
			err := compiler.validateTemplateInjection("jobs:\n  test:\n    steps:\n      - run: echo ${{ github.event.issue.title }}\n",
				filepath.Join(dir, "test.lock.yml"), filepath.Join(dir, "test.md"), nil)
			require.Error(t, err)
			if existing {
				content, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Equal(t, "original", string(content))
			} else {
				require.NoFileExists(t, path)
			}
		})
	}
}

// TestValidateTemplateInjection_BothPaths tests that validateTemplateInjection detects
// unsafe expressions in run: blocks consistently regardless of whether schema validation
// is enabled (parsedWorkflow != nil) or disabled (parsedWorkflow == nil).
func TestValidateTemplateInjection_BothPaths(t *testing.T) {
	// YAML with an unsafe GitHub Actions expression directly inside a run: block.
	// This is a template injection risk because ${{ github.event.issue.title }}
	// can contain attacker-controlled content that gets passed to the shell.
	unsafeYAML := `
name: unsafe
on: issues
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Unsafe step
        run: echo "${{ github.event.issue.title }}"
`

	// YAML with an unsafe expression only in an env: block (safe pattern: the
	// value is assigned to a shell variable, not interpolated directly into shell).
	safeYAML := `
name: safe
on: issues
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Safe step
        env:
          TITLE: ${{ github.event.issue.title }}
        run: echo "$TITLE"
`

	// YAML with a GitHub Actions expression in run: that should have been
	// rewritten into env: by the compiler.
	regressionYAML := `
name: regression
on: workflow_dispatch
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Regression step
        run: echo "${{ github.token }}"
`

	allowedGeneratedExpressionYAML := `
name: generated-safe-expression
on: workflow_dispatch
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Allowed generated expression
        run: echo "${{ job.services['redis'].ports['6379'] }}"
`

	flowStyleRegressionYAML := `
name: flow-style-regression
on: workflow_dispatch
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - { run: "echo ${{ github.actor }}" }
      - run: node ${{ runner.temp }}/actions/foo.cjs
`

	heredocExpressionYAML := `
name: heredoc-expression
on: workflow_dispatch
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Heredoc expression is file content
        run: |
          cat > config.txt << 'EOF'
          title=${{ github.event.issue.title }}
          EOF
`

	commentExpressionYAML := `
name: comment-expression
on: workflow_dispatch
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Comment expression
        run: |
          set -euo pipefail
          # docs: ${{ github.event.issue.title }}
          echo "ok"
`

	tmpDir := testutil.TempDir(t, "template-injection-test")
	markdownPath := filepath.Join(tmpDir, "test.md")
	lockFile := stringutil.MarkdownToLockFile(markdownPath)

	compiler := NewCompiler()

	parseYAML := func(t *testing.T, content string) map[string]any {
		t.Helper()
		var out map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(content), &out), "should parse test YAML")
		return out
	}

	t.Run("Path A - schema enabled - unsafe expression detected", func(t *testing.T) {
		// Path A: parsedWorkflow != nil (pre-parsed for schema validation).
		err := compiler.validateTemplateInjection(unsafeYAML, lockFile, markdownPath, parseYAML(t, unsafeYAML))
		require.Error(t, err, "should detect template injection with pre-parsed YAML")
		require.ErrorContains(t, err, "github.event", "error should mention the unsafe context")
	})

	t.Run("Path A - schema enabled - safe expression passes", func(t *testing.T) {
		err := compiler.validateTemplateInjection(safeYAML, lockFile, markdownPath, parseYAML(t, safeYAML))
		assert.NoError(t, err, "safe expression in env: block should not be flagged")
	})

	t.Run("Path B - schema disabled - unsafe expression detected", func(t *testing.T) {
		// Path B: parsedWorkflow == nil (schema validation skipped).
		err := compiler.validateTemplateInjection(unsafeYAML, lockFile, markdownPath, nil)
		require.Error(t, err, "should detect template injection via text scan fallback")
		require.ErrorContains(t, err, "github.event", "error should mention the unsafe context")
	})

	t.Run("Path B - schema disabled - safe expression passes", func(t *testing.T) {
		err := compiler.validateTemplateInjection(safeYAML, lockFile, markdownPath, nil)
		assert.NoError(t, err, "safe expression in env: block should not be flagged")
	})

	t.Run("Path A - schema enabled - run expression regression detected", func(t *testing.T) {
		err := compiler.validateTemplateInjection(regressionYAML, lockFile, markdownPath, parseYAML(t, regressionYAML))
		require.Error(t, err, "should detect raw GitHub Actions expression in run script")
		require.ErrorContains(t, err, "authored run: content")
		require.ErrorContains(t, err, "unquoted heredoc")
		require.ErrorContains(t, err, "github.token")
	})

	t.Run("Path B - schema disabled - run expression regression detected", func(t *testing.T) {
		err := compiler.validateTemplateInjection(regressionYAML, lockFile, markdownPath, nil)
		require.Error(t, err, "should detect raw GitHub Actions expression in run script")
		require.ErrorContains(t, err, "authored run: content")
		require.ErrorContains(t, err, "unquoted heredoc")
		require.ErrorContains(t, err, "github.token")
	})

	t.Run("Path A - schema enabled - allowed generated run expression passes", func(t *testing.T) {
		err := compiler.validateTemplateInjection(allowedGeneratedExpressionYAML, lockFile, markdownPath, parseYAML(t, allowedGeneratedExpressionYAML))
		assert.NoError(t, err, "compiler-owned job.services expression should be allowed")
	})

	t.Run("Path B - schema disabled - allowed generated run expression passes", func(t *testing.T) {
		err := compiler.validateTemplateInjection(allowedGeneratedExpressionYAML, lockFile, markdownPath, nil)
		assert.NoError(t, err, "compiler-owned job.services expression should be allowed")
	})

	t.Run("Path B - schema disabled - flow-style regression detected alongside allowed expression", func(t *testing.T) {
		err := compiler.validateTemplateInjection(flowStyleRegressionYAML, lockFile, markdownPath, nil)
		require.Error(t, err, "flow-style run expressions should still be detected in the fallback path")
		require.ErrorContains(t, err, "authored run: content")
		require.ErrorContains(t, err, "github.actor")
	})

	t.Run("Path A - schema enabled - heredoc expression rejected", func(t *testing.T) {
		err := compiler.validateTemplateInjection(heredocExpressionYAML, lockFile, markdownPath, parseYAML(t, heredocExpressionYAML))
		require.Error(t, err, "expressions inside heredoc content must be flagged")
		require.ErrorContains(t, err, "CTR-006")
		require.ErrorContains(t, err, "Actions interpolates expressions before Bash")
		require.ErrorContains(t, err, "unquoted heredoc")
		require.ErrorContains(t, err, "github.event")
	})

	t.Run("Path B - schema disabled - heredoc expression rejected", func(t *testing.T) {
		err := compiler.validateTemplateInjection(heredocExpressionYAML, lockFile, markdownPath, nil)
		require.Error(t, err, "expressions inside heredoc content must be flagged")
		require.ErrorContains(t, err, "CTR-006")
		require.ErrorContains(t, err, "Actions interpolates expressions before Bash")
		require.ErrorContains(t, err, "unquoted heredoc")
		require.ErrorContains(t, err, "github.event")
	})

	t.Run("Path A - schema enabled - comment expression rejected", func(t *testing.T) {
		err := compiler.validateTemplateInjection(commentExpressionYAML, lockFile, markdownPath, parseYAML(t, commentExpressionYAML))
		require.Error(t, err, "expressions inside bash comments must be flagged")
		require.ErrorContains(t, err, "CTR-006")
		require.ErrorContains(t, err, "Actions interpolates expressions before Bash")
		require.ErrorContains(t, err, "Remove expressions from comments")
		require.ErrorContains(t, err, "github.event")
	})

	t.Run("Path B - schema disabled - comment expression rejected", func(t *testing.T) {
		err := compiler.validateTemplateInjection(commentExpressionYAML, lockFile, markdownPath, nil)
		require.Error(t, err, "expressions inside bash comments must be flagged")
		require.ErrorContains(t, err, "CTR-006")
		require.ErrorContains(t, err, "Actions interpolates expressions before Bash")
		require.ErrorContains(t, err, "Remove expressions from comments")
		require.ErrorContains(t, err, "github.event")
	})

	t.Run("both paths agree on unsafe YAML", func(t *testing.T) {
		errA := compiler.validateTemplateInjection(unsafeYAML, lockFile, markdownPath, parseYAML(t, unsafeYAML))
		errB := compiler.validateTemplateInjection(unsafeYAML, lockFile, markdownPath, nil)

		require.Error(t, errA, "Path A must report an error")
		require.Error(t, errB, "Path B must report an error")
		require.ErrorContains(t, errA, "github.event", "Path A error should identify the context")
		require.ErrorContains(t, errB, "github.event", "Path B error should identify the context")
	})

	t.Run("both paths agree on safe YAML", func(t *testing.T) {
		errA := compiler.validateTemplateInjection(safeYAML, lockFile, markdownPath, parseYAML(t, safeYAML))
		errB := compiler.validateTemplateInjection(safeYAML, lockFile, markdownPath, nil)

		assert.NoError(t, errA, "Path A should not flag safe YAML")
		assert.NoError(t, errB, "Path B should not flag safe YAML")
	})
}
