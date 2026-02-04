// This file provides repository feature detection and validation.
//
// # Repository Features Validation
//
// This file validates that required repository features (discussions, issues) are enabled
// when safe-outputs are configured that depend on them. It provides caching to minimize
// API calls when checking repository capabilities.
//
// # Validation Functions
//
//   - validateRepositoryFeatures() - Main orchestrator validating safe-outputs requirements
//   - getCurrentRepository() - Gets current repository from git context (cached)
//   - getRepositoryFeatures() - Gets repository features with caching (discussions, issues)
//   - checkRepositoryHasDiscussions() - Checks if discussions are enabled (cached)
//   - checkRepositoryHasIssues() - Checks if issues are enabled (cached)
//   - ClearRepositoryFeaturesCache() - Clears all repository feature caches
//
// # Validation Pattern: Feature Detection with Caching
//
// Repository feature validation uses a caching pattern to amortize expensive API calls:
//   - sync.Map for thread-safe cache storage
//   - sync.Once for single-fetch guarantee
//   - Atomic LoadOrStore for race-free caching
//   - Separate logged cache to avoid duplicate success messages
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It checks repository capabilities (discussions, issues, projects, etc.)
//   - It validates safe-outputs dependencies on repository features
//   - It requires GitHub API calls to check repository settings
//   - It benefits from caching to reduce API usage
//
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var repositoryFeaturesLog = logger.New("workflow:repository_features_validation")

// RepositoryFeatures holds cached information about repository capabilities
type RepositoryFeatures struct {
	HasDiscussions bool
	HasIssues      bool
}

// Global cache for repository features and current repository info
var (
	repositoryFeaturesCache       = sync.Map{} // sync.Map is thread-safe and efficient for read-heavy workloads
	repositoryFeaturesLoggedCache = sync.Map{} // Tracks which repositories have had their success messages logged
	getCurrentRepositoryOnce      sync.Once
	currentRepositoryResult       string
	currentRepositoryError        error
)

// ClearRepositoryFeaturesCache clears the repository features cache
// This is useful for testing or when repository settings might have changed
func ClearRepositoryFeaturesCache() {
	// Clear the features cache
	repositoryFeaturesCache.Range(func(key, value any) bool {
		repositoryFeaturesCache.Delete(key)
		return true
	})

	// Clear the logged cache
	repositoryFeaturesLoggedCache.Range(func(key, value any) bool {
		repositoryFeaturesLoggedCache.Delete(key)
		return true
	})

	// Reset the current repository cache
	getCurrentRepositoryOnce = sync.Once{}
	currentRepositoryResult = ""
	currentRepositoryError = nil

	repositoryFeaturesLog.Print("Repository features and current repository caches cleared")
}

