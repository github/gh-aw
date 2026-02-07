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

	steps := compiler.buildThreatDetectionSteps(data, "agent")
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

// TestThreatDetectionSteps_IncludeBashReadTools verifies that bash read tools are configured
func TestThreatDetectionSteps_IncludeBashReadTools(t *testing.T) {
	compiler := createTestCompiler(t)
	data := createTestWorkflowData(t, &ThreatDetectionConfig{})

	steps := compiler.buildThreatDetectionSteps(data, "agent")
	stepsString := strings.Join(steps, "")

	// Verify bash tools are configured - check for the comments in the execution step
	expectedBashTools := []string{
		"Bash(cat)",
		"Bash(head)",
		"Bash(tail)",
		"Bash(wc)",
		"Bash(grep)",
		"Bash(ls)",
		"Bash(jq)",
	}

	for _, tool := range expectedBashTools {
		assert.Contains(t, stepsString, tool, "threat detection should include bash tool: %s", tool)
	}
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

// TestBuildDownloadArtifactStep_IncludesRequiredArtifacts tests artifact download step generation
func TestBuildDownloadArtifactStep_IncludesRequiredArtifacts(t *testing.T) {
	compiler := createTestCompiler(t)
	steps := compiler.buildDownloadArtifactStep("agent")
	stepsString := strings.Join(steps, "")

	tests := []struct {
		name      string
		substring string
		message   string
	}{
		{
			name:      "includes agent artifacts download step",
			substring: "Download agent artifacts",
			message:   "should include agent artifacts download step",
		},
		{
			name:      "includes agent output download step",
			substring: "Download agent output artifact",
			message:   "should include agent output download step",
		},
		{
			name:      "downloads agent-artifacts",
			substring: "agent-artifacts",
			message:   "should download agent-artifacts",
		},
		{
			name:      "downloads agent-output",
			substring: "agent-output",
			message:   "should download agent-output artifact",
		},
		{
			name:      "uses threat-detection directory",
			substring: "/tmp/gh-aw/threat-detection/",
			message:   "should download to threat-detection directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, stepsString, tt.substring, tt.message)
		})
	}
}

// TestBuildEchoAgentOutputsStep_GeneratesCorrectOutput tests agent outputs echo step generation
func TestBuildEchoAgentOutputsStep_GeneratesCorrectOutput(t *testing.T) {
	tests := []struct {
		name        string
		mainJobName string
		checkOutput func(*testing.T, string)
	}{
		{
			name:        "default agent job name",
			mainJobName: "agent",
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "Echo agent output types", "should have descriptive step name")
				assert.Contains(t, output, "AGENT_OUTPUT_TYPES: ${{ needs.agent.outputs.output_types }}", "should reference agent job outputs")
				assert.Contains(t, output, "echo \"Agent output-types: $AGENT_OUTPUT_TYPES\"", "should echo the output types")
			},
		},
		{
			name:        "custom job name",
			mainJobName: "custom_agent",
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "needs.custom_agent.outputs.output_types", "should reference custom agent job outputs")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := createTestCompiler(t)
			steps := compiler.buildEchoAgentOutputsStep(tt.mainJobName)
			stepsString := strings.Join(steps, "")

			tt.checkOutput(t, stepsString)
		})
	}
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
				assert.Contains(t, stepsString, "HAS_PATCH: ${{ needs.agent.outputs.has_patch }}", "should include HAS_PATCH env var from agent job outputs")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := createTestCompiler(t)
			steps := compiler.buildThreatDetectionAnalysisStep(tt.data, "agent")
			stepsString := strings.Join(steps, "")

			tt.checkStep(t, stepsString)
		})
	}
}
