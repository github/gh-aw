package actionslanguageservice

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompilerCacheModeSchema(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"workflow", "job", "reusable-job"} {
		for _, mode := range []string{"read", "write", "write-only", "none", "read-only", "invalid", "true", "[]", "{}", "''", "'${{ inputs.mode }}'"} {
			t.Run(scope+"/"+mode, func(t *testing.T) {
				t.Parallel()
				var source string
				switch scope {
				case "workflow":
					source = fmt.Sprintf("cache-mode: %s\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo hi}]\n", mode)
				case "job":
					source = fmt.Sprintf("on: push\njobs:\n  build:\n    cache-mode: %s\n    runs-on: ubuntu-latest\n    steps: [{run: echo hi}]\n", mode)
				case "reusable-job":
					source = fmt.Sprintf("on: push\njobs:\n  build:\n    cache-mode: %s\n    uses: ./.github/workflows/build.yml\n", mode)
				}
				err := ValidateWorkflowWithCacheMode("cache.yml", source)
				switch mode {
				case "read", "write", "write-only", "none":
					require.NoError(t, err)
				default:
					require.Error(t, err)
				}
				// The compiler overlay must not mutate the cached upstream schema.
				_, err = ParseWorkflow("upstream.yml", source)
				require.ErrorContains(t, err, `unexpected value "cache-mode"`)
			})
		}
	}
}

func TestCompilerCacheModePreservesValidation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, source, want string
	}{
		{
			"misplaced step key",
			"on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo hi, cache-mode: read}]\n",
			`unexpected value "cache-mode"`,
		},
		{
			"unknown root key",
			"cache-mode: read\ncache-modes: read\non: push\njobs: {}\n",
			`unexpected value "cache-modes"`,
		},
		{
			"implicit condition context",
			"cache-mode: read\non: push\njobs:\n  build:\n    if: secrets.TOKEN\n    runs-on: ubuntu-latest\n    steps: [{run: echo hi}]\n",
			"unrecognized named-value",
		},
		{
			"explicit expression syntax",
			"cache-mode: read\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    env: {VALUE: '${{ github.ref + 1 }}'}\n    steps: [{run: echo hi}]\n",
			"unexpected symbol",
		},
		{
			"duplicate key",
			"cache-mode: read\ncache-mode: none\non: push\njobs: {}\n",
			"cache-mode",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateWorkflowWithCacheMode("invalid.yml", test.source)
			require.ErrorContains(t, err, test.want)
			var diagnostic *Diagnostic
			require.ErrorAs(t, err, &diagnostic)
			require.Equal(t, "invalid.yml", diagnostic.File)
			require.Positive(t, diagnostic.Line)
			require.Positive(t, diagnostic.Column)
		})
	}
}
