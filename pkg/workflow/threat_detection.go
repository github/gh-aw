package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var threatLog = logger.New("workflow:threat_detection")

// ThreatDetectionConfig holds configuration for threat detection in agent output
type ThreatDetectionConfig struct {
	Prompt         string        `yaml:"prompt,omitempty"`        // Additional custom prompt instructions to append
	Steps          []any         `yaml:"steps,omitempty"`         // Array of extra job steps
	EngineConfig   *EngineConfig `yaml:"engine-config,omitempty"` // Extended engine configuration for threat detection
	EngineDisabled bool          `yaml:"-"`                       // Internal flag: true when engine is explicitly set to false
}

// parseThreatDetectionConfig handles threat-detection configuration
func (c *Compiler) parseThreatDetectionConfig(outputMap map[string]any) *ThreatDetectionConfig {
	if configData, exists := outputMap["threat-detection"]; exists {
		threatLog.Print("Found threat-detection configuration")
		// Handle boolean values
		if boolVal, ok := configData.(bool); ok {
			if !boolVal {
				threatLog.Print("Threat detection explicitly disabled")
				// When explicitly disabled, return nil
				return nil
			}
			threatLog.Print("Threat detection enabled with default settings")
			// When enabled as boolean, return empty config
			return &ThreatDetectionConfig{}
		}

		// Handle object configuration
		if configMap, ok := configData.(map[string]any); ok {
			// Check for enabled field
			if enabled, exists := configMap["enabled"]; exists {
				if enabledBool, ok := enabled.(bool); ok {
					if !enabledBool {
						threatLog.Print("Threat detection disabled via enabled field")
						// When explicitly disabled, return nil
						return nil
					}
				}
			}

			// Build the config (enabled by default when object is provided)
			threatConfig := &ThreatDetectionConfig{}

			// Parse prompt field
			if prompt, exists := configMap["prompt"]; exists {
				if promptStr, ok := prompt.(string); ok {
					threatConfig.Prompt = promptStr
				}
			}

			// Parse steps field
			if steps, exists := configMap["steps"]; exists {
				if stepsArray, ok := steps.([]any); ok {
					threatConfig.Steps = stepsArray
				}
			}

			// Parse engine field (supports string, object, and boolean false formats)
			if engine, exists := configMap["engine"]; exists {
				// Handle boolean false to disable AI engine
				if engineBool, ok := engine.(bool); ok {
					if !engineBool {
						threatLog.Print("Threat detection AI engine disabled")
						// engine: false means no AI engine steps
						threatConfig.EngineConfig = nil
						threatConfig.EngineDisabled = true
					}
				} else if engineStr, ok := engine.(string); ok {
					threatLog.Printf("Threat detection engine set to: %s", engineStr)
					// Handle string format
					threatConfig.EngineConfig = &EngineConfig{ID: engineStr}
				} else if engineObj, ok := engine.(map[string]any); ok {
					threatLog.Print("Parsing threat detection engine configuration")
					// Handle object format - use extractEngineConfig logic
					_, engineConfig := c.ExtractEngineConfig(map[string]any{"engine": engineObj})
					threatConfig.EngineConfig = engineConfig
				}
			}

			threatLog.Printf("Threat detection configured with custom prompt: %v, custom steps: %v", threatConfig.Prompt != "", len(threatConfig.Steps) > 0)
			return threatConfig
		}
	}

	// Default behavior: enabled if any safe-outputs are configured
	threatLog.Print("Using default threat detection configuration")
	return &ThreatDetectionConfig{}
}

