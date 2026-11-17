//go:build !integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper functions for test data creation

// createTestCompiler creates a new compiler for testing
func createTestCompiler(t *testing.T) *Compiler {
	t.Helper()
	return NewCompiler()
}

// createTestWorkflowData creates test workflow data with threat detection config
func createTestWorkflowData(t *testing.T, threatConfig *ThreatDetectionConfig) *WorkflowData {
	t.Helper()
	return &WorkflowData{
		Name:            "Test Workflow",
		Description:     "Test Description",
		MarkdownContent: "Test markdown content",
		Features: map[string]any{
			"gh-aw-detection": false,
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: threatConfig,
		},
	}
}

// TestThreatDetectionSteps_UseFilePathReferences verifies that threat detection
// uses file path references instead of inline content
func TestThreatDetectionSteps_UseFilePathReferences(t *testing.T) {
	compiler := createTestCompiler(t)
	data := createTestWorkflowData(t, &ThreatDetectionConfig{})

	steps := compiler.buildDetectionJobSteps(data)
	stepsString := strings.Join(steps, "")

	tests := []struct {
		name        string
		shouldExist bool
		substring   string
		message     string
	}{
		{
			name:        "requires setup script",
			shouldExist: true,
			substring:   "setup_threat_detection.cjs",
			message:     "threat detection steps should include setup_threat_detection.cjs for script execution",
		},
		{
			name:        "no inline template",
			shouldExist: false,
			substring:   "const templateContent = `# Threat Detection Analysis",
			message:     "threat detection should read template from file, not pass it inline",
		},
		{
			name:        "calls main without parameters",
			shouldExist: true,
			substring:   "await main()",
			message:     "threat detection should call main function without parameters",
		},
		{
			name:        "no environment variable for agent output",
			shouldExist: false,
			substring:   "AGENT_OUTPUT: ${{ needs.agent.outputs.output }}",
			message:     "threat detection should not pass agent output via environment variable to avoid CLI argument length overflow",
		},
		{
			name:        "no old AGENT_OUTPUT replacement",
			shouldExist: false,
			substring:   ".replace(/{AGENT_OUTPUT}/g, process.env.AGENT_OUTPUT",
			message:     "threat detection should not replace {AGENT_OUTPUT} with environment variable content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shouldExist {
				assert.Contains(t, stepsString, tt.substring, tt.message)
			} else {
				assert.NotContains(t, stepsString, tt.substring, tt.message)
			}
		})
	}
}

// TestThreatDetectionSteps_IncludeBashReadTools verifies that bash is configured as an
// unrestricted tool in the detection job. The detection job uses bash: ["*"] so the
// compiled steps reference "Bash" (no per-command restriction).
func TestThreatDetectionSteps_IncludeBashReadTools(t *testing.T) {
	compiler := createTestCompiler(t)
	data := createTestWorkflowData(t, &ThreatDetectionConfig{})

	steps := compiler.buildDetectionJobSteps(data)
	stepsString := strings.Join(steps, "")
	assert.Contains(t, stepsString, "Bash", "threat detection should include unrestricted Bash tool")
}

// TestThreatDetectionTemplate_UsesFilePathPlaceholder verifies the template markdown uses file path
func TestThreatDetectionTemplate_UsesFilePathPlaceholder(t *testing.T) {
	// Read the template file from actions/setup/md/threat_detection.md
	templatePath := "../../actions/setup/md/threat_detection.md"
	data, err := os.ReadFile(templatePath)
	require.NoError(t, err, "should read threat detection template file")

	templateContent := string(data)

	tests := []struct {
		name        string
		shouldExist bool
		substring   string
		message     string
	}{
		{
			name:        "has Agent Output File section",
			shouldExist: true,
			substring:   "Agent Output File",
			message:     "template should have 'Agent Output File' section",
		},
		{
			name:        "uses AGENT_OUTPUT_FILE placeholder",
			shouldExist: true,
			substring:   "{AGENT_OUTPUT_FILE}",
			message:     "template should use {AGENT_OUTPUT_FILE} placeholder for file path reference",
		},
		{
			name:        "instructs to read file",
			shouldExist: true,
			substring:   "Read and analyze this file",
			message:     "template should instruct agent to read the file",
		},
		{
			name:        "no old AGENT_OUTPUT placeholder",
			shouldExist: false,
			substring:   "{AGENT_OUTPUT}",
			message:     "template should not use old {AGENT_OUTPUT} placeholder for inline content",
		},
		{
			name:        "no agent-output tag",
			shouldExist: false,
			substring:   "<agent-output>",
			message:     "template should not have <agent-output> tag for inline content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shouldExist {
				assert.Contains(t, templateContent, tt.substring, tt.message)
			} else {
				assert.NotContains(t, templateContent, tt.substring, tt.message)
			}
		})
	}
}

