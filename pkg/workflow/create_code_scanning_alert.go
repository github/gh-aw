package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var createCodeScanningAlertLog = logger.New("workflow:create_code_scanning_alert")

// CreateCodeScanningAlertsConfig holds configuration for creating repository security advisories (SARIF format) from agent output
type CreateCodeScanningAlertsConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Driver               string `yaml:"driver,omitempty"` // Driver name for SARIF tool.driver.name field (default: "GitHub Agentic Workflows Security Scanner")
}

// buildCreateOutputCodeScanningAlertJob creates the create_code_scanning_alert job
func (c *Compiler) buildCreateOutputCodeScanningAlertJob(data *WorkflowData, mainJobName string, workflowFilename string) (*Job, error) {
	if data.SafeOutputs == nil || data.SafeOutputs.CreateCodeScanningAlerts == nil {
		return nil, fmt.Errorf("safe-outputs.create-code-scanning-alert configuration is required")
	}

	// Build custom environment variables specific to create-code-scanning-alert
	var customEnvVars []string
	if data.SafeOutputs.CreateCodeScanningAlerts.Max > 0 {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_SECURITY_REPORT_MAX: %d\n", data.SafeOutputs.CreateCodeScanningAlerts.Max))
	}
	// Pass the driver configuration, defaulting to frontmatter name
	driverName := data.SafeOutputs.CreateCodeScanningAlerts.Driver
	if driverName == "" {
		if data.FrontmatterName != "" {
			driverName = data.FrontmatterName
		} else {
			driverName = data.Name // fallback to H1 header name
		}
	}
	createCodeScanningAlertLog.Printf("Building create_code_scanning_alert job: driver=%s, max=%d", driverName, data.SafeOutputs.CreateCodeScanningAlerts.Max)
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_SECURITY_REPORT_DRIVER: %s\n", driverName))
	// Pass the workflow filename for rule ID prefix
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_WORKFLOW_FILENAME: %s\n", workflowFilename))

	// Add workflow metadata (name, source, and tracker-id) for consistency
	customEnvVars = append(customEnvVars, buildWorkflowMetadataEnvVarsWithTrackerID(data.Name, data.Source, data.TrackerID)...)

	// Build post-steps for SARIF artifact upload
	var postSteps []string
	// Add step to upload SARIF artifact
	postSteps = append(postSteps, "      - name: Upload SARIF artifact\n")
	postSteps = append(postSteps, "        if: steps.create_code_scanning_alert.outputs.sarif_file\n")
	postSteps = append(postSteps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/upload-artifact")))
	postSteps = append(postSteps, "        with:\n")
	postSteps = append(postSteps, "          name: code-scanning-alert.sarif\n")
	postSteps = append(postSteps, "          path: ${{ steps.create_code_scanning_alert.outputs.sarif_file }}\n")

	// Add step to upload SARIF to GitHub Code Scanning
	postSteps = append(postSteps, "      - name: Upload SARIF to GitHub Security\n")
	postSteps = append(postSteps, "        if: steps.create_code_scanning_alert.outputs.sarif_file\n")
	postSteps = append(postSteps, fmt.Sprintf("        uses: %s\n", GetActionPin("github/codeql-action/upload-sarif")))
	postSteps = append(postSteps, "        with:\n")
	postSteps = append(postSteps, "          sarif_file: ${{ steps.create_code_scanning_alert.outputs.sarif_file }}\n")

	// Create outputs for the job
	outputs := map[string]string{
		"sarif_file":        "${{ steps.create_code_scanning_alert.outputs.sarif_file }}",
		"findings_count":    "${{ steps.create_code_scanning_alert.outputs.findings_count }}",
		"artifact_uploaded": "${{ steps.create_code_scanning_alert.outputs.artifact_uploaded }}",
		"codeql_uploaded":   "${{ steps.create_code_scanning_alert.outputs.codeql_uploaded }}",
	}

	jobCondition := BuildSafeOutputType("create_code_scanning_alert")

	// Use the shared builder function to create the job
	return c.buildSafeOutputJob(data, SafeOutputJobConfig{
		JobName:       "create_code_scanning_alert",
		StepName:      "Create Code Scanning Alert",
		StepID:        "create_code_scanning_alert",
		MainJobName:   mainJobName,
		CustomEnvVars: customEnvVars,
		Script:        getCreateCodeScanningAlertScript(),
		Permissions:   NewPermissionsContentsReadSecurityEventsWriteActionsRead(),
		Outputs:       outputs,
		Condition:     jobCondition,
		PostSteps:     postSteps,
		Token:         data.SafeOutputs.CreateCodeScanningAlerts.GitHubToken,
	})
}

// parseCodeScanningAlertsConfig handles create-code-scanning-alert configuration
func (c *Compiler) parseCodeScanningAlertsConfig(outputMap map[string]any) *CreateCodeScanningAlertsConfig {
	if _, exists := outputMap["create-code-scanning-alert"]; !exists {
		return nil
	}

	createCodeScanningAlertLog.Print("Parsing create-code-scanning-alert configuration")
	configData := outputMap["create-code-scanning-alert"]
	securityReportsConfig := &CreateCodeScanningAlertsConfig{}

	if configMap, ok := configData.(map[string]any); ok {
		// Parse driver
		if driver, exists := configMap["driver"]; exists {
			if driverStr, ok := driver.(string); ok {
				securityReportsConfig.Driver = driverStr
			}
		}

		// Parse common base fields with default max of 0 (unlimited)
		c.parseBaseSafeOutputConfig(configMap, &securityReportsConfig.BaseSafeOutputConfig, 0)
	} else {
		// If configData is nil or not a map (e.g., "create-code-scanning-alert:" with no value),
		// still set the default max (0 = unlimited)
		securityReportsConfig.Max = 0
	}

	return securityReportsConfig
}
