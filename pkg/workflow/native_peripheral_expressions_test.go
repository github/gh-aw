package workflow

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativePeripheralCredentialPolicies(t *testing.T) {
	for _, test := range []struct {
		value                    string
		secrets, skills, plugins bool
	}{
		{value: "${{ secrets['TOKEN'] || secrets.FALLBACK }}", secrets: true, skills: true, plugins: true},
		{value: "${{ needs['prepare']['outputs']['token'] }}", skills: true, plugins: true},
		{value: "${{ steps['mint-token'].outputs['token-value'] }}", plugins: true},
		{value: "${{ env['TOKEN'] }}", plugins: true},
		{value: "${{ 'secrets.TOKEN' }}"},
		{value: "${{ secrets.TOKEN || 'literal' }}"},
		{value: "${{ secrets.TOKEN && secrets.FALLBACK }}"},
		{value: "${{ format('{0}', secrets.TOKEN) }}"},
		{value: "${{ secrets[inputs.key] }}"},
		{value: "${{ secrets['NOT A NAME'] }}"},
		{value: "${{ Secrets.TOKEN }}"},
		{value: "${{ secrets.TOKEN }}-${{ secrets.OTHER }}"},
	} {
		t.Run(test.value, func(t *testing.T) {
			assert.Equal(t, test.secrets, secretsExpressionPattern.MatchString(test.value))
			assert.Equal(t, test.skills, skillsGitHubTokenExpressionRegexp.MatchString(test.value))
			assert.Equal(t, test.plugins, pluginGitHubTokenExpressionRegexp.MatchString(test.value))
		})
	}
}

func TestNativePeripheralReferencesIgnoreLiterals(t *testing.T) {
	source := "needs.literal.outputs.token ${{ format('}} needs.fake.outputs.token secrets.FAKE steps.fake.outputs.token', needs['build'].outputs.token) }}"
	assert.Equal(t, []string{"build"}, extractNeedsJobReferencesFromPrompt(source))
	assert.False(t, containsPostStepSecret(source))
	assert.True(t, containsPostStepSecret("${{ format('}}', secrets['TOKEN']) }}"))
	assert.False(t, otlpResourceAttributeSecretRefPattern.MatchString("${{ 'secrets.TOKEN vars.TOKEN' }}"))
	assert.True(t, otlpResourceAttributeSecretRefPattern.MatchString("${{ format('}}', vars['TOKEN']) }}"))
	require.NoError(t, validateNoCacheKeyRunID("github.run_id-${{ 'github.run_id' }}"))
	require.Error(t, validateNoCacheKeyRunID("${{ github['run_id'] }}"))
	assert.Equal(t, []string{"runner"}, findUnavailableContextsInExpression("${{ format('}} matrix.os', runner['temp']) }}"))
	assert.True(t, runnerToolCacheInRunPattern.MatchString("${{ format('}}', runner['tool_cache']) }}"))
	assert.False(t, runnerToolCacheInRunPattern.MatchString("${{ 'runner.tool_cache' }}"))
	assert.Equal(t, []string{"build"}, extractNeedsJobReferencesFromPrompt("{{#if needs['build'].outputs.token}}yes{{/if}}"))
}

func TestNativePeripheralPromptStepReferences(t *testing.T) {
	data := &WorkflowData{
		CustomSteps:     "steps:\n  - id: build\n    run: echo ok\n",
		MarkdownContent: "${{ format('}} steps.fake.outputs.value', steps['build'].outputs['value']) }}",
	}
	require.ErrorContains(t, validateStepsOutputsNotInPrompt(data), "build")
	data.MarkdownContent = "${{ 'steps.build.outputs.value' }}"
	require.NoError(t, validateStepsOutputsNotInPrompt(data))
}

