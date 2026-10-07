//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestCopilotStructuredOutputConfig(t *testing.T) {
	engine := NewCopilotEngine()
	if !engine.GetCapabilities().StructuredOutput {
		t.Fatal("Copilot must advertise its native SDK structured output capability")
	}
	tests := []struct {
		name   string
		config *EngineConfig
		valid  bool
	}{
		{name: "CLI default"},
		{name: "explicit CLI", config: &EngineConfig{}},
		{name: "built-in SDK", config: &EngineConfig{CopilotSDK: true}, valid: true},
		{name: "pinned SDK", config: &EngineConfig{CopilotSDK: true, Version: "1.0.90"}, valid: true},
		{name: "latest SDK", config: &EngineConfig{CopilotSDK: true, Version: "latest"}, valid: true},
		{name: "older CLI runtime", config: &EngineConfig{CopilotSDK: true, Version: "1.0.89"}},
		{name: "unknown CLI runtime", config: &EngineConfig{CopilotSDK: true, Version: "branch"}},
		{name: "custom command", config: &EngineConfig{CopilotSDK: true, Command: "custom-copilot"}},
		{name: "custom driver", config: &EngineConfig{CopilotSDK: true, Driver: "driver.cjs"}},
		{name: "inline driver", config: &EngineConfig{CopilotSDK: true, InlineDriver: &InlineEngineDriver{Runtime: "node", Source: "source"}}},
		{name: "custom harness", config: &EngineConfig{CopilotSDK: true, HarnessScript: "custom.cjs"}},
		{name: "built-in harness", config: &EngineConfig{CopilotSDK: true, HarnessScript: "copilot_harness.cjs"}, valid: true},
		{name: "autopilot", config: &EngineConfig{CopilotSDK: true, MaxContinuations: 2}},
		{name: "autopilot args", config: &EngineConfig{CopilotSDK: true, Args: []string{"--autopilot"}}},
		{name: "autopilot continuation args", config: &EngineConfig{CopilotSDK: true, Args: []string{"--max-autopilot-continues=2"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := engine.ValidateStructuredOutputConfig(tt.config)
			if (err == nil) != tt.valid {
				t.Fatalf("ValidateStructuredOutputConfig() = %v, valid = %v", err, tt.valid)
			}
		})
	}
}

func TestCopilotStructuredOutputExecutionEnv(t *testing.T) {
	engine := NewCopilotEngine()
	data := &WorkflowData{Name: "structured-sdk", EngineConfig: &EngineConfig{CopilotSDK: true}}
	ordinary := strings.Join([]string(engine.GetExecutionSteps(data, "/tmp/gh-aw/copilot.log")[0]), "\n")
	if strings.Contains(ordinary, "GH_AW_STRUCTURED_OUTPUT_") {
		t.Fatal("ordinary SDK execution must not enable structured output")
	}
	data.StructuredOutput = &StructuredOutputConfig{Schema: map[string]any{"type": "object"}}
	structured := strings.Join([]string(engine.GetExecutionSteps(data, "/tmp/gh-aw/copilot.log")[0]), "\n")
	for key, value := range map[string]string{
		"GH_AW_STRUCTURED_OUTPUT_SCHEMA_FILE": StructuredOutputSchemaPath,
		"GH_AW_STRUCTURED_OUTPUT_FILE":        StructuredOutputFilePath,
	} {
		if !containsEnvValue(structured, key, value) {
			t.Fatalf("structured SDK execution is missing %s=%s", key, value)
		}
	}
}
