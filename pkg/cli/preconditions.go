package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var preconditionsLog = logger.New("cli:preconditions")

// PreconditionCheckResult holds the result of precondition checks
type PreconditionCheckResult struct {
	RepoSlug     string // The repository slug (owner/repo)
	IsPublicRepo bool   // Whether the repository is public
}

// CheckInteractivePreconditions runs common precondition checks for interactive commands
// like `gh aw add` and `gh aw init`. These checks verify:
// - GitHub CLI authentication
// - Git repository presence
// - GitHub Actions enabled
// - User has write permissions
//
// The verbose parameter controls whether success messages are printed.
// Returns the repository slug and whether it's public on success.
func CheckInteractivePreconditions(verbose bool) (*PreconditionCheckResult, error) {
	result := &PreconditionCheckResult{}

	// Step 1: Check gh auth status
	if err := checkGHAuthStatusShared(verbose); err != nil {
		return nil, err
	}

	// Step 2: Check git repository and get org/repo
	repoSlug, err := checkGitRepositoryShared(verbose)
	if err != nil {
		return nil, err
	}
	result.RepoSlug = repoSlug

	// Step 3: Check GitHub Actions is enabled
	if err := checkActionsEnabledShared(repoSlug, verbose); err != nil {
		return nil, err
	}

	// Step 4: Check user permissions
	if err := checkUserPermissionsShared(repoSlug, verbose); err != nil {
		return nil, err
	}

	// Step 5: Check repository visibility
	result.IsPublicRepo = checkRepoVisibilityShared(repoSlug)

	return result, nil
}

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
		return fmt.Errorf("not authenticated with GitHub CLI")
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("GitHub CLI authenticated"))
		preconditionsLog.Printf("gh auth status output: %s", string(output))
	}

	return nil
}

// checkGitRepositoryShared verifies we're in a git repo and returns the repo slug
func checkGitRepositoryShared(verbose bool) (string, error) {
	preconditionsLog.Print("Checking git repository status")

	// Check if we're in a git repository
	if !isGitRepo() {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage("Not in a git repository."))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Please navigate to a git repository or initialize one with:")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, console.FormatCommandMessage("  git init"))
		fmt.Fprintln(os.Stderr, "")
		return "", fmt.Errorf("not in a git repository")
	}

	// Try to get the repository slug
	repoSlug, err := GetCurrentRepoSlug()
	if err != nil {
		preconditionsLog.Printf("Could not determine repository automatically: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage("Could not determine the repository automatically."))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Please ensure you have a remote configured:")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, console.FormatCommandMessage("  git remote add origin https://github.com/owner/repo.git"))
		fmt.Fprintln(os.Stderr, "")
		return "", fmt.Errorf("could not determine repository: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Target repository: %s", repoSlug)))
	}
	preconditionsLog.Printf("Target repository: %s", repoSlug)

	return repoSlug, nil
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
		return fmt.Errorf("") // Error already displayed above
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
		return fmt.Errorf("") // Error already displayed above
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

// checkUserPermissionsShared verifies the user has write/admin access
func checkUserPermissionsShared(repoSlug string, verbose bool) error {
	preconditionsLog.Print("Checking user permissions")

	parts := strings.Split(repoSlug, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repository format: %s", repoSlug)
	}
	owner, repo := parts[0], parts[1]

	hasAccess, err := checkRepositoryAccess(owner, repo)
	if err != nil {
		preconditionsLog.Printf("Failed to check repository access: %v", err)
		// If we can't check, warn but continue - actual operations will fail if no access
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Could not verify repository permissions. Proceeding anyway..."))
		return nil
	}

	if !hasAccess {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("You do not have write access to %s/%s.", owner, repo)))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "You can still add workflows, but you'll need to propose changes via pull requests.")
		fmt.Fprintln(os.Stderr, "")
	} else if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Repository permissions verified"))
	}

	return nil
}

// checkRepoVisibilityShared checks if the repository is public or private
func checkRepoVisibilityShared(repoSlug string) bool {
	preconditionsLog.Print("Checking repository visibility")

	// Use gh api to check repository visibility
	output, err := workflow.RunGH("Checking repository visibility...", "api", fmt.Sprintf("/repos/%s", repoSlug), "--jq", ".visibility")
	if err != nil {
		preconditionsLog.Printf("Could not check repository visibility: %v", err)
		// Default to public if we can't determine
		return true
	}

	visibility := strings.TrimSpace(string(output))
	isPublic := visibility == "public"
	preconditionsLog.Printf("Repository visibility: %s (isPublic=%v)", visibility, isPublic)
	return isPublic
}
