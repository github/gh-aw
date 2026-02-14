package workflow

import "fmt"

// generateGitConfigurationSteps generates standardized git credential setup as string steps
func (c *Compiler) generateGitConfigurationSteps() []string {
	return c.generateGitConfigurationStepsWithToken("${{ github.token }}", "")
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
	} else if c.trialMode && c.trialLogicalRepoSlug != "" {
		repoNameValue = fmt.Sprintf("%q", c.trialLogicalRepoSlug)
	}

	return []string{
		"      - name: Configure Git credentials\n",
		"        env:\n",
		fmt.Sprintf("          REPO_NAME: %s\n", repoNameValue),
		"          SERVER_URL: ${{ github.server_url }}\n",
		"        run: |\n",
		"          git config --global user.email \"github-actions[bot]@users.noreply.github.com\"\n",
		"          git config --global user.name \"github-actions[bot]\"\n",
		"          # Re-authenticate git with GitHub token\n",
		"          SERVER_URL_STRIPPED=\"${SERVER_URL#https://}\"\n",
		fmt.Sprintf("          git remote set-url origin \"https://x-access-token:%s@${SERVER_URL_STRIPPED}/${REPO_NAME}.git\"\n", token),
		"          echo \"Git configured with standard GitHub Actions identity\"\n",
	}
}

// generateGitCredentialsCleanerStep generates a step that removes git credentials from .git/config
// This is a security measure to prevent credentials left by injected steps from being accessed by the agent
func (c *Compiler) generateGitCredentialsCleanerStep() []string {
	return []string{
		"      - name: Clean git credentials\n",
		"        run: bash /opt/gh-aw/actions/clean_git_credentials.sh\n",
	}
}
