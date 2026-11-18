//go:build !integration

package workflow

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeModelPolicyOverlays_UnionizesAllowedAndBlocked(t *testing.T) {
	imported := []map[string][]string{
		{
			"allowed": {"gpt-5", "claude-sonnet"},
			"blocked": {"gpt-5-pro"},
		},
		{
			"allowed": {"gpt-5-mini"},
			"blocked": {"claude-opus"},
		},
	}
	main := map[string][]string{
		"allowed": {"gpt-5"},
		"blocked": {"gemini-pro"},
	}

	allowed, disallowed := mergeModelPolicyOverlays(imported, main)
	assert.Equal(t, []string{"claude-sonnet", "gpt-5", "gpt-5-mini"}, allowed)
	assert.Equal(t, []string{"claude-opus", "gemini-pro", "gpt-5-pro"}, disallowed)
}

func TestMergeModelPolicyOverlays_BlockedWinsOnConflict(t *testing.T) {
	imported := []map[string][]string{
		{
			"allowed": {"gpt-5"},
			"blocked": {"gpt-5"},
		},
	}
	allowed, disallowed := mergeModelPolicyOverlays(imported, nil)
	assert.Empty(t, allowed)
	assert.Equal(t, []string{"gpt-5"}, disallowed)
}

func TestMergeModelPolicyOverlays_BlockedWildcardWinsOnConflict(t *testing.T) {
	imported := []map[string][]string{
		{
			"allowed": {"claude-opus", "claude-sonnet"},
			"blocked": {"*opus*"},
		},
	}
	allowed, disallowed := mergeModelPolicyOverlays(imported, nil)
	assert.Equal(t, []string{"claude-sonnet"}, allowed)
	assert.Equal(t, []string{"*opus*"}, disallowed)
}

func TestMergeModelPolicyOverlays_AllowedWildcardConflictsWithBlockedExact(t *testing.T) {
	imported := []map[string][]string{
		{
			"allowed": {"*opus*"},
			"blocked": {"claude-opus"},
		},
	}
	allowed, disallowed := mergeModelPolicyOverlays(imported, nil)
	assert.Empty(t, allowed)
	assert.Equal(t, []string{"claude-opus"}, disallowed)
}

func TestExtractMainModelPolicyOverlay_UsesParsedFrontmatterWhenPresent(t *testing.T) {
	toolsResult := &toolsProcessingResult{
		parsedFrontmatter: &FrontmatterConfig{
			ModelPolicyAllowed: []string{"gpt-5"},
			ModelPolicyBlocked: []string{"gpt-5-pro"},
		},
	}

	policy := extractMainModelPolicyOverlay(toolsResult, map[string]any{})
	require.NotNil(t, policy)
	assert.Equal(t, []string{"gpt-5"}, policy["allowed"])
	assert.Equal(t, []string{"gpt-5-pro"}, policy["blocked"])
}

func TestExtractMainModelPolicyOverlay_FallsBackToRawFrontmatter(t *testing.T) {
	frontmatter := map[string]any{
		"models": map[string]any{
			"allowed": []any{"gpt-5-mini"},
			"blocked": []any{"claude-opus"},
		},
	}

	for _, tt := range []struct {
		name              string
		parsedFrontmatter *FrontmatterConfig
	}{
		{name: "nil parsed frontmatter"},
		{name: "empty parsed frontmatter", parsedFrontmatter: &FrontmatterConfig{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			toolsResult := &toolsProcessingResult{parsedFrontmatter: tt.parsedFrontmatter}
			policy := extractMainModelPolicyOverlay(toolsResult, frontmatter)
			require.NotNil(t, policy)
			assert.Equal(t, []string{"gpt-5-mini"}, policy["allowed"])
			assert.Equal(t, []string{"claude-opus"}, policy["blocked"])
		})
	}
}

