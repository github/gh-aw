//go:build !integration

package workflow

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRuntimeSetupCaches(t *testing.T) {
	tests := []struct {
		name     string
		with     string
		runtimes map[string]any
		unsafe   bool
	}{
		{name: "preserved uv without cache input", with: "version: '0.10.0'", unsafe: true},
		{name: "preserved uv auto cache", with: "version: '0.10.0'\n      enable-cache: auto", unsafe: true},
		{name: "preserved uv true cache", with: "version: '0.10.0'\n      enable-cache: true", unsafe: true},
		{name: "preserved uv expression cache", with: "version: '0.10.0'\n      enable-cache: ${{ inputs.cache }}", unsafe: true},
		{name: "preserved uv cache disabled", with: "version: '0.10.0'\n      enable-cache: false"},
		{name: "preserved uv string cache disabled", with: "version: '0.10.0'\n      enable-cache: 'false'"},
		{name: "undetected uv without cache input", unsafe: true},
		{name: "undetected uv cache disabled", with: "enable-cache: false"},
		{name: "removed duplicate cannot enable cache", with: "enable-cache: true", runtimes: map[string]any{"uv": map[string]any{}}},
		{name: "matching uv version removed", with: "version: '0.10.0'\n      enable-cache: true", runtimes: map[string]any{"uv": map[string]any{"version": "0.10.0"}}},
	}
	for _, tt := range tests {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/strict=%t", tt.name, strict), func(t *testing.T) {
				compiler := NewCompiler()
				compiler.SetStrictMode(strict)
				steps := "steps:\n  - uses: astral-sh/setup-uv@v10\n"
				if tt.with != "" {
					steps += "    with:\n      " + tt.with + "\n"
				}
				data := &WorkflowData{CustomSteps: steps, Runtimes: tt.runtimes, RawFrontmatter: map[string]any{"strict": strict}}
				err := compiler.validateRuntimeSetupCaches(data)
				if tt.unsafe && strict {
					require.ErrorContains(t, err, "strict mode:")
					require.ErrorContains(t, err, "enable-cache: false")
				} else {
					require.NoError(t, err)
				}
				expectedWarnings := 0
				if tt.unsafe && !strict {
					expectedWarnings = 1
				}
				assert.Equal(t, expectedWarnings, compiler.GetWarningCount())
				assert.Equal(t, steps, data.CustomSteps, "validation must not mutate custom steps")
			})
		}
	}
}

func TestValidateRuntimeSetupCachesAgentStepSections(t *testing.T) {
	for _, section := range []string{"pre-steps", "pre-agent-steps", "post-steps"} {
		t.Run(section, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.SetStrictMode(true)
			data := &WorkflowData{}
			steps := section + ":\n  - uses: astral-sh/setup-uv@v10\n"
			switch section {
			case "pre-steps":
				data.PreSteps = steps
			case "pre-agent-steps":
				data.PreAgentSteps = steps
			case "post-steps":
				data.PostSteps = steps
			}
			require.ErrorContains(t, compiler.validateRuntimeSetupCaches(data), "enable-cache: false")
		})
	}
}

func TestValidateRuntimeSetupCachesAgentJobSections(t *testing.T) {
	for _, section := range []string{"setup-steps", "pre-steps"} {
		t.Run(section, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.SetStrictMode(true)
			data := &WorkflowData{
				Jobs: map[string]any{
					"agent": map[string]any{
						section: []any{map[string]any{"uses": "astral-sh/setup-uv@v10"}},
					},
				},
			}
			require.ErrorContains(t, compiler.validateRuntimeSetupCaches(data), "enable-cache: false")
		})
	}
}

func TestValidateRuntimeSetupCachesIgnoresOtherActions(t *testing.T) {
	compiler := NewCompiler()
	compiler.SetStrictMode(true)
	data := &WorkflowData{CustomSteps: "steps:\n  - uses: other/astral-sh/setup-uv@v10\n  - run: uv sync\n"}
	require.NoError(t, compiler.validateRuntimeSetupCaches(data))
}

func TestValidateRuntimeSetupCachesCaseInsensitive(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{CustomSteps: "steps:\n  - uses: Astral-Sh/Setup-UV@v10\n"}
	require.ErrorContains(t, compiler.validateRuntimeSetupCaches(data), "enable-cache: false")
}
