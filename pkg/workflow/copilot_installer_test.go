//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestGenerateCopilotInstallerSteps(t *testing.T) {
	tests := []struct {
		name             string
		version          string
		stepName         string
		shouldContain    []string
		shouldNotContain []string
		// checkRunLine, when set, verifies the exact "run:" line present in the step.
		checkRunLine string
	}{
		{
			name:     "version without v prefix",
			version:  "0.0.369",
			stepName: "Install GitHub Copilot CLI",
			shouldContain: []string{
				"bash \"${RUNNER_TEMP}/gh-aw/actions/install_copilot_cli.sh\" 0.0.369",
				"name: Install GitHub Copilot CLI",
				"GH_HOST: github.com", // Must pin GH_HOST to prevent GHES workflow-level overrides
			},
			shouldNotContain: []string{
				"gh.io/copilot-install | sudo bash", // Should not pipe directly to bash
			},
		},
		{
			name:     "version with v prefix",
			version:  "v0.0.370",
			stepName: "Install GitHub Copilot CLI",
			shouldContain: []string{
				"bash \"${RUNNER_TEMP}/gh-aw/actions/install_copilot_cli.sh\" v0.0.370",
				"GH_HOST: github.com", // Must pin GH_HOST to prevent GHES workflow-level overrides
			},
			shouldNotContain: []string{
				"gh.io/copilot-install | sudo bash",
			},
		},
		{
			name:     "custom version",
			version:  "1.2.3",
			stepName: "Custom Install Step",
			shouldContain: []string{
				"bash \"${RUNNER_TEMP}/gh-aw/actions/install_copilot_cli.sh\" 1.2.3",
				"name: Custom Install Step",
				"GH_HOST: github.com", // Must pin GH_HOST to prevent GHES workflow-level overrides
			},
			shouldNotContain: []string{
				"gh.io/copilot-install | sudo bash",
			},
		},
		{
			// When no engine.version is set the step must NOT embed a hardcoded version arg.
			// Instead the script resolves the version at runtime via compat.json (priority 2)
			// or falls back to its baked-in DEFAULT_COPILOT_VERSION (priority 3).
			name:     "empty version defers to script (no explicit version arg)",
			version:  "",
			stepName: "Install GitHub Copilot CLI",
			shouldContain: []string{
				"install_copilot_cli.sh",
				"GH_HOST: github.com",
			},
			shouldNotContain: []string{
				// Must NOT hardcode the default version — that would bypass compat resolution.
				"install_copilot_cli.sh\" " + string(constants.DefaultCopilotVersion),
				"gh.io/copilot-install | sudo bash",
			},
			checkRunLine: `run: bash "${RUNNER_TEMP}/gh-aw/actions/install_copilot_cli.sh"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := GenerateCopilotInstallerSteps(tt.version, tt.stepName, false, "")

			if len(steps) != 1 {
				t.Errorf("Expected 1 step, got %d", len(steps))
				return
			}

			stepContent := strings.Join(steps[0], "\n")

			// Check expected content
			for _, expected := range tt.shouldContain {
				if !strings.Contains(stepContent, expected) {
					t.Errorf("Expected step to contain '%s', but it didn't.\nStep content:\n%s", expected, stepContent)
				}
			}

			// Check content that should not be present
			for _, notExpected := range tt.shouldNotContain {
				if strings.Contains(stepContent, notExpected) {
					t.Errorf("Expected step NOT to contain '%s', but it did.\nStep content:\n%s", notExpected, stepContent)
				}
			}

			if tt.checkRunLine != "" && !strings.Contains(stepContent, tt.checkRunLine) {
				t.Errorf("Expected step to contain run line %q, but step content was:\n%s", tt.checkRunLine, stepContent)
			}
		})
	}
}

func TestGenerateCopilotInstallerSteps_EmptyVersionWithCompiledVersion(t *testing.T) {
	// When engine.version is not set but a compiledVersion is available, the step must
	// include GH_AW_COMPILED_VERSION in its env so the script can resolve compat.json.
	compiledVersion := "v0.72.5"
	steps := GenerateCopilotInstallerSteps("", "Install GitHub Copilot CLI", false, compiledVersion)

	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}
	stepContent := strings.Join(steps[0], "\n")

	if !strings.Contains(stepContent, "GH_AW_COMPILED_VERSION: "+compiledVersion) {
		t.Errorf("Expected step to contain GH_AW_COMPILED_VERSION env var, got:\n%s", stepContent)
	}
	if strings.Contains(stepContent, "install_copilot_cli.sh\" ") {
		t.Errorf("Step must not embed an explicit version arg when version is empty, got:\n%s", stepContent)
	}
}

func TestGenerateCopilotInstallerSteps_EmptyVersionNoCompiledVersion(t *testing.T) {
	// When both version and compiledVersion are empty, GH_AW_COMPILED_VERSION must be
	// omitted so the script falls back to its baked-in DEFAULT_COPILOT_VERSION.
	steps := GenerateCopilotInstallerSteps("", "Install GitHub Copilot CLI", false, "")

	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}
	stepContent := strings.Join(steps[0], "\n")

	if strings.Contains(stepContent, "GH_AW_COMPILED_VERSION") {
		t.Errorf("Step must not contain GH_AW_COMPILED_VERSION when compiledVersion is empty, got:\n%s", stepContent)
	}
	if strings.Contains(stepContent, "install_copilot_cli.sh\" ") {
		t.Errorf("Step must not embed an explicit version arg when version is empty, got:\n%s", stepContent)
	}
}

func TestGenerateCopilotInstallerSteps_DoesNotRequireSystemRipgrep(t *testing.T) {
	steps := GenerateCopilotInstallerSteps("", "Install GitHub Copilot CLI", false, "v0.99.0")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}

	stepContent := strings.Join(steps[0], "\n")
	unexpected := []string{
		"install_ripgrep.sh",
		"Install ripgrep",
		"apt-get install -y ripgrep",
		"ripgrep not found; installing with apt-get",
	}
	for _, pattern := range unexpected {
		if strings.Contains(stepContent, pattern) {
			t.Errorf("Copilot install step must not require system ripgrep, found %q in:\n%s", pattern, stepContent)
		}
	}
}

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

func TestGenerateCopilotInstallerSteps_ExpressionVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		envVar  string
	}{
		{
			name:    "workflow_call input expression",
			version: "${{ inputs.engine-version }}",
			envVar:  "ENGINE_VERSION: ${{ inputs.engine-version }}",
		},
		{
			name:    "github event input expression",
			version: "${{ github.event.inputs.engine-version }}",
			envVar:  "ENGINE_VERSION: ${{ github.event.inputs.engine-version }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := GenerateCopilotInstallerSteps(tt.version, "Install GitHub Copilot CLI", false, "")

			if len(steps) != 1 {
				t.Errorf("Expected 1 step, got %d", len(steps))
				return
			}

			stepContent := strings.Join(steps[0], "\n")

			// Should use env var section
			if !strings.Contains(stepContent, "env:") {
				t.Errorf("Expected step to contain 'env:' section for expression version, got:\n%s", stepContent)
			}

			// Should define ENGINE_VERSION env var with the expression
			if !strings.Contains(stepContent, tt.envVar) {
				t.Errorf("Expected step to contain %q, got:\n%s", tt.envVar, stepContent)
			}

			// Should reference ENGINE_VERSION in the run command
			if !strings.Contains(stepContent, `"${ENGINE_VERSION}"`) {
				t.Errorf(`Expected step to use "$ENGINE_VERSION" in run command, got:\n%s`, stepContent)
			}

			// Should NOT embed the expression directly in the shell command
			if strings.Contains(stepContent, "install_copilot_cli.sh "+tt.version) {
				t.Errorf("Expression version should NOT be embedded directly in shell command, got:\n%s", stepContent)
			}
		})
	}
}

func TestGenerateCopilotInstallerSteps_Rootless(t *testing.T) {
	steps := GenerateCopilotInstallerSteps("1.2.3", "Install Copilot CLI", true, "")

	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}

	stepContent := strings.Join(steps[0], "\n")

	if !strings.Contains(stepContent, "--rootless") {
		t.Errorf("Expected step to contain --rootless flag, got:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "install_copilot_cli.sh") {
		t.Errorf("Expected step to use install_copilot_cli.sh, got:\n%s", stepContent)
	}
}

func TestGenerateCopilotInstallerSteps_RootlessWithExpression(t *testing.T) {
	steps := GenerateCopilotInstallerSteps("${{ inputs.version }}", "Install Copilot CLI", true, "")

	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}

	stepContent := strings.Join(steps[0], "\n")

	if !strings.Contains(stepContent, "--rootless") {
		t.Errorf("Expected step to contain --rootless flag, got:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, `"${ENGINE_VERSION}"`) {
		t.Errorf("Expected step to use ENGINE_VERSION env var, got:\n%s", stepContent)
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