// validateRepositoryFeatures validates that required repository features are enabled
// when safe-outputs are configured that depend on them (discussions, issues)
func (c *Compiler) validateRepositoryFeatures(workflowData *WorkflowData) error {
	if workflowData.SafeOutputs == nil {
		return nil
	}

	repositoryFeaturesLog.Print("Validating repository features for safe-outputs")

	// Get the repository from the current git context
	// This will work when running in a git repository
	repo, err := getCurrentRepository()
	if err != nil {
		repositoryFeaturesLog.Printf("Could not determine repository: %v", err)
		// Don't fail if we can't determine the repository (e.g., not in a git repo)
		// This allows validation to pass in non-git environments
		return nil
	}

	repositoryFeaturesLog.Printf("Checking repository features for: %s", repo)

	// Collect all validation errors using ErrorCollector
	collector := NewErrorCollector(c.failFast)

	// Check if discussions are enabled when create-discussion or add-comment with discussion: true is configured
	needsDiscussions := workflowData.SafeOutputs.CreateDiscussions != nil ||
		(workflowData.SafeOutputs.AddComments != nil &&
			workflowData.SafeOutputs.AddComments.Discussion != nil &&
			*workflowData.SafeOutputs.AddComments.Discussion)

	if needsDiscussions {
		hasDiscussions, err := checkRepositoryHasDiscussions(repo, c.verbose)

		if err != nil {
			// If we can't check, log but don't fail
			// This could happen due to network issues or auth problems
			repositoryFeaturesLog.Printf("Warning: Could not check if discussions are enabled: %v", err)
			if c.verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
					fmt.Sprintf("Could not verify if discussions are enabled: %v", err)))
			}
			// Continue checking other features even if this check fails
		} else if !hasDiscussions {
			// Changed to warning instead of error per issue feedback
			// Strategy: Always try to create the discussion at runtime and investigate if it fails
			// The runtime create_discussion handler will provide better error messages if creation fails
			var warningMsg string
			if workflowData.SafeOutputs.CreateDiscussions != nil {
				warningMsg = fmt.Sprintf("Repository %s may not have discussions enabled. The workflow will attempt to create discussions at runtime. If creation fails, enable discussions in repository settings.", repo)
			} else {
				// For add-comment with discussion: true
				warningMsg = fmt.Sprintf("Repository %s may not have discussions enabled for add-comment with discussion: true. The workflow will attempt to add comments at runtime. If this fails, enable discussions in repository settings.", repo)
			}
			repositoryFeaturesLog.Printf("Warning: %s", warningMsg)
			if c.verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(warningMsg))
			}
			// Don't add to error collector - this is a warning, not an error
		}
	}

	// Check if issues are enabled when create-issue is configured
	if workflowData.SafeOutputs.CreateIssues != nil {
		hasIssues, err := checkRepositoryHasIssues(repo, c.verbose)

		if err != nil {
			// If we can't check, log but don't fail
			repositoryFeaturesLog.Printf("Warning: Could not check if issues are enabled: %v", err)
			if c.verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
					fmt.Sprintf("Could not verify if issues are enabled: %v", err)))
			}
			// Continue to return aggregated errors even if this check fails
		} else if !hasIssues {
			issueErr := fmt.Errorf("workflow uses safe-outputs.create-issue but repository %s does not have issues enabled. Enable issues in repository settings or remove create-issue from safe-outputs", repo)
			if returnErr := collector.Add(issueErr); returnErr != nil {
				return returnErr // Fail-fast mode
			}
		}
	}

	repositoryFeaturesLog.Printf("Repository features validation completed: error_count=%d", collector.Count())

	// Return aggregated errors with formatted output
	return collector.FormattedError("repository features")
}

// getCurrentRepository gets the current repository from git context (with caching)
func getCurrentRepository() (string, error) {
	getCurrentRepositoryOnce.Do(func() {
		currentRepositoryResult, currentRepositoryError = getCurrentRepositoryUncached()
	})

	if currentRepositoryError != nil {
		return "", currentRepositoryError
	}

	repositoryFeaturesLog.Printf("Using cached current repository: %s", currentRepositoryResult)
	return currentRepositoryResult, nil
}

// getCurrentRepositoryUncached fetches the current repository from gh CLI (no caching)
func getCurrentRepositoryUncached() (string, error) {
	repositoryFeaturesLog.Print("Fetching current repository using repository.Current()")

	// Use native repository.Current() to get the current repository
	// This works when in a git repository with GitHub remote and respects GH_REPO
	repo, err := repository.Current()
	if err != nil {
		return "", fmt.Errorf("failed to get current repository: %w", err)
	}

	// Validate that owner and name are not empty
	if repo.Owner == "" || repo.Name == "" {
		return "", fmt.Errorf("repository owner or name is empty (owner: %q, name: %q)", repo.Owner, repo.Name)
	}

	repoName := fmt.Sprintf("%s/%s", repo.Owner, repo.Name)
	repositoryFeaturesLog.Printf("Cached current repository: %s", repoName)
	return repoName, nil
}

