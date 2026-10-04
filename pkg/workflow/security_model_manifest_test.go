//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityModelDetectionPolicyManifest(t *testing.T) {
	condition := "${{ inputs.enable_detection }}"
	tests := []struct {
		name   string
		config *SafeOutputsConfig
		mode   string
		expr   string
	}{
		{"not-configured", nil, "disabled", ""},
		{"disabled", &SafeOutputsConfig{}, "disabled", ""},
		{"enabled", &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}}, "enabled", ""},
		{"no-runnable-engine", &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{EngineDisabled: true}}, "disabled", ""},
		{"custom-detector", &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{EngineDisabled: true, Steps: []any{"scan"}}}, "enabled", ""},
		{"conditional", &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{EnabledExpr: &condition}}, "conditional", condition},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			compiler := NewCompiler()
			var header strings.Builder
			err := compiler.generateWorkflowHeader(&header, &WorkflowData{
				RawFrontmatter: map[string]any{"on": "workflow_dispatch"},
				SafeOutputs:    tc.config,
			}, "hash", "body", nil, nil)
			require.NoError(t, err)
			manifest, err := ExtractGHAWManifestFromLockFile(header.String())
			require.NoError(t, err)
			require.NotNil(t, manifest)
			require.NotNil(t, manifest.ThreatDetection)
			require.Equal(t, tc.mode, manifest.ThreatDetection.Mode)
			require.Equal(t, tc.expr, manifest.ThreatDetection.Condition)
		})
	}
}