func TestNativePeripheralStepTokenConsumption(t *testing.T) {
	config := &SafeOutputsConfig{GitHubToken: "${{ format('}} steps.fake.outputs.token', steps['mint-token'].outputs.token) }}"}
	ids := collectSafeOutputStepTokenIDs(config)
	assert.Contains(t, ids, "mint-token")
	assert.NotContains(t, ids, "fake")
	value := "    token: ${{ format('}}', steps['mint-token']['outputs']['token']) }}\n"
	assert.Equal(t, 0, jobStepOutputConsumptionIndex(value, "mint-token"))
	assert.Equal(t, -1, jobStepOutputConsumptionIndex("    token: ${{ 'steps.mint-token.outputs.token' }}\n", "mint-token"))
	assert.Equal(t, 0, jobStepOutputConsumptionIndex("    token: ${{ format(\n'}}', steps['mint-token'].outputs.token) }}\n", "mint-token"))
}

func TestNativePeripheralExpressionRewrites(t *testing.T) {
	source := "${{ format('}} experiments.model', experiments['model']) }}"
	want := "${{ format('}} experiments.model', needs.activation.outputs.model) }}"
	assert.Equal(t, want, RewriteExperimentsReferenceForDownstreamJobs(source, map[string][]string{"model": {"model-v1"}}))
	literal := "${{ format('}} toJSON(secrets)', toJSON(secrets['TOKEN'])) }}"
	assert.Empty(t, findSecretsSerializationExpressions(literal))
	source = "${{ format('}}', toJSON(secrets)) }}"
	assert.Equal(t, []string{source}, findSecretsSerializationExpressions(source))
	assert.Equal(t, "${{ format('}}', false) }}", neutralizeSecretsSerializationExpressions(source))
	assert.Equal(t, "${{ false }}", neutralizeSecretsSerializationExpressions("${{ toJSON((secrets)) }}"))
	assert.Equal(t, []string{"${{ secrets['OTHER'] }}"}, filterBuiltinTokens([]string{
		"${{ secrets['GITHUB_TOKEN'] }}", "${{ secrets['OTHER'] }}",
	}))
}

func TestNativePeripheralConditionalQuotedBraces(t *testing.T) {
	source := "{{#if ${{ contains(inputs.text, '}}') }} }}\nhello\n{{/if}}"
	assert.Equal(t, source, wrapExpressionsInTemplateConditionals(source))
	conditions := (conditionalPattern{TemplateIfPattern}).FindAllStringSubmatch(source, -1)
	require.Len(t, conditions, 1)
	assert.Equal(t, "${{ contains(inputs.text, '}}') }} ", conditions[0][1])
	warnings := detectMidlineTemplateSeparators("prefix " + strings.Split(source, "\n")[0])
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], strings.Split(source, "\n")[0])
}

func TestNativePeripheralWholeExpressionBoundaries(t *testing.T) {
	assert.True(t, isGitHubExpression("${{ format('}}', inputs.value) }}"))
	assert.False(t, isGitHubExpression("${{ inputs.one }}-${{ inputs.two }}"))
	assert.False(t, isGitHubExpression("${{ inputs.one }}literal"))
	assert.False(t, isGitHubExpression("${{ }}"))
	job, ok := extractNeedsJobFromOutputExpression("${{ needs['build'].outputs['value'] }}")
	assert.True(t, ok)
	assert.Equal(t, "build", job)
	_, ok = extractNeedsJobFromOutputExpression("${{ needs.build.result.value }}")
	assert.False(t, ok)
	assert.Equal(t, "${{ toJSON (needs['build'].outputs.value) }}", wrapExpressionWithToJSON("${{ toJSON (needs['build'].outputs.value) }}"))
}

func TestNativePeripheralShellReferencePolicy(t *testing.T) {
	assert.Equal(t, `node "${RUNNER_TEMP}/gh-aw/actions/script.cjs"`, rewriteShellActionCommands("node ${{ runner['temp'] }}/gh-aw/actions/script.cjs"))
	assert.True(t, postStepIssueAPI("repos/${{ github['repository'] }}/issues"))
	assert.True(t, postStepArtifactPath("${{ runner['temp'] }}/gh-aw/**"))
	assert.False(t, postStepArtifactPath("${{ 'runner.temp' }}/gh-aw/**"))
	assert.True(t, slices.Equal(extractNeedsJobReferencesFromPrompt("${{ needs['a'].outputs.x || needs.b.outputs.y }}"), []string{"a", "b"}))
}