func TestThreatDetectionTemplate_IncludesNpmValidationGuidance(t *testing.T) {
	templatePath := "../../actions/setup/md/threat_detection.md"
	data, err := os.ReadFile(templatePath)
	require.NoError(t, err, "should read threat detection template file")

	templateContent := string(data)
	assert.Contains(t, templateContent, "npm Lockfile Validation", "template should include npm lockfile validation guidance")
	assert.Contains(t, templateContent, "npm view <package>@<version> --json", "template should document how to fetch current npm metadata")
	assert.Contains(t, templateContent, "Confirm the package version exists in the npm registry", "template should require verifying npm package existence")
	assert.Contains(t, templateContent, "Confirm `resolved` matches npm's official tarball URL for that exact version", "template should require validating resolved tarball URL")
	assert.Contains(t, templateContent, "Confirm `integrity` matches npm's official integrity hash for that exact version", "template should require validating integrity hash")
	assert.Contains(t, templateContent, "Treat stale-version claims as false positives", "template should avoid stale metadata false positives")
	assert.Contains(t, templateContent, "report the verification as unavailable", "unavailable registry metadata must not be treated as evidence")
}

// TestBuildThreatDetectionAnalysisStep_ConfiguresEnvironment tests threat analysis step generation
func TestBuildThreatDetectionAnalysisStep_ConfiguresEnvironment(t *testing.T) {
	tests := []struct {
		name      string
		data      *WorkflowData
		checkStep func(*testing.T, string)
	}{
		{
			name: "basic threat detection without custom prompt",
			data: createTestWorkflowData(t, &ThreatDetectionConfig{}),
			checkStep: func(t *testing.T, stepsString string) {
				assert.Contains(t, stepsString, "Setup threat detection", "should have setup step")
				assert.Contains(t, stepsString, "setup_threat_detection.cjs", "should require setup script")
				assert.Contains(t, stepsString, "await main()", "should call main function")
				assert.NotContains(t, stepsString, "CUSTOM_PROMPT:", "should not have custom prompt env var")
			},
		},
		{
			name: "with custom prompt",
			data: createTestWorkflowData(t, &ThreatDetectionConfig{
				Prompt: "Focus on SQL injection vulnerabilities",
			}),
			checkStep: func(t *testing.T, stepsString string) {
				assert.Contains(t, stepsString, "CUSTOM_PROMPT: \"Focus on SQL injection vulnerabilities\"", "should include custom prompt")
			},
		},
		{
			name: "includes workflow context env vars",
			data: createTestWorkflowData(t, &ThreatDetectionConfig{}),
			checkStep: func(t *testing.T, stepsString string) {
				assert.Contains(t, stepsString, "WORKFLOW_NAME: \"Test Workflow\"", "should include workflow name")
				assert.Contains(t, stepsString, "WORKFLOW_DESCRIPTION: \"Test Description\"", "should include workflow description")
			},
		},
		{
			name: "includes HAS_PATCH env var",
			data: createTestWorkflowData(t, &ThreatDetectionConfig{}),
			checkStep: func(t *testing.T, stepsString string) {
				assert.Contains(t, stepsString, "HAS_PATCH: ${{ needs.agent.outputs.has_patch }}", "should include HAS_PATCH env var from agent job output")
			},
		},
		{
			name: "includes GH_AW_DETECTION_CONTINUE_ON_ERROR env var with default (true)",
			data: createTestWorkflowData(t, &ThreatDetectionConfig{}),
			checkStep: func(t *testing.T, stepsString string) {
				assert.Contains(t, stepsString, "GH_AW_DETECTION_CONTINUE_ON_ERROR:", "should include GH_AW_DETECTION_CONTINUE_ON_ERROR env var in setup step")
				assert.Contains(t, stepsString, `GH_AW_DETECTION_CONTINUE_ON_ERROR: "true"`, "default should be true")
			},
		},
		{
			name: "includes GH_AW_DETECTION_CONTINUE_ON_ERROR env var when strict mode",
			data: func() *WorkflowData {
				f := false
				return createTestWorkflowData(t, &ThreatDetectionConfig{ContinueOnError: &f})
			}(),
			checkStep: func(t *testing.T, stepsString string) {
				assert.Contains(t, stepsString, `GH_AW_DETECTION_CONTINUE_ON_ERROR: "false"`, "strict mode should emit false")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := createTestCompiler(t)
			steps := compiler.buildThreatDetectionAnalysisStep(tt.data)
			stepsString := strings.Join(steps, "")

			tt.checkStep(t, stepsString)
		})
	}
}
