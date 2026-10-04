package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var gitConfigStepsLog = logger.New("workflow:git_configuration_steps")

// generateGitConfigurationSteps generates standardized git credential setup as string steps
func (c *Compiler) generateGitConfigurationSteps() []string {
	return c.generateGitConfigurationStepsWithToken("${{ github.token }}", "")
}

// generateGitConfigurationStepsForData generates git credential setup with awareness of the
// workflow's checkout configuration. When a checkout is marked current: true and is located
// in a subdirectory, the generated step targets that subdirectory via working-directory so
// that git operations succeed even when the workspace root is no longer a git repository
// (for example, after a pre-agent step that removes the private control-plane checkout).
// Falls back to the standard workspace-root step when no current-checkout subdirectory is
// configured.
func (c *Compiler) generateGitConfigurationStepsForData(data *WorkflowData) []string {
	token := "${{ github.token }}"

	if data == nil || len(data.CheckoutConfigs) == 0 {
		return c.generateGitConfigurationStepsWithToken(token, "")
	}

	checkoutMgr := NewCheckoutManager(data.CheckoutConfigs)
	currentPath := checkoutMgr.GetCurrentCheckoutPath()

	if currentPath == "" {
		// No current checkout in a subdirectory — use the standard workspace-root behaviour.
		return c.generateGitConfigurationStepsWithToken(token, "")
	}

	// current: true checkout is in a subdirectory. Emit the step with a working-directory
	// so configure_git_credentials.sh runs from that directory. This ensures that
	// `git remote set-url origin` targets the correct git repository even when the
	// workspace root no longer contains a .git directory.
	currentRepo := checkoutMgr.GetCurrentRepository()
	repoNameValue := "${{ github.repository }}"
	if currentRepo != "" {
		repoNameValue = fmt.Sprintf("%q", currentRepo)
		gitConfigStepsLog.Printf("Generating git config steps for current checkout: path=%s repo=%s", currentPath, currentRepo)
	} else if c.trialMode && c.trialLogicalRepoSlug != "" {
		repoNameValue = fmt.Sprintf("%q", c.trialLogicalRepoSlug)
		gitConfigStepsLog.Printf("Generating git config steps in trial mode with logical repo and current path=%s", currentPath)
	} else {
		gitConfigStepsLog.Printf("Generating git config steps for current checkout: path=%s (workflow repo)", currentPath)
	}

	return []string{
		"      - name: Configure Git credentials\n",
		fmt.Sprintf("        working-directory: %q\n", currentPath),
		"        env:\n",
		fmt.Sprintf("          GITHUB_REPOSITORY: %s\n", repoNameValue),
		"          GITHUB_SERVER_URL: ${{ github.server_url }}\n",
		// SECURITY: token moved to env mapping so the shell treats it as data,
		// not syntax. Prevents shell injection if token value contains metacharacters.
		fmt.Sprintf("          GITHUB_TOKEN: %s\n", token),
		"        run: bash \"${RUNNER_TEMP}/gh-aw/actions/configure_git_credentials.sh\"\n",
	}
}

// generateGitConfigurationStepsWithToken generates git credential setup with a custom token
// and optional target repository for cross-repo operations
// Parameters:
//   - token: GitHub token to use for authentication
//   - targetRepoSlug: optional target repository (e.g., "org/repo") for cross-repo operations
//     If empty, uses source repository (github.repository)
//     If set, configures git remote to point to the target repository
func (c *Compiler) generateGitConfigurationStepsWithToken(token string, targetRepoSlug string) []string {
	// Determine which repository to configure git remote for
	// Priority: targetRepoSlug > trialLogicalRepoSlug > default (source repo)
	repoNameValue := "${{ github.repository }}"
	if targetRepoSlug != "" {
		repoNameValue = fmt.Sprintf("%q", targetRepoSlug)
		gitConfigStepsLog.Printf("Generating git config steps with target repo: %s", targetRepoSlug)
	} else if c.trialMode && c.trialLogicalRepoSlug != "" {
		repoNameValue = fmt.Sprintf("%q", c.trialLogicalRepoSlug)
		gitConfigStepsLog.Printf("Generating git config steps in trial mode with logical repo: %s", c.trialLogicalRepoSlug)
	} else {
		gitConfigStepsLog.Print("Generating git config steps with default github.repository")
	}

	return []string{
		"      - name: Configure Git credentials\n",
		"        env:\n",
		fmt.Sprintf("          GITHUB_REPOSITORY: %s\n", repoNameValue),
		"          GITHUB_SERVER_URL: ${{ github.server_url }}\n",
		// SECURITY: token moved to env mapping so the shell treats it as data,
		// not syntax. Prevents shell injection if token value contains metacharacters.
		fmt.Sprintf("          GITHUB_TOKEN: %s\n", token),
		"        run: bash \"${RUNNER_TEMP}/gh-aw/actions/configure_git_credentials.sh\"\n",
	}
}

// getGitIdentityEnvVars returns a map of git identity environment variables.
// These mirror the values set by generateGitConfigurationSteps so that git commits
// work correctly inside the AWF sandbox container, which cannot read the host-side
// ~/.gitconfig written by "Configure Git credentials".
//
// Git environment variables take precedence over gitconfig settings and are forwarded
// into the container by AWF via --env-all, ensuring the first git commit succeeds
// without the agent needing to self-configure.
func getGitIdentityEnvVars() map[string]string {
	return map[string]string{
		"GIT_AUTHOR_NAME":     "github-actions[bot]",
		"GIT_AUTHOR_EMAIL":    "github-actions[bot]@users.noreply.github.com",
		"GIT_COMMITTER_NAME":  "github-actions[bot]",
		"GIT_COMMITTER_EMAIL": "github-actions[bot]@users.noreply.github.com",
	}
}

// generateCredentialsCleanerStep generates a single "Clean credentials" step that removes
// git credentials from .git/config and, when known credential-leaking actions were
// detected (envVars non-empty), also removes cloud-provider / registry credentials.
//
// When envVars is empty the step runs only clean_git_credentials.sh.
// When envVars is non-empty the env block is included and both scripts are run in sequence.
//
// Cleanup and its postcondition fail closed. With no checkout, both scripts are no-ops.
func (c *Compiler) generateCredentialsCleanerStep(envVars map[string]struct{}) []string {
	lines := []string{
		"      - name: Clean credentials\n",
	}

	if len(envVars) > 0 {
		lines = append(lines, "        env:\n")
		// Emit env vars in a stable, deterministic order (knownCredentialLeakingActions order)
		for _, known := range knownCredentialLeakingActions {
			if setutil.Contains(envVars, known.envVar) {
				lines = append(lines, fmt.Sprintf("          %s: \"true\"\n", known.envVar))
			}
		}
	}
	lines = append(lines,
		"        run: |\n",
		"          bash \"${RUNNER_TEMP}/gh-aw/actions/clean_git_credentials.sh\"\n",
	)
	if len(envVars) > 0 {
		lines = append(lines, "          bash \"${RUNNER_TEMP}/gh-aw/actions/clean_known_action_credentials.sh\"\n")
	}
	lines = append(lines, "          bash \"${RUNNER_TEMP}/gh-aw/actions/verify_git_credentials.sh\"\n")
	return lines
}
