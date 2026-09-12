//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestThreatDetectionInlineStepsDependencies(t *testing.T) {
	// Test that inline detection steps are generated when threat detection is enabled
	// and that safe-output jobs can check detection results via agent job outputs
	compiler := NewCompiler()

	data := &WorkflowData{
		Features: map[string]any{
			string(constants.GHAWDetectionFeatureFlag): false,
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	// Build inline detection steps
	steps := compiler.buildDetectionJobSteps(data)
	if steps == nil {
		t.Fatal("Expected inline detection steps to be created")
	}

	joined := strings.Join(steps, "")

	// Verify detection guard step exists (determines if detection should run)
	if !strings.Contains(joined, "detection_guard") {
		t.Error("Expected inline steps to include detection_guard step")
	}

	// Verify detection conclusion step exists (sets final detection outputs)
	if !strings.Contains(joined, "detection_conclusion") {
		t.Error("Expected inline steps to include detection_conclusion step")
	}

	// Verify the conclusion step references the parsing script (combined step)
	if !strings.Contains(joined, "parse_threat_detection_results.cjs") {
		t.Error("Expected inline steps to reference parse_threat_detection_results.cjs in combined conclusion step")
	}
}

func TestThreatDetectionStepsOrdering(t *testing.T) {
	compiler := NewCompiler()

	t.Run("pre-steps come before engine execution", func(t *testing.T) {
		data := &WorkflowData{
			Features: map[string]any{
				string(constants.GHAWDetectionFeatureFlag): false,
			},
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{
					Steps: []any{
						map[string]any{
							"name": "Custom Pre Scan",
							"run":  "echo 'Custom pre-scanning...'",
						},
					},
				},
			},
		}

		steps := compiler.buildDetectionJobSteps(data)

		if len(steps) == 0 {
			t.Fatal("Expected non-empty steps")
		}

		// Join all steps into a single string for easier verification
		stepsString := strings.Join(steps, "")

		// Find the positions of key steps
		preStepPos := strings.Index(stepsString, "Custom Pre Scan")
		setupStepPos := strings.Index(stepsString, "Setup threat detection")
		initializePos := strings.Index(stepsString, "Initialize detection execution evidence")
		engineStepPos := strings.Index(stepsString, "id: detection_agentic_execution")
		startedPos := -1
		if engineStepPos >= 0 {
			startedPos = strings.Index(stepsString[engineStepPos:], `"state":"started"`)
		}
		uploadStepPos := strings.Index(stepsString, "Upload threat detection log")

		// Verify all steps exist
		if preStepPos == -1 {
			t.Error("Expected to find 'Custom Pre Scan' step")
		}
		if setupStepPos == -1 {
			t.Error("Expected to find 'Setup threat detection' step")
		}
		if uploadStepPos == -1 {
			t.Error("Expected to find 'Upload threat detection log' step")
		}
		if initializePos < 0 || initializePos > preStepPos || engineStepPos < setupStepPos || startedPos < 0 {
			t.Error("Expected detection evidence to initialize before setup and start inside engine execution")
		}
		if !strings.Contains(stepsString[uploadStepPos:], "if: always()") || !strings.Contains(stepsString[uploadStepPos:], "/tmp/gh-aw/threat-detection/execution.json") {
			t.Error("Expected detection execution evidence to be uploaded after pre-execution failures")
		}
		if !strings.Contains(stepsString, "Parse and conclude threat detection") {
			t.Error("Expected to find 'Parse and conclude threat detection' step")
		}

		// Verify ordering: pre-steps should come before setup threat detection
		if preStepPos > setupStepPos {
			t.Errorf("Custom pre-steps should come before 'Setup threat detection'. Got pre-step at position %d, setup at position %d", preStepPos, setupStepPos)
		}

		// Verify ordering: pre-steps should come before upload and conclude
		if preStepPos > uploadStepPos {
			t.Errorf("Custom pre-steps should come before 'Upload threat detection log'. Got pre-step at position %d, upload at position %d", preStepPos, uploadStepPos)
		}
	})

	t.Run("post-steps come after engine execution and before upload", func(t *testing.T) {
		data := &WorkflowData{
			Features: map[string]any{
				string(constants.GHAWDetectionFeatureFlag): false,
			},
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{
					PostSteps: []any{
						map[string]any{
							"name": "Custom Post Scan",
							"run":  "echo 'Custom post-scanning...'",
						},
					},
				},
			},
		}

		steps := compiler.buildDetectionJobSteps(data)

		if len(steps) == 0 {
			t.Fatal("Expected non-empty steps")
		}

		stepsString := strings.Join(steps, "")

		postStepPos := strings.Index(stepsString, "Custom Post Scan")
		// Use the engine execution step ID as the stable marker for the engine step boundary
		engineStepPos := strings.Index(stepsString, "id: detection_agentic_execution")
		uploadStepPos := strings.Index(stepsString, "Upload threat detection log")
		concludeStepPos := strings.Index(stepsString, "Parse and conclude threat detection")

		if postStepPos == -1 {
			t.Error("Expected to find 'Custom Post Scan' step")
		}
		if engineStepPos == -1 {
			t.Error("Expected to find 'id: detection_agentic_execution' engine step")
		}
		if uploadStepPos == -1 {
			t.Error("Expected to find 'Upload threat detection log' step")
		}
		if concludeStepPos == -1 {
			t.Error("Expected to find 'Parse and conclude threat detection' step")
		}

		// Verify ordering: post-steps should come after the engine execution step
		if postStepPos < engineStepPos {
			t.Errorf("Custom post-steps should come after engine execution step. Got post-step at position %d, engine at position %d", postStepPos, engineStepPos)
		}
		if postStepPos > uploadStepPos {
			t.Errorf("Custom post-steps should come before 'Upload threat detection log'. Got post-step at position %d, upload at position %d", postStepPos, uploadStepPos)
		}
		if postStepPos > concludeStepPos {
			t.Errorf("Custom post-steps should come before 'Parse and conclude threat detection'. Got post-step at position %d, conclude at position %d", postStepPos, concludeStepPos)
		}
	})

	t.Run("pre-steps and post-steps both present in correct order", func(t *testing.T) {
		data := &WorkflowData{
			Features: map[string]any{
				string(constants.GHAWDetectionFeatureFlag): false,
			},
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{
					Steps: []any{
						map[string]any{
							"name": "Custom Pre Step",
							"run":  "echo 'pre'",
						},
					},
					PostSteps: []any{
						map[string]any{
							"name": "Custom Post Step",
							"run":  "echo 'post'",
						},
					},
				},
			},
		}

		steps := compiler.buildDetectionJobSteps(data)
		stepsString := strings.Join(steps, "")

		preStepPos := strings.Index(stepsString, "Custom Pre Step")
		postStepPos := strings.Index(stepsString, "Custom Post Step")
		engineStepPos := strings.Index(stepsString, "id: detection_agentic_execution")
		uploadStepPos := strings.Index(stepsString, "Upload threat detection log")

		if preStepPos == -1 {
			t.Error("Expected to find 'Custom Pre Step'")
		}
		if postStepPos == -1 {
			t.Error("Expected to find 'Custom Post Step'")
		}
		if engineStepPos == -1 {
			t.Error("Expected to find 'id: detection_agentic_execution' engine step")
		}

		// pre-steps before engine, post-steps after engine but before upload
		if preStepPos > engineStepPos {
			t.Errorf("Pre-steps should come before engine execution step. Got pre=%d, engine=%d", preStepPos, engineStepPos)
		}
		if postStepPos < engineStepPos {
			t.Errorf("Post-steps should come after engine execution step. Got post=%d, engine=%d", postStepPos, engineStepPos)
		}
		if postStepPos > uploadStepPos {
			t.Errorf("Post-steps should come before 'Upload threat detection log'. Got post=%d, upload=%d", postStepPos, uploadStepPos)
		}
		// pre-steps before post-steps
		if preStepPos > postStepPos {
			t.Errorf("Pre-steps should come before post-steps. Got pre=%d, post=%d", preStepPos, postStepPos)
		}
	})
}

