package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var publishAssetsLog = logger.New("workflow:publish_assets")

// UploadAssetsConfig holds configuration for publishing assets to an orphaned git branch
type UploadAssetsConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	BranchName           string   `yaml:"branch,omitempty"`       // Branch name (default: "assets/${{ github.workflow }}")
	MaxSizeKB            int      `yaml:"max-size,omitempty"`     // Maximum file size in KB (default: 10240 = 10MB)
	AllowedExts          []string `yaml:"allowed-exts,omitempty"` // Allowed file extensions (default: common non-executable types)
}

// parseUploadAssetConfig handles upload-asset configuration
func (c *Compiler) parseUploadAssetConfig(outputMap map[string]any) *UploadAssetsConfig {
	if configData, exists := outputMap["upload-asset"]; exists {
		publishAssetsLog.Print("Parsing upload-asset configuration")
		config := &UploadAssetsConfig{
			BranchName: "assets/${{ github.workflow }}", // Default branch name
			MaxSizeKB:  10240,                           // Default 10MB
			AllowedExts: []string{
				// Default set of extensions as specified in problem statement
				".png",
				".jpg",
				".jpeg",
			},
		}

		if configMap, ok := configData.(map[string]any); ok {
			// Parse branch
			if branchName, exists := configMap["branch"]; exists {
				if branchNameStr, ok := branchName.(string); ok {
					config.BranchName = branchNameStr
				}
			}

			// Parse max-size
			if maxSize, exists := configMap["max-size"]; exists {
				if maxSizeInt, ok := parseIntValue(maxSize); ok && maxSizeInt > 0 {
					config.MaxSizeKB = maxSizeInt
				}
			}

			// Parse allowed-exts
			if allowedExts, exists := configMap["allowed-exts"]; exists {
				if allowedExtsArray, ok := allowedExts.([]any); ok {
					var extStrings []string
					for _, ext := range allowedExtsArray {
						if extStr, ok := ext.(string); ok {
							extStrings = append(extStrings, extStr)
						}
					}
					if len(extStrings) > 0 {
						config.AllowedExts = extStrings
					}
				}
			}

			// Parse common base fields with default max of 0 (no limit)
			c.parseBaseSafeOutputConfig(configMap, &config.BaseSafeOutputConfig, 0)
			publishAssetsLog.Printf("Parsed upload-asset config: branch=%s, max_size_kb=%d, allowed_exts=%d", config.BranchName, config.MaxSizeKB, len(config.AllowedExts))
		} else if configData == nil {
			// Handle null case: create config with defaults
			publishAssetsLog.Print("Using default upload-asset configuration")
			return config
		}

		return config
	}

	return nil
}

