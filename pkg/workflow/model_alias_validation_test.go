//go:build !integration

package workflow

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── T-MAF-001 through T-MAF-009: ParseModelIdentifier (Section 12.1.1) ──────

// TestParseModelIdentifier_T_MAF_001 – bare alias name.
func TestParseModelIdentifier_T_MAF_001(t *testing.T) {
	p, err := ParseModelIdentifier("sonnet")
	require.NoError(t, err, "bare alias name should parse without error")
	assert.Equal(t, "sonnet", p.Base, "base should be 'sonnet'")
	assert.Empty(t, p.Provider, "provider should be empty for bare name")
	assert.Empty(t, p.Params, "params should be empty when no query string")
	assert.False(t, p.IsGlob, "bare name should not be a glob")
}

// TestParseModelIdentifier_T_MAF_002 – bare alias name with effort parameter.
func TestParseModelIdentifier_T_MAF_002(t *testing.T) {
	p, err := ParseModelIdentifier("opus?effort=high")
	require.NoError(t, err, "bare name with effort param should parse")
	assert.Equal(t, "opus", p.Base, "base should be 'opus'")
	assert.Equal(t, "high", p.Params["effort"], "effort param should be 'high'")
}

// TestParseModelIdentifier_T_MAF_003 – provider-scoped exact name.
func TestParseModelIdentifier_T_MAF_003(t *testing.T) {
	p, err := ParseModelIdentifier("copilot/gpt-5")
	require.NoError(t, err, "provider-scoped name should parse")
	assert.Equal(t, "copilot", p.Provider, "provider should be 'copilot'")
	assert.Equal(t, "gpt-5", p.ModelToken, "model token should be 'gpt-5'")
	assert.False(t, p.IsGlob, "exact name should not be a glob")
}

// TestParseModelIdentifier_T_MAF_004 – provider-scoped with multiple parameters.
func TestParseModelIdentifier_T_MAF_004(t *testing.T) {
	p, err := ParseModelIdentifier("openai/o3?effort=low&temperature=0.2")
	require.NoError(t, err, "provider-scoped with multiple params should parse")
	assert.Equal(t, "openai", p.Provider, "provider should be 'openai'")
	assert.Equal(t, "o3", p.ModelToken, "model token should be 'o3'")
	assert.Equal(t, "low", p.Params["effort"], "effort should be 'low'")
	assert.Equal(t, "0.2", p.Params["temperature"], "temperature should be '0.2'")
}

// TestParseModelIdentifier_T_MAF_005 – glob pattern in engine.model must be rejected.
func TestParseModelIdentifier_T_MAF_005(t *testing.T) {
	compiler := NewCompiler()
	// Glob patterns are allowed in alias entries but NOT in engine.model.
	err := compiler.validateModelAliasMap(
		BuiltinModelAliases(),
		nil,
		"copilot/*sonnet*",
		"/fake/path/workflow.md",
	)
	require.Error(t, err, "glob pattern in engine.model should be rejected (V-MAF-004)")
	require.ErrorContains(t, err, "V-MAF-004", "error should reference V-MAF-004")
}

// TestParseModelIdentifier_T_MAF_006 – invalid effort value must be rejected.
func TestParseModelIdentifier_T_MAF_006(t *testing.T) {
	_, err := ParseModelIdentifier("opus?effort=extreme")
	require.NoError(t, err, "syntax parsing should succeed for unknown values")

	// Known-param validation rejects invalid effort values.
	p, _ := ParseModelIdentifier("opus?effort=extreme")
	err = ValidateKnownParams(p.Params)
	require.Error(t, err, "effort=extreme should be rejected (V-MAF-002)")
	require.ErrorContains(t, err, "V-MAF-002", "error should reference V-MAF-002")
}