func TestCustomThreatDetectionStepsGuardCondition(t *testing.T) {
	compiler := NewCompiler()

	t.Run("injects detection guard condition when no if: present", func(t *testing.T) {
		steps := []any{
			map[string]any{
				"name": "No If Step",
				"run":  "echo hello",
			},
		}
		result := compiler.buildCustomThreatDetectionSteps(steps)
		stepsStr := strings.Join(result, "")
		if !strings.Contains(stepsStr, detectionStepCondition) {
			t.Errorf("Expected detection guard condition to be injected, got:\n%s", stepsStr)
		}
	})

	t.Run("preserves user-provided if: condition", func(t *testing.T) {
		userCondition := "always()"
		steps := []any{
			map[string]any{
				"name": "User If Step",
				"if":   userCondition,
				"run":  "echo hello",
			},
		}
		result := compiler.buildCustomThreatDetectionSteps(steps)
		stepsStr := strings.Join(result, "")
		if strings.Contains(stepsStr, detectionStepCondition) {
			t.Error("Expected detection guard condition NOT to be injected when user provides if:")
		}
		if !strings.Contains(stepsStr, userCondition) {
			t.Errorf("Expected user if: condition %q to be preserved, got:\n%s", userCondition, stepsStr)
		}
	})
}

func TestBuildUploadDetectionLogStep(t *testing.T) {
	compiler := NewCompiler()

	// Test that upload detection log step is created with correct properties
	steps := compiler.buildUploadDetectionLogStep(&WorkflowData{})

	if len(steps) == 0 {
		t.Fatal("Expected non-empty steps for upload detection log")
	}

	// Join all steps into a single string for easier verification
	stepsString := strings.Join(steps, "")

	// Verify key components of the upload step
	expectedComponents := []string{
		"name: Upload threat detection log",
		"if: always()",
		"uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a",
		"name: " + constants.DetectionArtifactName.String(),
		"            /tmp/gh-aw/threat-detection/detection.log",
		"            /tmp/gh-aw/threat-detection/detection_usage.json",
		"            /tmp/gh-aw/threat-detection/detection_usage.jsonl",
		"if-no-files-found: ignore",
	}

	for _, expected := range expectedComponents {
		if !strings.Contains(stepsString, expected) {
			t.Errorf("Expected upload detection log step to contain %q, but it was not found.\nGenerated steps:\n%s", expected, stepsString)
		}
	}
}

