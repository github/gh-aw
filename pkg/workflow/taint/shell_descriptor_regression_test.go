package taint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellDescriptorRoutingCoverageLateSources(t *testing.T) {
	rule := Rule{
		ID:       "environment-file-injection",
		IsSource: func(reference string) bool { return reference == "github.event.comment.body" },
		IsSink:   func(site Site) bool { return site.Kind == EnvironmentFile },
	}
	for _, run := range []string{
		`exec 2>> "$GITHUB_ENV"
printf 'VALUE=%s\n' '${{ github.event.comment.body }}' >&2`,
		`{ printf 'VALUE=%s\n' '${{ github.event.comment.body }}' >&2; } 2>> "$GITHUB_ENV"`,
	} {
		t.Run(run, func(t *testing.T) {
			producer := "      - run: |\n          " + strings.ReplaceAll(run, "\n", "\n          ") + "\n"
			result, err := Analyze(outputWorkflow(t, producer, "", ""), rule)
			require.NoError(t, err)
			assert.NotEmpty(t, result.Gaps)
		})
	}
}
