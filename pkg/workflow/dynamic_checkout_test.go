package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrontmatterConfigDynamicCheckout(t *testing.T) {
	expression := "${{ fromJSON(inputs.checkouts) }}"
	config, err := ParseFrontmatterConfig(map[string]any{
		"name":     "dynamic-checkout",
		"engine":   "copilot",
		"checkout": expression,
	})

	require.NoError(t, err)
	assert.Equal(t, []string{expression}, config.CheckoutExpressions)
	assert.Empty(t, config.CheckoutConfigs)
	assert.False(t, config.CheckoutDisabled)
}

func TestGenerateDynamicCheckoutSteps(t *testing.T) {
	compiler := NewCompiler()
	steps := compiler.generateDynamicCheckoutSteps(
		[]string{"${{ fromJSON(inputs.checkouts) }}"},
		"${{ secrets.PUSH_TOKEN }}",
		true,
	)

	require.Len(t, steps, 1)
	assert.Contains(t, steps[0], "name: Checkout dynamic repositories (1)")
	assert.Contains(t, steps[0], "GH_AW_DYNAMIC_CHECKOUTS: ${{ toJSON(fromJSON(inputs.checkouts)) }}")
	assert.Contains(t, steps[0], "GH_AW_DYNAMIC_CHECKOUT_TOKEN: ${{ secrets.PUSH_TOKEN }}")
	assert.Contains(t, steps[0], "GH_AW_DYNAMIC_CHECKOUT_PERSIST_CREDENTIALS: true")
	assert.Contains(t, steps[0], "dynamic_checkouts.cjs")
}

func TestGenerateDynamicCheckoutStepsPreservesToJSON(t *testing.T) {
	compiler := NewCompiler()
	steps := compiler.generateDynamicCheckoutSteps([]string{"${{ toJSON(inputs.checkouts) }}"}, "", false)

	require.Len(t, steps, 1)
	assert.Contains(t, steps[0], "GH_AW_DYNAMIC_CHECKOUTS: ${{ toJSON(inputs.checkouts) }}")
	assert.NotContains(t, steps[0], "toJSON(toJSON(")
}

func TestBuildDynamicCheckoutsPromptContent(t *testing.T) {
	content := buildDynamicCheckoutsPromptContent([]string{"${{ inputs.checkouts }}"})

	assert.Contains(t, content, "selected and checked out at runtime")
	assert.Contains(t, content, "checkout-manifest.json")
	assert.Empty(t, buildDynamicCheckoutsPromptContent(nil))
}
