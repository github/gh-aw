package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/logger"
)

var resolverLog = logger.New("workflow:action_resolver")

// ActionResolver handles resolving action SHAs using GitHub CLI
type ActionResolver struct {
	cache *ActionCache
}

// NewActionResolver creates a new action resolver
func NewActionResolver(cache *ActionCache) *ActionResolver {
	return &ActionResolver{
		cache: cache,
	}
}

// ResolveSHA resolves the SHA for a given action@version using GitHub CLI
// Returns the SHA and an error if resolution fails
func (r *ActionResolver) ResolveSHA(repo, version string) (string, error) {
	resolverLog.Printf("Resolving SHA for action: %s@%s", repo, version)

	// Check cache first
	if sha, found := r.cache.Get(repo, version); found {
		resolverLog.Printf("Cache hit for %s@%s: %s", repo, version, sha)
		return sha, nil
	}

	resolverLog.Printf("Cache miss for %s@%s, querying GitHub API", repo, version)
	resolverLog.Printf("This may take a moment as we query GitHub API at /repos/%s/git/ref/tags/%s", extractBaseRepo(repo), version)

	// Resolve using GitHub CLI
	sha, err := r.resolveFromGitHub(repo, version)
	if err != nil {
		resolverLog.Printf("Failed to resolve %s@%s: %v", repo, version, err)
		return "", err
	}

	resolverLog.Printf("Successfully resolved %s@%s to SHA: %s", repo, version, sha)

	// Cache the result
	resolverLog.Printf("Caching result: %s@%s → %s", repo, version, sha)
	r.cache.Set(repo, version, sha)

	return sha, nil
}

// resolveFromGitHub uses gh CLI to resolve the SHA for an action@version
func (r *ActionResolver) resolveFromGitHub(repo, version string) (string, error) {
	// Extract base repository (for actions like "github/codeql-action/upload-sarif")
	baseRepo := extractBaseRepo(repo)
	resolverLog.Printf("Extracted base repository: %s from %s", baseRepo, repo)

	// Use gh api to get the git ref for the tag
	// API endpoint: GET /repos/{owner}/{repo}/git/ref/tags/{tag}
	apiPath := fmt.Sprintf("/repos/%s/git/ref/tags/%s", baseRepo, version)
	resolverLog.Printf("Querying GitHub API: %s", apiPath)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmd := ExecGHContext(ctx, "api", apiPath, "--jq", ".object.sha")
	output, err := cmd.Output()
	if err != nil {
		// Try without "refs/tags/" prefix in case version is already a ref
		return "", fmt.Errorf("failed to resolve %s@%s: %w", repo, version, err)
	}

	sha := strings.TrimSpace(string(output))
	if sha == "" {
		return "", fmt.Errorf("empty SHA returned for %s@%s", repo, version)
	}

	// Validate SHA format (should be 40 hex characters)
	if len(sha) != 40 {
		return "", fmt.Errorf("invalid SHA format for %s@%s: %s", repo, version, sha)
	}

	return sha, nil
}

// extractBaseRepo extracts the base repository from a repo path
// For "actions/checkout" -> "actions/checkout"
// For "github/codeql-action/upload-sarif" -> "github/codeql-action"
func extractBaseRepo(repo string) string {
	parts := strings.Split(repo, "/")
	if len(parts) >= 2 {
		// Take first two parts (owner/repo)
		return parts[0] + "/" + parts[1]
	}
	return repo
}
