package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/github/gh-aw/pkg/console"
)

// checkGHAuthStatus verifies the user is logged in to GitHub CLI
func (c *AddInteractiveConfig) checkGHAuthStatus() error {
	return checkGHAuthStatusShared(c.Verbose)
}

// checkGitRepository verifies we're in a git repo and gets org/repo info
// This version has special interactive handling to prompt user for repo if not found
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

		// Ask the user for the repository (interactive-only feature)
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
	c.isPublicRepo = checkRepoVisibilityShared(c.RepoOverride)

	return nil
}

// checkActionsEnabled verifies that GitHub Actions is enabled for the repository
func (c *AddInteractiveConfig) checkActionsEnabled() error {
	return checkActionsEnabledShared(c.RepoOverride, c.Verbose)
}

// checkUserPermissions verifies the user has write/admin access
func (c *AddInteractiveConfig) checkUserPermissions() error {
	return checkUserPermissionsShared(c.RepoOverride, c.Verbose)
}
