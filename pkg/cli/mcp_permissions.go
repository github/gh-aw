package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// queryActorRole queries the GitHub API to determine the actor's role in the repository.
// Returns the permission level (admin, maintain, write, triage, read) or an error.
// Results are cached for 1 hour to avoid excessive API calls.
func queryActorRole(ctx context.Context, actor string, repo string) (string, error) {
	if actor == "" {
		return "", errors.New("actor not specified")
	}
	if repo == "" {
		return "", errors.New("repository not specified")
	}

	// Check cache first
	if perm, ok := mcpCache.GetPermission(actor, repo); ok {
		mcpLog.Printf("Using cached permission for %s in %s: %s", actor, repo, perm)
		return perm, nil
	}

	// Query GitHub API for user's permission level
	// GET /repos/{owner}/{repo}/collaborators/{username}/permission
	apiPath := fmt.Sprintf("/repos/%s/collaborators/%s/permission", repo, actor)
	mcpLog.Printf("Querying GitHub API for %s's permission in %s", actor, repo)

	cmd := workflow.ExecGHContext(ctx, "api", apiPath, "--jq", ".permission")
	output, err := runMCPSubprocessOutput(ctx, cmd)
	if err != nil {
		mcpLog.Printf("Failed to query actor permission: %v", err)
		return "", fmt.Errorf("failed to query actor permission: %w", err)
	}

	permission := strings.TrimSpace(string(output))
	if permission == "" {
		return "", fmt.Errorf("no permission found for actor %s in repository %s", actor, repo)
	}

	mcpCache.SetPermission(actor, repo, permission)
	mcpLog.Printf("Cached permission for %s in %s: %s", actor, repo, permission)

	return permission, nil
}

// hasWriteAccess checks if the given permission level is write or higher.
// Permission levels from highest to lowest: admin, maintain, write, triage, read
func hasWriteAccess(permission string) bool {
	switch permission {
	case "admin", "maintain", "write":
		return true
	default:
		return false
	}
}

// checkActorPermission validates if the actor has sufficient permissions for restricted tools.
// Returns nil if access is allowed, or a jsonrpc.Error if access is denied.
// Uses GitHub API to query the actor's actual repository role with 1-hour caching.
func checkActorPermission(ctx context.Context, actor string, validateActor bool, toolName string) error {
	// If validation is disabled, always allow access
	if !validateActor {
		mcpLog.Printf("Tool %s: access allowed (validation disabled)", toolName)
		return nil
	}

	// If validation is enabled but no actor is specified, deny access
	if actor == "" {
		mcpLog.Printf("Tool %s: access denied (no actor specified, validation enabled)", toolName)
		return newMCPError(jsonrpc.CodeInvalidRequest, "permission denied: insufficient role", map[string]any{
			"error":  "GITHUB_ACTOR environment variable not set",
			"tool":   toolName,
			"reason": "This tool requires at least write access to the repository. Set GITHUB_ACTOR environment variable to enable access.",
		})
	}

	// Get repository using cached lookup
	repo, err := getRepository(ctx)
	if err != nil {
		mcpLog.Printf("Tool %s: failed to get repository context, denying access: %v", toolName, err)
		return newMCPError(jsonrpc.CodeInternalError, "permission check failed", map[string]any{
			"error":  err.Error(),
			"tool":   toolName,
			"reason": "Could not determine repository context to verify permissions. Ensure you are running from within a git repository with gh authenticated.",
		})
	}

	if repo == "" {
		mcpLog.Printf("Tool %s: no repository context, denying access", toolName)
		return newMCPError(jsonrpc.CodeInvalidRequest, "permission check failed", map[string]any{
			"tool":   toolName,
			"reason": "No repository context available. Run from within a git repository.",
		})
	}

	// Query actor's role in the repository with caching
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	permission, err := queryActorRole(ctx, actor, repo)
	if err != nil {
		mcpLog.Printf("Tool %s: failed to query actor role, denying access: %v", toolName, err)
		return newMCPError(jsonrpc.CodeInternalError, "permission denied: unable to verify repository access", map[string]any{
			"error":      err.Error(),
			"tool":       toolName,
			"actor":      actor,
			"repository": repo,
			"reason":     "Failed to query actor's repository permissions from GitHub API.",
		})
	}

	// Check if the actor has write+ access
	if !hasWriteAccess(permission) {
		mcpLog.Printf("Tool %s: access denied for actor %s (permission: %s, requires: write+)", toolName, actor, permission)
		return insufficientActorRoleError(toolName, actor, repo, permission)
	}

	mcpLog.Printf("Tool %s: access allowed for actor %s (permission: %s)", toolName, actor, permission)
	return nil
}

func insufficientActorRoleError(toolName, actor, repo, permission string) error {
	return newMCPError(jsonrpc.CodeInvalidRequest, "permission denied: insufficient role", map[string]any{
		"error":      "insufficient repository permissions",
		"tool":       toolName,
		"actor":      actor,
		"repository": repo,
		"role":       permission,
		"required":   "write, maintain, or admin",
		"reason":     fmt.Sprintf("Actor %s has %s access to %s. This tool requires at least write access.", actor, permission, repo),
	})
}
