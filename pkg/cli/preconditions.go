package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var preconditionsLog = logger.New("cli:preconditions")

// checkGHAuthStatusShared verifies the user is logged in to GitHub CLI
func checkGHAuthStatusShared(verbose bool) error {
	preconditionsLog.Print("Checking GitHub CLI authentication status")

	output, err := workflow.RunGHCombined("Checking GitHub authentication...", "auth", "status")

	if err != nil {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage("You are not logged in to GitHub CLI."))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Please run the following command to authenticate:")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, console.FormatCommandMessage("  gh auth login"))
		fmt.Fprintln(os.Stderr, "")
		return errors.New("not authenticated with GitHub CLI")
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("GitHub CLI authenticated"))
		preconditionsLog.Printf("gh auth status output: %s", string(output))
	}

	return nil
}

// checkActionsEnabledShared verifies that GitHub Actions is enabled for the repository
// and that the allowed actions settings permit running agentic workflows
func checkActionsEnabledShared(repoSlug string, verbose bool) error {
	preconditionsLog.Print("Checking if GitHub Actions is enabled")

	// Use gh api to check Actions permissions - get the full JSON response
	output, err := workflow.RunGH("Checking GitHub Actions status...", "api", fmt.Sprintf("/repos/%s/actions/permissions", repoSlug))
	if err != nil {
		preconditionsLog.Printf("Failed to check Actions status: %v", err)
		// If we can't check, warn but continue - actual operations will fail if Actions is disabled
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Could not verify GitHub Actions status. Proceeding anyway..."))
		return nil
	}

	// Parse the JSON response
	var permissions struct {
		Enabled            bool   `json:"enabled"`
		AllowedActions     string `json:"allowed_actions"`
		SelectedActionsURL string `json:"selected_actions_url"`
	}
	if err := parseJSON(output, &permissions); err != nil {
		preconditionsLog.Printf("Failed to parse Actions permissions: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Could not parse GitHub Actions settings. Proceeding anyway..."))
		return nil
	}

	// Check if Actions is enabled
	if !permissions.Enabled {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("GitHub Actions appears to be disabled for this repository."))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "You can still add workflows, but they won't run until Actions is enabled.")
		fmt.Fprintln(os.Stderr, "To enable GitHub Actions, go to Settings → Actions → General.")
		fmt.Fprintln(os.Stderr, "")
		return nil
	}

	// Check allowed actions setting
	switch permissions.AllowedActions {
	case "all":
		// All actions allowed - good to go
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("GitHub Actions is enabled (all actions allowed)"))
		}
	case "local_only":
		// Only local actions allowed - this won't work for agentic workflows
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage("This repository only allows local actions (actions defined in this repository)."))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Agentic workflows require GitHub-owned actions to run.")
		fmt.Fprintln(os.Stderr, "To allow this, go to Settings → Actions → General → Actions permissions")
		fmt.Fprintln(os.Stderr, "and select 'Allow all actions' or 'Allow select actions' with GitHub-owned actions enabled.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Note: For organization repositories, this setting may be controlled at the org level.")
		fmt.Fprintln(os.Stderr, "Contact an organization owner if you cannot change this setting.")
		fmt.Fprintln(os.Stderr, "")
		return errors.New("repository action permissions prevent agentic workflows from running")
	case "selected":
		// Selected actions - need to check if GitHub-owned actions are allowed
		if err := checkSelectedActionsPermissions(permissions.SelectedActionsURL, verbose); err != nil {
			return err
		}
	default:
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("GitHub Actions is enabled"))
		}
	}

	return nil
}

// checkSelectedActionsPermissions checks if GitHub-owned actions are allowed when using selected actions
func checkSelectedActionsPermissions(selectedActionsURL string, verbose bool) error {
	if selectedActionsURL == "" {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Could not verify selected actions settings. Proceeding anyway..."))
		return nil
	}

	preconditionsLog.Printf("Checking selected actions permissions at: %s", selectedActionsURL)

	output, err := workflow.RunGH("Checking selected actions...", "api", selectedActionsURL)
	if err != nil {
		preconditionsLog.Printf("Failed to check selected actions: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Could not verify selected actions settings. Proceeding anyway..."))
		return nil
	}

	var selectedActions struct {
		GitHubOwnedAllowed bool     `json:"github_owned_allowed"`
		VerifiedAllowed    bool     `json:"verified_allowed"`
		PatternsAllowed    []string `json:"patterns_allowed"`
	}
	if err := parseJSON(output, &selectedActions); err != nil {
		preconditionsLog.Printf("Failed to parse selected actions: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Could not parse selected actions settings. Proceeding anyway..."))
		return nil
	}

	if !selectedActions.GitHubOwnedAllowed {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage("This repository does not allow GitHub-owned actions."))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Agentic workflows require GitHub-owned actions (like actions/checkout) to run.")
		fmt.Fprintln(os.Stderr, "To allow this, go to Settings → Actions → General → Actions permissions")
		fmt.Fprintln(os.Stderr, "and enable 'Allow actions created by GitHub'.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Note: For organization repositories, this setting may be controlled at the org level.")
		fmt.Fprintln(os.Stderr, "Contact an organization owner if you cannot change this setting.")
		fmt.Fprintln(os.Stderr, "")
		return errors.New("GitHub-owned actions are not allowed in this repository")
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("GitHub Actions is enabled (GitHub-owned actions allowed)"))
	}

	return nil
}

// parseJSON is a helper to parse JSON from gh api output
func parseJSON(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// checkUserPermissionsShared verifies the user has write/admin access.
// Returns (hasWriteAccess, error) to allow callers to track write access status.
func checkUserPermissionsShared(repoSlug string, verbose bool) (bool, error) {
	preconditionsLog.Print("Checking user permissions")

	owner, repo, err := repoutil.SplitRepoSlug(repoSlug)
	if err != nil {
		return false, fmt.Errorf("invalid repository format: %s", repoSlug)
	}

	hasAccess, err := checkRepositoryAccess(owner, repo)
	if err != nil {
		preconditionsLog.Printf("Failed to check repository access: %v", err)
		// If we can't verify permissions, assume no write access to avoid
		// prompting users for secrets they cannot configure. Users can always
		// set secrets manually later with: gh aw secrets set <SECRET> --repo <REPO>
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Could not verify repository permissions. Proceeding anyway..."))
		return false, nil
	}

	if !hasAccess {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("You do not have write access to %s/%s.", owner, repo)))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "You can still add workflows, but you'll need to propose changes via pull requests.")
		fmt.Fprintln(os.Stderr, "")
	} else if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Repository permissions verified"))
	}

	return hasAccess, nil
}

func getRepoVisibilityShared(repoSlug string) string {
	preconditionsLog.Print("Checking repository visibility")

	// Use gh api to check repository visibility
	output, err := workflow.RunGH("Checking repository visibility...", "api", "/repos/"+repoSlug, "--jq", ".visibility")
	if err != nil {
		preconditionsLog.Printf("Could not check repository visibility: %v", err)
		return "unknown"
	}

	visibility := strings.TrimSpace(string(output))
	preconditionsLog.Printf("Repository visibility: %s", visibility)
	return visibility
}
