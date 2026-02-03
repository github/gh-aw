package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var projectLog = logger.New("cli:project")

// ProjectConfig holds configuration for creating a GitHub Project
type ProjectConfig struct {
	Title            string // Project title
	Owner            string // Owner login (user or org)
	OwnerType        string // "user" or "org"
	Description      string // Project description (note: not currently supported by GitHub Projects V2 API during creation)
	Repo             string // Repository to link project to (optional, format: owner/repo)
	Verbose          bool   // Verbose output
	WithProjectSetup bool   // Whether to create standard project views and fields
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

Project Setup:
	Use --with-project-setup to automatically create:
	- Standard views (Progress Board, Task Tracker, Roadmap)
	- Custom fields (Tracker Id, Worker Workflow, Target Repo, Priority, Size, dates)
  - Enhanced Status field with "Review Required" option

Examples:
  gh aw project new "My Project" --owner @me                           # Create user project
  gh aw project new "Team Board" --owner myorg                         # Create org project  
  gh aw project new "Bugs" --owner myorg --link myorg/myrepo           # Create and link to repo
	gh aw project new "Project Q1" --owner myorg --with-project-setup     # With project setup`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			owner, _ := cmd.Flags().GetString("owner")
			link, _ := cmd.Flags().GetString("link")
			verbose, _ := cmd.Flags().GetBool("verbose")
			withProjectSetup, _ := cmd.Flags().GetBool("with-project-setup")

			if owner == "" {
				return fmt.Errorf("--owner flag is required. Use '@me' for current user or specify org name")
			}

			config := ProjectConfig{
				Title:            args[0],
				Owner:            owner,
				Repo:             link,
				Verbose:          verbose,
				WithProjectSetup: withProjectSetup,
			}

			return RunProjectNew(cmd.Context(), config)
		},
	}

	cmd.Flags().StringP("owner", "o", "", "Project owner: '@me' for current user or organization name (required)")
	cmd.Flags().StringP("link", "l", "", "Repository to link project to (format: owner/repo)")
	cmd.Flags().Bool("with-project-setup", false, "Create standard project views and custom fields")
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

	// Create views and fields if requested
	projectURL, ok := project["url"].(string)
	if !ok || projectURL == "" {
		return fmt.Errorf("failed to get project URL from response")
	}

	projectNumberFloat, ok := project["number"].(float64)
	if !ok || projectNumberFloat <= 0 {
		return fmt.Errorf("failed to get valid project number from response")
	}
	projectNumber := int(projectNumberFloat)

	if config.WithProjectSetup {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Creating standard project views..."))
		if err := createStandardViews(ctx, projectURL, config.Verbose); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Warning: Failed to create views: %v", err)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ Created standard views"))
		}

		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Creating custom fields..."))
		if err := createStandardFields(ctx, projectURL, projectNumber, config.Owner, config.Verbose); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Warning: Failed to create fields: %v", err)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ Created custom fields"))
		}
	}

	if config.WithProjectSetup {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Enhancing Status field..."))
		if err := ensureStatusOption(ctx, projectURL, "Review Required", config.Verbose); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Warning: Failed to update Status field: %v", err)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ Added 'Review Required' status option"))
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

// projectURLInfo contains parsed project URL information
type projectURLInfo struct {
	scope         string // "users" or "orgs"
	ownerLogin    string
	projectNumber int
}

