package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var resolutionLog = logger.New("cli:add_workflow_resolution")

// ResolvedWorkflow contains metadata about a workflow that has been resolved and is ready to add
type ResolvedWorkflow struct {
	// Spec is the parsed workflow specification
	Spec *WorkflowSpec
	// Content is the raw workflow content
	Content []byte
	// SourceInfo contains source metadata (package path, commit SHA)
	SourceInfo *WorkflowSourceInfo
	// Description is the workflow description extracted from frontmatter
	Description string
	// Engine is the preferred engine extracted from frontmatter (empty if not specified)
	Engine string
	// HasWorkflowDispatch indicates if the workflow has workflow_dispatch trigger
	HasWorkflowDispatch bool
}

// ResolvedWorkflows contains all resolved workflows ready to be added
type ResolvedWorkflows struct {
	// Workflows is the list of resolved workflows
	Workflows []*ResolvedWorkflow
	// HasWildcard indicates if any of the original specs contained wildcards
	HasWildcard bool
	// HasWorkflowDispatch is true if any of the workflows has a workflow_dispatch trigger
	HasWorkflowDispatch bool
}

// ResolveWorkflows resolves workflow specifications by parsing specs, installing repositories,
// expanding wildcards, and fetching workflow content (including descriptions).
// This is useful for showing workflow information before actually adding them.
func ResolveWorkflows(workflows []string, verbose bool) (*ResolvedWorkflows, error) {
	resolutionLog.Printf("Resolving workflows: count=%d", len(workflows))

	if len(workflows) == 0 {
		return nil, fmt.Errorf("at least one workflow name is required")
	}

	for i, workflow := range workflows {
		if workflow == "" {
			return nil, fmt.Errorf("workflow name cannot be empty (workflow %d)", i+1)
		}
	}

	// Parse workflow specifications and group by repository
	repoVersions := make(map[string]string) // repo -> version
	parsedSpecs := []*WorkflowSpec{}        // List of parsed workflow specs

	for _, workflow := range workflows {
		spec, err := parseWorkflowSpec(workflow)
		if err != nil {
			return nil, fmt.Errorf("invalid workflow specification '%s': %w", workflow, err)
		}

		// Handle repository installation and workflow name extraction
		if existing, exists := repoVersions[spec.RepoSlug]; exists && existing != spec.Version {
			return nil, fmt.Errorf("conflicting versions for repository %s: %s vs %s", spec.RepoSlug, existing, spec.Version)
		}
		repoVersions[spec.RepoSlug] = spec.Version

		// Create qualified name for processing
		parsedSpecs = append(parsedSpecs, spec)
	}

	// Check if any workflow is from the current repository
	// Skip this check if we can't determine the current repository (e.g., not in a git repo)
	currentRepoSlug, repoErr := GetCurrentRepoSlug()
	if repoErr == nil {
		// We successfully determined the current repository, check all workflow specs
		for _, spec := range parsedSpecs {
			// Skip local workflow specs (starting with "./")
			if strings.HasPrefix(spec.WorkflowPath, "./") {
				continue
			}

			if spec.RepoSlug == currentRepoSlug {
				return nil, fmt.Errorf("cannot add workflows from the current repository (%s). The 'add' command is for installing workflows from other repositories", currentRepoSlug)
			}
		}
	}
	// If we can't determine the current repository, proceed without the check

	// Install required repositories
	for repo, version := range repoVersions {
		repoWithVersion := repo
		if version != "" {
			repoWithVersion = fmt.Sprintf("%s@%s", repo, version)
		}

		resolutionLog.Printf("Installing repository: %s", repoWithVersion)

		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Installing repository %s before adding workflows...", repoWithVersion)))
		}

		// Install as global package (not local) to match the behavior expected
		if err := InstallPackage(repoWithVersion, verbose); err != nil {
			resolutionLog.Printf("Failed to install repository %s: %v", repoWithVersion, err)
			return nil, fmt.Errorf("failed to install repository %s: %w", repoWithVersion, err)
		}
	}

	// Check if any workflow specs contain wildcards before expansion
	hasWildcard := false
	for _, spec := range parsedSpecs {
		if spec.IsWildcard {
			hasWildcard = true
			break
		}
	}

	// Expand wildcards after installation
	var err error
	parsedSpecs, err = expandWildcardWorkflows(parsedSpecs, verbose)
	if err != nil {
		return nil, err
	}

	// Fetch workflow content and metadata for each workflow
	resolvedWorkflows := make([]*ResolvedWorkflow, 0, len(parsedSpecs))
	hasWorkflowDispatch := false

	for _, spec := range parsedSpecs {
		// Fetch workflow content
		content, sourceInfo, err := findWorkflowInPackageForRepo(spec, verbose)
		if err != nil {
			return nil, fmt.Errorf("workflow '%s' not found: %w", spec.WorkflowPath, err)
		}

		// Extract description from content
		description := ExtractWorkflowDescription(string(content))

		// Extract engine from content (if specified in frontmatter)
		engine := ExtractWorkflowEngine(string(content))

		// Check for workflow_dispatch trigger
		workflowHasDispatch := checkWorkflowHasDispatch(spec, verbose)
		if workflowHasDispatch {
			hasWorkflowDispatch = true
		}

		resolvedWorkflows = append(resolvedWorkflows, &ResolvedWorkflow{
			Spec:                spec,
			Content:             content,
			SourceInfo:          sourceInfo,
			Description:         description,
			Engine:              engine,
			HasWorkflowDispatch: workflowHasDispatch,
		})
	}

	return &ResolvedWorkflows{
		Workflows:           resolvedWorkflows,
		HasWildcard:         hasWildcard,
		HasWorkflowDispatch: hasWorkflowDispatch,
	}, nil
}

