//go:build !integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestTemporaryIDSmokeAllowsParentSummaryComment(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/smoke-temporary-id.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(source), "---", 3)
	require.Len(t, parts, 3)
	var frontmatter struct {
		SafeOutputs struct {
			AddComment struct {
				Target string `yaml:"target"`
				Max    int    `yaml:"max"`
			} `yaml:"add-comment"`
		} `yaml:"safe-outputs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &frontmatter))
	require.Equal(t, "*", frontmatter.SafeOutputs.AddComment.Target,
		"scheduled and dispatched runs must allow the explicit temporary-ID parent target")
	require.Equal(t, 2, frontmatter.SafeOutputs.AddComment.Max)
	require.Contains(t, parts[2], `"item_number": "aw_test01"`)
}
