//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeExpressionExtraction(t *testing.T) {
	source := "first ${{ format('}}{0}', inputs.branch) }}\nsecond ${{ inputs['branch'] }}"
	mappings, err := NewExpressionExtractor().ExtractExpressions(source)
	require.NoError(t, err)
	var originals []string
	for _, mapping := range mappings {
		originals = append(originals, mapping.Original)
	}
	require.Contains(t, originals, "${{ format('}}{0}', inputs.branch) }}")
	require.Contains(t, originals, "${{ inputs['branch'] }}")
	require.NotContains(t, originals, "${{ inputs.branch }}", "function internals do not create extra runtime mappings")
	for _, invalid := range []string{
		"${{ inputs.branch", "${{ inputs.branch || }}", "${{ }}",
		"${{ inputs.branch || \"main\" }}", "${{ format('unterminated) }}",
	} {
		_, err := NewExpressionExtractor().ExtractExpressions(invalid)
		require.Error(t, err, invalid)
	}
}

func TestNativeReferenceTransformers(t *testing.T) {
	require.Equal(t,
		"format('needs.activation.outputs.text }}', steps.sanitized.outputs.text)",
		transformActivationOutputs("format('needs.activation.outputs.text }}', needs['activation'].outputs.text)"))
	require.Equal(t,
		"format('github.aw.context.items', fromJSON(steps.generate_aw_info.outputs.aw_context || '{}').items)",
		transformAwContextExpression("format('github.aw.context.items', github.aw.context.items)"))
	require.Equal(t,
		"format('gateway.events', fromJSON(steps.generate_aw_info.outputs.aw_context || '{}').webhook.deliveries[0].payload)",
		transformGatewayEventsExpression("format('gateway.events', gateway['events'][0].payload)"))
	require.Equal(t, "steps.pick-experiment.outputs.variant == 'can''t'",
		transformExperimentsExpression(`experiments.variant === "can't"`))
}

func TestNativeExpressionSafetyReferences(t *testing.T) {
	for _, allowed := range []string{
		"${{ inputs['branch'] || '}} secrets.TOKEN &&' }}",
		"${{ github['event']['issue']['number'] }}",
		"${{ inputs.branch == 'constructor' }}",
	} {
		require.NoError(t, validateExpressionSafety(allowed), allowed)
	}
	for _, denied := range []string{
		"${{ github['event']['comment']['body'] }}",
		"${{ inputs['constructor'] }}",
		"${{ inputs.branch == secrets['TOKEN'] }}",
		"${{ inputs[secrets.TOKEN] }}",
	} {
		require.Error(t, validateExpressionSafety(denied), denied)
	}
	tree, err := ParseExpression("!inputs.enabled == true")
	require.NoError(t, err)
	require.Equal(t, "!inputs.enabled == true", tree.Render(), "NOT binds to the left comparison operand, not to the comparison")
}

func TestNativeSecretReferenceExtraction(t *testing.T) {
	require.Empty(t, ExtractSecretsFromValue("${{ 'secrets.TOKEN' }}"))
	expression := "${{ format('}}{0}', (secrets)['TOKEN']) }}"
	require.Equal(t, map[string]string{"TOKEN": expression}, ExtractSecretsFromValue(expression))
	require.False(t, ContainsJobOutputExpr("${{ 'needs.auth.outputs.token' }}"))
	require.True(t, ContainsJobOutputExpr("${{ format('}}{0}', needs['auth']['outputs']['token']) }}"))
	require.True(t, ContainsJobOutputExpr("${{ needs[inputs.job].outputs.token }}"))
	require.NotContains(t,
		replaceEnvExpressionsWithPrefixedEnvVars("${{ env['HOST'] || '}}' }}", "${"),
		"{{")
	unsafeName := "${{ env['A[$(command)]'] }}"
	require.Empty(t, ExtractEnvExpressionsFromValue(unsafeName))
	require.Equal(t, unsafeName, replaceEnvExpressionsWithPrefixedEnvVars(unsafeName, "${"),
		"property names cannot become shell parameter syntax")
}