// buildThreatDetectionJob creates the detection job
func (c *Compiler) buildThreatDetectionJob(data *WorkflowData, mainJobName string) (*Job, error) {
	threatLog.Printf("Building threat detection job for main job: %s", mainJobName)
	if data.SafeOutputs == nil || data.SafeOutputs.ThreatDetection == nil {
		return nil, fmt.Errorf("threat detection is not enabled")
	}

	// Build steps using a more structured approach
	steps := c.buildThreatDetectionSteps(data, mainJobName)
	threatLog.Printf("Generated %d steps for threat detection job", len(steps))

	// Determine if checkout is needed (dev or script mode with actions checkout)
	needsContentsRead := (c.actionMode.IsDev() || c.actionMode.IsScript()) && len(c.generateCheckoutActionsFolder(data)) > 0
	if needsContentsRead {
		threatLog.Print("Detection job needs contents:read permission for checkout")
	}

	// Set permissions based on whether checkout is needed
	var permissions string
	if needsContentsRead {
		permissions = NewPermissionsContentsRead().RenderToYAML()
	} else {
		permissions = NewPermissionsEmpty().RenderToYAML()
	}

	// Generate agent concurrency configuration (same as main agent job)
	agentConcurrency := GenerateJobConcurrencyConfig(data)

	// Build conditional: detection should run when there are safe outputs OR when there's a patch
	// output_types != '' OR has_patch == 'true'
	hasOutputTypes := BuildComparison(
		BuildPropertyAccess(fmt.Sprintf("needs.%s.outputs.output_types", mainJobName)),
		"!=",
		BuildStringLiteral(""),
	)
	hasPatch := BuildComparison(
		BuildPropertyAccess(fmt.Sprintf("needs.%s.outputs.has_patch", mainJobName)),
		"==",
		BuildStringLiteral("true"),
	)
	condition := BuildDisjunction(false, hasOutputTypes, hasPatch)

	job := &Job{
		Name:           string(constants.DetectionJobName),
		If:             condition.Render(),
		RunsOn:         "runs-on: ubuntu-latest",
		Permissions:    permissions,
		Concurrency:    c.indentYAMLLines(agentConcurrency, "    "),
		TimeoutMinutes: 10,
		Steps:          steps,
		Needs:          []string{mainJobName},
		Outputs: map[string]string{
			"success": "${{ steps.parse_results.outputs.success }}",
		},
	}

	return job, nil
}

// buildThreatDetectionSteps builds the steps for the threat detection job
func (c *Compiler) buildThreatDetectionSteps(data *WorkflowData, mainJobName string) []string {
	var steps []string

	// Add setup action steps at the beginning of the job
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef != "" || c.actionMode.IsScript() {
		// For dev mode (local action path), checkout the actions folder first
		steps = append(steps, c.generateCheckoutActionsFolder(data)...)

		// Threat detection job doesn't need project support
		steps = append(steps, c.generateSetupStep(setupActionRef, SetupActionDestination, false)...)
	}

	// Step 1: Download agent artifacts
	steps = append(steps, c.buildDownloadArtifactStep(mainJobName)...)

	// Step 2: Echo agent outputs for debugging
	steps = append(steps, c.buildEchoAgentOutputsStep(mainJobName)...)

	// Step 3: Setup and run threat detection
	steps = append(steps, c.buildThreatDetectionAnalysisStep(data, mainJobName)...)

	// Step 4: Add custom steps if configured
	if len(data.SafeOutputs.ThreatDetection.Steps) > 0 {
		steps = append(steps, c.buildCustomThreatDetectionSteps(data.SafeOutputs.ThreatDetection.Steps)...)
	}

	// Step 5: Parse threat detection results (after custom steps)
	steps = append(steps, c.buildParsingStep()...)

	// Step 6: Upload detection log artifact
	steps = append(steps, c.buildUploadDetectionLogStep()...)

	return steps
}

// buildDownloadArtifactStep creates the artifact download step
// Downloads from unified agent-artifacts (contains prompt, patch, etc.) and separate agent-output
func (c *Compiler) buildDownloadArtifactStep(mainJobName string) []string {
	var steps []string

	// Download unified agent-artifacts (contains prompt, patch, logs, etc.)
	steps = append(steps, buildArtifactDownloadSteps(ArtifactDownloadConfig{
		ArtifactName: "agent-artifacts",
		DownloadPath: "/tmp/gh-aw/threat-detection/",
		SetupEnvStep: false,
		StepName:     "Download agent artifacts",
	})...)

	// Download agent output artifact (still separate)
	steps = append(steps, buildArtifactDownloadSteps(ArtifactDownloadConfig{
		ArtifactName: constants.AgentOutputArtifactName,
		DownloadPath: "/tmp/gh-aw/threat-detection/",
		SetupEnvStep: false,
		StepName:     "Download agent output artifact",
	})...)

	return steps
}

// buildEchoAgentOutputsStep creates a step that echoes the agent outputs
func (c *Compiler) buildEchoAgentOutputsStep(mainJobName string) []string {
	return []string{
		"      - name: Echo agent output types\n",
		"        env:\n",
		fmt.Sprintf("          AGENT_OUTPUT_TYPES: ${{ needs.%s.outputs.output_types }}\n", mainJobName),
		"        run: |\n",
		"          echo \"Agent output-types: $AGENT_OUTPUT_TYPES\"\n",
	}
}