// TestParseModelIdentifier_T_MAF_007 – temperature out of range must be rejected.
func TestParseModelIdentifier_T_MAF_007(t *testing.T) {
	p, err := ParseModelIdentifier("gpt-5?temperature=3.0")
	require.NoError(t, err, "syntax parsing should succeed")

	err = ValidateKnownParams(p.Params)
	require.Error(t, err, "temperature=3.0 should be rejected (V-MAF-003)")
	require.ErrorContains(t, err, "V-MAF-003", "error should reference V-MAF-003")
}

// TestParseModelIdentifier_T_MAF_008 – whitespace in identifier must be rejected.
func TestParseModelIdentifier_T_MAF_008(t *testing.T) {
	_, err := ParseModelIdentifier("my model")
	require.Error(t, err, "whitespace in model identifier should be rejected (V-MAF-006)")
	require.ErrorContains(t, err, "segment type", "error should name the segment type (V-MAF-006)")
}

// TestParseModelIdentifier_T_MAF_009 – colon in identifier must be rejected; error must name the char.
func TestParseModelIdentifier_T_MAF_009(t *testing.T) {
	_, err := ParseModelIdentifier("my:model")
	require.Error(t, err, "colon in model identifier should be rejected (V-MAF-006)")
	require.ErrorContains(t, err, ":", "error message must identify the offending character (V-MAF-006)")
	require.ErrorContains(t, err, "segment type", "error must name the segment type (V-MAF-006)")
}

// ─── Additional syntax tests ──────────────────────────────────────────────────

func TestParseModelIdentifier_ProviderScopedWithVersion(t *testing.T) {
	p, err := ParseModelIdentifier("copilot/claude-opus-4.5")
	require.NoError(t, err, "provider-scoped name with version should parse")
	assert.Equal(t, "copilot", p.Provider)
	assert.Equal(t, "claude-opus-4.5", p.ModelToken)
}

func TestParseModelIdentifier_NamespacedModel(t *testing.T) {
	for _, model := range []string{"anthropic/claude-sonnet-4", "moonshotai/kimi-k3", "vendor/team/model.v2"} {
		t.Run(model, func(t *testing.T) {
			p, err := ParseModelIdentifier("openrouter/" + model + "?effort=high")
			require.NoError(t, err)
			assert.Equal(t, "openrouter", p.Provider)
			assert.Equal(t, model, p.ModelToken)
			assert.Equal(t, "high", p.Params["effort"])
			assert.False(t, p.IsGlob)
		})
	}
	for _, model := range []string{"/model", "vendor/", "vendor//model", "vendor/../model", "vendor/.model", "vendor/-model", "vendor/model.", "vendor/my model", "vendor/model:tag", "vendor/model%2Fname"} {
		t.Run(model, func(t *testing.T) {
			_, err := ParseModelIdentifier("openrouter/" + model)
			require.Error(t, err)
			require.ErrorContains(t, err, "segment type: model")
		})
	}
}

func TestParseModelIdentifier_GlobPattern(t *testing.T) {
	p, err := ParseModelIdentifier("copilot/*sonnet*")
	require.NoError(t, err, "glob pattern should parse")
	assert.Equal(t, "copilot", p.Provider)
	assert.True(t, p.IsGlob, "identifier with * should be a glob")
}

func TestParseModelIdentifier_BareNameStartDot(t *testing.T) {
	_, err := ParseModelIdentifier(".hidden")
	require.Error(t, err, "bare name starting with '.' must be rejected")
}

func TestParseModelIdentifier_BareNameStartDash(t *testing.T) {
	_, err := ParseModelIdentifier("-model")
	require.Error(t, err, "bare name starting with '-' must be rejected")
}

func TestParseModelIdentifier_ProviderEndsDash(t *testing.T) {
	_, err := ParseModelIdentifier("copilot-/model")
	require.Error(t, err, "provider token ending with '-' must be rejected")
}

func TestParseModelIdentifier_EmptyModelToken(t *testing.T) {
	_, err := ParseModelIdentifier("copilot/")
	require.Error(t, err, "empty model token must be rejected")
}

func TestParseModelIdentifier_EmptyString(t *testing.T) {
	_, err := ParseModelIdentifier("")
	require.Error(t, err, "empty string must be rejected")
}

