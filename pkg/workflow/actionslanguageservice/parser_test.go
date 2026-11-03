package actionslanguageservice

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpressionLimits(t *testing.T) {
	require.NoError(t, ValidateExpression("'"+strings.Repeat("x", MaxExpressionLength-2)+"'"))
	require.ErrorContains(t, ValidateExpression("'"+strings.Repeat("x", MaxExpressionLength-1)+"'"), "length")
	require.NoError(t, ValidateExpression("'"+strings.Repeat("😊", (MaxExpressionLength-2)/2)+"'"))
	require.ErrorContains(t, ValidateExpression("'"+strings.Repeat("😊", MaxExpressionLength/2)+"'"), "length")
	require.NoError(t, ValidateExpression(strings.Repeat("!", MaxExpressionDepth-1)+"true"))
	require.ErrorContains(t, ValidateExpression(strings.Repeat("!", MaxExpressionDepth)+"true"), "depth")
}

func TestExtractExpressions(t *testing.T) {
	source := "prefix-${{ format('}}-{0}', github.ref) }}-${{ 'a''}}b' }}"
	expressions, err := ExtractExpressions(source)
	require.NoError(t, err)
	require.Len(t, expressions, 2)
	assert.Equal(t, " format('}}-{0}', github.ref) ", expressions[0].Content)
	assert.Equal(t, " 'a''}}b' ", expressions[1].Content)
	for _, expression := range expressions {
		assert.Equal(t, "${{"+expression.Content+"}}", source[expression.Start:expression.End])
	}
	_, err = ExtractExpressions("${{ github.ref")
	require.ErrorContains(t, err, "without matching closing")
}

func TestExtractExpressionsSkipsDoubledQuotesInsideStrings(t *testing.T) {
	source := `${{ contains('it''s }} still a literal', secrets.TOKEN) }}-${{ github.ref }}`
	expressions, err := ExtractExpressions(source)
	require.NoError(t, err)
	require.Len(t, expressions, 2)
	assert.Equal(t, " contains('it''s }} still a literal', secrets.TOKEN) ", expressions[0].Content)
	assert.Equal(t, " github.ref ", expressions[1].Content)
	parsed, err := ParseSyntax(expressions[0].Content)
	require.NoError(t, err)
	assert.Contains(t, References(parsed), []string{"secrets", "TOKEN"})
}

func TestExtractExpressionsRecoversAfterUnclosedQuote(t *testing.T) {
	source := "${{ env.BEFORE }} ${{ 'unclosed }}\n${{ env.AFTER }} ${{ env.SUFFIX"
	expressions, err := ExtractExpressions(source)
	require.ErrorContains(t, err, "opening '${{' at position 18")
	require.ErrorContains(t, err, "unclosed single quote")
	require.Len(t, expressions, 2)
	assert.Equal(t, " env.BEFORE ", expressions[0].Content)
	assert.Equal(t, " env.AFTER ", expressions[1].Content)
	for _, expression := range expressions {
		assert.Equal(t, "${{"+expression.Content+"}}", source[expression.Start:expression.End])
	}
}

func TestWorkflowParsingIsolation(t *testing.T) {
	source := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo hi}]\n"
	var group sync.WaitGroup
	for i := range 12 {
		group.Go(func() {
			workflow, err := ParseWorkflow(fmt.Sprintf("test-%d.yml", i), source)
			assert.NoError(t, err)
			if err == nil {
				// Mutating the caller's tree must not mutate the cached schema.
				workflow.Document.Content = nil
			}
		})
	}
	group.Wait()
	_, err := ParseWorkflow("after.yml", source)
	require.NoError(t, err)
}

func TestWorkflowRejectsCircularAliasesAndMultipleDocuments(t *testing.T) {
	_, err := ParseWorkflow("cycle.yml", "on: push\nenv: &env {VALUE: *env}\njobs: {}\n")
	require.Error(t, err)
	_, err = ParseWorkflow("multiple.yml", "on: push\njobs: {}\n---\non: push\njobs: {}\n")
	require.ErrorContains(t, err, "exactly one YAML document")
}

func FuzzExpressionParser(f *testing.F) {
	for _, source := range []string{"github.ref", "contains(github.ref, 'main')", "'a''b'", "!!true", "github[", "case(true, 1, 0)"} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		_, _ = ParseExpression(source, syntaxContexts)
	})
}