// parseProjectURL parses a GitHub Project V2 URL
func parseProjectURL(projectURL string) (projectURLInfo, error) {
	// Extract scope, owner, and project number from URL
	// Expected format: https://github.com/orgs/myorg/projects/123 or https://github.com/users/myuser/projects/123
	parts := strings.Split(projectURL, "/")
	if len(parts) < 6 {
		return projectURLInfo{}, fmt.Errorf("invalid project URL format")
	}

	var scope, ownerLogin, numberStr string
	for i, part := range parts {
		if part == "orgs" || part == "users" {
			if i+2 < len(parts) && parts[i+2] == "projects" && i+3 < len(parts) {
				scope = part
				ownerLogin = parts[i+1]
				numberStr = parts[i+3]
				break
			}
		}
	}

	if scope == "" {
		return projectURLInfo{}, fmt.Errorf("invalid project URL: could not find orgs/users segment")
	}

	projectNumber, err := strconv.Atoi(numberStr)
	if err != nil {
		return projectURLInfo{}, fmt.Errorf("invalid project number: %w", err)
	}

	return projectURLInfo{
		scope:         scope,
		ownerLogin:    ownerLogin,
		projectNumber: projectNumber,
	}, nil
}

// createStandardViews creates the standard project views
func createStandardViews(ctx context.Context, projectURL string, verbose bool) error {
	projectLog.Print("Creating standard views")
	console.LogVerbose(verbose, "Creating standard project views...")

	info, err := parseProjectURL(projectURL)
	if err != nil {
		return fmt.Errorf("failed to parse project URL: %w", err)
	}

	views := []struct {
		name   string
		layout string
	}{
		{name: "Progress Board", layout: "board"},
		{name: "Task Tracker", layout: "table"},
		{name: "Roadmap", layout: "roadmap"},
	}

	for _, view := range views {
		if err := createView(ctx, info, view.name, view.layout, verbose); err != nil {
			return fmt.Errorf("failed to create view %q (%s): %w", view.name, view.layout, err)
		}
		console.LogVerbose(verbose, fmt.Sprintf("Created view: %s (%s)", view.name, view.layout))
	}

	return nil
}

// createView creates a single project view
func createView(ctx context.Context, info projectURLInfo, name, layout string, verbose bool) error {
	projectLog.Printf("Creating view: name=%s, layout=%s", name, layout)

	var path string
	if info.scope == "orgs" {
		path = fmt.Sprintf("/orgs/%s/projectsV2/%d/views", info.ownerLogin, info.projectNumber)
	} else {
		path = fmt.Sprintf("/users/%s/projectsV2/%d/views", info.ownerLogin, info.projectNumber)
	}

	_, err := workflow.RunGH(
		fmt.Sprintf("Creating view %s...", name),
		"api",
		"--method", "POST",
		path,
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: 2022-11-28",
		"-f", "name="+name,
		"-f", "layout="+layout,
	)
	if err != nil {
		return fmt.Errorf("failed to create view: %w", err)
	}

	return nil
}

// createStandardFields creates the standard project fields
func createStandardFields(ctx context.Context, projectURL string, projectNumber int, owner string, verbose bool) error {
	projectLog.Print("Creating standard fields")
	console.LogVerbose(verbose, "Creating custom fields...")

	// Define required fields
	// Note: We use "Target Repo" instead of "Repository" because GitHub has a built-in
	// REPOSITORY field type that conflicts with custom field creation
	fields := []struct {
		name     string
		dataType string
		options  []string // For SINGLE_SELECT fields
	}{
		{"Tracker Id", "TEXT", nil},
		{"Worker Workflow", "TEXT", nil},
		{"Target Repo", "TEXT", nil},
		{"Priority", "SINGLE_SELECT", []string{"High", "Medium", "Low"}},
		{"Size", "SINGLE_SELECT", []string{"Small", "Medium", "Large"}},
		{"Start Date", "DATE", nil},
		{"End Date", "DATE", nil},
	}

	// Create each field
	for _, field := range fields {
		if err := createField(ctx, projectNumber, owner, field.name, field.dataType, field.options, verbose); err != nil {
			return fmt.Errorf("failed to create field '%s': %w", field.name, err)
		}
		console.LogVerbose(verbose, fmt.Sprintf("Created field: %s", field.name))
	}

	return nil
}

