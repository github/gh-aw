package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var resolutionLog = logger.New("cli:add_workflow_resolution")
var fetchWorkflowFromSourceWithContextFn = FetchWorkflowFromSourceWithContext

// errNotHandled is a sentinel used by package-spec parse helpers to signal
// that a workflow string was not recognized as their type; callers should
// fall through to the next parser.
var errNotHandled = errors.New("not handled")

// ResolvedWorkflow contains metadata about a workflow that has been resolved and is ready to add
type ResolvedWorkflow struct {
	// Spec is the parsed workflow specification
	Spec *WorkflowSpec
	// Content is the raw workflow content (convenience accessor, same as SourceInfo.Content)
	Content []byte
	// SourceInfo contains fetched workflow data including content, commit SHA, and source path
	SourceInfo *FetchedWorkflow
	// Description is the workflow description extracted from frontmatter
	Description string
	// Engine is the preferred engine extracted from frontmatter (empty if not specified)
	Engine string
	// HasWorkflowDispatch indicates if the workflow has workflow_dispatch trigger
	HasWorkflowDispatch bool
	// IsPrivate indicates if the workflow has private: true in its frontmatter
	IsPrivate bool
	// IsActionWorkflow indicates that the source is a raw GitHub Actions YAML file (.yml)
	// rather than an agentic workflow markdown file (.md). When true, the file is installed
	// directly to .github/workflows/ without frontmatter processing or compilation.
	IsActionWorkflow bool
	// IsPackageSkillFile is true when the file belongs to a skill directory from an aw.yml
	// package manifest. The file is installed as-is to the agentic engine skill folder.
	IsPackageSkillFile bool
	// IsPackageAgentFile is true when the file is an agent .md from an aw.yml package
	// manifest. The file is installed as-is to the agentic engine agents folder.
	IsPackageAgentFile bool
	// IsPackageResourceFile is true when the file is a declarative repository resource
	// from an aw.yml package manifest. The file is installed as-is to DestinationPath.
	IsPackageResourceFile bool
	// IsPackageProjectFile is true when the file is an aw.json project file from a package.
	// It is merged into the target repository's aw.json instead of copied as-is.
	IsPackageProjectFile bool
	// SkillName is the skill directory name for package skill files (e.g. "my-skill").
	// Only meaningful when IsPackageSkillFile is true.
	SkillName string
}

// ResolvedWorkflows contains all resolved workflows ready to be added
type ResolvedWorkflows struct {
	// Workflows is the list of resolved workflows
	Workflows []*ResolvedWorkflow
	// HasWildcard indicates if any of the original specs contained wildcards (local only)
	HasWildcard bool
	// HasWorkflowDispatch is true if any of the workflows has a workflow_dispatch trigger
	HasWorkflowDispatch bool
	// Warnings contains non-fatal package-resolution warnings to show during add
	Warnings []string
	// BootstrapProfile holds the bootstrap profile from an aw.yml package manifest,
	// when exactly one source package declares a config section.
	// Used by add (non-interactive TODO list) and add-wizard (interactive setup).
	BootstrapProfile *resolvedBootstrapProfile
}

// ResolveWorkflows resolves workflow specifications by parsing specs and fetching workflow content.
// For remote workflows, content is fetched directly from GitHub without cloning.
// Wildcards are only supported for local workflows (not remote repositories).
func ResolveWorkflows(ctx context.Context, workflows []string, verbose bool) (*ResolvedWorkflows, error) {
	resolutionLog.Printf("Resolving workflows: count=%d", len(workflows))

	if err := validateResolveWorkflowsInput(workflows); err != nil {
		return nil, err
	}

	specResolution, err := parseWorkflowSpecsForResolution(ctx, workflows)
	if err != nil {
		return nil, err
	}
	if err := validateCurrentRepositorySpecs(specResolution.ParsedSpecs); err != nil {
		return nil, err
	}

	parsedSpecs, hasWildcard, err := expandWorkflowSpecsIfNeeded(specResolution.ParsedSpecs, verbose)
	if err != nil {
		return nil, err
	}

	resolvedWorkflows, hasWorkflowDispatch, resolutionWarnings, err := resolveWorkflowSpecs(
		ctx,
		parsedSpecs,
		specResolution.Warnings,
		verbose,
	)
	if err != nil {
		return nil, err
	}

	bootstrapProfile, updatedWarnings := selectBootstrapProfile(specResolution.BootstrapProfiles, resolutionWarnings)
	resolutionWarnings = updatedWarnings

	resolutionLog.Printf("Resolution complete: resolved=%d workflows, has_wildcard=%t, has_dispatch=%t",
		len(resolvedWorkflows), hasWildcard, hasWorkflowDispatch)

	return &ResolvedWorkflows{
		Workflows:           resolvedWorkflows,
		HasWildcard:         hasWildcard,
		HasWorkflowDispatch: hasWorkflowDispatch,
		Warnings:            resolutionWarnings,
		BootstrapProfile:    bootstrapProfile,
	}, nil
}

type specResolutionResult struct {
	ParsedSpecs       []*WorkflowSpec
	Warnings          []string
	BootstrapProfiles []*resolvedBootstrapProfile
}