func TestThreatDetectionStepsIncludeUpload(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		Features: map[string]any{
			string(constants.GHAWDetectionFeatureFlag): false,
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	steps := compiler.buildDetectionJobSteps(data)

	if len(steps) == 0 {
		t.Fatal("Expected non-empty steps")
	}

	// Join all steps into a single string for easier verification
	stepsString := strings.Join(steps, "")

	// Verify that the upload detection log step is included
	if !strings.Contains(stepsString, "Upload threat detection log") {
		t.Error("Expected inline detection steps to include upload detection log step")
	}

	if !strings.Contains(stepsString, "detection") {
		t.Error("Expected inline detection steps to include detection artifact name")
	}

	// Verify it ignores missing files
	if !strings.Contains(stepsString, "if-no-files-found: ignore") {
		t.Error("Expected upload step to have 'if-no-files-found: ignore'")
	}
}

func TestSetupScriptReferencesPromptFile(t *testing.T) {
	compiler := NewCompiler()

	// Test that the setup script requires the external .cjs file
	script := compiler.buildSetupScriptRequire()

	// Verify the script uses require to load setup_threat_detection.cjs
	if !strings.Contains(script, "require('"+SetupActionDestination+"/setup_threat_detection.cjs')") {
		t.Error("Expected setup script to require setup_threat_detection.cjs")
	}

	// Verify setupGlobals is called
	if !strings.Contains(script, "setupGlobals(core, github, context, exec, io, getOctokit)") {
		t.Error("Expected setup script to call setupGlobals")
	}

	// Verify main() is awaited without parameters (template is read from file)
	if !strings.Contains(script, "await main()") {
		t.Error("Expected setup script to await main() without parameters")
	}

	// Verify template content is NOT passed as parameter (now read from file)
	if strings.Contains(script, "templateContent") {
		t.Error("Expected setup script to NOT pass templateContent parameter (should read from file)")
	}
}

func TestBuildWorkflowContextEnvVarsExcludesMarkdown(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		Name:            "Test Workflow",
		Description:     "Test Description",
		MarkdownContent: "This should not be included",
	}

	envVars := compiler.buildWorkflowContextEnvVars(data)

	// Join all env vars into a single string for easier verification
	envVarsString := strings.Join(envVars, "")

	// Verify WORKFLOW_NAME and WORKFLOW_DESCRIPTION are present
	if !strings.Contains(envVarsString, "WORKFLOW_NAME:") {
		t.Error("Expected env vars to include WORKFLOW_NAME")
	}
	if !strings.Contains(envVarsString, "WORKFLOW_DESCRIPTION:") {
		t.Error("Expected env vars to include WORKFLOW_DESCRIPTION")
	}

	// Verify WORKFLOW_MARKDOWN is NOT present
	if strings.Contains(envVarsString, "WORKFLOW_MARKDOWN") {
		t.Error("Environment variables should not include WORKFLOW_MARKDOWN")
	}
}

