package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/logger"
)

var safeOutputsEnvLog = logger.New("workflow:safe_outputs_env")

// ========================================
// Safe Output Environment Variables
// ========================================

// applySafeOutputEnvToMap adds safe-output related environment variables to an env map
// This extracts the duplicated safe-output env setup logic across all engines (copilot, codex, claude, custom)
func applySafeOutputEnvToMap(env map[string]string, data *WorkflowData) {
	if data.SafeOutputs == nil {
		return
	}

	env["GH_AW_SAFE_OUTPUTS"] = "${{ env.GH_AW_SAFE_OUTPUTS }}"

	// Add staged flag if specified
	if data.TrialMode || data.SafeOutputs.Staged {
		env["GH_AW_SAFE_OUTPUTS_STAGED"] = "true"
	}
	if data.TrialMode && data.TrialLogicalRepo != "" {
		env["GH_AW_TARGET_REPO_SLUG"] = data.TrialLogicalRepo
	}

	// Add branch name if upload assets is configured
	if data.SafeOutputs.UploadAssets != nil {
		env["GH_AW_ASSETS_BRANCH"] = fmt.Sprintf("%q", data.SafeOutputs.UploadAssets.BranchName)
		env["GH_AW_ASSETS_MAX_SIZE_KB"] = fmt.Sprintf("%d", data.SafeOutputs.UploadAssets.MaxSizeKB)
		env["GH_AW_ASSETS_ALLOWED_EXTS"] = fmt.Sprintf("%q", strings.Join(data.SafeOutputs.UploadAssets.AllowedExts, ","))
	}
}

// applySafeOutputEnvToSlice adds safe-output related environment variables to a YAML string slice
// This is for engines that build YAML line-by-line (like Claude)
func applySafeOutputEnvToSlice(stepLines *[]string, workflowData *WorkflowData) {
	if workflowData.SafeOutputs == nil {
		return
	}

	*stepLines = append(*stepLines, "          GH_AW_SAFE_OUTPUTS: ${{ env.GH_AW_SAFE_OUTPUTS }}")

	// Add staged flag if specified
	if workflowData.TrialMode || workflowData.SafeOutputs.Staged {
		*stepLines = append(*stepLines, "          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"")
	}
	if workflowData.TrialMode && workflowData.TrialLogicalRepo != "" {
		*stepLines = append(*stepLines, fmt.Sprintf("          GH_AW_TARGET_REPO_SLUG: %q", workflowData.TrialLogicalRepo))
	}

	// Add branch name if upload assets is configured
	if workflowData.SafeOutputs.UploadAssets != nil {
		*stepLines = append(*stepLines, fmt.Sprintf("          GH_AW_ASSETS_BRANCH: %q", workflowData.SafeOutputs.UploadAssets.BranchName))
		*stepLines = append(*stepLines, fmt.Sprintf("          GH_AW_ASSETS_MAX_SIZE_KB: %d", workflowData.SafeOutputs.UploadAssets.MaxSizeKB))
		*stepLines = append(*stepLines, fmt.Sprintf("          GH_AW_ASSETS_ALLOWED_EXTS: %q", strings.Join(workflowData.SafeOutputs.UploadAssets.AllowedExts, ",")))
	}
}

// buildWorkflowMetadataEnvVars builds workflow name and source environment variables
// This extracts the duplicated workflow metadata setup logic from safe-output job builders
func buildWorkflowMetadataEnvVars(workflowName string, workflowSource string) []string {
	var customEnvVars []string

	// Add workflow name
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_WORKFLOW_NAME: %q\n", workflowName))

	// Add workflow source and source URL if present
	if workflowSource != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_WORKFLOW_SOURCE: %q\n", workflowSource))
		sourceURL := buildSourceURL(workflowSource)
		if sourceURL != "" {
			customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_WORKFLOW_SOURCE_URL: %q\n", sourceURL))
		}
	}

	return customEnvVars
}