func TestExtractMainModelCostsOverlay_FallsBackToRawFrontmatterWhenParsedCostsAreUnavailable(t *testing.T) {
	rawProviders := map[string]any{
		"openai": map[string]any{
			"models": map[string]any{
				"gpt-5": map[string]any{
					"cost": map[string]any{"input": "1e-6"},
				},
			},
		},
	}
	frontmatter := map[string]any{
		"models": map[string]any{
			"providers": rawProviders,
		},
	}

	tests := []struct {
		name              string
		parsedFrontmatter *FrontmatterConfig
	}{
		{
			name: "parsed costs without providers",
			parsedFrontmatter: &FrontmatterConfig{
				ModelCosts: map[string]any{"allowed": []any{"gpt-5"}},
			},
		},
		{
			name:              "empty parsed costs",
			parsedFrontmatter: &FrontmatterConfig{},
		},
		{
			name: "nil parsed frontmatter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolsResult := &toolsProcessingResult{parsedFrontmatter: tt.parsedFrontmatter}

			costs := extractMainModelCostsOverlay(toolsResult, frontmatter)
			require.Equal(t, map[string]any{"providers": rawProviders}, costs)
		})
	}
}

func TestExtractMainModelCostsOverlay_ExtractsNilWhenModelCostsHasOnlyPolicyKeys(t *testing.T) {
	toolsResult := &toolsProcessingResult{
		parsedFrontmatter: &FrontmatterConfig{
			ModelCosts: map[string]any{
				"allowed": []any{"gpt-5"},
			},
		},
	}

	costs := extractMainModelCostsOverlay(toolsResult, map[string]any{})
	assert.Nil(t, costs)
}

func TestExtractMainModelCostsOverlay_ExtractsOnlyProvidersAndExcludesPolicyKeys(t *testing.T) {
	toolsResult := &toolsProcessingResult{}
	frontmatter := map[string]any{
		"models": map[string]any{
			"allowed": []any{"gpt-5"},
			"providers": map[string]any{
				"openai": map[string]any{
					"models": map[string]any{
						"gpt-5": map[string]any{
							"cost": map[string]any{"input": "1e-6"},
						},
					},
				},
			},
		},
	}

	costs := extractMainModelCostsOverlay(toolsResult, frontmatter)
	require.NotNil(t, costs)
	assert.Contains(t, costs, "providers")
	assert.NotContains(t, costs, "allowed")
}

func TestMergeExcludedEnvVarNames_UnionizesAndSorts(t *testing.T) {
	got := mergeExcludedEnvVarNames(
		[]string{"TOKEN_B", "TOKEN_A"},
		[]string{"TOKEN_C", "TOKEN_A"},
	)
	assert.Equal(t, []string{"TOKEN_A", "TOKEN_B", "TOKEN_C"}, got)
}

func TestMergeExcludedEnvVarNames_ImportsOnly(t *testing.T) {
	got := mergeExcludedEnvVarNames([]string{"IMPORT_TOKEN"}, nil)
	assert.Equal(t, []string{"IMPORT_TOKEN"}, got)
}

func TestMergeExcludedEnvVarNames_MainOnly(t *testing.T) {
	got := mergeExcludedEnvVarNames(nil, []string{"MAIN_TOKEN"})
	assert.Equal(t, []string{"MAIN_TOKEN"}, got)
}

func TestMergeExcludedEnvVarNames_BothEmpty(t *testing.T) {
	got := mergeExcludedEnvVarNames(nil, nil)
	assert.Nil(t, got)
}

func TestMergeExcludedEnvVarNames_DeduplicatesAcrossSources(t *testing.T) {
	got := mergeExcludedEnvVarNames(
		[]string{"SHARED", "IMPORT_ONLY"},
		[]string{"SHARED", "MAIN_ONLY"},
	)
	assert.Equal(t, []string{"IMPORT_ONLY", "MAIN_ONLY", "SHARED"}, got)
}

func TestMergeExcludedEnvVarNames_LargeInputs(t *testing.T) {
	// Exercises the code path that triggered CWE-190 alerts #648/#649.
	n := 100_000
	fromImports := make([]string, n)
	fromMain := make([]string, n)
	for i := range fromImports {
		fromImports[i] = fmt.Sprintf("IMPORT_%d", i)
		fromMain[i] = fmt.Sprintf("MAIN_%d", i)
	}
	got := mergeExcludedEnvVarNames(fromImports, fromMain)
	assert.Len(t, got, 2*n)
	assert.True(t, sort.StringsAreSorted(got))
	// Confirm both sources are represented in the result.
	assert.Contains(t, got, "IMPORT_0")
	assert.Contains(t, got, fmt.Sprintf("IMPORT_%d", n-1))
	assert.Contains(t, got, "MAIN_0")
	assert.Contains(t, got, fmt.Sprintf("MAIN_%d", n-1))
}