func TestParseModelIdentifier_ParamMissingValue(t *testing.T) {
	_, err := ParseModelIdentifier("opus?effort=")
	require.Error(t, err, "param with empty value must be rejected")
}

func TestParseModelIdentifier_ParamMissingSeparator(t *testing.T) {
	_, err := ParseModelIdentifier("opus?effortonly")
	require.Error(t, err, "param without '=' separator must be rejected")
}

func TestParseModelIdentifier_ParamKeyStartsDigit(t *testing.T) {
	_, err := ParseModelIdentifier("opus?1key=val")
	require.Error(t, err, "param key starting with digit must be rejected")
}

func TestParseModelIdentifier_AtSign(t *testing.T) {
	_, err := ParseModelIdentifier("my@model")
	require.Error(t, err, "@ sign should be rejected (V-MAF-006)")
}

func TestParseModelIdentifier_ExclamationMark(t *testing.T) {
	_, err := ParseModelIdentifier("my!model")
	require.Error(t, err, "! sign should be rejected")
}

// ─── ValidateEffortParam ──────────────────────────────────────────────────────

func TestValidateEffortParam(t *testing.T) {
	tests := []struct {
		value   string
		wantErr bool
	}{
		{"low", false},
		{"medium", false},
		{"high", false},
		{"extreme", true},
		{"HIGH", true},
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			err := ValidateEffortParam(tt.value)
			if tt.wantErr {
				assert.Error(t, err, "effort=%q should fail validation", tt.value)
			} else {
				assert.NoError(t, err, "effort=%q should pass validation", tt.value)
			}
		})
	}
}

// ─── ValidateTemperatureParam ─────────────────────────────────────────────────

func TestValidateTemperatureParam(t *testing.T) {
	tests := []struct {
		value   string
		wantErr bool
	}{
		{"0.0", false},
		{"0.7", false},
		{"1.0", false},
		{"2.0", false},
		{"0", false},
		{"2", false},
		{"-0.1", true},
		{"2.1", true},
		{"3.0", true},
		{"abc", true},
		{"", true},
		// NaN and Inf must be rejected (strconv.ParseFloat accepts them)
		{"NaN", true},
		{"nan", true},
		{"+Inf", true},
		{"-Inf", true},
		{"Inf", true},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			err := ValidateTemperatureParam(tt.value)
			if tt.wantErr {
				assert.Error(t, err, "temperature=%q should fail validation", tt.value)
			} else {
				assert.NoError(t, err, "temperature=%q should pass validation", tt.value)
			}
		})
	}
}

// ─── UnrecognizedParams ───────────────────────────────────────────────────────

func TestUnrecognizedParams(t *testing.T) {
	t.Run("known params produce no warnings", func(t *testing.T) {
		unknown := UnrecognizedParams(map[string]string{"effort": "high", "temperature": "0.5"})
		assert.Empty(t, unknown, "known params should not be reported as unknown")
	})

	t.Run("unknown param is detected", func(t *testing.T) {
		unknown := UnrecognizedParams(map[string]string{"foo": "bar"})
		assert.Contains(t, unknown, "foo", "unrecognised param 'foo' should be reported")
	})

	t.Run("mixed known and unknown", func(t *testing.T) {
		unknown := UnrecognizedParams(map[string]string{"effort": "medium", "custom": "value"})
		assert.Contains(t, unknown, "custom", "unrecognised 'custom' should be reported")
		assert.NotContains(t, unknown, "effort", "'effort' is known and should not appear")
	})
}

// ─── V-MAF-005: alias key validation ─────────────────────────────────────────

