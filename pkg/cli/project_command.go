package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/githubnext/gh-aw/pkg/console"
	"github.com/githubnext/gh-aw/pkg/logger"
	"github.com/githubnext/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var projectLog = logger.New("cli:project")

// ProjectConfig holds configuration for creating a GitHub Project
type ProjectConfig struct {
	Title       string // Project title
	Owner       string // Owner login (user or org)
	OwnerType   string // "user" or "org"
	Description string // Project description (note: not currently supported by GitHub Projects V2 API during creation)
	Repo        string // Repository to link project to (optional, format: owner/repo)
	Verbose     bool   // Verbose output
}

// NewProjectCommand creates the project command
func NewProjectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage GitHub Projects V2",
		Long: `Manage GitHub Projects V2 boards linked to repositories.

GitHub Projects V2 provides kanban-style project boards for tracking issues,
pull requests, and tasks across repositories.

This command allows you to create new projects owned by users or organizations
and optionally link them to specific repositories.

Examples:
  gh aw project new "My Project" --owner @me                      # Create user project
  gh aw project new "Team Board" --owner myorg                    # Create org project
  gh aw project new "Bugs" --owner myorg --link myorg/myrepo     # Create and link to repo`,
	}

	// Add subcommands
	cmd.AddCommand(NewProjectNewCommand())

	return cmd
}

// NewProjectNewCommand creates the "project new" subcommand
func NewProjectNewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create a new GitHub Project V2",
		Long: `Create a new GitHub Project V2 board owned by a user or organization.

The project can optionally be linked to a specific repository.

Token Requirements:
  The default GITHUB_TOKEN cannot create projects. You must use a PAT with:
  - Classic PAT: 'project' scope (user projects) or 'project' + 'repo' (org projects)  
  - Fine-grained PAT: Organization permissions → Projects: Read & Write

  Set GH_AW_PROJECT_GITHUB_TOKEN environment variable or configure your gh CLI
  with a token that has the required permissions.

Examples:
  gh aw project new "My Project" --owner @me                      # Create user project
  gh aw project new "Team Board" --owner myorg                    # Create org project  
  gh aw project new "Bugs" --owner myorg --link myorg/myrepo     # Create and link to repo`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			owner, _ := cmd.Flags().GetString("owner")
			link, _ := cmd.Flags().GetString("link")
			verbose, _ := cmd.Flags().GetBool("verbose")

			if owner == "" {
				return fmt.Errorf("--owner flag is required. Use '@me' for current user or specify org name")
			}

			config := ProjectConfig{
				Title:   args[0],
				Owner:   owner,
				Repo:    link,
				Verbose: verbose,
			}

			return RunProjectNew(cmd.Context(), config)
		},
	}

	cmd.Flags().StringP("owner", "o", "", "Project owner: '@me' for current user or organization name (required)")
	cmd.Flags().StringP("link", "l", "", "Repository to link project to (format: owner/repo)")
	_ = cmd.MarkFlagRequired("owner")

	return cmd
}

// RunProjectNew executes the project creation logic
func RunProjectNew(ctx context.Context, config ProjectConfig) error {
	projectLog.Printf("Creating project: title=%s, owner=%s, repo=%s", config.Title, config.Owner, config.Repo)

	// Resolve owner type
	ownerType := "org"
	ownerLogin := config.Owner
	if config.Owner == "@me" {
		ownerType = "user"
		// Get current user
		currentUser, err := getCurrentUser(ctx)
		if err != nil {
			return fmt.Errorf("failed to get current user: %w", err)
		}
		ownerLogin = currentUser
		console.LogVerbose(config.Verbose, fmt.Sprintf("Resolved @me to user: %s", ownerLogin))
	}

	config.OwnerType = ownerType
	config.Owner = ownerLogin

	// Validate owner exists
	if err := validateOwner(ctx, config.OwnerType, config.Owner, config.Verbose); err != nil {
		return fmt.Errorf("owner validation failed: %w", err)
	}

	// Get owner ID
	ownerId, err := getOwnerNodeId(ctx, config.OwnerType, config.Owner, config.Verbose)
	if err != nil {
		return fmt.Errorf("failed to get owner ID: %w", err)
	}

	// Create project
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Creating project '%s' for %s %s...", config.Title, config.OwnerType, config.Owner)))

	project, err := createProject(ctx, ownerId, config.Title, config.Verbose)
	if err != nil {
		return fmt.Errorf("failed to create project: %w", err)
	}

	// Link to repository if specified
	if config.Repo != "" {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Linking project to repository %s...", config.Repo)))
		if err := linkProjectToRepo(ctx, project["id"].(string), config.Repo, config.Verbose); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Warning: Failed to link project to repository: %v", err)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ Project linked to repository"))
		}
	}

	// Output success
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("✓ Created project #%v: %s", project["number"], config.Title)))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("  URL: %s", project["url"])))

	return nil
}

// getCurrentUser gets the current authenticated user's login
func getCurrentUser(ctx context.Context) (string, error) {
	projectLog.Print("Getting current user")

	output, err := workflow.RunGH("Fetching user info...", "api", "user", "--jq", ".login")
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}

	login := strings.TrimSpace(string(output))
	if login == "" {
		return "", fmt.Errorf("failed to get current user login")
	}

	return login, nil
}