func validateResolveWorkflowsInput(workflows []string) error {
	if len(workflows) == 0 {
		return errors.New("at least one workflow name is required")
	}
	for i, workflow := range workflows {
		if workflow == "" {
			return fmt.Errorf("workflow name cannot be empty (workflow %d)", i+1)
		}
	}
	return nil
}

func parseWorkflowSpecsForResolution(ctx context.Context, workflows []string) (*specResolutionResult, error) {
	result := &specResolutionResult{
		ParsedSpecs: make([]*WorkflowSpec, 0, len(workflows)),
	}
	for _, workflow := range workflows {
		specs, warnings, bootstrapProfile, err := parseSingleWorkflowSpecForResolution(ctx, workflow)
		if err != nil {
			return nil, err
		}
		result.ParsedSpecs = append(result.ParsedSpecs, specs...)
		result.Warnings = append(result.Warnings, warnings...)
		if bootstrapProfile != nil {
			result.BootstrapProfiles = append(result.BootstrapProfiles, bootstrapProfile)
		}
	}
	return result, nil
}

func parseSingleWorkflowSpecForResolution(ctx context.Context, workflow string) ([]*WorkflowSpec, []string, *resolvedBootstrapProfile, error) {
	specs, warnings, bootstrapProfile, err := resolveLocalPackageWorkflowSpec(workflow)
	if err == nil {
		return specs, warnings, bootstrapProfile, nil
	}
	if !errors.Is(err, errNotHandled) {
		return nil, nil, nil, err
	}

	specs, warnings, bootstrapProfile, err = resolveRepositoryPackageWorkflowSpec(ctx, workflow)
	if err == nil {
		return specs, warnings, bootstrapProfile, nil
	}
	if !errors.Is(err, errNotHandled) {
		return nil, nil, nil, err
	}

	spec, err := parseWorkflowSpec(workflow)
	if err == nil {
		if spec.IsWildcard && !isLocalWorkflowPath(spec.WorkflowPath) {
			return nil, nil, nil, fmt.Errorf("wildcards are only supported for local workflows, not remote repositories: %s", workflow)
		}
		return []*WorkflowSpec{spec}, nil, nil, nil
	}

	specs, warnings, bootstrapProfile, err = resolveRepositoryPackageFallback(ctx, workflow)
	return specs, warnings, bootstrapProfile, err
}

func validateCurrentRepositorySpecs(parsedSpecs []*WorkflowSpec) error {
	currentRepoSlug, err := GetCurrentRepoSlug()
	if err != nil {
		resolutionLog.Printf("Could not determine current repository: %v", err)
		return nil
	}
	resolutionLog.Printf("Current repository: %s", currentRepoSlug)
	for _, spec := range parsedSpecs {
		if isLocalWorkflowPath(spec.WorkflowPath) {
			continue
		}
		if spec.RepoSlug == currentRepoSlug {
			return fmt.Errorf("cannot add workflows from the current repository (%s). The 'add' command is for installing workflows from other repositories", currentRepoSlug)
		}
	}
	return nil
}

func resolveWorkflowSpecs(ctx context.Context, parsedSpecs []*WorkflowSpec, warnings []string, verbose bool) ([]*ResolvedWorkflow, bool, []string, error) {
	resolvedWorkflows := make([]*ResolvedWorkflow, 0, len(parsedSpecs))
	resolutionWarnings := warnings
	hasWorkflowDispatch := false

	for _, spec := range parsedSpecs {
		result, err := resolveSingleWorkflowSpec(ctx, spec, verbose)
		if err != nil {
			return nil, false, nil, err
		}
		if result.Workflow.HasWorkflowDispatch {
			hasWorkflowDispatch = true
		}
		if result.Warning != "" {
			resolutionWarnings = append(resolutionWarnings, result.Warning)
		}
		resolvedWorkflows = append(resolvedWorkflows, result.Workflow)
	}

	return resolvedWorkflows, hasWorkflowDispatch, resolutionWarnings, nil
}

// resolvedWorkflowResult holds the outcome of resolving a single workflow spec.
type resolvedWorkflowResult struct {
	Workflow *ResolvedWorkflow
	Warning  string
}

func resolveSingleWorkflowSpec(ctx context.Context, spec *WorkflowSpec, verbose bool) (*resolvedWorkflowResult, error) {
	resolvedSpec, fetched, err := resolveAddWorkflowSpecAndContent(ctx, spec, verbose)
	if err != nil {
		return nil, fmt.Errorf("workflow '%s' not found: %w", spec.String(), err)
	}

	if resolvedWorkflow, handled := resolvePackageOrActionWorkflow(spec, resolvedSpec, fetched); handled {
		return &resolvedWorkflowResult{Workflow: resolvedWorkflow}, nil
	}

	return resolveStandardWorkflow(spec, resolvedSpec, fetched)
}