func TestValidateAliasKey(t *testing.T) {
	tests := []struct {
		key     string
		wantErr bool
	}{
		{"sonnet", false},
		{"my-alias", false},
		{"", false},   // empty string = default policy, always allowed
		{"a/b", true}, // V-MAF-005: slash
		{"a?b", true}, // V-MAF-005: question mark
		{"a&b", true}, // V-MAF-005: ampersand
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			err := validateAliasKey(tt.key, "/fake/path.md")
			if tt.wantErr {
				require.Error(t, err, "alias key %q should fail validation (V-MAF-005)", tt.key)
				require.ErrorContains(t, err, "V-MAF-005", "error should reference V-MAF-005")
			} else {
				assert.NoError(t, err, "alias key %q should pass validation", tt.key)
			}
		})
	}
}

// ─── V-MAF-010: circular alias detection ─────────────────────────────────────

// T-MAF-040: direct 2-node cycle must be detected.
func TestDetectCircularModelAliases_T_MAF_040(t *testing.T) {
	aliasMap := map[string][]string{
		"a": {"b"},
		"b": {"a"},
	}
	err := detectCircularModelAliases(aliasMap, "/fake/path.md")
	require.Error(t, err, "2-node cycle a → b → a must be detected (T-MAF-040)")
	require.ErrorContains(t, err, "V-MAF-010", "error should reference V-MAF-010")
}

// T-MAF-041: longer 3-node cycle must be detected; error message names all aliases.
func TestDetectCircularModelAliases_T_MAF_041(t *testing.T) {
	aliasMap := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"a"},
	}
	err := detectCircularModelAliases(aliasMap, "/fake/path.md")
	require.Error(t, err, "3-node cycle a → b → c → a must be detected (T-MAF-041)")
	// Error message must name all three aliases.
	require.ErrorContains(t, err, "a", "cycle error should name alias 'a'")
	require.ErrorContains(t, err, "b", "cycle error should name alias 'b'")
	require.ErrorContains(t, err, "c", "cycle error should name alias 'c'")
}

// Acyclic map should not produce an error.
func TestDetectCircularModelAliases_Acyclic(t *testing.T) {
	aliasMap := map[string][]string{
		"small": {"mini"},
		"mini":  {"haiku"},
		"haiku": {"copilot/*haiku*"},
	}
	err := detectCircularModelAliases(aliasMap, "/fake/path.md")
	assert.NoError(t, err, "acyclic alias chain should not be reported as cyclic")
}

// Provider-scoped entries must NOT be treated as alias references.
func TestDetectCircularModelAliases_ProviderScopedNotFollowed(t *testing.T) {
	// "sonnet" has a provider-scoped pattern; there is no "copilot" alias → no cycle.
	aliasMap := map[string][]string{
		"sonnet": {"copilot/*sonnet*"},
	}
	err := detectCircularModelAliases(aliasMap, "/fake/path.md")
	assert.NoError(t, err, "provider-scoped entries should not be treated as alias references")
}

func BenchmarkDetectCircularModelAliases_Builtin(b *testing.B) {
	aliasMap := BuiltinModelAliases()
	b.ReportAllocs()
	for b.Loop() {
		if err := detectCircularModelAliases(aliasMap, "/fake/path.md"); err != nil {
			b.Fatal(err)
		}
	}
}

// ─── validateModelAliasMap (integration) ─────────────────────────────────────

func TestValidateModelAliasMap_ValidWorkflow(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.validateModelAliasMap(
		BuiltinModelAliases(),
		map[string][]string{
			"deep-think": {"opus?effort=high", "gpt-5?effort=high"},
			"":           {"deep-think", "sonnet"},
		},
		"copilot/gpt-5",
		"/fake/path/workflow.md",
	)
	assert.NoError(t, err, "valid alias map should pass validation")
}

func TestValidateModelAliasMap_InvalidEffortInFrontmatter(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.validateModelAliasMap(
		BuiltinModelAliases(),
		map[string][]string{
			"bad": {"opus?effort=extreme"},
		},
		"",
		"/fake/path/workflow.md",
	)
	require.Error(t, err, "invalid effort value in frontmatter should fail (V-MAF-002)")
}