// expandWildcardWorkflows expands wildcard workflow specifications into individual workflow specs.
// For each wildcard spec, it discovers all workflows in the installed package and replaces
// the wildcard with the discovered workflows. Non-wildcard specs are passed through unchanged.
func expandWildcardWorkflows(specs []*WorkflowSpec, verbose bool) ([]*WorkflowSpec, error) {
	expandedWorkflows := []*WorkflowSpec{}

	for _, spec := range specs {
		if spec.IsWildcard {
			resolutionLog.Printf("Expanding wildcard for repository: %s", spec.RepoSlug)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Discovering workflows in %s...", spec.RepoSlug)))
			}

			discovered, err := discoverWorkflowsInPackage(spec.RepoSlug, spec.Version, verbose)
			if err != nil {
				return nil, fmt.Errorf("failed to discover workflows in %s: %w", spec.RepoSlug, err)
			}

			if len(discovered) == 0 {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No workflows found in %s", spec.RepoSlug)))
			} else {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Found %d workflow(s) in %s", len(discovered), spec.RepoSlug)))
				}
				expandedWorkflows = append(expandedWorkflows, discovered...)
			}
		} else {
			expandedWorkflows = append(expandedWorkflows, spec)
		}
	}

	if len(expandedWorkflows) == 0 {
		return nil, fmt.Errorf("no workflows to add after expansion")
	}

	return expandedWorkflows, nil
}

// checkWorkflowHasDispatch checks if a single workflow has a workflow_dispatch trigger
func checkWorkflowHasDispatch(spec *WorkflowSpec, verbose bool) bool {
	resolutionLog.Printf("Checking if workflow %s has workflow_dispatch trigger", spec.WorkflowName)

	// Find and read the workflow content
	sourceContent, _, err := findWorkflowInPackageForRepo(spec, verbose)
	if err != nil {
		resolutionLog.Printf("Could not fetch workflow content: %v", err)
		return false
	}

	// Parse frontmatter to check on: triggers
	result, err := parser.ExtractFrontmatterFromContent(string(sourceContent))
	if err != nil {
		resolutionLog.Printf("Could not parse workflow frontmatter: %v", err)
		return false
	}

	// Check if 'on' section exists and contains workflow_dispatch
	onSection, exists := result.Frontmatter["on"]
	if !exists {
		resolutionLog.Print("No 'on' section found in workflow")
		return false
	}

	// Handle different on: formats
	switch on := onSection.(type) {
	case map[string]any:
		_, hasDispatch := on["workflow_dispatch"]
		resolutionLog.Printf("workflow_dispatch in on map: %v", hasDispatch)
		return hasDispatch
	case string:
		hasDispatch := strings.Contains(strings.ToLower(on), "workflow_dispatch")
		resolutionLog.Printf("workflow_dispatch in on string: %v", hasDispatch)
		return hasDispatch
	case []any:
		for _, item := range on {
			if str, ok := item.(string); ok && strings.ToLower(str) == "workflow_dispatch" {
				resolutionLog.Print("workflow_dispatch found in on array")
				return true
			}
		}
		return false
	default:
		resolutionLog.Printf("Unknown on: section type: %T", onSection)
		return false
	}
}