// buildWorkflowMetadataEnvVarsWithTrackerID builds workflow metadata env vars including tracker-id
func buildWorkflowMetadataEnvVarsWithTrackerID(workflowName string, workflowSource string, trackerID string) []string {
	customEnvVars := buildWorkflowMetadataEnvVars(workflowName, workflowSource)

	// Add tracker-id if present
	if trackerID != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_TRACKER_ID: %q\n", trackerID))
	}

	return customEnvVars
}

// buildSafeOutputJobEnvVars builds environment variables for safe-output jobs with staged/target repo handling
// This extracts the duplicated env setup logic in safe-output job builders (create_issue, add_comment, etc.)
func buildSafeOutputJobEnvVars(trialMode bool, trialLogicalRepoSlug string, staged bool, targetRepoSlug string) []string {
	var customEnvVars []string

	// Pass the staged flag if it's set to true
	if trialMode || staged {
		customEnvVars = append(customEnvVars, "          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n")
	}

	// Set GH_AW_TARGET_REPO_SLUG - prefer target-repo config over trial target repo
	if targetRepoSlug != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_TARGET_REPO_SLUG: %q\n", targetRepoSlug))
	} else if trialMode && trialLogicalRepoSlug != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_TARGET_REPO_SLUG: %q\n", trialLogicalRepoSlug))
	}

	return customEnvVars
}

// buildStandardSafeOutputEnvVars builds the standard set of environment variables
// that all safe-output job builders need: metadata + staged/target repo handling
// This reduces duplication in safe-output job builders
func (c *Compiler) buildStandardSafeOutputEnvVars(data *WorkflowData, targetRepoSlug string) []string {
	var customEnvVars []string

	// Add workflow metadata (name, source, and tracker-id)
	customEnvVars = append(customEnvVars, buildWorkflowMetadataEnvVarsWithTrackerID(data.Name, data.Source, data.TrackerID)...)

	// Add engine metadata (id, version, model) for XML comment marker
	customEnvVars = append(customEnvVars, buildEngineMetadataEnvVars(data.EngineConfig)...)

	// Add common safe output job environment variables (staged/target repo)
	customEnvVars = append(customEnvVars, buildSafeOutputJobEnvVars(
		c.trialMode,
		c.trialLogicalRepoSlug,
		data.SafeOutputs.Staged,
		targetRepoSlug,
	)...)

	// Add messages config if present
	if data.SafeOutputs.Messages != nil {
		messagesJSON, err := serializeMessagesConfig(data.SafeOutputs.Messages)
		if err != nil {
			safeOutputsEnvLog.Printf("Warning: failed to serialize messages config: %v", err)
		} else if messagesJSON != "" {
			customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_SAFE_OUTPUT_MESSAGES: %q\n", messagesJSON))
		}
	}

	return customEnvVars
}

// buildStepLevelSafeOutputEnvVars builds environment variables for consolidated safe output steps
// This excludes variables that are already set at the job level in consolidated jobs
func (c *Compiler) buildStepLevelSafeOutputEnvVars(data *WorkflowData, targetRepoSlug string) []string {
	var customEnvVars []string

	// Only add target repo slug if it's different from the job-level setting
	// (i.e., this step has a specific target-repo config that overrides the global trial mode target)
	if targetRepoSlug != "" {
		// Step-specific target repo overrides job-level setting
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_TARGET_REPO_SLUG: %q\n", targetRepoSlug))
	} else if !c.trialMode && data.SafeOutputs.Staged {
		// Step needs staged flag but there's no job-level target repo (not in trial mode)
		// Job level only sets this if trialMode is true
		customEnvVars = append(customEnvVars, "          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n")
	}

	// Note: The following are now set at job level and should NOT be included here:
	// - GH_AW_WORKFLOW_NAME
	// - GH_AW_WORKFLOW_SOURCE
	// - GH_AW_WORKFLOW_SOURCE_URL
	// - GH_AW_TRACKER_ID
	// - GH_AW_ENGINE_ID
	// - GH_AW_ENGINE_VERSION
	// - GH_AW_ENGINE_MODEL
	// - GH_AW_SAFE_OUTPUTS_STAGED (if in trial mode)
	// - GH_AW_TARGET_REPO_SLUG (if in trial mode and no step override)
	// - GH_AW_SAFE_OUTPUT_MESSAGES

	return customEnvVars
}

