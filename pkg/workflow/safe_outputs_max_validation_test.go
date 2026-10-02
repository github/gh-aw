//go:build !integration

package workflow

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSafeOutputsMax(t *testing.T) {
	t.Run("nil config is valid", func(t *testing.T) {
		err := validateSafeOutputsMax(nil)
		assert.NoError(t, err, "nil config should be valid")
	})

	t.Run("config with no max fields is valid", func(t *testing.T) {
		config := &SafeOutputsConfig{}
		err := validateSafeOutputsMax(config)
		assert.NoError(t, err, "config with no max fields should be valid")
	})

	t.Run("max of 1 is valid", func(t *testing.T) {
		config := &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
			},
		}
		err := validateSafeOutputsMax(config)
		assert.NoError(t, err, "max: 1 should be valid")
	})

	t.Run("max of 5 is valid", func(t *testing.T) {
		config := &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
			},
		}
		err := validateSafeOutputsMax(config)
		assert.NoError(t, err, "max: 5 should be valid")
	})

	t.Run("max of -1 is valid (unlimited)", func(t *testing.T) {
		config := &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("-1")},
			},
		}
		err := validateSafeOutputsMax(config)
		assert.NoError(t, err, "max: -1 should be valid (means unlimited per spec)")
	})

	t.Run("max of 0 is invalid", func(t *testing.T) {
		config := &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("0")},
			},
		}
		err := validateSafeOutputsMax(config)
		require.Error(t, err, "max: 0 should be invalid")
		require.ErrorContains(t, err, "max must be a positive integer or -1", "error should explain valid values")
		require.ErrorContains(t, err, "add-comment", "error should mention the field name")
	})

	t.Run("max of -2 is invalid", func(t *testing.T) {
		config := &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("-2")},
			},
		}
		err := validateSafeOutputsMax(config)
		require.Error(t, err, "max: -2 should be invalid")
		require.ErrorContains(t, err, "max must be a positive integer or -1", "error should explain valid values")
	})

	t.Run("max as GitHub Actions expression is skipped", func(t *testing.T) {
		config := &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("${{ inputs.max }}")},
			},
		}
		err := validateSafeOutputsMax(config)
		assert.NoError(t, err, "GitHub Actions expression should be skipped")
	})

	t.Run("nil max is valid", func(t *testing.T) {
		config := &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: nil},
			},
		}
		err := validateSafeOutputsMax(config)
		assert.NoError(t, err, "nil max should be valid")
	})

	t.Run("dispatch_repository tool max of 0 is invalid", func(t *testing.T) {
		maxVal := "0"
		config := &SafeOutputsConfig{
			DispatchRepository: &DispatchRepositoryConfig{
				Tools: map[string]*DispatchRepositoryToolConfig{
					"my-tool": {Max: &maxVal},
				},
			},
		}
		err := validateSafeOutputsMax(config)
		require.Error(t, err, "dispatch_repository max: 0 should be invalid")
		require.ErrorContains(t, err, "max must be a positive integer or -1", "error should explain valid values")
		require.ErrorContains(t, err, "my-tool", "error should mention the tool name")
		require.ErrorContains(t, err, "dispatch_repository", "error should use underscore form")
	})

	t.Run("dispatch_repository tool max of -1 is valid (unlimited)", func(t *testing.T) {
		maxVal := "-1"
		config := &SafeOutputsConfig{
			DispatchRepository: &DispatchRepositoryConfig{
				Tools: map[string]*DispatchRepositoryToolConfig{
					"my-tool": {Max: &maxVal},
				},
			},
		}
		err := validateSafeOutputsMax(config)
		assert.NoError(t, err, "dispatch_repository max: -1 should be valid")
	})

	t.Run("dispatch_repository tool max of 1 is valid", func(t *testing.T) {
		maxVal := "1"
		config := &SafeOutputsConfig{
			DispatchRepository: &DispatchRepositoryConfig{
				Tools: map[string]*DispatchRepositoryToolConfig{
					"my-tool": {Max: &maxVal},
				},
			},
		}
		err := validateSafeOutputsMax(config)
		assert.NoError(t, err, "dispatch_repository max: 1 should be valid")
	})

	t.Run("dispatch_repository tool max as expression is skipped", func(t *testing.T) {
		maxVal := "${{ inputs.max }}"
		config := &SafeOutputsConfig{
			DispatchRepository: &DispatchRepositoryConfig{
				Tools: map[string]*DispatchRepositoryToolConfig{
					"my-tool": {Max: &maxVal},
				},
			},
		}
		err := validateSafeOutputsMax(config)
		assert.NoError(t, err, "GitHub Actions expression for dispatch_repository should be skipped")
	})

	t.Run("multiple configs with one invalid returns error", func(t *testing.T) {
		config := &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("3")},
			},
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("0")},
			},
		}
		err := validateSafeOutputsMax(config)
		require.Error(t, err, "config with one invalid max should return error")
		require.ErrorContains(t, err, "max must be a positive integer or -1", "error should explain valid values")
	})
}