func resolvePackageOrActionWorkflow(spec, resolvedSpec *WorkflowSpec, fetched *FetchedWorkflow) (*ResolvedWorkflow, bool) {
	if spec.IsPackageSkillFile {
		resolutionLog.Printf("Resolved package skill file: spec=%s, skill=%s, content_size=%d bytes",
			spec.String(), spec.SkillName, len(fetched.Content))
		return &ResolvedWorkflow{
			Spec:               resolvedSpec,
			Content:            fetched.Content,
			SourceInfo:         fetched,
			IsPackageSkillFile: true,
			SkillName:          spec.SkillName,
		}, true
	}

	if spec.IsPackageAgentFile {
		resolutionLog.Printf("Resolved package agent file: spec=%s, content_size=%d bytes",
			spec.String(), len(fetched.Content))
		return &ResolvedWorkflow{
			Spec:               resolvedSpec,
			Content:            fetched.Content,
			SourceInfo:         fetched,
			IsPackageAgentFile: true,
		}, true
	}

	if spec.IsPackageResourceFile {
		isProjectFile := filepath.ToSlash(filepath.Clean(spec.DestinationPath)) == workflow.RepoConfigFileName
		resolutionLog.Printf("Resolved package resource file: spec=%s, destination=%s, content_size=%d bytes",
			spec.String(), spec.DestinationPath, len(fetched.Content))
		return &ResolvedWorkflow{
			Spec:                  resolvedSpec,
			Content:               fetched.Content,
			SourceInfo:            fetched,
			IsPackageResourceFile: true,
			IsPackageProjectFile:  isProjectFile,
		}, true
	}

	if isActionWorkflowPath(resolvedSpec.WorkflowPath) {
		resolutionLog.Printf("Resolved action workflow: spec=%s, content_size=%d bytes",
			spec.String(), len(fetched.Content))
		return &ResolvedWorkflow{
			Spec:             resolvedSpec,
			Content:          fetched.Content,
			SourceInfo:       fetched,
			IsActionWorkflow: true,
		}, true
	}

	return nil, false
}

func resolveStandardWorkflow(spec, resolvedSpec *WorkflowSpec, fetched *FetchedWorkflow) (*resolvedWorkflowResult, error) {
	content := string(fetched.Content)
	description := ExtractWorkflowDescription(content)
	engine := ExtractWorkflowEngine(content)

	if err := validateManifestWorkflowPrivateSetting(spec, resolvedSpec, content); err != nil {
		return nil, err
	}

	if ExtractWorkflowPrivate(content) {
		return nil, fmt.Errorf("workflow '%s' is private and cannot be added to other repositories", spec.String())
	}

	workflowHasDispatch := checkWorkflowHasDispatchFromContent(content)
	resolutionLog.Printf("Resolved workflow: spec=%s, engine=%s, has_dispatch=%t, content_size=%d bytes",
		spec.String(), engine, workflowHasDispatch, len(fetched.Content))

	var warning string
	if fetched.ConvertedFromJSON {
		warning = fmt.Sprintf(
			"JSON workflow import for %q was best-effort; run an agentic prompt to refine .github/workflows/%s.md",
			resolvedSpec.WorkflowName,
			resolvedSpec.WorkflowName,
		)
	}

	return &resolvedWorkflowResult{
		Workflow: &ResolvedWorkflow{
			Spec:                resolvedSpec,
			Content:             fetched.Content,
			SourceInfo:          fetched,
			Description:         description,
			Engine:              engine,
			HasWorkflowDispatch: workflowHasDispatch,
		},
		Warning: warning,
	}, nil
}

func validateManifestWorkflowPrivateSetting(spec, resolvedSpec *WorkflowSpec, content string) error {
	if !spec.FromRepositoryManifest {
		return nil
	}
	privateValue, hasPrivate := ExtractWorkflowPrivateSetting(content)
	if !hasPrivate || !privateValue {
		return nil
	}
	manifestPath := joinRepositoryPackagePath(spec.PackagePath, repositoryPackageManifestFileName)
	return fmt.Errorf(
		"invalid Agentic Workflow manifest %q: workflow %q sets private: true and cannot be included because private workflows cannot be added",
		manifestPath,
		resolvedSpec.WorkflowPath,
	)
}

func selectBootstrapProfile(bootstrapProfiles []*resolvedBootstrapProfile, resolutionWarnings []string) (*resolvedBootstrapProfile, []string) {
	switch len(bootstrapProfiles) {
	case 0:
		return nil, resolutionWarnings
	case 1:
		for _, bootstrapProfile := range bootstrapProfiles {
			resolutionLog.Printf("Bootstrap profile found: packageID=%s", bootstrapProfile.PackageID)
			return bootstrapProfile, resolutionWarnings
		}
		return nil, resolutionWarnings
	default:
		ids := make([]string, 0, len(bootstrapProfiles))
		for _, p := range bootstrapProfiles {
			ids = append(ids, p.PackageID)
		}
		resolutionLog.Printf("Multiple bootstrap profiles found (%v); skipping all", ids)
		return nil, append(resolutionWarnings,
			fmt.Sprintf("multiple bootstrap profiles found (%s); bootstrap config will be skipped — run each package separately to apply its config", strings.Join(ids, ", ")))
	}
}