// buildUploadAssetsJob creates the publish_assets job
func (c *Compiler) buildUploadAssetsJob(data *WorkflowData, mainJobName string, threatDetectionEnabled bool) (*Job, error) {
	publishAssetsLog.Printf("Building upload_assets job: workflow=%s, main_job=%s, threat_detection=%v", data.Name, mainJobName, threatDetectionEnabled)

	if data.SafeOutputs == nil || data.SafeOutputs.UploadAssets == nil {
		return nil, fmt.Errorf("safe-outputs.upload-asset configuration is required")
	}

	var preSteps []string

	// Permission checks are now handled by the separate check_membership job
	// which is always created when needed (when activation job is created)

	// Add setup step to copy scripts
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef != "" || c.actionMode.IsScript() {
		// For dev mode (local action path), checkout the actions folder first
		preSteps = append(preSteps, c.generateCheckoutActionsFolder(data)...)

		// Publish assets job doesn't need project support
		preSteps = append(preSteps, c.generateSetupStep(setupActionRef, SetupActionDestination, false)...)
	}

	// Step 1: Checkout repository
	preSteps = buildCheckoutRepository(preSteps, c)

	// Step 2: Configure Git credentials
	preSteps = append(preSteps, c.generateGitConfigurationSteps()...)

	// Step 3: Download assets artifact if it exists
	preSteps = append(preSteps, "      - name: Download assets\n")
	preSteps = append(preSteps, "        continue-on-error: true\n") // Continue if no assets were uploaded
	preSteps = append(preSteps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/download-artifact")))
	preSteps = append(preSteps, "        with:\n")
	preSteps = append(preSteps, "          name: safe-outputs-assets\n")
	preSteps = append(preSteps, "          path: /tmp/gh-aw/safeoutputs/assets/\n")

	// Step 4: List files
	preSteps = append(preSteps, "      - name: List downloaded asset files\n")
	preSteps = append(preSteps, "        continue-on-error: true\n") // Continue if no assets were uploaded
	preSteps = append(preSteps, "        run: |\n")
	preSteps = append(preSteps, "          echo \"Downloaded asset files:\"\n")
	preSteps = append(preSteps, "          find /tmp/gh-aw/safeoutputs/assets/ -maxdepth 1 -ls\n")

	// Build custom environment variables specific to upload-assets
	var customEnvVars []string
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_ASSETS_BRANCH: %q\n", data.SafeOutputs.UploadAssets.BranchName))
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_ASSETS_MAX_SIZE_KB: %d\n", data.SafeOutputs.UploadAssets.MaxSizeKB))
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_ASSETS_ALLOWED_EXTS: %q\n", strings.Join(data.SafeOutputs.UploadAssets.AllowedExts, ",")))

	// Add standard environment variables (metadata + staged/target repo)
	customEnvVars = append(customEnvVars, c.buildStandardSafeOutputEnvVars(data, "")...) // No target repo for upload assets

	// Create outputs for the job
	outputs := map[string]string{
		"published_count": "${{ steps.upload_assets.outputs.published_count }}",
		"branch_name":     "${{ steps.upload_assets.outputs.branch_name }}",
	}

	// Build the job condition using expression tree
	jobCondition := BuildSafeOutputType("upload_asset")

	// Build job dependencies
	needs := []string{mainJobName}
	if threatDetectionEnabled {
		needs = append(needs, string(constants.DetectionJobName))
		publishAssetsLog.Printf("Added detection job dependency for upload_assets")
	}

	// Use the shared builder function to create the job
	return c.buildSafeOutputJob(data, SafeOutputJobConfig{
		JobName:       "upload_assets",
		StepName:      "Upload Assets to Orphaned Branch",
		StepID:        "upload_assets",
		ScriptName:    "upload_assets",
		MainJobName:   mainJobName,
		CustomEnvVars: customEnvVars,
		Script:        getUploadAssetsScript(),
		Permissions:   NewPermissionsContentsWrite(),
		Outputs:       outputs,
		Condition:     jobCondition,
		PreSteps:      preSteps,
		Token:         data.SafeOutputs.UploadAssets.GitHubToken,
		Needs:         needs,
	})
}

// generateSafeOutputsAssetsArtifactUpload generates a step to upload safe-outputs assets as a separate artifact
// This artifact is then downloaded by the upload_assets job to publish files to orphaned branches
func generateSafeOutputsAssetsArtifactUpload(builder *strings.Builder, data *WorkflowData) {
	if data.SafeOutputs == nil || data.SafeOutputs.UploadAssets == nil {
		return
	}

	publishAssetsLog.Print("Generating safe-outputs assets artifact upload step")

	builder.WriteString("      # Upload safe-outputs assets for upload_assets job\n")
	builder.WriteString("      - name: Upload safe-outputs assets\n")
	builder.WriteString("        if: always()\n")
	fmt.Fprintf(builder, "        uses: %s\n", GetActionPin("actions/upload-artifact"))
	builder.WriteString("        with:\n")
	builder.WriteString("          name: safe-outputs-assets\n")
	builder.WriteString("          path: /tmp/gh-aw/safeoutputs/assets/\n")
	builder.WriteString("          retention-days: 1\n")
	builder.WriteString("          if-no-files-found: ignore\n")
}