func TestValidateModelAliasMap_InvalidTemperatureInFrontmatter(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.validateModelAliasMap(
		BuiltinModelAliases(),
		map[string][]string{
			"hot": {"gpt-5?temperature=5.0"},
		},
		"",
		"/fake/path/workflow.md",
	)
	require.Error(t, err, "out-of-range temperature in frontmatter should fail (V-MAF-003)")
}

func TestValidateModelAliasMap_CircularFrontmatter(t *testing.T) {
	mergedWithCycle := map[string][]string{
		"a": {"b"},
		"b": {"a"},
	}
	// Add builtins (non-cyclic)
	maps.Copy(mergedWithCycle, BuiltinModelAliases())

	compiler := NewCompiler()
	err := compiler.validateModelAliasMap(
		mergedWithCycle,
		map[string][]string{
			"a": {"b"},
			"b": {"a"},
		},
		"",
		"/fake/path/workflow.md",
	)
	require.Error(t, err, "cycle in frontmatter aliases should fail (V-MAF-010)")
}

func TestValidateModelAliasMap_GlobInEngineModel(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.validateModelAliasMap(
		BuiltinModelAliases(),
		nil,
		"copilot/*gpt*",
		"/fake/path/workflow.md",
	)
	require.Error(t, err, "glob in engine.model should fail (V-MAF-004)")
}

func TestValidateModelAliasMap_InvalidAliasKey(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.validateModelAliasMap(
		BuiltinModelAliases(),
		map[string][]string{
			"bad/key": {"copilot/some-model"},
		},
		"",
		"/fake/path/workflow.md",
	)
	require.Error(t, err, "alias key with '/' should fail (V-MAF-005)")
}

func TestValidateModelAliasMap_ExpressionModelSkipped(t *testing.T) {
	// GitHub Actions expressions are exempt from syntax validation.
	compiler := NewCompiler()
	err := compiler.validateModelAliasMap(
		BuiltinModelAliases(),
		nil,
		"${{ inputs.model }}",
		"/fake/path/workflow.md",
	)
	assert.NoError(t, err, "GitHub Actions expression in engine.model should be skipped")
}

func TestValidateModelAliasMap_PartialExpressionEngineModelSkipped(t *testing.T) {
	// Partial expressions (expression + query params) must also be exempt from validation.
	// The query params cannot be evaluated until the expression is resolved at runtime.
	compiler := NewCompiler()
	err := compiler.validateModelAliasMap(
		BuiltinModelAliases(),
		nil,
		"${{ inputs.model }}?effort=high",
		"/fake/path/workflow.md",
	)
	assert.NoError(t, err, "partial expression in engine.model should be skipped")
}

func TestValidateModelAliasMap_ExpressionInAliasEntriesSkipped(t *testing.T) {
	// Expressions in alias list entries must be exempt from validation.
	compiler := NewCompiler()
	err := compiler.validateModelAliasMap(
		BuiltinModelAliases(),
		map[string][]string{
			"dynamic": {"${{ inputs.model }}", "${{ inputs.fallback }}"},
		},
		"",
		"/fake/path/workflow.md",
	)
	assert.NoError(t, err, "GitHub Actions expressions in alias entries should be skipped")
}

func TestValidateModelAliasMap_PartialExpressionInAliasEntriesSkipped(t *testing.T) {
	// Partial expressions in alias entries (expression with query params) must be exempt.
	compiler := NewCompiler()
	err := compiler.validateModelAliasMap(
		BuiltinModelAliases(),
		map[string][]string{
			"dynamic": {"${{ inputs.model }}?effort=high", "copilot/${{ inputs.model_token }}"},
		},
		"",
		"/fake/path/workflow.md",
	)
	assert.NoError(t, err, "partial expressions in alias entries should be skipped")
}

func TestValidateModelAliasMap_NoEngineModel(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.validateModelAliasMap(
		BuiltinModelAliases(),
		nil,
		"",
		"/fake/path/workflow.md",
	)
	assert.NoError(t, err, "empty engine.model should pass validation")
}

// ─── T-MAF-030/031/032/033: merge precedence tests (already covered in model_aliases_test.go)
// but also exercised here via validateModelAliasMap for confidence.

