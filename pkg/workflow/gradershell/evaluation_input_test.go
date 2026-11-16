package gradershell

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/syntax"
)

func TestHasShellCodeEvaluationInputRedirections(t *testing.T) {
	for _, test := range []struct {
		source string
		want   bool
	}{
		{`< input`, false},
		{`value=1 < input`, false},
		{`printf data | < input`, false},
		{"<<'EOF'\ndata\nEOF", false},
		{`bash < input`, true},
		{`printf data | bash`, true},
	} {
		t.Run(test.source, func(t *testing.T) {
			tree, err := syntax.NewParser().Parse(strings.NewReader(test.source), "")
			require.NoError(t, err)
			assert.Equal(t, test.want, HasShellCodeEvaluation(tree, nil))
		})
	}
}