// TestDetectionGuardStepCondition verifies that the inline detection guard step
// has the correct conditional logic to skip when there are no safe outputs and no patches
func TestDetectionGuardStepCondition(t *testing.T) {
	compiler := NewCompiler()

	// Build the detection guard step
	steps := compiler.buildDetectionGuardStep()

	if len(steps) == 0 {
		t.Fatal("Expected non-empty guard steps")
	}

	joined := strings.Join(steps, "")

	// Verify the guard step has the detection_guard ID
	if !strings.Contains(joined, "id: detection_guard") {
		t.Error("Expected guard step to have id 'detection_guard'")
	}

	// Verify the condition checks for output types
	if !strings.Contains(joined, "OUTPUT_TYPES") {
		t.Error("Expected guard step to check OUTPUT_TYPES")
	}

	// Verify the condition checks for has_patch
	if !strings.Contains(joined, "HAS_PATCH") {
		t.Error("Expected guard step to check HAS_PATCH")
	}

	// Verify it uses always() to run even after agent failure
	if !strings.Contains(joined, "if: always()") {
		t.Error("Expected guard step to use always() condition")
	}

	// Verify it sets run_detection output
	if !strings.Contains(joined, "run_detection=true") {
		t.Error("Expected guard step to set run_detection=true")
	}
	if !strings.Contains(joined, "run_detection=false") {
		t.Error("Expected guard step to set run_detection=false")
	}
}

func TestPrepareDetectionFilesStepInvokesSetupScript(t *testing.T) {
	compiler := NewCompiler()

	steps := compiler.buildPrepareDetectionFilesStep()
	if len(steps) == 0 {
		t.Fatal("Expected non-empty prepare detection files steps")
	}

	joined := strings.Join(steps, "")
	if !strings.Contains(joined, `bash "${RUNNER_TEMP}/gh-aw/actions/prepare_threat_detection_files.sh"`) {
		t.Error("Expected prepare step to invoke prepare_threat_detection_files.sh")
	}
}

func TestSetupThreatDetectionPromptSummarySuppressedOnExternalPath(t *testing.T) {
	// The setup step renders a prompt that the external detector never uses (threat-detect
	// renders and publishes its own prompt), so its step summary write must be suppressed to
	// avoid two different prompt blocks in a single detection run.
	compiler := NewCompiler()

	newData := func(features map[string]any) *WorkflowData {
		return &WorkflowData{
			AI:   "copilot",
			Name: "Test Workflow",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
			Features: features,
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeAWF,
				},
			},
		}
	}

	t.Run("external detector path sets the suppression flag", func(t *testing.T) {
		data := newData(map[string]any{string(constants.GHAWDetectionFeatureFlag): true})
		joined := strings.Join(compiler.buildThreatDetectionAnalysisStep(data), "")
		if !strings.Contains(joined, `GH_AW_DETECTION_SKIP_PROMPT_SUMMARY: "true"`) {
			t.Errorf("expected GH_AW_DETECTION_SKIP_PROMPT_SUMMARY on the external detector path\ngot:\n%s", joined)
		}
	})

	t.Run("inline path does not set the suppression flag", func(t *testing.T) {
		data := newData(map[string]any{string(constants.GHAWDetectionFeatureFlag): false})
		joined := strings.Join(compiler.buildThreatDetectionAnalysisStep(data), "")
		if strings.Contains(joined, "GH_AW_DETECTION_SKIP_PROMPT_SUMMARY") {
			t.Errorf("did not expect GH_AW_DETECTION_SKIP_PROMPT_SUMMARY on the inline path\ngot:\n%s", joined)
		}
	})
}
