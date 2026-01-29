package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/githubnext/gh-aw/pkg/console"
	"github.com/githubnext/gh-aw/pkg/workflow"
)

// checkGHAuthStatus verifies the user is logged in to GitHub CLI
func (c *AddInteractiveConfig) checkGHAuthStatus() error {
	addInteractiveLog.Print("Checking GitHub CLI authentication status")

	output, err := workflow.RunGHCombined("Checking GitHub authentication...", "auth", "status")

	if err != nil {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage("You are not logged in to GitHub CLI."))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Please run the following command to authenticate:")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, console.FormatCommandMessage("  gh auth login"))
		fmt.Fprintln(os.Stderr, "")
		return fmt.Errorf("not authenticated with GitHub CLI")
	}

	if c.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("GitHub CLI authenticated"))
		addInteractiveLog.Printf("gh auth status output: %s", string(output))
	}

	return nil
}

// checkGitRepository verifies we're in a git repo and gets org/repo info
func (c *AddInteractiveConfig) checkGitRepository() error {
	addInteractiveLog.Print("Checking git repository status")

	// Check if we're in a git repository
	if !isGitRepo() {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage("Not in a git repository."))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Please navigate to a git repository or initialize one with:")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, console.FormatCommandMessage("  git init"))
		fmt.Fprintln(os.Stderr, "")
		return fmt.Errorf("not in a git repository")
	}

	// Try to get the repository slug
	repoSlug, err := GetCurrentRepoSlug()
	if err != nil {
		addInteractiveLog.Printf("Could not determine repository automatically: %v", err)

		// Ask the user for the repository
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Could not determine the repository automatically."))
		fmt.Fprintln(os.Stderr, "")

		var userRepo string
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Enter the target repository (owner/repo):").
					Description("For example: myorg/myrepo").
					Value(&userRepo).
					Validate(func(s string) error {
						parts := strings.Split(s, "/")
						if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
							return fmt.Errorf("please enter in format 'owner/repo'")
						}
						return nil
					}),
			),
		).WithAccessible(console.IsAccessibleMode())

		if err := form.Run(); err != nil {
			return fmt.Errorf("failed to get repository info: %w", err)
		}

		c.RepoOverride = userRepo
		repoSlug = userRepo
	} else {
		c.RepoOverride = repoSlug
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Target repository: %s", repoSlug)))
	addInteractiveLog.Printf("Target repository: %s", repoSlug)

	// Check if repository is public or private
	c.isPublicRepo = c.checkRepoVisibility()

	return nil
}

// checkRepoVisibility checks if the repository is public or private
func (c *AddInteractiveConfig) checkRepoVisibility() bool {
	addInteractiveLog.Print("Checking repository visibility")

	// Use gh api to check repository visibility
	output, err := workflow.RunGH("Checking repository visibility...", "api", fmt.Sprintf("/repos/%s", c.RepoOverride), "--jq", ".visibility")
	if err != nil {
		addInteractiveLog.Printf("Could not check repository visibility: %v", err)
		// Default to public if we can't determine
		return true
	}

	visibility := strings.TrimSpace(string(output))
	isPublic := visibility == "public"
	addInteractiveLog.Printf("Repository visibility: %s (isPublic=%v)", visibility, isPublic)
	return isPublic
}

// checkActionsEnabled verifies that GitHub Actions is enabled for the repository
func (c *AddInteractiveConfig) checkActionsEnabled() error {
	addInteractiveLog.Print("Checking if GitHub Actions is enabled")

	// Use gh api to check Actions permissions
	output, err := workflow.RunGH("Checking GitHub Actions status...", "api", fmt.Sprintf("/repos/%s/actions/permissions", c.RepoOverride), "--jq", ".enabled")
	if err != nil {
		addInteractiveLog.Printf("Failed to check Actions status: %v", err)
		// If we can't check, warn but continue - actual operations will fail if Actions is disabled
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Could not verify GitHub Actions status. Proceeding anyway..."))
		return nil
	}

	enabled := strings.TrimSpace(string(output))
	if enabled != "true" {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage("GitHub Actions is disabled for this repository."))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "To enable GitHub Actions:")
		fmt.Fprintln(os.Stderr, "  1. Go to your repository on GitHub")
		fmt.Fprintln(os.Stderr, "  2. Navigate to Settings → Actions → General")
		fmt.Fprintln(os.Stderr, "  3. Under 'Actions permissions', select 'Allow all actions and reusable workflows'")
		fmt.Fprintln(os.Stderr, "  4. Click 'Save'")
		fmt.Fprintln(os.Stderr, "")
		return fmt.Errorf("GitHub Actions is not enabled for this repository")
	}

	if c.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("GitHub Actions is enabled"))
	}

	return nil
}

// checkUserPermissions verifies the user has write/admin access
func (c *AddInteractiveConfig) checkUserPermissions() error {
	addInteractiveLog.Print("Checking user permissions")

	parts := strings.Split(c.RepoOverride, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repository format: %s", c.RepoOverride)
	}
	owner, repo := parts[0], parts[1]

	hasAccess, err := checkRepositoryAccess(owner, repo)
	if err != nil {
		addInteractiveLog.Printf("Failed to check repository access: %v", err)
		// If we can't check, warn but continue - actual operations will fail if no access
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Could not verify repository permissions. Proceeding anyway..."))
		return nil
	}

	if !hasAccess {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(fmt.Sprintf("You do not have write access to %s/%s.", owner, repo)))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "You need to be a maintainer, admin, or have write permissions on this repository.")
		fmt.Fprintln(os.Stderr, "Please contact the repository owner or request access.")
		fmt.Fprintln(os.Stderr, "")
		return fmt.Errorf("insufficient repository permissions")
	}

	if c.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Repository permissions verified"))
	}

	return nil
}