func TestValidateModelAliasMap_BuiltinCyclePreventedBySpec(t *testing.T) {
	// The builtin aliases are guaranteed acyclic by the spec. Verify no false positive.
	compiler := NewCompiler()
	err := compiler.validateModelAliasMap(
		BuiltinModelAliases(),
		nil,
		"",
		"/fake/path/workflow.md",
	)
	assert.NoError(t, err, "builtin alias map must be acyclic")
}

func TestValidateModelAliasMap_EngineModelUnknownParamEmitsWarning(t *testing.T) {
	// V-MAF-011: engine.model with an unrecognised query parameter should not cause an
	// error but should increment the warning counter.
	compiler := NewCompiler()
	err := compiler.validateModelAliasMap(
		BuiltinModelAliases(),
		nil,
		"opus?unknownparam=value",
		"/fake/path/workflow.md",
	)
	require.NoError(t, err, "unknown param in engine.model should not be a hard error")
	assert.Positive(t, compiler.GetWarningCount(), "unknown param in engine.model should emit a V-MAF-011 warning")
}

// isAliasReference helper tests.
func TestIsAliasReference(t *testing.T) {
	aliasMap := map[string][]string{"sonnet": {"copilot/*sonnet*"}}

	assert.True(t, isAliasReference("sonnet", aliasMap), "bare alias key should be detected as alias reference")
	assert.False(t, isAliasReference("copilot/model", aliasMap), "provider-scoped entry should not be alias reference")
	assert.False(t, isAliasReference("copilot/*sonnet*", aliasMap), "glob entry should not be alias reference")
	assert.False(t, isAliasReference("unknown", aliasMap), "unknown bare name is not an alias reference")
}

// ─── Expression skip tests for validateModelIdentifierStrings ─────────────────

// TestValidateModelIdentifierStrings_ExpressionForms covers every expression
// form that must be silently skipped by validateModelIdentifierStrings.
func TestValidateModelIdentifierStrings_ExpressionForms(t *testing.T) {
	tests := []struct {
		name        string
		identifiers []string
		wantErrs    bool
	}{
		// Whole-string expression (original supported form).
		{
			name:        "whole-string expression",
			identifiers: []string{"${{ inputs.model }}"},
			wantErrs:    false,
		},
		// Expression with trailing query params — new form supported after this change.
		{
			name:        "expression with effort param",
			identifiers: []string{"${{ inputs.model }}?effort=high"},
			wantErrs:    false,
		},
		{
			name:        "expression with temperature param",
			identifiers: []string{"${{ inputs.model }}?temperature=0.7"},
			wantErrs:    false,
		},
		{
			name:        "expression with multiple params",
			identifiers: []string{"${{ inputs.model }}?effort=high&temperature=0.5"},
			wantErrs:    false,
		},
		// Expression embedded inside a provider-scoped identifier — new form.
		{
			name:        "expression as model token in provider-scoped identifier",
			identifiers: []string{"copilot/${{ inputs.model_token }}"},
			wantErrs:    false,
		},
		// Multiple expressions in the same list — each entry evaluated independently.
		{
			name: "multiple expression entries",
			identifiers: []string{
				"${{ inputs.primary }}",
				"${{ inputs.fallback }}",
			},
			wantErrs: false,
		},
		// Mix of expression and valid literal — literal must still be validated.
		{
			name: "mix of expression and valid literal",
			identifiers: []string{
				"${{ inputs.model }}",
				"opus",
			},
			wantErrs: false,
		},
		// Mix of expression and invalid literal — literal error must still be reported.
		{
			name: "mix of expression and invalid literal",
			identifiers: []string{
				"${{ inputs.model }}",
				"invalid identifier!",
			},
			wantErrs: true,
		},
		// Expression with compact (no-space) syntax.
		{
			name:        "compact expression syntax",
			identifiers: []string{"${{inputs.model}}"},
			wantErrs:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateModelIdentifierStrings(tt.identifiers, "test context")
			if tt.wantErrs {
				assert.NotEmpty(t, errs, "expected validation errors but got none")
			} else {
				assert.Empty(t, errs, "expected no validation errors but got: %v", errs)
			}
		})
	}
}

