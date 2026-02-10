package workflow

import "fmt"

// generateGitConfigurationSteps generates standardized git credential setup as string steps
func (c *Compiler) generateGitConfigurationSteps() []string {
	return c.generateGitConfigurationStepsWithToken("${{ github.token }}")
}

// generateGitConfigurationStepsWithToken generates git credential setup with a custom token
func (c *Compiler) generateGitConfigurationStepsWithToken(token string) []string {
	return []string{
		"      - name: Configure Git credentials\n",
		"        env:\n",
		"          REPO_NAME: ${{ github.repository }}\n",
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