// getRepositoryFeatures gets repository features with caching to amortize API calls
func getRepositoryFeatures(repo string, verbose bool) (*RepositoryFeatures, error) {
	// Check cache first using sync.Map
	if cached, exists := repositoryFeaturesCache.Load(repo); exists {
		features := cached.(*RepositoryFeatures)
		repositoryFeaturesLog.Printf("Using cached repository features for: %s", repo)
		return features, nil
	}

	repositoryFeaturesLog.Printf("Fetching repository features from API for: %s", repo)

	// Fetch from API
	features := &RepositoryFeatures{}

	// Check discussions
	hasDiscussions, err := checkRepositoryHasDiscussionsUncached(repo)
	if err != nil {
		return nil, fmt.Errorf("failed to check discussions: %w", err)
	}
	features.HasDiscussions = hasDiscussions

	// Check issues
	hasIssues, err := checkRepositoryHasIssuesUncached(repo)
	if err != nil {
		return nil, fmt.Errorf("failed to check issues: %w", err)
	}
	features.HasIssues = hasIssues

	// Cache the result using sync.Map's LoadOrStore for atomic caching
	// This handles the race condition where multiple goroutines might fetch the same repo
	actual, loaded := repositoryFeaturesCache.LoadOrStore(repo, features)
	actualFeatures := actual.(*RepositoryFeatures)

	repositoryFeaturesLog.Printf("Cached repository features for: %s (discussions: %v, issues: %v)", repo, actualFeatures.HasDiscussions, actualFeatures.HasIssues)

	// Only log the success messages if this is the first time we're caching these features
	// and we haven't logged them before (checking loaded flag and logged cache)
	if !loaded {
		// Mark as logged atomically
		// Log success messages only if we haven't logged them before
		if _, alreadyLogged := repositoryFeaturesLoggedCache.LoadOrStore(repo, true); !alreadyLogged && verbose {
			if actualFeatures.HasDiscussions {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
					fmt.Sprintf("✓ Repository %s has discussions enabled", repo)))
			}
			if actualFeatures.HasIssues {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
					fmt.Sprintf("✓ Repository %s has issues enabled", repo)))
			}
		}
	}

	return actualFeatures, nil
}

// checkRepositoryHasDiscussions checks if a repository has discussions enabled (with caching)
func checkRepositoryHasDiscussions(repo string, verbose bool) (bool, error) {
	features, err := getRepositoryFeatures(repo, verbose)
	if err != nil {
		return false, err
	}
	return features.HasDiscussions, nil
}

// checkRepositoryHasDiscussionsUncached checks if a repository has discussions enabled (no caching)
func checkRepositoryHasDiscussionsUncached(repo string) (bool, error) {
	// Use GitHub GraphQL API to check if discussions are enabled
	// The hasDiscussionsEnabled field is the canonical way to check this
	query := `query($owner: String!, $name: String!) {
		repository(owner: $owner, name: $name) {
			hasDiscussionsEnabled
		}
	}`

	// Split repo into owner and name
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false, fmt.Errorf("invalid repository format: %s. Expected format: owner/repo. Example: github/gh-aw", repo)
	}
	owner, name := parts[0], parts[1]

	// Execute GraphQL query using gh CLI
	type GraphQLResponse struct {
		Data struct {
			Repository struct {
				HasDiscussionsEnabled bool `json:"hasDiscussionsEnabled"`
			} `json:"repository"`
		} `json:"data"`
	}

	stdOut, _, err := gh.Exec("api", "graphql", "-f", fmt.Sprintf("query=%s", query),
		"-f", fmt.Sprintf("owner=%s", owner), "-f", fmt.Sprintf("name=%s", name))
	if err != nil {
		return false, fmt.Errorf("failed to query discussions status: %w", err)
	}

	var response GraphQLResponse
	if err := json.Unmarshal(stdOut.Bytes(), &response); err != nil {
		return false, fmt.Errorf("failed to parse GraphQL response: %w", err)
	}

	return response.Data.Repository.HasDiscussionsEnabled, nil
}

// checkRepositoryHasIssues checks if a repository has issues enabled (with caching)
func checkRepositoryHasIssues(repo string, verbose bool) (bool, error) {
	features, err := getRepositoryFeatures(repo, verbose)
	if err != nil {
		return false, err
	}
	return features.HasIssues, nil
}

// checkRepositoryHasIssuesUncached checks if a repository has issues enabled (no caching)
func checkRepositoryHasIssuesUncached(repo string) (bool, error) {
	// Use GitHub REST API to check if issues are enabled
	// The has_issues field indicates if issues are enabled
	type RepositoryResponse struct {
		HasIssues bool `json:"has_issues"`
	}

	// Create REST client
	client, err := api.DefaultRESTClient()
	if err != nil {
		return false, fmt.Errorf("failed to create REST client: %w", err)
	}

	// Fetch repository data using REST client
	var response RepositoryResponse
	err = client.Get(fmt.Sprintf("repos/%s", repo), &response)
	if err != nil {
		return false, fmt.Errorf("failed to query repository: %w", err)
	}

	return response.HasIssues, nil
}
