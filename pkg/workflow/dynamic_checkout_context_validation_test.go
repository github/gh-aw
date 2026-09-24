package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDynamicCheckoutContexts(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.validateDynamicCheckoutContexts(&WorkflowData{
		DynamicCheckouts: []DynamicCheckoutConfig{{Expression: "${{ fromJSON(steps.select.outputs.checkouts) }}"}},
	})
	require.ErrorContains(t, err, "cannot reference steps.*")

	err = compiler.validateDynamicCheckoutContexts(&WorkflowData{
		DynamicCheckouts: []DynamicCheckoutConfig{{Expression: "${{ fromJSON(inputs.checkouts) }}"}},
	})
	assert.NoError(t, err)
}
