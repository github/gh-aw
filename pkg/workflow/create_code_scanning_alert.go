package workflow

import (
	"fmt"
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var createCodeScanningAlertLog = logger.New("workflow:create_code_scanning_alert")

// CreateCodeScanningAlertsConfig holds configuration for creating repository security advisories (SARIF format) from agent output
type CreateCodeScanningAlertsConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Driver               string   `yaml:"driver,omitempty"`        // Driver name for SARIF tool.driver.name field (default: "GitHub Agentic Workflows Security Scanner")
	TargetRepoSlug       string   `yaml:"target-repo,omitempty"`   // Target repository in format "owner/repo" for cross-repository code scanning alert creation
	AllowedRepos         []string `yaml:"allowed-repos,omitempty"` // List of additional repositories in format "owner/repo" that code scanning alerts can be created in
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
				createCodeScanningAlertLog.Printf("Using custom SARIF driver name: %s", driverStr)
			}
		}

		// Parse target-repo
		securityReportsConfig.TargetRepoSlug = extractStringFromMap(configMap, "target-repo", createCodeScanningAlertLog)
		if securityReportsConfig.TargetRepoSlug != "" {
			createCodeScanningAlertLog.Printf("Target repo for code scanning alerts: %s", securityReportsConfig.TargetRepoSlug)
		}

		// Parse allowed-repos
		securityReportsConfig.AllowedRepos = ParseStringArrayFromConfig(configMap, "allowed-repos", createCodeScanningAlertLog)
		if len(securityReportsConfig.AllowedRepos) > 0 {
			createCodeScanningAlertLog.Printf("Allowed repos for cross-repo alerts: %d configured", len(securityReportsConfig.AllowedRepos))
		}

		// Parse common base fields with default max of 0 (unlimited)
		c.parseBaseSafeOutputConfig(configMap, &securityReportsConfig.BaseSafeOutputConfig, 0)
	} else {
		// If configData is nil or not a map (e.g., "create-code-scanning-alert:" with no value),
		// still set the default max (nil = unlimited)
		createCodeScanningAlertLog.Print("No config map provided, using defaults (unlimited max)")
		securityReportsConfig.Max = nil
	}

	createCodeScanningAlertLog.Printf("Parsed create-code-scanning-alert config: driver=%q, target-repo=%q, allowed-repos=%d",
		securityReportsConfig.Driver, securityReportsConfig.TargetRepoSlug, len(securityReportsConfig.AllowedRepos))
	return securityReportsConfig
}

