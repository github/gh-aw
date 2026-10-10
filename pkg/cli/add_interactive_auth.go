package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/github/gh-aw/pkg/console"
)

// checkGHAuthStatus verifies the user is logged in to GitHub CLI
func (c *AddInteractiveConfig) checkGHAuthStatus() error {
	addInteractiveLog.Print("Checking gh auth status")
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
		return errors.New("not in a git repository")
	}

	// Try to get the repository slug
	repoSlug, err := GetCurrentRepoSlug()
	if err != nil {
		addInteractiveLog.Printf("Could not determine repository automatically: %v", err)

		// Ask the user for the repository (interactive-only feature)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Could not determine the repository automatically."))

		var userRepo string
		form := console.NewInputForm(
			huh.NewInput().
				Title("Enter the target repository (owner/repo):").
				Description("For example: myorg/myrepo").
				Value(&userRepo).
				Validate(func(s string) error {
					parts := strings.Split(s, "/")
					if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
						return errors.New("please enter in format 'owner/repo'")
					}
					return nil
				}),
		)

		if err := form.RunWithContext(c.Ctx); err != nil {
			return fmt.Errorf("failed to get repository info: %w", err)
		}

		c.RepoOverride = userRepo
		repoSlug = userRepo
	} else {
		c.RepoOverride = repoSlug
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Target repository: "+repoSlug))
	addInteractiveLog.Printf("Target repository: %s", repoSlug)

	c.repositoryVisibility = getRepoVisibilityShared(c.RepoOverride)

	return nil
}

// checkActionsEnabled verifies that GitHub Actions is enabled for the repository
func (c *AddInteractiveConfig) checkActionsEnabled() error {
	addInteractiveLog.Printf("Checking Actions enabled for repo=%s", c.RepoOverride)
	return checkActionsEnabledShared(c.RepoOverride, c.Verbose)
}

// checkUserPermissions verifies the user has write/admin access
func (c *AddInteractiveConfig) checkUserPermissions() error {
	hasWrite, err := checkUserPermissionsShared(c.RepoOverride, c.Verbose)
	if err != nil {
		return err
	}
	addInteractiveLog.Printf("User permission check complete: hasWriteAccess=%t", hasWrite)
	c.hasWriteAccess = hasWrite
	return nil
}
