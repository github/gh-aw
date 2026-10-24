//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testEngineChecksumsPin = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testAWFChecksumsPin = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func TestDefaultCopilotReleaseChecksumPin(t *testing.T) {
	t.Parallel()
	for _, digest := range []string{"", "invalid", strings.Repeat("A", 64)} {
		err := validateReleaseManifestPin("compiler.default-copilot", string(constants.DefaultCopilotVersion), digest)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "compiler.default-copilot.checksums-sha256")
	}
	require.NoError(t, NewCompiler().validateReleaseIntegrityPins(&WorkflowData{}))
}

func TestDefaultCopilotReleaseChecksumPinsPropagateAcrossJobs(t *testing.T) {
	t.Parallel()
	for _, config := range []*EngineConfig{
		nil,
		{ID: "copilot"},
		{ID: "copilot", Version: string(constants.DefaultCopilotVersion)},
		{ID: "copilot", Version: "v" + string(constants.DefaultCopilotVersion)},
	} {
		data := &WorkflowData{
			Name: "default-release-pins", AI: "copilot", EngineConfig: config,
			SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
		}
		compiler := NewCompiler()
		var main strings.Builder
		for _, step := range NewCopilotEngine().GetInstallationSteps(data) {
			main.WriteString(strings.Join(step, "\n"))
		}
		for name, content := range map[string]string{
			"main":      main.String(),
			"detection": strings.Join(compiler.buildInstallDetectionEngineForExternalDetectorStep(data), ""),
			"evals":     strings.Join(compiler.buildEvalsEngineSteps(data), ""),
		} {
			assert.Contains(t, content, "--checksums-sha256 "+constants.DefaultCopilotChecksumsSHA256, name)
			assert.Contains(t, content, "--prefer-toolcache", name)
			assert.Contains(t, content, string(constants.DefaultCopilotVersion), name)
			assert.NotContains(t, content, "GH_AW_COMPILED_VERSION:", name)
		}
		assert.Same(t, config, data.EngineConfig)
	}
}

func TestCompileDefaultCopilotReleaseChecksumPin(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "default-release-pin.md")
	require.NoError(t, os.WriteFile(path, []byte(
		"---\non: workflow_dispatch\nstrict: false\nengine: copilot\n---\nInspect the repository.\n"), 0o644))
	require.NoError(t, NewCompiler(WithSkipValidation(true)).CompileWorkflow(path))
	result, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
	require.NoError(t, err)
	assert.Contains(t, string(result), `install_copilot_cli.sh" `+string(constants.DefaultCopilotVersion)+` --checksums-sha256 `+constants.DefaultCopilotChecksumsSHA256)
	assert.NotContains(t, string(result), "GH_AW_COMPILED_VERSION:")
}

func releasePinnedWorkflowData() *WorkflowData {
	return &WorkflowData{
		Name: "release-pins",
		AI:   "copilot",
		EngineConfig: &EngineConfig{
			ID: "copilot", Version: "1.0.99", ChecksumsSHA256: testEngineChecksumsPin,
		},
		SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{
			Type: SandboxTypeAWF, Version: "v0.28.63", ChecksumsSHA256: testAWFChecksumsPin,
		}},
		SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
	}
}

func TestReleaseChecksumPinExtraction(t *testing.T) {
	t.Parallel()
	compiler := NewCompiler()
	_, engine, _ := compiler.ExtractEngineConfig(map[string]any{"engine": map[string]any{
		"id": "copilot", "version": "1.0.99", "checksums-sha256": testEngineChecksumsPin,
	}})
	require.NotNil(t, engine)
	assert.Equal(t, testEngineChecksumsPin, engine.ChecksumsSHA256)
	agent := compiler.extractAgentSandboxConfig(map[string]any{
		"version": "v0.28.63", "checksums-sha256": testAWFChecksumsPin,
	})
	require.NotNil(t, agent)
	assert.Equal(t, testAWFChecksumsPin, agent.ChecksumsSHA256)
}

func TestValidateReleaseChecksumPins(t *testing.T) {
	t.Parallel()
	require.NoError(t, NewCompiler().validateReleaseIntegrityPins(releasePinnedWorkflowData()))
	defaultEngine := releasePinnedWorkflowData()
	defaultEngine.EngineConfig.ID = ""
	defaultEngine.SafeOutputs.ThreatDetection.EngineConfig = &EngineConfig{
		Version: "1.0.100", ChecksumsSHA256: testEngineChecksumsPin,
	}
	require.NoError(t, NewCompiler().validateReleaseIntegrityPins(defaultEngine), "default Copilot identities must accept pins")
	for _, test := range []struct {
		name, want string
		change     func(*WorkflowData)
	}{
		{"engine version absent", "engine.version", func(d *WorkflowData) { d.EngineConfig.Version = "" }},
		{"engine latest", "semantic release", func(d *WorkflowData) { d.EngineConfig.Version = "latest" }},
		{"engine expression", "semantic release", func(d *WorkflowData) { d.EngineConfig.Version = "${{ inputs.version }}" }},
		{"uppercase engine digest", "64 lowercase", func(d *WorkflowData) { d.EngineConfig.ChecksumsSHA256 = strings.Repeat("A", 64) }},
		{"digest expression", "literal SHA256", func(d *WorkflowData) { d.EngineConfig.ChecksumsSHA256 = "${{ inputs.pin }}" }},
		{"other engine", "Copilot CLI installer", func(d *WorkflowData) { d.EngineConfig.ID = "claude" }},
		{"custom engine command", "Copilot CLI installer", func(d *WorkflowData) { d.EngineConfig.Command = "my-copilot" }},
		{"AWF version absent", "sandbox.agent.version", func(d *WorkflowData) { d.SandboxConfig.Agent.Version = "" }},
		{"AWF latest", "semantic release", func(d *WorkflowData) { d.SandboxConfig.Agent.Version = "latest" }},
		{"AWF short digest", "64 lowercase", func(d *WorkflowData) { d.SandboxConfig.Agent.ChecksumsSHA256 = "abc" }},
		{"AWF custom command", "AWF installer", func(d *WorkflowData) { d.SandboxConfig.Agent.Command = "my-awf" }},
		{"AWF disabled", "AWF installer", func(d *WorkflowData) { d.SandboxConfig.Agent.Disabled = true }},
		{"detection version absent", "safe-outputs.threat-detection.engine.version", func(d *WorkflowData) {
			d.SafeOutputs.ThreatDetection.EngineConfig = &EngineConfig{ID: "copilot", ChecksumsSHA256: testEngineChecksumsPin}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := releasePinnedWorkflowData()
			test.change(data)
			err := NewCompiler().validateReleaseIntegrityPins(data)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.want)
		})
	}
}