// ─── Expression skip tests for warnUnrecognizedModelParams ────────────────────

// TestWarnUnrecognizedModelParams_ExpressionSkipped verifies that
// expression-containing identifiers do not trigger V-MAF-011 warnings.
func TestWarnUnrecognizedModelParams_ExpressionSkipped(t *testing.T) {
	tests := []struct {
		name        string
		identifiers []string
		wantWarning bool
	}{
		{
			name:        "whole-string expression emits no warning",
			identifiers: []string{"${{ inputs.model }}"},
			wantWarning: false,
		},
		{
			name:        "partial expression emits no warning",
			identifiers: []string{"${{ inputs.model }}?unknownparam=value"},
			wantWarning: false,
		},
		{
			name:        "provider-scoped expression emits no warning",
			identifiers: []string{"copilot/${{ inputs.model }}?unknownparam=value"},
			wantWarning: false,
		},
		{
			name:        "non-expression with unknown param emits warning",
			identifiers: []string{"opus?unknownparam=value"},
			wantWarning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.warnUnrecognizedModelParams(tt.identifiers, "/fake/path.md")
			if tt.wantWarning {
				assert.Positive(t, compiler.GetWarningCount(),
					"expected V-MAF-011 warning but warning counter is zero")
			} else {
				assert.Zero(t, compiler.GetWarningCount(),
					"expected no V-MAF-011 warning for expression-containing identifier")
			}
		})
	}
}

// ─── Engine.model table-driven expression tests ───────────────────────────────

// TestValidateModelAliasMap_EngineModelExpressionForms covers every expression
// form allowed in engine.model (resolved at runtime, exempt from compile-time checks).
func TestValidateModelAliasMap_EngineModelExpressionForms(t *testing.T) {
	tests := []struct {
		name        string
		engineModel string
		wantErr     bool
	}{
		// Whole/partial expressions — no "*" in literal parts — accepted.
		{"whole-string expression", "${{ inputs.model }}", false},
		{"expression with effort param", "${{ inputs.model }}?effort=high", false},
		{"expression with temperature param", "${{ inputs.model }}?temperature=0.5", false},
		{"expression with multiple params", "${{ inputs.model }}?effort=low&temperature=0.3", false},
		{"expression with unknown param — no error (no static parse)", "${{ inputs.model }}?unknownparam=x", false},
		{"compact expression syntax", "${{inputs.model}}", false},
		{"provider-scoped expression", "copilot/${{ inputs.model }}", false},
		// Non-expression values are still validated normally.
		{"valid literal is accepted", "copilot/gpt-5", false},
		// V-MAF-004: glob in literal part — always rejected regardless of expressions.
		{"glob in engine.model is still rejected (V-MAF-004)", "copilot/*gpt*", true},
		// V-MAF-004 must fire even when "*" appears outside an expression.
		{"expression + trailing glob rejected (V-MAF-004)", "${{ inputs.model }}*", true},
		{"literal glob before expression rejected (V-MAF-004)", "copilot/*${{ inputs.model }}", true},
		{"expression between globs rejected (V-MAF-004)", "*${{ inputs.model }}*", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			err := compiler.validateModelAliasMap(
				BuiltinModelAliases(),
				nil,
				tt.engineModel,
				"/fake/path/workflow.md",
			)
			if tt.wantErr {
				require.Error(t, err, "engine.model=%q should be rejected", tt.engineModel)
				require.ErrorContains(t, err, "V-MAF-004", "engine.model=%q error should reference V-MAF-004", tt.engineModel)
				require.ErrorContains(t, err, tt.engineModel, "engine.model error should quote the offending value")
			} else {
				assert.NoError(t, err, "engine.model=%q should be accepted", tt.engineModel)
			}
		})
	}
}