// createField creates a single field in the project
func createField(ctx context.Context, projectNumber int, owner, name, dataType string, options []string, verbose bool) error {
	projectLog.Printf("Creating field: name=%s, type=%s", name, dataType)

	args := []string{
		"project", "field-create", fmt.Sprintf("%d", projectNumber),
		"--owner", owner,
		"--name", name,
		"--data-type", dataType,
	}

	// Add options for SINGLE_SELECT fields
	if dataType == "SINGLE_SELECT" && len(options) > 0 {
		for _, option := range options {
			args = append(args, "--single-select-options", option)
		}
	}

	_, err := workflow.RunGH(fmt.Sprintf("Creating field %s...", name), args...)
	if err != nil {
		return fmt.Errorf("failed to create field: %w", err)
	}

	return nil
}

// ensureStatusOption ensures a status option exists in the project's Status field
func ensureStatusOption(ctx context.Context, projectURL, optionName string, verbose bool) error {
	projectLog.Printf("Ensuring Status option: %s", optionName)
	console.LogVerbose(verbose, fmt.Sprintf("Adding '%s' status option...", optionName))

	info, err := parseProjectURL(projectURL)
	if err != nil {
		return fmt.Errorf("failed to parse project URL: %w", err)
	}

	// Get the Status field information
	statusField, err := getStatusField(ctx, info, verbose)
	if err != nil {
		return fmt.Errorf("failed to get Status field: %w", err)
	}

	// Check if the option already exists and is properly ordered
	updatedOptions, changed := ensureSingleSelectOptionBefore(
		statusField.options,
		singleSelectOption{Name: optionName, Color: "BLUE", Description: "Needs review before moving to Done"},
		"Done",
	)

	if !changed {
		console.LogVerbose(verbose, fmt.Sprintf("Status option already present and ordered: %s", optionName))
		return nil
	}

	// Update the field with new options
	if err := updateSingleSelectFieldOptions(ctx, statusField.fieldID, updatedOptions, verbose); err != nil {
		return fmt.Errorf("failed to update Status field: %w", err)
	}

	console.LogVerbose(verbose, fmt.Sprintf("Status option added before 'Done': %s", optionName))
	return nil
}

// singleSelectOption represents a single-select field option
type singleSelectOption struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description,omitempty"`
}

// statusFieldInfo contains information about the Status field
type statusFieldInfo struct {
	projectID string
	fieldID   string
	options   []singleSelectOption
}

// getStatusField retrieves the Status field information for a project
func getStatusField(ctx context.Context, info projectURLInfo, verbose bool) (statusFieldInfo, error) {
	var query string
	var jqProjectID, jqFields string

	if info.scope == "orgs" {
		query = fmt.Sprintf(`query {
			organization(login: "%s") {
				projectV2(number: %d) {
					id
					fields(first: 100) {
						nodes {
							... on ProjectV2SingleSelectField {
								id
								name
								options { name color description }
							}
						}
					}
				}
			}
		}`, escapeGraphQLString(info.ownerLogin), info.projectNumber)
		jqProjectID = ".data.organization.projectV2.id"
		jqFields = ".data.organization.projectV2.fields.nodes"
	} else {
		query = fmt.Sprintf(`query {
			user(login: "%s") {
				projectV2(number: %d) {
					id
					fields(first: 100) {
						nodes {
							... on ProjectV2SingleSelectField {
								id
								name
								options { name color description }
							}
						}
					}
				}
			}
		}`, escapeGraphQLString(info.ownerLogin), info.projectNumber)
		jqProjectID = ".data.user.projectV2.id"
		jqFields = ".data.user.projectV2.fields.nodes"
	}

	// Get project ID
	projectIDOutput, err := workflow.RunGH("Getting project info...", "api", "graphql", "-f", fmt.Sprintf("query=%s", query), "--jq", jqProjectID)
	if err != nil {
		return statusFieldInfo{}, fmt.Errorf("failed to get project ID: %w", err)
	}
	projectID := strings.TrimSpace(string(projectIDOutput))

	// Get fields
	fieldsOutput, err := workflow.RunGH("Getting project fields...", "api", "graphql", "-f", fmt.Sprintf("query=%s", query), "--jq", jqFields)
	if err != nil {
		return statusFieldInfo{}, fmt.Errorf("failed to get project fields: %w", err)
	}

	// Parse fields to find Status field
	var fields []map[string]any
	if err := json.Unmarshal(fieldsOutput, &fields); err != nil {
		return statusFieldInfo{}, fmt.Errorf("failed to parse fields: %w", err)
	}

	for _, field := range fields {
		if fieldName, ok := field["name"].(string); ok && fieldName == "Status" {
			fieldID, _ := field["id"].(string)
			if fieldID == "" {
				continue
			}

			// Parse options
			var options []singleSelectOption
			if optionsData, ok := field["options"].([]any); ok {
				for _, optData := range optionsData {
					if optMap, ok := optData.(map[string]any); ok {
						opt := singleSelectOption{
							Name:  getString(optMap, "name"),
							Color: getString(optMap, "color"),
						}
						if desc := getString(optMap, "description"); desc != "" {
							opt.Description = desc
						}
						options = append(options, opt)
					}
				}
			}

			return statusFieldInfo{
				projectID: projectID,
				fieldID:   fieldID,
				options:   options,
			}, nil
		}
	}

	return statusFieldInfo{}, fmt.Errorf("status field not found in project")
}

