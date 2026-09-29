package workflow

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var publishAssetsLog = logger.New("workflow:publish_assets")
var githubExpressionPattern = regexp.MustCompile(`(?s)^\$\{\{.*\}\}$`)

func isGitHubExpression(value string) bool {
	trimmed := strings.TrimSpace(value)
	return githubExpressionPattern.MatchString(trimmed)
}

func normalizeAllowedExtension(extension string) string {
	trimmed := strings.TrimSpace(extension)
	if trimmed == "" {
		return ""
	}
	if isGitHubExpression(trimmed) {
		return trimmed
	}
	normalized := strings.ToLower(trimmed)
	if !strings.HasPrefix(normalized, ".") {
		normalized = "." + normalized
	}
	return normalized
}

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
		// Explicit false disables upload-asset (e.g. when passed via import-inputs)
		if b, ok := configData.(bool); ok && !b {
			publishAssetsLog.Print("upload-asset explicitly set to false, skipping")
			return nil
		}
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
				if maxSizeInt, ok := typeutil.ParseIntValue(maxSize); ok && maxSizeInt > 0 {
					config.MaxSizeKB = maxSizeInt
				}
			}

			// Parse allowed-exts
			if allowedExts, exists := configMap["allowed-exts"]; exists {
				if allowedExtsArray, ok := allowedExts.([]any); ok {
					var extStrings []string
					seen := make(map[string]struct{})
					for _, ext := range allowedExtsArray {
						if extStr, ok := ext.(string); ok {
							normalized := normalizeAllowedExtension(extStr)
							if normalized == "" {
								continue
							}
							if _, exists := seen[normalized]; exists {
								continue
							}
							seen[normalized] = struct{}{}
							extStrings = append(extStrings, normalized)
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
		return nil, errors.New("safe-outputs.upload-asset configuration is required")
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
		// Publish assets job depends on the agent job; reuse its trace ID so all jobs share one OTLP trace
		publishTraceID := fmt.Sprintf("${{ needs.%s.outputs.setup-trace-id }}", constants.ActivationJobName)
		publishParentSpanID := setupParentSpanNeedsExpr(constants.ActivationJobName)
		preSteps = append(preSteps, c.generateSetupStep(data, setupActionRef, SetupActionDestination, false, publishTraceID, publishParentSpanID)...)
	}

	// Step 1: Checkout repository
	preSteps = buildCheckoutRepository(preSteps, c, "", "")

	// Step 2: Configure Git credentials
	preSteps = append(preSteps, c.generateGitConfigurationSteps()...)

	// Step 3: Download assets artifact if it exists.
	// In workflow_call context, use the per-invocation prefix from the agent job to match the uploaded artifact name.
	assetsArtifactPrefix := artifactPrefixExprForAgentDownstreamJob(data)
	preSteps = append(preSteps, "      - name: Download assets\n")
	preSteps = append(preSteps, "        continue-on-error: true\n") // Continue if no assets were uploaded
	preSteps = append(preSteps, fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/download-artifact")))
	preSteps = append(preSteps, "        with:\n")
	preSteps = append(preSteps, fmt.Sprintf("          name: %ssafe-outputs-assets\n", assetsArtifactPrefix))
	preSteps = append(preSteps, fmt.Sprintf("          path: %s\n", constants.TmpGhAwAssetsDirSlash))

	// Step 4: List files
	preSteps = append(preSteps, "      - name: List downloaded asset files\n")
	preSteps = append(preSteps, "        continue-on-error: true\n") // Continue if no assets were uploaded
	preSteps = append(preSteps, "        run: |\n")
	preSteps = append(preSteps, "          echo \"Downloaded asset files:\"\n")
	preSteps = append(preSteps, fmt.Sprintf("          find %s -maxdepth 1 -ls\n", constants.TmpGhAwAssetsDirSlash))

	// Build custom environment variables specific to upload-assets
	var customEnvVars []string
	// Single source of truth for the staged-assets directory: the consumer script
	// reads exactly the directory the download step above wrote to.
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_ASSETS_DIR: %q\n", constants.TmpGhAwAssetsDir))
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

	// Build the job condition using expression tree.
	// When detection is expression-controlled the detection job may be skipped at runtime.
	// Wrap the condition with always() + detection-passed so this job still runs when the
	// caller disabled threat detection for this invocation via the expression.
	jobCondition := BuildSafeOutputType("upload_asset")
	if IsConditionalDetection(data.SafeOutputs) {
		jobCondition = BuildAnd(
			BuildAnd(BuildFunctionCall("always"), BuildSafeOutputType("upload_asset")),
			buildDetectionPassedCondition(),
		)
	}

	// Build job dependencies — always include activation job for OTLP trace ID correlation
	needs := []string{mainJobName, string(constants.ActivationJobName)}

	// In dev mode the setup action is referenced via a local path (./actions/setup), so its
	// files live in the workspace. The upload_assets step does a git checkout to the assets
	// branch, which replaces the workspace content and removes the actions/setup directory.
	// Without restoring it, the runner's post-step for Setup Scripts would fail with
	// "Can't find 'action.yml', 'action.yaml' or 'Dockerfile' under .../actions/setup".
	// We add a restore checkout step (if: always()) after the main step so the post-step
	// can always find action.yml and complete its /tmp/gh-aw cleanup.
	var postSteps []string
	if c.actionMode.IsDev() {
		postSteps = append(postSteps, c.generateRestoreActionsSetupStep())
		publishAssetsLog.Print("Added restore actions folder step to upload_assets job (dev mode)")
	}

	// Use the shared builder function to create the job
	return c.buildSafeOutputJob(data, SafeOutputJobConfig{
		JobName:       "upload_assets",
		StepName:      "Push assets",
		StepID:        "upload_assets",
		ScriptName:    "upload_assets",
		MainJobName:   mainJobName,
		CustomEnvVars: customEnvVars,
		Script:        getUploadAssetsScript(),
		Permissions:   NewPermissionsContentsWrite(),
		Outputs:       outputs,
		Condition:     jobCondition,
		PreSteps:      preSteps,
		PostSteps:     postSteps,
		Token:         data.SafeOutputs.UploadAssets.GitHubToken,
		Needs:         needs,
	})
}

// generateSafeOutputsAssetsArtifactUpload generates a step to upload safe-outputs assets as a separate artifact.
// This artifact is then downloaded by the upload_assets job to publish files to orphaned branches.
// In workflow_call context, the artifact name is prefixed to avoid clashes.
// pinAction resolves the upload-artifact action reference; pass c.getActionPin from Compiler methods.
func generateSafeOutputsAssetsArtifactUpload(builder *strings.Builder, data *WorkflowData, pinAction func(string) string) {
	if data.SafeOutputs == nil || data.SafeOutputs.UploadAssets == nil {
		return
	}

	publishAssetsLog.Print("Generating safe-outputs assets artifact upload step")

	// In workflow_call context, apply the per-invocation prefix to avoid artifact name clashes.
	prefix := artifactPrefixExprForDownstreamJob(data)

	builder.WriteString("      # Upload safe-outputs assets for upload_assets job\n")
	builder.WriteString("      - name: Upload Safe Outputs Assets\n")
	builder.WriteString("        if: always() && steps.redact_secrets.outcome == 'success'\n")
	fmt.Fprintf(builder, "        uses: %s\n", pinAction("actions/upload-artifact"))
	builder.WriteString("        with:\n")
	fmt.Fprintf(builder, "          name: %ssafe-outputs-assets\n", prefix)
	builder.WriteString("          path: ${{ runner.temp }}/gh-aw/safeoutputs/assets/\n")
	builder.WriteString("          retention-days: 1\n")
	builder.WriteString("          if-no-files-found: ignore\n")
}
