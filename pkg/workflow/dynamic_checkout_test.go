package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrontmatterConfigDynamicCheckout(t *testing.T) {
	config, err := ParseFrontmatterConfig(map[string]any{
		"name":   "dynamic-checkout",
		"engine": "copilot",
		"checkout": map[string]any{
			"repos":         "${{ fromJSON(inputs.checkouts) }}",
			"allowed-repos": []any{"owner/repo"},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, []DynamicCheckoutConfig{{Expression: "${{ fromJSON(inputs.checkouts) }}", AllowedRepos: []string{"owner/repo"}}}, config.DynamicCheckouts)
	assert.Empty(t, config.CheckoutConfigs)
	assert.False(t, config.CheckoutDisabled)
}

func TestParseFrontmatterConfigDynamicCheckoutTrimsExpression(t *testing.T) {
	config, err := ParseFrontmatterConfig(map[string]any{
		"name":   "dynamic-checkout",
		"engine": "copilot",
		"checkout": map[string]any{
			"repos":         "  ${{ fromJSON(inputs.checkouts) }}  ",
			"allowed-repos": "${{ fromJSON(inputs.allowed_repos) }}",
		},
	})

	require.NoError(t, err)
	assert.Equal(t, []DynamicCheckoutConfig{{Expression: "${{ fromJSON(inputs.checkouts) }}", AllowedRepos: []string{"${{ fromJSON(inputs.allowed_repos) }}"}}}, config.DynamicCheckouts)
}

func TestParseFrontmatterConfigDynamicCheckoutRequiresAllowedRepos(t *testing.T) {
	_, err := ParseFrontmatterConfig(map[string]any{
		"name":   "dynamic-checkout",
		"engine": "copilot",
		"checkout": map[string]any{
			"repos": "${{ fromJSON(inputs.checkouts) }}",
		},
	})

	require.ErrorContains(t, err, "requires allowed-repos")
}

func TestParseFrontmatterConfigDynamicCheckoutRejectsLegacyDynamicField(t *testing.T) {
	_, err := ParseFrontmatterConfig(map[string]any{
		"name":   "dynamic-checkout",
		"engine": "copilot",
		"checkout": map[string]any{
			"dynamic":       "${{ fromJSON(inputs.checkouts) }}",
			"allowed-repos": []any{"owner/repo"},
		},
	})

	require.ErrorContains(t, err, "checkout.repos")
}

func TestParseFrontmatterConfigDynamicCheckoutRequiresReposExpression(t *testing.T) {
	_, err := ParseFrontmatterConfig(map[string]any{
		"name":   "dynamic-checkout",
		"engine": "copilot",
		"checkout": map[string]any{
			"repos":         "owner/repo",
			"allowed-repos": []any{"owner/repo"},
		},
	})

	require.ErrorContains(t, err, "repos must be a GitHub Actions expression")
}

func TestParseFrontmatterConfigDynamicCheckoutRequiresReposString(t *testing.T) {
	_, err := ParseFrontmatterConfig(map[string]any{
		"name":   "dynamic-checkout",
		"engine": "copilot",
		"checkout": map[string]any{
			"repos":         []any{"owner/repo"},
			"allowed-repos": []any{"owner/repo"},
		},
	})

	require.ErrorContains(t, err, "repos must be a string containing a GitHub Actions expression")
}

func TestParseFrontmatterConfigDynamicCheckoutRejectsUnsupportedField(t *testing.T) {
	_, err := ParseFrontmatterConfig(map[string]any{
		"name":   "dynamic-checkout",
		"engine": "copilot",
		"checkout": map[string]any{
			"repos":         "${{ fromJSON(inputs.checkouts) }}",
			"allowed-repos": []any{"owner/repo"},
			"fetch-depth":   1,
			"path":          "repo",
		},
	})

	require.ErrorContains(t, err, `field(s) "fetch-depth, path" are not supported`)
}

func TestGenerateDynamicCheckoutSteps(t *testing.T) {
	compiler := NewCompiler()
	steps := compiler.generateDynamicCheckoutSteps(
		[]DynamicCheckoutConfig{{Expression: "${{ fromJSON(inputs.checkouts) }}", AllowedRepos: []string{"owner/repo"}}},
		"${{ secrets.PUSH_TOKEN }}",
		true,
	)

	require.Len(t, steps, 1)
	assert.Contains(t, steps[0], "name: Checkout dynamic repositories (1)")
	assert.Contains(t, steps[0], "GH_AW_DYNAMIC_CHECKOUTS: ${{ toJSON(fromJSON(inputs.checkouts)) }}")
	assert.Contains(t, steps[0], "GH_AW_DYNAMIC_CHECKOUT_ALLOWED_REPOS: \"[\\\"owner/repo\\\"]\"")
	assert.Contains(t, steps[0], "GH_AW_DYNAMIC_CHECKOUT_TOKEN: ${{ secrets.PUSH_TOKEN }}")
	assert.Contains(t, steps[0], "GH_AW_DYNAMIC_CHECKOUT_PERSIST_CREDENTIALS: true")
	assert.Contains(t, steps[0], "dynamic_checkouts.cjs")
}

func TestGenerateDynamicCheckoutStepsPreservesToJSON(t *testing.T) {
	compiler := NewCompiler()
	steps := compiler.generateDynamicCheckoutSteps([]DynamicCheckoutConfig{{Expression: "${{ toJSON(inputs.checkouts) }}", AllowedRepos: []string{"${{ inputs.allowed_repos }}"}}}, "", false)

	require.Len(t, steps, 1)
	assert.Contains(t, steps[0], "GH_AW_DYNAMIC_CHECKOUTS: ${{ toJSON(inputs.checkouts) }}")
	assert.NotContains(t, steps[0], "toJSON(toJSON(")
}

func TestBuildDynamicCheckoutsPromptContent(t *testing.T) {
	content := buildDynamicCheckoutsPromptContent([]DynamicCheckoutConfig{{Expression: "${{ inputs.checkouts }}"}})

	assert.Contains(t, content, "selected and checked out at runtime")
	assert.Contains(t, content, "checkout-manifest.json")
	assert.Empty(t, buildDynamicCheckoutsPromptContent(nil))
}
