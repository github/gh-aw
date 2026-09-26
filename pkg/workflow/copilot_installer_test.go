//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestCopilotEngineWithExpressionVersion(t *testing.T) {
	// expression engine.version must be honored: the value must flow through env-var
	// injection (not embedded directly in the shell command) and compat.json must be skipped.
	engine := NewCopilotEngine()

	expressionVersion := "${{ inputs.engine-version }}"
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			Version: expressionVersion,
		},
	}

	steps := engine.GetInstallationSteps(workflowData)

	// EngineConfig.Version must remain as the expression value.
	if workflowData.EngineConfig.Version != expressionVersion {
		t.Fatalf("Expected engine config version to remain %q, got: %q", expressionVersion, workflowData.EngineConfig.Version)
	}

	// Find the install step
	var installStep string
	for _, step := range steps {
		stepContent := strings.Join(step, "\n")
		if strings.Contains(stepContent, "install_copilot_cli.sh") {
			installStep = stepContent
			break
		}
	}

	if installStep == "" {
		t.Fatal("Could not find install step with install_copilot_cli.sh")
	}

	// Should use env var injection (not embed expression directly in shell command).
	if !strings.Contains(installStep, "ENGINE_VERSION: "+expressionVersion) {
		t.Errorf("Expected ENGINE_VERSION env var with expression, got:\n%s", installStep)
	}
	if !strings.Contains(installStep, `"${ENGINE_VERSION}"`) {
		t.Errorf(`Expected step to reference "$ENGINE_VERSION" in run command, got:\n%s`, installStep)
	}
	if strings.Contains(installStep, "install_copilot_cli.sh "+expressionVersion) {
		t.Errorf("Expression version should NOT be embedded directly in shell command, got:\n%s", installStep)
	}
}

func TestCopilotEngineWithExpressionVersionAndCompiledVersion(t *testing.T) {
	// When engine.version is an expression AND CompiledVersion is set, both ENGINE_VERSION
	// and GH_AW_COMPILED_VERSION must appear in the install step so the script can fall back
	// to compat.json resolution when the expression evaluates to an empty string at runtime.
	engine := NewCopilotEngine()

	expressionVersion := "${{ inputs.engine-version }}"
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			Version: expressionVersion,
		},
		CompiledVersion: "v0.99.0",
	}

	steps := engine.GetInstallationSteps(workflowData)

	var installStep string
	for _, step := range steps {
		stepContent := strings.Join(step, "\n")
		if strings.Contains(stepContent, "install_copilot_cli.sh") {
			installStep = stepContent
			break
		}
	}

	if installStep == "" {
		t.Fatal("Could not find install step with install_copilot_cli.sh")
	}

	if !strings.Contains(installStep, "ENGINE_VERSION: "+expressionVersion) {
		t.Errorf("Expected ENGINE_VERSION env var with expression, got:\n%s", installStep)
	}
	if !strings.Contains(installStep, "GH_AW_COMPILED_VERSION: v0.99.0") {
		t.Errorf("Expected GH_AW_COMPILED_VERSION env var when CompiledVersion is set, got:\n%s", installStep)
	}
}

func TestCopilotEngineWithVersionAndByokFeature(t *testing.T) {
	// engine.version must be honored even when the BYOK feature flag is enabled.
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			Version: "1.0.0",
		},
		Features: map[string]any{
			string(constants.ByokCopilotFeatureFlag): true,
		},
	}

	steps := engine.GetInstallationSteps(workflowData)

	var installStep string
	for _, step := range steps {
		stepContent := strings.Join(step, "\n")
		if strings.Contains(stepContent, "install_copilot_cli.sh") {
			installStep = stepContent
			break
		}
	}

	if installStep == "" {
		t.Fatal("Could not find install step with install_copilot_cli.sh")
	}

	if !strings.Contains(installStep, `install_copilot_cli.sh" 1.0.0`) {
		t.Errorf("Expected user-specified version in install step, got:\n%s", installStep)
	}
	if strings.Contains(installStep, `install_copilot_cli.sh" `+string(constants.DefaultCopilotVersion)) {
		t.Errorf("Expected user-specified version, not default version, in install step:\n%s", installStep)
	}
}