// buildEngineMetadataEnvVars builds engine metadata environment variables (id, version, model)
// These are used by the JavaScript footer generation to create XML comment markers for traceability
func buildEngineMetadataEnvVars(engineConfig *EngineConfig) []string {
	var customEnvVars []string

	if engineConfig == nil {
		return customEnvVars
	}

	// Add engine ID if present
	if engineConfig.ID != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_ENGINE_ID: %q\n", engineConfig.ID))
	}

	// Add engine version if present
	if engineConfig.Version != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_ENGINE_VERSION: %q\n", engineConfig.Version))
	}

	// Add engine model if present
	if engineConfig.Model != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_ENGINE_MODEL: %q\n", engineConfig.Model))
	}

	return customEnvVars
}

// ========================================
// Safe Output Environment Helpers
// ========================================

// addCustomSafeOutputEnvVars adds custom environment variables to safe output job steps
func (c *Compiler) addCustomSafeOutputEnvVars(steps *[]string, data *WorkflowData) {
	if data.SafeOutputs != nil && len(data.SafeOutputs.Env) > 0 {
		for key, value := range data.SafeOutputs.Env {
			*steps = append(*steps, fmt.Sprintf("          %s: %s\n", key, value))
		}
	}
}

// addSafeOutputGitHubTokenForConfig adds github-token to the with section, preferring per-config token over global
// Uses precedence: config token > safe-outputs global github-token > top-level github-token > GH_AW_GITHUB_TOKEN || GITHUB_TOKEN
func (c *Compiler) addSafeOutputGitHubTokenForConfig(steps *[]string, data *WorkflowData, configToken string) {
	var safeOutputsToken string
	if data.SafeOutputs != nil {
		safeOutputsToken = data.SafeOutputs.GitHubToken
	}

	// If app is configured, use app token
	if data.SafeOutputs != nil && data.SafeOutputs.App != nil {
		*steps = append(*steps, "          github-token: ${{ steps.safe-outputs-app-token.outputs.token }}\n")
		return
	}

	// Get effective token using double precedence: config > safe-outputs, then > top-level > GH_AW_GITHUB_TOKEN || GITHUB_TOKEN
	effectiveToken := getEffectiveSafeOutputGitHubToken(configToken, getEffectiveSafeOutputGitHubToken(safeOutputsToken, data.GitHubToken))
	*steps = append(*steps, fmt.Sprintf("          github-token: %s\n", effectiveToken))
}

// addSafeOutputCopilotGitHubTokenForConfig adds github-token to the with section for Copilot-related operations
// Uses precedence: config token > safe-outputs global github-token > top-level github-token > COPILOT_GITHUB_TOKEN > GH_AW_GITHUB_TOKEN (legacy)
func (c *Compiler) addSafeOutputCopilotGitHubTokenForConfig(steps *[]string, data *WorkflowData, configToken string) {
	var safeOutputsToken string
	if data.SafeOutputs != nil {
		safeOutputsToken = data.SafeOutputs.GitHubToken
	}

	// If app is configured, use app token
	if data.SafeOutputs != nil && data.SafeOutputs.App != nil {
		*steps = append(*steps, "          github-token: ${{ steps.safe-outputs-app-token.outputs.token }}\n")
		return
	}

	// Get effective token using double precedence: config > safe-outputs, then > top-level > Copilot default
	effectiveToken := getEffectiveCopilotGitHubToken(configToken, getEffectiveCopilotGitHubToken(safeOutputsToken, data.GitHubToken))
	*steps = append(*steps, fmt.Sprintf("          github-token: %s\n", effectiveToken))
}