// getString safely extracts a string value from a map
func getString(m map[string]any, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return ""
}

// ensureSingleSelectOptionBefore ensures an option exists before a specific option
// If beforeName is not found in the options list, the desired option is appended to the end
func ensureSingleSelectOptionBefore(options []singleSelectOption, desired singleSelectOption, beforeName string) ([]singleSelectOption, bool) {
	var existing *singleSelectOption
	without := make([]singleSelectOption, 0, len(options))

	// Find if the desired option already exists and collect other options
	for _, opt := range options {
		if opt.Name == desired.Name {
			if existing == nil {
				copyOpt := opt
				existing = &copyOpt
			}
			continue
		}
		without = append(without, opt)
	}

	// Determine what to insert
	toInsert := desired
	if existing != nil {
		toInsert = *existing
		toInsert.Color = desired.Color
		if desired.Description != "" {
			toInsert.Description = desired.Description
		}
	}

	// Find insertion point (before the specified option, or at end if not found)
	insertAt := len(without)
	for i, opt := range without {
		if opt.Name == beforeName {
			insertAt = i
			break
		}
	}

	// Build the updated list with the option inserted
	withInserted := make([]singleSelectOption, 0, len(without)+1)
	withInserted = append(withInserted, without[:insertAt]...)
	withInserted = append(withInserted, toInsert)
	withInserted = append(withInserted, without[insertAt:]...)

	// Check if anything changed
	return withInserted, !singleSelectOptionsEqual(options, withInserted)
}

// singleSelectOptionsEqual checks if two option slices are equal
func singleSelectOptionsEqual(a, b []singleSelectOption) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// updateSingleSelectFieldOptions updates a single-select field's options
func updateSingleSelectFieldOptions(ctx context.Context, fieldID string, options []singleSelectOption, verbose bool) error {
	projectLog.Print("Updating single-select field options")

	mutation := `mutation($input: UpdateProjectV2FieldInput!) {
		updateProjectV2Field(input: $input) {
			projectV2Field {
				... on ProjectV2SingleSelectField {
					name
					options { name }
				}
			}
		}
	}`

	variables := map[string]any{
		"input": map[string]any{
			"fieldId":             fieldID,
			"singleSelectOptions": options,
		},
	}

	requestBody := map[string]any{
		"query":     mutation,
		"variables": variables,
	}

	requestJSON, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal GraphQL request: %w", err)
	}

	// Use ExecGH to create command and pipe input
	cmd := workflow.ExecGH("api", "graphql", "--input", "-")
	cmd.Stdin = bytes.NewReader(requestJSON)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to update field options: %w\nOutput: %s", err, string(output))
	}

	return nil
}
