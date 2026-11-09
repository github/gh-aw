package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompilerMetadataHeaderSize(t *testing.T) {
	c := NewCompiler()
	header := `# gh-aw-manifest: {"version":1,"payload":"` + strings.Repeat("x", MaxExpressionSize+1) + "\"}\n"
	require.NoError(t, c.validateExpressionSizes(header+"jobs:\n  test:\n    runs-on: ubuntu-latest\n"))
	require.Error(t, c.validateExpressionSizes(header+"env:\n  VALUE: "+strings.Repeat("x", MaxExpressionSize+1)+"\n"))
}