// buildThreatDetectionAnalysisStep creates the main threat analysis step
func (c *Compiler) buildThreatDetectionAnalysisStep(data *WorkflowData, mainJobName string) []string {
	var steps []string

	// Setup step
	steps = append(steps, []string{
		"      - name: Setup threat detection\n",
		fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")),
		"        env:\n",
	}...)
	steps = append(steps, c.buildWorkflowContextEnvVars(data)...)

	// Add HAS_PATCH environment variable from agent job output
	steps = append(steps, fmt.Sprintf("          HAS_PATCH: ${{ needs.%s.outputs.has_patch }}\n", mainJobName))

	// Add custom prompt instructions if configured
	customPrompt := ""
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil {
		customPrompt = data.SafeOutputs.ThreatDetection.Prompt
	}
	if customPrompt != "" {
		steps = append(steps, fmt.Sprintf("          CUSTOM_PROMPT: %q\n", customPrompt))
	}

	steps = append(steps, []string{
		"        with:\n",
		"          script: |\n",
	}...)

	// Require the setup_threat_detection.cjs module and call main with the template
	setupScript := c.buildSetupScriptRequire()
	formattedSetupScript := FormatJavaScriptForYAML(setupScript)
	steps = append(steps, formattedSetupScript...)

	// Add a small shell step in YAML to ensure the output directory and log file exist
	steps = append(steps, []string{
		"      - name: Ensure threat-detection directory and log\n",
		"        run: |\n",
		"          mkdir -p /tmp/gh-aw/threat-detection\n",
		"          touch /tmp/gh-aw/threat-detection/detection.log\n",
	}...)

	// Add engine execution steps
	steps = append(steps, c.buildEngineSteps(data)...)

	return steps
}

// buildSetupScriptRequire creates the setup script that requires the .cjs module
func (c *Compiler) buildSetupScriptRequire() string {
	// Build a simple require statement that calls the main function
	// The template is now read from file at runtime by the JavaScript module
	script := `const { setupGlobals } = require('` + SetupActionDestination + `/setup_globals.cjs');
setupGlobals(core, github, context, exec, io);
const { main } = require('` + SetupActionDestination + `/setup_threat_detection.cjs');
await main();`

	return script
}

// buildEngineSteps creates the engine execution steps
func (c *Compiler) buildEngineSteps(data *WorkflowData) []string {
	// Check if threat detection has engine explicitly disabled
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil {
		if data.SafeOutputs.ThreatDetection.EngineDisabled {
			// Engine explicitly disabled with engine: false
			return []string{"      # AI engine disabled for threat detection (engine: false)\n"}
		}
	}

	// Determine which engine to use - threat detection engine if specified, otherwise main engine
	engineSetting := data.AI
	engineConfig := data.EngineConfig

	// Check if threat detection has its own engine configuration
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil {
		if data.SafeOutputs.ThreatDetection.EngineConfig != nil {
			engineConfig = data.SafeOutputs.ThreatDetection.EngineConfig
		}
	}

	// Use engine config ID if available
	if engineConfig != nil {
		engineSetting = engineConfig.ID
	}
	if engineSetting == "" {
		engineSetting = "claude"
	}

	// Get the engine instance
	engine, err := c.getAgenticEngine(engineSetting)
	if err != nil {
		// Return a fallback if engine not found
		return []string{"      # Engine not found, skipping execution\n"}
	}

	// Apply default detection model if the engine provides one and no model is specified
	// Detection models can be configured via:
	// 1. Explicit model in threat-detection engine config (highest priority)
	// 2. Engine-specific environment variables (e.g., GH_AW_MODEL_DETECTION_COPILOT)
	// 3. Default detection model from the engine (as environment variable fallback)
	detectionEngineConfig := engineConfig

	// Only copy the engine config if a model is explicitly configured
	// If no model is configured, we'll let the environment variable mechanism handle it
	modelExplicitlyConfigured := engineConfig != nil && engineConfig.Model != ""

	if modelExplicitlyConfigured {
		// Model is explicitly configured, use it as-is
		detectionEngineConfig = engineConfig
	} else {
		// No model configured - create/update config but don't set model
		// This allows the engine execution to use environment variables
		if detectionEngineConfig == nil {
			detectionEngineConfig = &EngineConfig{
				ID: engineSetting,
			}
		} else {
			// Create a copy without setting the model
			detectionEngineConfig = &EngineConfig{
				ID:          detectionEngineConfig.ID,
				Model:       "", // Explicitly leave empty for env var mechanism
				Version:     detectionEngineConfig.Version,
				MaxTurns:    detectionEngineConfig.MaxTurns,
				Concurrency: detectionEngineConfig.Concurrency,
				UserAgent:   detectionEngineConfig.UserAgent,
				Env:         detectionEngineConfig.Env,
				Steps:       detectionEngineConfig.Steps,
				Config:      detectionEngineConfig.Config,
				Args:        detectionEngineConfig.Args,
				Firewall:    detectionEngineConfig.Firewall,
			}
		}
	}

	// Create minimal WorkflowData for threat detection
	// Configure bash read tools for accessing the agent output file
	threatDetectionData := &WorkflowData{
		Tools: map[string]any{
			"bash": []any{"cat", "head", "tail", "wc", "grep", "ls", "jq"},
		},
		SafeOutputs:  nil,
		Network:      "",
		EngineConfig: detectionEngineConfig,
		AI:           engineSetting,
	}

	var steps []string

	// Add engine installation steps (includes Node.js setup for npm-based engines)
	installSteps := engine.GetInstallationSteps(threatDetectionData)
	for _, step := range installSteps {
		for _, line := range step {
			steps = append(steps, line+"\n")
		}
	}

	// Add engine execution steps
	logFile := "/tmp/gh-aw/threat-detection/detection.log"
	executionSteps := engine.GetExecutionSteps(threatDetectionData, logFile)
	for _, step := range executionSteps {
		for _, line := range step {
			steps = append(steps, line+"\n")
		}
	}

	return steps
}

