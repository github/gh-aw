//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActionsParserSchemaIntegration(t *testing.T) {
	for _, test := range []struct {
		name, expression, want string
	}{
		{"valid", "github.ref == 'refs/heads/main'", ""},
		{"invalid comparison", "github.ref = 'main'", "unexpected symbol"},
		{"invalid function", "nonexistent(github.ref)", "unrecognized function"},
		{"invalid function arguments", "contains(github.ref)", "too few parameters"},
		{"unavailable context", "secrets.TOKEN", "unrecognized named-value"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "on: push\njobs:\n  test:\n    if: " + test.expression +
				"\n    runs-on: ubuntu-latest\n    steps: [{run: echo hello}]\n"
			err := NewCompiler().validateGitHubActionsSchema(source)
			if test.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.want)
			}
		})
	}
}

func TestActionsParserCompilationIntegration(t *testing.T) {
	for _, skip := range []bool{false, true} {
		t.Run(map[bool]string{false: "validation enabled", true: "validation skipped"}[skip], func(t *testing.T) {
			compiler := NewCompiler(WithNoEmit(true), WithSkipValidation(skip))
			source := "---\non: workflow_dispatch\nstrict: false\nengine: copilot\njobs:\n  custom:\n    runs-on: ubuntu-latest\n    if: github.ref = 'main'\n    steps: [{run: echo hello}]\n---\nTest workflow.\n"
			path := t.TempDir() + "/test.md"
			data, err := compiler.ParseWorkflowString(source, path)
			require.NoError(t, err)
			_, err = compiler.CompileToYAML(data, path)
			if skip {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "unexpected symbol")
				require.ErrorContains(t, err, "test.lock.yml (Line:")
			}
		})
	}
}

func TestActionsParserConcurrencyIntegration(t *testing.T) {
	for _, expression := range []string{
		"${{ format('}}-{0}', github.ref) }}",
		"${{ contains('(', '(') }}",
		"${{ 'a''}}b' }}",
	} {
		require.NoError(t, validateConcurrencyGroupExpression(expression))
	}
	for _, expression := range []string{
		"${{ github.ref = 'main' }}",
		"${{ contains(github.ref) }}",
		"${{ github.ref + 'main' }}",
	} {
		require.ErrorContains(t, validateConcurrencyGroupExpression(expression), "invalid expression syntax")
	}
}