// buildCodeScanningUploadJob creates a dedicated job that uploads the SARIF file produced by
// the create_code_scanning_alert handler to the GitHub Code Scanning API.
//
// This is a separate job (not a step inside safe_outputs) so that the checkout and SARIF
// upload do not interfere with other safe-output operations running in safe_outputs.
//
// The job:
//   - depends on safe_outputs (needs: [safe_outputs])
//   - runs only when the safe_outputs job exported a SARIF file
//     (if: needs.safe_outputs.outputs.sarif_file != ”)
//   - restores the workspace to the triggering commit via actions/checkout before upload so
//     that github/codeql-action/upload-sarif can resolve the commit reference
//   - uploads the SARIF file with explicit ref/sha to pin the result to the triggering commit
func (c *Compiler) buildCodeScanningUploadJob(data *WorkflowData) (*Job, error) {
	createCodeScanningAlertLog.Print("Building upload_code_scanning_sarif job")

	// Compute the resolved token for checkout/upload in this job.
	// We cannot pass tokens through job outputs (GitHub Actions masks secret references).
	// We must either compute the static token directly or mint a fresh GitHub App token.
	checkoutMgr := NewCheckoutManager(data.CheckoutConfigs)

	var restoreToken string
	var tokenMintSteps []string

	// Check if the default checkout uses GitHub App auth. If so, mint a fresh token
	// in this job — activation/safe_outputs app tokens have expired by this point.
	defaultOverride := checkoutMgr.GetDefaultCheckoutOverride()
	if defaultOverride != nil && defaultOverride.githubApp != nil {
		permissions := NewPermissionsContentsReadSecurityEventsWrite()
		for _, step := range c.buildGitHubAppTokenMintStep(defaultOverride.githubApp, permissions, "") {
			tokenMintSteps = append(tokenMintSteps,
				strings.ReplaceAll(step, "id: safe-outputs-app-token", "id: checkout-restore-app-token"))
		}
		//nolint:gosec // G101: False positive - this is a GitHub Actions expression template, not a hardcoded credential
		restoreToken = "${{ steps.checkout-restore-app-token.outputs.token }}"
	} else {
		// No GitHub App configured for checkout — compute a static secret reference
		// directly. This is safe because secret references are evaluated in the job's own
		// context (not through job outputs which would be masked by GitHub Actions).
		restoreToken = resolveStaticCheckoutToken(data.SafeOutputs, checkoutMgr)
	}

	// Artifact prefix for workflow_call context (so the download name matches the upload name).
	agentArtifactPrefix := artifactPrefixExprForDownstreamJob(data)

	var steps []string

	// Prepend any token minting steps (needed when checkout uses GitHub App auth).
	steps = append(steps, tokenMintSteps...)

	// Step: Restore workspace to the triggering commit.
	// The safe_outputs job may have checked out a different branch (e.g., the base branch for
	// a PR) which would leave HEAD pointing at a different commit. The SARIF upload action
	// requires HEAD to match the commit being scanned, otherwise it fails with "commit not found".
	steps = append(steps, "      - name: Restore checkout to triggering commit\n")
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getActionPin("actions/checkout")))
	steps = append(steps, "        with:\n")
	steps = append(steps, "          ref: ${{ github.sha }}\n")
	steps = append(steps, fmt.Sprintf("          token: %s\n", restoreToken))
	steps = append(steps, "          persist-credentials: false\n")
	steps = append(steps, "          fetch-depth: 1\n")

	// Step: Download the SARIF artifact produced by safe_outputs.
	// The SARIF file was written to the safe_outputs job workspace and uploaded as an artifact.
	// This job runs in a fresh workspace so we must download the artifact before uploading
	// to GitHub Code Scanning.
	sarifDownloadSteps := buildArtifactDownloadSteps(ArtifactDownloadConfig{
		ArtifactName: agentArtifactPrefix + constants.SarifArtifactName.String(),
		DownloadPath: constants.SarifArtifactDownloadPath.String(),
		StepName:     "Download SARIF artifact",
	}, c.getActionPin)
	steps = append(steps, sarifDownloadSteps...)

	// The local SARIF file path after the artifact download completes.
	localSarifPath := path.Join(constants.SarifArtifactDownloadPath.String(), constants.SarifFileName.String())

	// Step: Upload SARIF file to GitHub Code Scanning.
	steps = append(steps, "      - name: Upload SARIF to GitHub Code Scanning\n")
	steps = append(steps, fmt.Sprintf("        id: %s\n", constants.UploadCodeScanningJobName))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", c.getActionPin("github/codeql-action/upload-sarif")))
	steps = append(steps, "        with:\n")
	// NOTE: github/codeql-action/upload-sarif uses 'token' as the input name, not 'github-token'
	// Pass restoreToken as the fallback so GitHub App-minted tokens flow through consistently.
	c.addUploadSARIFToken(&steps, data, data.SafeOutputs.CreateCodeScanningAlerts.GitHubToken, restoreToken)
	// sarif_file now references the locally-downloaded artifact, not the path from safe_outputs
	steps = append(steps, fmt.Sprintf("          sarif_file: %s\n", localSarifPath))
	// ref and sha pin the upload to the exact triggering commit regardless of local git state
	steps = append(steps, "          ref: ${{ github.ref }}\n")
	steps = append(steps, "          sha: ${{ github.sha }}\n")
	steps = append(steps, "          wait-for-processing: true\n")

	// The job only runs when the safe_outputs job exported a non-empty SARIF file path.
	jobCondition := fmt.Sprintf("needs.%s.outputs.sarif_file != ''", constants.SafeOutputsJobName)

	// Permissions: contents:read to checkout, security-events:write to upload SARIF,
	// actions:read for upload-sarif workflow run lookup in private repos
	permissions := NewPermissionsContentsReadSecurityEventsWriteActionsRead()

	job := &Job{
		Name:           string(constants.UploadCodeScanningJobName),
		If:             jobCondition,
		RunsOn:         c.formatFrameworkJobRunsOn(data),
		Environment:    c.indentYAMLLines(resolveSafeOutputsEnvironment(data), "    "),
		Permissions:    permissions.RenderToYAML(),
		TimeoutMinutes: 10,
		Steps:          steps,
		Needs:          []string{string(constants.SafeOutputsJobName)},
	}

	createCodeScanningAlertLog.Print("Built upload_code_scanning_sarif job")
	return job, nil
}

// addUploadSARIFToken adds the 'token' input for github/codeql-action/upload-sarif.
// This action uses 'token' as the input name (not 'github-token' like other GitHub Actions).
// This runs inside the upload_code_scanning_sarif job (a separate job from safe_outputs), so
// the token must be computed directly in this job from static secret references or a freshly
// minted GitHub App token.
//
// Token precedence:
//  1. Per-config github-token (configToken)
//  2. Safe-outputs level github-token
//  3. fallbackToken (either resolveStaticCheckoutToken result or a minted app token)
func (c *Compiler) addUploadSARIFToken(steps *[]string, data *WorkflowData, configToken string, fallbackToken string) {
	var safeOutputsToken string
	if data.SafeOutputs != nil {
		safeOutputsToken = data.SafeOutputs.GitHubToken
	}

	// Choose the first non-empty per-config or safe-outputs-level static PAT.
	// GitHub App tokens are NOT used here because they are minted and revoked in safe_outputs;
	// they are unavailable in this separate downstream job.
	resolvedCustomToken := configToken
	if resolvedCustomToken == "" {
		resolvedCustomToken = safeOutputsToken
	}

	if resolvedCustomToken != "" {
		resolvedToken := resolveSafeOutputGitHubToken(resolvedCustomToken)
		tokenSource := "per-config github-token"
		if configToken == "" {
			tokenSource = "safe-outputs github-token"
		}
		createCodeScanningAlertLog.Printf("Using token for SARIF upload from source: %s (upload-sarif uses 'token' not 'github-token')", tokenSource)
		*steps = append(*steps, fmt.Sprintf("          token: %s\n", resolvedToken))
		return
	}

	// No per-config or safe-outputs token — use the fallback token (static secret reference
	// or minted GitHub App token) computed by the caller. This avoids the GitHub Actions
	// behaviour of masking secret references when they are passed through job outputs.
	createCodeScanningAlertLog.Printf("Using fallback token for SARIF upload token")
	*steps = append(*steps, fmt.Sprintf("          token: %s\n", fallbackToken))
}