// buildParsingStep creates the results parsing step
func (c *Compiler) buildParsingStep() []string {
	steps := []string{
		"      - name: Parse threat detection results\n",
		"        id: parse_results\n",
		fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")),
		"        with:\n",
		"          script: |\n",
	}

	// Use require() to load script from the separate .cjs file
	parsingScript := c.buildResultsParsingScriptRequire()
	formattedParsingScript := FormatJavaScriptForYAML(parsingScript)
	steps = append(steps, formattedParsingScript...)

	return steps
}

// buildWorkflowContextEnvVars creates environment variables for workflow context
func (c *Compiler) buildWorkflowContextEnvVars(data *WorkflowData) []string {
	workflowName := data.Name
	if workflowName == "" {
		workflowName = "Unnamed Workflow"
	}

	workflowDescription := data.Description
	if workflowDescription == "" {
		workflowDescription = "No description provided"
	}

	return []string{
		fmt.Sprintf("          WORKFLOW_NAME: %q\n", workflowName),
		fmt.Sprintf("          WORKFLOW_DESCRIPTION: %q\n", workflowDescription),
	}
}

// buildResultsParsingScriptRequire creates the parsing script that requires the .cjs module
func (c *Compiler) buildResultsParsingScriptRequire() string {
	// Build a simple require statement that calls the main function
	script := `const { setupGlobals } = require('` + SetupActionDestination + `/setup_globals.cjs');
setupGlobals(core, github, context, exec, io);
const { main } = require('` + SetupActionDestination + `/parse_threat_detection_results.cjs');
await main();`

	return script
}

// buildUploadDetectionLogStep creates the step to upload the detection log
func (c *Compiler) buildCustomThreatDetectionSteps(steps []any) []string {
	var result []string
	for _, step := range steps {
		if stepMap, ok := step.(map[string]any); ok {
			if stepYAML, err := c.convertStepToYAML(stepMap); err == nil {
				result = append(result, stepYAML)
			}
		}
	}
	return result
}

// buildUploadDetectionLogStep creates the step to upload the detection log
func (c *Compiler) buildUploadDetectionLogStep() []string {
	return []string{
		"      - name: Upload threat detection log\n",
		"        if: always()\n",
		fmt.Sprintf("        uses: %s\n", GetActionPin("actions/upload-artifact")),
		"        with:\n",
		"          name: threat-detection.log\n",
		"          path: /tmp/gh-aw/threat-detection/detection.log\n",
		"          if-no-files-found: ignore\n",
	}
}
