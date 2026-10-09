package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateRuntimeSetupSteps_UVDisablesActionsCache(t *testing.T) {
	steps := GenerateRuntimeSetupSteps([]RuntimeRequirement{{
		Runtime: findRuntimeByID("uv"),
		ExtraFields: map[string]any{
			"enable-cache":     true,
			"python-downloads": false,
		},
	}}, nil)
	require.Len(t, steps, 1)

	content := strings.Join(steps[0], "\n")
	assert.Equal(t, 1, strings.Count(content, "enable-cache:"))
	assert.Contains(t, content, "enable-cache: false")
	assert.NotContains(t, content, "enable-cache: true")
	assert.Contains(t, content, "python-downloads: false")
}

func TestDeduplicateRuntimeSetupSteps_DoesNotCarryUVCacheSetting(t *testing.T) {
	customSteps := `steps:
  - name: Setup uv
    uses: astral-sh/setup-uv@v5
    with:
      enable-cache: true
      python-downloads: false
  - name: Check uv
    run: uv --version`

	requirements := []RuntimeRequirement{{
		Runtime: findRuntimeByID("uv"),
	}}
	deduplicatedSteps, filteredRequirements, err := DeduplicateRuntimeSetupStepsFromCustomSteps(customSteps, requirements)
	require.NoError(t, err)
	require.Len(t, filteredRequirements, 1)
	assert.NotContains(t, filteredRequirements[0].ExtraFields, "enable-cache")
	assert.Equal(t, false, filteredRequirements[0].ExtraFields["python-downloads"])
	assert.NotContains(t, deduplicatedSteps, "Setup uv")

	generatedSteps := GenerateRuntimeSetupSteps(filteredRequirements, nil)
	require.Len(t, generatedSteps, 1)
	content := strings.Join(generatedSteps[0], "\n")
	assert.Contains(t, content, "enable-cache: false")
	assert.NotContains(t, content, "enable-cache: true")
	assert.Contains(t, content, "python-downloads: false")
}

func TestPrepareRuntimeSetupAndCheckoutInfo_UVCacheValidation(t *testing.T) {
	tests := []struct {
		name          string
		with          string
		strictMode    bool
		expectWarning bool
		expectError   bool
	}{
		{
			name:          "omitted cache setting warns",
			with:          "      version: '0.8.0'\n",
			expectWarning: true,
		},
		{
			name:          "enabled cache setting warns",
			with:          "      version: '0.8.0'\n      enable-cache: true\n",
			expectWarning: true,
		},
		{
			name:        "omitted cache setting errors in strict mode",
			with:        "      version: '0.8.0'\n",
			strictMode:  true,
			expectError: true,
		},
		{
			name:        "enabled cache setting errors in strict mode",
			with:        "      version: '0.8.0'\n      enable-cache: true\n",
			strictMode:  true,
			expectError: true,
		},
		{
			name: "disabled cache setting is valid",
			with: "      version: '0.8.0'\n      enable-cache: false\n      python-downloads: false\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				CustomSteps: "steps:\n  - name: Custom uv setup\n    uses: astral-sh/setup-uv@v5\n    with:\n" + tt.with,
			}
			compiler := &Compiler{strictMode: tt.strictMode}

			_, _, err := compiler.prepareRuntimeSetupAndCheckoutInfo(data)
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), `step "Custom uv setup"`)
				assert.Contains(t, err.Error(), "enable-cache: false")
			} else {
				require.NoError(t, err)
			}

			if tt.expectWarning {
				assert.Equal(t, 1, compiler.GetWarningCount())
			} else {
				assert.Zero(t, compiler.GetWarningCount())
			}
			assert.Contains(t, data.CustomSteps, "version: '0.8.0'")
			if tt.name == "disabled cache setting is valid" {
				assert.Contains(t, data.CustomSteps, "enable-cache: false")
				assert.Contains(t, data.CustomSteps, "python-downloads: false")
			}
		})
	}
}

func TestPrepareRuntimeSetupAndCheckoutInfo_UVCacheValidationWithoutUVCommand(t *testing.T) {
	data := &WorkflowData{
		CustomSteps: `steps:
  - name: Custom uv setup
    uses: astral-sh/setup-uv@v5
    with:
      version: '0.8.0'
`,
	}
	compiler := &Compiler{}

	runtimeSteps, _, err := compiler.prepareRuntimeSetupAndCheckoutInfo(data)
	require.NoError(t, err)
	assert.Empty(t, runtimeSteps)
	assert.Equal(t, 1, compiler.GetWarningCount())
	assert.Contains(t, data.CustomSteps, "version: '0.8.0'")
}

func TestCustomSetupUVCacheWarningsAcceptsExplicitFalse(t *testing.T) {
	warnings := customSetupUVCacheWarnings(`steps:
  - name: Safe uv setup
    uses: astral-sh/setup-uv@v5
    with:
      enable-cache: "false"
`)
	assert.Empty(t, warnings)
}
