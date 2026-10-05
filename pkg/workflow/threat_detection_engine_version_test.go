//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectionEngineDoesNotInheritCustomEngineVersion(t *testing.T) {
	for _, engineID := range []string{"aider", "pi", "gemini"} {
		for _, configID := range []string{"", engineID} {
			t.Run(engineID+"/"+configID, func(t *testing.T) {
				data := &WorkflowData{
					AI: engineID,
					EngineConfig: &EngineConfig{
						ID: configID, Version: "0.86.2",
						Args: []string{"--custom-engine-only"}, Driver: "custom-driver.cjs",
					},
					SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
				}
				compiler := NewCompiler()
				for _, steps := range [][]string{
					compiler.buildDetectionEngineExecutionStep(data),
					compiler.buildInstallDetectionEngineForExternalDetectorStep(data),
				} {
					content := strings.Join(steps, "")
					assert.Contains(t, content, `install_copilot_cli.sh"`)
					assert.NotContains(t, content, "0.86.2")
					assert.NotContains(t, content, "--custom-engine-only")
					assert.NotContains(t, content, "custom-driver.cjs")
				}
				assert.Equal(t, "0.86.2", data.EngineConfig.Version)
			})
		}
	}
}

func TestDetectionEngineVersionOverrides(t *testing.T) {
	for _, test := range []struct {
		name     string
		override *EngineConfig
		want     string
	}{
		{"same engine", nil, "1.0.87"},
		{"explicit Copilot version", &EngineConfig{ID: "copilot", Version: "1.0.88"}, "1.0.88"},
		{"normalized Pi override", &EngineConfig{ID: "pi", Version: "0.86.2"}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := &WorkflowData{
				AI:           "copilot",
				EngineConfig: &EngineConfig{Version: "1.0.87"},
				SafeOutputs:  &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{EngineConfig: test.override}},
			}
			compiler := NewCompiler()
			for _, steps := range [][]string{
				compiler.buildDetectionEngineExecutionStep(data),
				compiler.buildInstallDetectionEngineForExternalDetectorStep(data),
			} {
				content := strings.Join(steps, "")
				assert.Contains(t, content, `install_copilot_cli.sh"`)
				if test.want != "" {
					assert.Contains(t, content, `install_copilot_cli.sh" `+test.want)
				} else {
					assert.NotContains(t, content, "0.86.2")
				}
			}
		})
	}
}
