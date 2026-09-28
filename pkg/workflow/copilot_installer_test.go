//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestCopilotEngineWithVersion(t *testing.T) {
	// engine.version must be honored: when an explicit version is set it should be
	// passed to the installer and compat.json resolution must be skipped.
	engine := NewCopilotEngine()

	customVersion := "1.0.0"
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			Version: customVersion,
		},
	}

	steps := engine.GetInstallationSteps(workflowData)

	// EngineConfig.Version must remain as the user-specified value.
	if workflowData.EngineConfig.Version != customVersion {
		t.Fatalf("Expected engine config version to remain %q, got: %q", customVersion, workflowData.EngineConfig.Version)
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

	// Should pass the user-specified version to the installer (compat.json skipped).
	if !strings.Contains(installStep, `install_copilot_cli.sh" `+customVersion) {
		t.Errorf("Expected user-specified version %q in install step, got:\n%s", customVersion, installStep)
	}
	if strings.Contains(installStep, `install_copilot_cli.sh" `+string(constants.DefaultCopilotVersion)) {
		t.Errorf("Expected user-specified version, not default version, in install step:\n%s", installStep)
	}

	// Must pin GH_HOST: github.com to prevent workflow-level GHES overrides from
	// leaking into the Copilot CLI install step.
	if !strings.Contains(installStep, "GH_HOST: github.com") {
		t.Errorf("Install step should pin GH_HOST: github.com to prevent GHES workflow-level overrides, got:\n%s", installStep)
	}
}

func TestCopilotEngineWithoutVersion(t *testing.T) {
	// When engine.version is not set:
	// - EngineConfig.Version must remain unset (no normalization mutation) so that
	//   threat-detection/evals config clones do not receive an explicit version arg.
	// - The install step must NOT embed a hardcoded version arg — the script resolves the
	//   version at runtime via compat.json (priority 2) or its baked-in default (priority 3).
	// - GH_AW_COMPILED_VERSION must be injected when CompiledVersion is set on WorkflowData.
	engine := NewCopilotEngine()

	workflowData := &WorkflowData{
		Name:            "test-workflow",
		EngineConfig:    &EngineConfig{},
		CompiledVersion: "v0.99.0",
	}

	steps := engine.GetInstallationSteps(workflowData)

	// EngineConfig.Version must remain empty — no normalization mutation.
	if workflowData.EngineConfig.Version != "" {
		t.Fatalf("Expected engine config version to remain empty (no mutation), got: %q", workflowData.EngineConfig.Version)
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

	// Must NOT hardcode a version arg — that would bypass compat.json resolution.
	if strings.Contains(installStep, `install_copilot_cli.sh" `+string(constants.DefaultCopilotVersion)) {
		t.Errorf("Install step must not embed an explicit version arg when engine.version is unset; got:\n%s", installStep)
	}

	// Must inject GH_AW_COMPILED_VERSION so the script can do compat.json resolution.
	if !strings.Contains(installStep, "GH_AW_COMPILED_VERSION: v0.99.0") {
		t.Errorf("Install step must inject GH_AW_COMPILED_VERSION when CompiledVersion is set; got:\n%s", installStep)
	}

	// Must still pin GH_HOST to github.com.
	if !strings.Contains(installStep, "GH_HOST: github.com") {
		t.Errorf("Install step should pin GH_HOST: github.com to prevent GHES workflow-level overrides, got:\n%s", installStep)
	}
}

func TestCopilotEngineWithWebSearchWithoutPinnedVersionInjectsMinVersion(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name:            "test-workflow",
		EngineConfig:    &EngineConfig{},
		CompiledVersion: "v0.99.0",
		Tools: map[string]any{
			"web-search": nil,
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
	if !strings.Contains(installStep, "GH_AW_COPILOT_MIN_VERSION: "+string(constants.CopilotWebSearchMinVersion)) {
		t.Errorf("Expected web-search workflow without pinned engine.version to inject GH_AW_COPILOT_MIN_VERSION, got:\n%s", installStep)
	}
	if strings.Contains(installStep, "install_copilot_cli.sh\" "+string(constants.DefaultCopilotVersion)) {
		t.Errorf("Install step must not embed an explicit version arg when engine.version is unset; got:\n%s", installStep)
	}
}

func TestCopilotEngineWithWebSearchExpressionVersionInjectsMinVersion(t *testing.T) {
	engine := NewCopilotEngine()
	expressionVersion := "${{ inputs.engine-version }}"
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			Version: expressionVersion,
		},
		CompiledVersion: "v0.99.0",
		Tools: map[string]any{
			"web-search": nil,
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
	if !strings.Contains(installStep, "ENGINE_VERSION: "+expressionVersion) {
		t.Errorf("Expected ENGINE_VERSION env var with expression, got:\n%s", installStep)
	}
	if !strings.Contains(installStep, "GH_AW_COPILOT_MIN_VERSION: "+string(constants.CopilotWebSearchMinVersion)) {
		t.Errorf("Expected web-search workflow with expression engine.version to inject GH_AW_COPILOT_MIN_VERSION, got:\n%s", installStep)
	}
	if !strings.Contains(installStep, `"${ENGINE_VERSION}"`) {
		t.Errorf(`Expected install step to pass resolved ENGINE_VERSION to script, got:\n%s`, installStep)
	}
}

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