// TestValidateSafeOutputsMaxFieldCoverage verifies that validateSafeOutputsMax detects
// invalid max values for every field listed in safeOutputFieldMapping (except
// DispatchRepository, which has a different map-of-tools structure and is validated
// separately). This acts as a regression guard to ensure that when a new safe output
// type is added to safeOutputFieldMapping the developer also adds a direct-access
// check to validateSafeOutputsMax.
func TestValidateSafeOutputsMaxFieldCoverage(t *testing.T) {
	invalidMax := strPtr("0") // 0 is always an invalid max value

	for fieldName, toolName := range safeOutputFieldMapping {
		if fieldName == "DispatchRepository" {
			// DispatchRepository uses a map-of-tools structure and is validated
			// separately at the end of validateSafeOutputsMax.
			continue
		}

		t.Run(fieldName, func(t *testing.T) {
			cfg := &SafeOutputsConfig{}
			val := reflect.ValueOf(cfg).Elem()
			field := val.FieldByName(fieldName)
			require.Truef(t, field.IsValid(),
				"safeOutputFieldMapping references unknown struct field %q", fieldName)
			require.Equalf(t, reflect.Pointer, field.Kind(),
				"safeOutputFieldMapping field %q is expected to be a pointer type", fieldName)

			// Create a zero-value instance of the field's element type and set Max to an invalid value.
			elem := reflect.New(field.Type().Elem())
			baseCfgField := elem.Elem().FieldByName("BaseSafeOutputConfig")
			require.Truef(t, baseCfgField.IsValid(),
				"field %q does not embed BaseSafeOutputConfig — add a direct check in validateSafeOutputsMax", fieldName)
			maxField := baseCfgField.FieldByName("Max")
			require.Truef(t, maxField.IsValid(), "BaseSafeOutputConfig.Max field not found for field %q", fieldName)
			maxField.Set(reflect.ValueOf(invalidMax))
			field.Set(elem)

			err := validateSafeOutputsMax(cfg)
			require.Errorf(t, err,
				"validateSafeOutputsMax should detect invalid max for field %q (tool: %q); add a direct-access check", fieldName, toolName)
			assert.Containsf(t, err.Error(), "max must be a positive integer or -1",
				"error for field %q should explain valid values", fieldName)
		})
	}
}

func TestValidateSafeOutputsMaxIntegration(t *testing.T) {
	compiler := &Compiler{}

	t.Run("max of 0 is rejected during config extraction via compiler", func(t *testing.T) {
		frontmatter := map[string]any{
			"safe-outputs": map[string]any{
				"add-comment": map[string]any{
					"max": 0,
				},
			},
		}

		config := compiler.extractSafeOutputsConfig(frontmatter)
		require.NotNil(t, config, "config should be extracted")

		err := validateSafeOutputsMax(config)
		require.Error(t, err, "max: 0 should fail validation")
		require.ErrorContains(t, err, "max must be a positive integer or -1", "error message should explain valid values")
	})

	t.Run("max of -2 is rejected during config extraction via compiler", func(t *testing.T) {
		frontmatter := map[string]any{
			"safe-outputs": map[string]any{
				"create-issue": map[string]any{
					"max": -2,
				},
			},
		}

		config := compiler.extractSafeOutputsConfig(frontmatter)
		require.NotNil(t, config, "config should be extracted")

		err := validateSafeOutputsMax(config)
		require.Error(t, err, "max: -2 should fail validation")
		require.ErrorContains(t, err, "max must be a positive integer or -1", "error message should explain valid values")
	})

	t.Run("max of -1 passes validation (unlimited)", func(t *testing.T) {
		frontmatter := map[string]any{
			"safe-outputs": map[string]any{
				"add-comment": map[string]any{
					"max": -1,
				},
			},
		}

		config := compiler.extractSafeOutputsConfig(frontmatter)
		require.NotNil(t, config, "config should be extracted")

		err := validateSafeOutputsMax(config)
		assert.NoError(t, err, "max: -1 should pass validation (unlimited per spec)")
	})

	t.Run("max of 1 passes validation", func(t *testing.T) {
		frontmatter := map[string]any{
			"safe-outputs": map[string]any{
				"add-comment": map[string]any{
					"max": 1,
				},
			},
		}

		config := compiler.extractSafeOutputsConfig(frontmatter)
		require.NotNil(t, config, "config should be extracted")

		err := validateSafeOutputsMax(config)
		assert.NoError(t, err, "max: 1 should pass validation")
	})
}