// addSafeOutputAgentGitHubTokenForConfig adds github-token to the with section for agent assignment operations
// Uses precedence: config token > safe-outputs token > workflow token > GH_AW_AGENT_TOKEN || GH_AW_GITHUB_TOKEN || GITHUB_TOKEN
// This is specifically for assign-to-agent operations which require elevated permissions.
func (c *Compiler) addSafeOutputAgentGitHubTokenForConfig(steps *[]string, data *WorkflowData, configToken string) {
	// If app is configured, use app token
	if data.SafeOutputs != nil && data.SafeOutputs.App != nil {
		*steps = append(*steps, "          github-token: ${{ steps.safe-outputs-app-token.outputs.token }}\n")
		return
	}

	// Get safe-outputs level token
	var safeOutputsToken string
	if data.SafeOutputs != nil {
		safeOutputsToken = data.SafeOutputs.GitHubToken
	}

	// Get effective token using full precedence chain: config > safe-outputs > workflow > GH_AW_AGENT_TOKEN fallback
	effectiveToken := getEffectiveAgentGitHubToken(configToken, getEffectiveAgentGitHubToken(safeOutputsToken, data.GitHubToken))
	*steps = append(*steps, fmt.Sprintf("          github-token: %s\n", effectiveToken))
}

// buildTitlePrefixEnvVar builds a title-prefix environment variable line for safe-output jobs.
// envVarName should be the full env var name like "GH_AW_ISSUE_TITLE_PREFIX" or "GH_AW_DISCUSSION_TITLE_PREFIX".
// Returns an empty slice if titlePrefix is empty.
func buildTitlePrefixEnvVar(envVarName string, titlePrefix string) []string {
	if titlePrefix == "" {
		return nil
	}
	return []string{fmt.Sprintf("          %s: %q\n", envVarName, titlePrefix)}
}

// buildLabelsEnvVar builds a labels environment variable line for safe-output jobs.
// envVarName should be the full env var name like "GH_AW_ISSUE_LABELS" or "GH_AW_PR_LABELS".
// Returns an empty slice if labels is empty.
func buildLabelsEnvVar(envVarName string, labels []string) []string {
	if len(labels) == 0 {
		return nil
	}
	labelsStr := strings.Join(labels, ",")
	return []string{fmt.Sprintf("          %s: %q\n", envVarName, labelsStr)}
}

// buildCategoryEnvVar builds a category environment variable line for discussion safe-output jobs.
// envVarName should be the full env var name like "GH_AW_DISCUSSION_CATEGORY".
// Returns an empty slice if category is empty.
func buildCategoryEnvVar(envVarName string, category string) []string {
	if category == "" {
		return nil
	}
	return []string{fmt.Sprintf("          %s: %q\n", envVarName, category)}
}

// buildAllowedReposEnvVar builds an allowed-repos environment variable line for safe-output jobs.
// envVarName should be the full env var name like "GH_AW_ALLOWED_REPOS".
// Returns an empty slice if allowedRepos is empty.
func buildAllowedReposEnvVar(envVarName string, allowedRepos []string) []string {
	if len(allowedRepos) == 0 {
		return nil
	}
	reposStr := strings.Join(allowedRepos, ",")
	return []string{fmt.Sprintf("          %s: %q\n", envVarName, reposStr)}
}

// ========================================
// Test Helpers
// ========================================

// assertEnvVarsInSteps checks that all expected environment variables are present in the job steps.
// This is a helper function to reduce duplication in safe outputs env tests.
func assertEnvVarsInSteps(t *testing.T, steps []string, expectedEnvVars []string) {
	t.Helper()
	stepsStr := strings.Join(steps, "")
	for _, expectedEnvVar := range expectedEnvVars {
		if !strings.Contains(stepsStr, expectedEnvVar) {
			t.Errorf("Expected env var %q not found in job YAML", expectedEnvVar)
		}
	}
}