func TestReleaseChecksumPinsPropagateAcrossJobs(t *testing.T) {
	t.Parallel()
	data := releasePinnedWorkflowData()
	compiler := NewCompiler()
	var main strings.Builder
	for _, step := range NewCopilotEngine().GetInstallationSteps(data) {
		main.WriteString(strings.Join(step, "\n"))
	}
	detection := strings.Join(compiler.buildInstallDetectionEngineForExternalDetectorStep(data), "")
	evals := strings.Join(compiler.buildEvalsEngineSteps(data), "")
	awfDetection := strings.Join(compiler.buildInstallAWFForExternalDetectorStep(data), "")
	for name, content := range map[string]string{"main": main.String(), "detection": detection, "evals": evals} {
		assert.Contains(t, content, "--checksums-sha256 "+testEngineChecksumsPin, name)
		assert.NotContains(t, content, "--prefer-toolcache", name)
	}
	for name, content := range map[string]string{"main": main.String(), "detection": awfDetection, "evals": evals} {
		assert.Contains(t, content, "--checksums-sha256 "+testAWFChecksumsPin, name)
	}
	assert.Equal(t, testEngineChecksumsPin, data.EngineConfig.ChecksumsSHA256)
	assert.Equal(t, testAWFChecksumsPin, data.SandboxConfig.Agent.ChecksumsSHA256)
}

func TestDetectionReleaseChecksumPinOverrides(t *testing.T) {
	t.Parallel()
	data := releasePinnedWorkflowData()
	data.SafeOutputs.ThreatDetection.EngineConfig = &EngineConfig{
		ID: "copilot", Version: "1.0.100", ChecksumsSHA256: testAWFChecksumsPin,
	}
	config := resolveExternalDetectorEngineConfig(data, "copilot")
	assert.Equal(t, "1.0.100", config.Version)
	assert.Equal(t, testAWFChecksumsPin, config.ChecksumsSHA256)
	for _, steps := range [][]string{
		NewCompiler().buildDetectionEngineExecutionStep(data),
		NewCompiler().buildInstallDetectionEngineForExternalDetectorStep(data),
	} {
		content := strings.Join(steps, "")
		assert.Contains(t, content, "--checksums-sha256 "+testAWFChecksumsPin)
		assert.NotContains(t, content, "--checksums-sha256 "+testEngineChecksumsPin)
	}
	cloned := cloneThreatDetectionEngineConfig("copilot", &EngineConfig{
		ID: "pi", Version: "1.2.3", ChecksumsSHA256: testEngineChecksumsPin,
	})
	assert.Empty(t, cloned.Version)
	assert.Empty(t, cloned.ChecksumsSHA256, "a pin must not cross engine identities without its version")
}

func TestCompileRejectsUnboundReleaseChecksumPin(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "unbound-release-pin.md")
	content := fmt.Sprintf("---\non: workflow_dispatch\nengine:\n  id: copilot\n  checksums-sha256: \"%s\"\n---\nInspect the repository.\n", testEngineChecksumsPin)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	err := NewCompiler(WithSkipValidation(true)).CompileWorkflow(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires an explicit semantic release version")
}

func TestCompileReleaseChecksumPins(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "release-pins.md")
	content := fmt.Sprintf(`---
on: workflow_dispatch
strict: false
engine:
  id: copilot
  version: "1.0.99"
  checksums-sha256: "%s"
sandbox:
  agent:
    version: v0.28.62
    checksums-sha256: "%s"
---
Inspect the repository.
`, testEngineChecksumsPin, testAWFChecksumsPin)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	require.NoError(t, NewCompiler(WithSkipValidation(true)).CompileWorkflow(path))
	compiled, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
	require.NoError(t, err)
	assert.Contains(t, string(compiled), `install_copilot_cli.sh" 1.0.99 --checksums-sha256 `+testEngineChecksumsPin)
	assert.Contains(t, string(compiled), `install_awf_binary.sh" v0.28.62 --checksums-sha256 `+testAWFChecksumsPin)
}