// validateOwner validates that the owner exists
func validateOwner(ctx context.Context, ownerType, owner string, verbose bool) error {
	projectLog.Printf("Validating %s: %s", ownerType, owner)
	console.LogVerbose(verbose, fmt.Sprintf("Validating %s exists: %s", ownerType, owner))

	var query string
	if ownerType == "org" {
		query = fmt.Sprintf(`query { organization(login: "%s") { id login } }`, escapeGraphQLString(owner))
	} else {
		query = fmt.Sprintf(`query { user(login: "%s") { id login } }`, escapeGraphQLString(owner))
	}

	_, err := workflow.RunGH("Validating owner...", "api", "graphql", "-f", fmt.Sprintf("query=%s", query))
	if err != nil {
		if ownerType == "org" {
			return fmt.Errorf("organization '%s' not found or not accessible", owner)
		}
		return fmt.Errorf("user '%s' not found or not accessible", owner)
	}

	console.LogVerbose(verbose, fmt.Sprintf("✓ %s '%s' validated", capitalizeFirst(ownerType), owner))
	return nil
}

// capitalizeFirst capitalizes the first letter of a string
func capitalizeFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// getOwnerNodeId gets the node ID for the owner
func getOwnerNodeId(ctx context.Context, ownerType, owner string, verbose bool) (string, error) {
	projectLog.Printf("Getting node ID for %s: %s", ownerType, owner)
	console.LogVerbose(verbose, fmt.Sprintf("Getting node ID for %s: %s", ownerType, owner))

	var query string
	var jqPath string
	if ownerType == "org" {
		query = fmt.Sprintf(`query { organization(login: "%s") { id } }`, escapeGraphQLString(owner))
		jqPath = ".data.organization.id"
	} else {
		query = fmt.Sprintf(`query { user(login: "%s") { id } }`, escapeGraphQLString(owner))
		jqPath = ".data.user.id"
	}

	output, err := workflow.RunGH("Getting owner ID...", "api", "graphql", "-f", fmt.Sprintf("query=%s", query), "--jq", jqPath)
	if err != nil {
		return "", fmt.Errorf("failed to get owner node ID: %w", err)
	}

	nodeId := strings.TrimSpace(string(output))
	if nodeId == "" {
		return "", fmt.Errorf("failed to get owner node ID from response")
	}

	console.LogVerbose(verbose, fmt.Sprintf("✓ Got node ID: %s", nodeId))
	return nodeId, nil
}

// createProject creates a GitHub Project V2
func createProject(ctx context.Context, ownerId, title string, verbose bool) (map[string]any, error) {
	projectLog.Printf("Creating project: ownerId=%s, title=%s", ownerId, title)
	console.LogVerbose(verbose, fmt.Sprintf("Creating project with owner ID: %s", ownerId))

	mutation := fmt.Sprintf(`mutation {
		createProjectV2(input: { ownerId: "%s", title: "%s" }) {
			projectV2 {
				id
				number
				title
				url
			}
		}
	}`, ownerId, escapeGraphQLString(title))

	output, err := workflow.RunGH("Creating project...", "api", "graphql", "-f", fmt.Sprintf("query=%s", mutation))
	if err != nil {
		// Check for permission errors
		if strings.Contains(err.Error(), "INSUFFICIENT_SCOPES") || strings.Contains(err.Error(), "NOT_FOUND") {
			return nil, fmt.Errorf("insufficient permissions. You need a PAT with Projects access (classic: 'project' scope, fine-grained: Organization → Projects: Read & Write). Set GH_AW_PROJECT_GITHUB_TOKEN or configure gh CLI with a suitable token")
		}
		return nil, fmt.Errorf("GraphQL mutation failed: %w", err)
	}

	// Parse response
	var response map[string]any
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("failed to parse GraphQL response: %w", err)
	}

	// Extract project data
	data, ok := response["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid response: missing 'data' field")
	}

	createResult, ok := data["createProjectV2"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid response: missing 'createProjectV2' field")
	}

	project, ok := createResult["projectV2"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid response: missing 'projectV2' field")
	}

	console.LogVerbose(verbose, fmt.Sprintf("✓ Project created: #%v", project["number"]))
	return project, nil
}

// linkProjectToRepo links a project to a repository
func linkProjectToRepo(ctx context.Context, projectId, repoSlug string, verbose bool) error {
	projectLog.Printf("Linking project %s to repository %s", projectId, repoSlug)
	console.LogVerbose(verbose, fmt.Sprintf("Linking project to repository: %s", repoSlug))

	// Parse repo slug
	parts := strings.Split(repoSlug, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repository format. Expected 'owner/repo', got '%s'", repoSlug)
	}
	repoOwner := parts[0]
	repoName := parts[1]

	// Get repository ID
	query := fmt.Sprintf(`query { repository(owner: "%s", name: "%s") { id } }`, escapeGraphQLString(repoOwner), escapeGraphQLString(repoName))
	output, err := workflow.RunGH("Getting repository ID...", "api", "graphql", "-f", fmt.Sprintf("query=%s", query), "--jq", ".data.repository.id")
	if err != nil {
		return fmt.Errorf("repository '%s' not found: %w", repoSlug, err)
	}

	repoId := strings.TrimSpace(string(output))
	if repoId == "" {
		return fmt.Errorf("failed to get repository ID")
	}

	// Link project to repository
	mutation := fmt.Sprintf(`mutation {
		linkProjectV2ToRepository(input: { projectId: "%s", repositoryId: "%s" }) {
			repository {
				id
			}
		}
	}`, projectId, repoId)

	_, err = workflow.RunGH("Linking project to repository...", "api", "graphql", "-f", fmt.Sprintf("query=%s", mutation))
	if err != nil {
		return fmt.Errorf("failed to link project to repository: %w", err)
	}

	console.LogVerbose(verbose, fmt.Sprintf("✓ Linked project to repository %s", repoSlug))
	return nil
}

// escapeGraphQLString escapes special characters in GraphQL strings
func escapeGraphQLString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}
