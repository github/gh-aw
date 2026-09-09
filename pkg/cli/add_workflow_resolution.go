package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/sliceutil"
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

func resolveLocalPackageWorkflowSpec(workflow string) ([]*WorkflowSpec, []string, *resolvedBootstrapProfile, error) {
	pkg, err := resolveLocalRepositoryPackage(workflow)
	if err != nil {
		return nil, nil, nil, err
	}
	if pkg == nil {
		return nil, nil, nil, errNotHandled
	}

	var bootstrapProfile *resolvedBootstrapProfile
	if pkg.Bootstrap != nil {
		bootstrapProfile = &resolvedBootstrapProfile{
			PackageID: pkg.ManifestPath,
			Source:    workflow,
			Profile:   pkg.Bootstrap,
		}
	}
	specs := appendLocalRepositoryPackageWorkflowSpecs(nil, pkg)
	return specs, pkg.Warnings, bootstrapProfile, nil
}

func resolveRepositoryPackageWorkflowSpec(ctx context.Context, workflow string) ([]*WorkflowSpec, []string, *resolvedBootstrapProfile, error) {
	repoSpec, ok, err := parseRepositoryPackageSpec(workflow)
	if !ok {
		return nil, nil, nil, errNotHandled
	}
	if err != nil {
		return nil, nil, nil, err
	}
	specs, warnings, bootstrapProfile, err := resolveRepositoryPackageSpecs(ctx, workflow, repoSpec)
	if err == nil {
		return specs, warnings, bootstrapProfile, nil
	}
	if repoSpec.PackagePath != "" && isRepositoryPackageManifestNotFound(err) {
		return nil, nil, nil, errNotHandled
	}
	return nil, nil, nil, err
}

func resolveRepositoryPackageFallback(ctx context.Context, workflow string) ([]*WorkflowSpec, []string, *resolvedBootstrapProfile, error) {
	repoSpec, repoErr := parseRepoSpec(workflow)
	if repoErr != nil {
		return nil, nil, nil, fmt.Errorf("invalid specification '%s': not a valid workflow path or repository package: %w", workflow, repoErr)
	}
	return resolveRepositoryPackageSpecs(ctx, workflow, repoSpec)
}

func resolveRepositoryPackageSpecs(ctx context.Context, workflow string, repoSpec *RepoSpec) ([]*WorkflowSpec, []string, *resolvedBootstrapProfile, error) {
	pkg, err := resolveRepositoryPackage(ctx, repoSpec, explicitHostForRepo(repoSpec.RepoSlug))
	if err != nil {
		return nil, nil, nil, err
	}
	specs := appendRepositoryPackageWorkflowSpecs(nil, repoSpec, pkg)

	var bootstrapProfile *resolvedBootstrapProfile
	if pkg.Bootstrap != nil {
		bootstrapProfile = &resolvedBootstrapProfile{
			PackageID: repositoryPackageIdentifier(repoSpec.RepoSlug, repoSpec.PackagePath),
			Source:    workflow,
			Profile:   pkg.Bootstrap,
		}
	}
	return specs, pkg.Warnings, bootstrapProfile, nil
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

func expandWorkflowSpecsIfNeeded(parsedSpecs []*WorkflowSpec, verbose bool) ([]*WorkflowSpec, bool, error) {
	hasWildcard := sliceutil.Any(parsedSpecs, func(spec *WorkflowSpec) bool {
		return spec.IsWildcard
	})
	if !hasWildcard {
		return parsedSpecs, false, nil
	}
	expandedSpecs, err := expandLocalWildcardWorkflows(parsedSpecs, verbose)
	if err != nil {
		return nil, false, err
	}
	return expandedSpecs, true, nil
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
		bootstrapProfile := bootstrapProfiles[0]
		resolutionLog.Printf("Bootstrap profile found: packageID=%s", bootstrapProfile.PackageID)
		return bootstrapProfile, resolutionWarnings
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

func resolveLocalRepositoryPackage(source string) (*resolvedRepositoryPackage, error) {
	if !isLocalWorkflowPath(source) {
		return nil, nil
	}

	manifestPath, packageDir, err := localRepositoryPackageManifest(source)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if manifestPath == "" {
		return nil, nil
	}

	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Agentic Workflow manifest %q: %w", manifestPath, err)
	}

	manifest, warnings, err := parseRepositoryPackageManifest(manifestPath, content)
	if err != nil {
		return nil, err
	}
	if err := validateLocalRepositoryPackageContents(manifestPath); err != nil {
		return nil, err
	}
	manifestNodes, importWarnings, err := resolveRepositoryPackageManifestGraph(manifestPath, manifest, func(importPath string) ([]byte, error) {
		return readLocalImportedManifest(importPath, packageDir)
	})
	if err != nil {
		return nil, err
	}
	for _, node := range manifestNodes {
		visibilityWarnings, err := validateRepositoryPackageVisibility(node.Manifest, node.Path)
		if err != nil {
			return nil, err
		}
		warnings = append(warnings, visibilityWarnings...)
	}
	warnings = append(warnings, importWarnings...)

	assets, err := resolveLocalRepositoryPackageManifestNodes(manifestNodes, packageDir)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, assets.warnings...)
	projectFile, err := resolveLocalPackageProjectFileAndValidateAssets(packageDir, assets)
	if err != nil {
		return nil, err
	}
	if err := validateUniqueResolvedPackageFiles(assets.installationSources, assets.resourceFiles, assets.extensionFiles.skillFiles, assets.extensionFiles.agentFiles, manifestPath); err != nil {
		return nil, err
	}
	if err := validateUniqueManifestWorkflowFilenames(assets.installationSources, manifestPath); err != nil {
		return nil, err
	}
	return newResolvedLocalRepositoryPackage(manifestPath, packageDir, manifest, assets, projectFile, warnings), nil
}

func resolveLocalPackageProjectFileAndValidateAssets(packageDir string, assets *resolvedRepositoryPackageAssets) (*resolvedPackageResource, error) {
	projectFile, err := resolveLocalRepositoryPackageProjectFile(packageDir)
	if err != nil {
		return nil, err
	}
	if len(assets.installationSources) == 0 && len(assets.resourceFiles) == 0 && len(assets.extensionFiles.skillFiles) == 0 && len(assets.extensionFiles.agentFiles) == 0 && projectFile == nil {
		return nil, fmt.Errorf("repository package at %q does not contain any installable workflows, resources, skills, agents, or aw.json project settings (either explicitly declared or auto-discovered)", packageDir)
	}
	return projectFile, nil
}

func resolveLocalRepositoryPackageProjectFile(packageDir string) (*resolvedPackageResource, error) {
	projectFilePath := filepath.Join(packageDir, filepath.FromSlash(workflow.RepoConfigFileName))
	info, err := os.Lstat(projectFilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to inspect package project file %q: %w", projectFilePath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("package project file %q is a symbolic link, which is not allowed", projectFilePath)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("package project file %q is not a regular file", projectFilePath)
	}
	return &resolvedPackageResource{
		SourcePath:      projectFilePath,
		DestinationPath: workflow.RepoConfigFileName,
	}, nil
}

func newResolvedLocalRepositoryPackage(manifestPath, packageDir string, manifest *repositoryPackageManifest, assets *resolvedRepositoryPackageAssets, projectFile *resolvedPackageResource, warnings []string) *resolvedRepositoryPackage {
	return &resolvedRepositoryPackage{
		ManifestPath:       manifestPath,
		Name:               manifest.Name,
		Emoji:              manifest.Emoji,
		Icon:               manifest.Icon,
		Description:        manifest.Description,
		License:            manifest.License,
		DocsPath:           filepath.Join(packageDir, "README.md"),
		InstallationSource: assets.installationSources,
		ResourceFiles:      assets.resourceFiles,
		ProjectFile:        projectFile,
		Bootstrap:          manifest.Bootstrap,
		SkillFiles:         assets.extensionFiles.skillFiles,
		AgentFiles:         assets.extensionFiles.agentFiles,
		Warnings:           warnings,
	}
}

func resolveLocalRepositoryPackageManifestNodes(nodes []repositoryPackageManifestNode, packageRoot string) (*resolvedRepositoryPackageAssets, error) {
	assets := &resolvedRepositoryPackageAssets{extensionFiles: &repositoryPackageExtensionFiles{}}
	for _, node := range nodes {
		expandedIncludes, err := expandLocalPackageWildcardIncludes(node.Manifest.Includes, node.PackagePath, packageRoot)
		if err != nil {
			return nil, err
		}
		includeInstallablePaths, includeSkillDirs, includeAgentFiles := splitManifestIncludePaths(expandedIncludes)
		includeInstallablePaths = append(includeInstallablePaths, manifestIncludesFromPaths(node.Manifest.Files)...)
		nodeInstallables, err := normalizeLocalPackageInstallablePaths(includeInstallablePaths, node.PackagePath, packageRoot)
		if err != nil {
			return nil, err
		}
		hasExplicitWorkflowSelector := len(node.Manifest.Files) > 0
		for _, include := range node.Manifest.Includes {
			if include.isMapping() || isSupportedPackageInstallablePath(include.Source) {
				hasExplicitWorkflowSelector = true
				break
			}
			if parent, wildcard := manifestIncludeWildcardParent(include.Source); wildcard &&
				(parent == "workflows" || parent == "agentic-workflows" || parent == constants.WorkflowsDir) {
				hasExplicitWorkflowSelector = true
				break
			}
		}
		if len(nodeInstallables) == 0 && !hasExplicitWorkflowSelector && len(node.Manifest.Imports) == 0 {
			scanned, err := scanLocalRepositoryPackageInstallablePaths(node.PackagePath)
			if err != nil {
				return nil, err
			}
			nodeInstallables = localPackageInstallablesFromScannedPaths(scanned, node.PackagePath)
		}
		assets.installationSources = append(assets.installationSources, nodeInstallables...)

		nodeResources, err := normalizeLocalPackageResourcePaths(node.Manifest.Resources, node.PackagePath)
		if err != nil {
			return nil, err
		}
		assets.resourceFiles = append(assets.resourceFiles, nodeResources...)

		nodeSkillFiles, skillWarnings, err := resolveLocalPackageSkillFiles(node.PackagePath, packageRoot, append(append([]string{}, node.Manifest.Skills...), includeSkillDirs...))
		if err != nil {
			return nil, err
		}
		assets.extensionFiles.skillFiles = append(assets.extensionFiles.skillFiles, nodeSkillFiles...)
		assets.warnings = append(assets.warnings, skillWarnings...)

		nodeAgentFiles, agentWarnings, err := resolveLocalPackageAgentFiles(node.PackagePath, packageRoot, append(append([]string{}, node.Manifest.Agents...), includeAgentFiles...))
		if err != nil {
			return nil, err
		}
		assets.extensionFiles.agentFiles = append(assets.extensionFiles.agentFiles, nodeAgentFiles...)
		assets.warnings = append(assets.warnings, agentWarnings...)
	}
	return assets, nil
}

func expandLocalPackageWildcardIncludes(includes []repositoryPackageInclude, packageDir, packageRoot string) ([]repositoryPackageInclude, error) {
	expanded := make([]repositoryPackageInclude, 0, len(includes))
	for _, include := range includes {
		parent, wildcard := manifestIncludeWildcardParent(include.Source)
		if !wildcard {
			expanded = append(expanded, include)
			continue
		}

		sourceDir := packageDir
		if !include.isMapping() && strings.HasPrefix(parent, constants.GithubDir) {
			sourceDir = packageRoot
		}
		wildcardDir := filepath.Join(sourceDir, filepath.FromSlash(parent))
		resolvedWildcardDir, err := filepath.EvalSymlinks(wildcardDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("failed to resolve includes wildcard %q in %q: %w", include.Source, packageDir, err)
		}
		resolvedSourceDir, err := filepath.EvalSymlinks(sourceDir)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve package directory %q: %w", sourceDir, err)
		}
		relativeToRoot, err := filepath.Rel(resolvedSourceDir, resolvedWildcardDir)
		if err != nil || relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, ".."+string(os.PathSeparator)) {
			return nil, fmt.Errorf("includes wildcard %q resolves outside package directory %q", include.Source, sourceDir)
		}
		entries, err := os.ReadDir(resolvedWildcardDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("failed to expand includes wildcard %q in %q: %w", include.Source, packageDir, err)
		}
		fileCandidates := make([]string, 0, len(entries))
		dirCandidates := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			candidate := path.Join(parent, entry.Name())
			if entry.IsDir() {
				dirCandidates = append(dirCandidates, candidate)
			} else {
				fileCandidates = append(fileCandidates, candidate)
			}
		}
		matches, err := expandManifestWildcardCandidates(include, parent, fileCandidates, dirCandidates)
		if err != nil {
			return nil, fmt.Errorf("failed to expand includes wildcard %q in %q: %w", include.Source, packageDir, err)
		}
		expanded = append(expanded, matches...)
	}
	return deduplicateManifestIncludes(expanded), nil
}

func localRepositoryPackageManifest(source string) (string, string, error) {
	resolvedPath, err := filepath.Abs(source)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve local package source %q: %w", source, err)
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", "", err
	}

	if info.IsDir() {
		manifestPath := filepath.Join(resolvedPath, repositoryPackageManifestFileName)
		if _, err := os.Stat(manifestPath); err != nil {
			return "", "", err
		}
		return manifestPath, resolvedPath, nil
	}

	if filepath.Base(resolvedPath) != repositoryPackageManifestFileName {
		return "", "", nil
	}

	return resolvedPath, filepath.Dir(resolvedPath), nil
}

func normalizeLocalPackageInstallablePaths(includes []repositoryPackageInclude, packageDir, packageRoot string) ([]resolvedPackageInstallable, error) {
	normalized := make([]resolvedPackageInstallable, 0, len(includes))
	seen := make(map[string]struct{})
	for _, include := range includes {
		if !include.isMapping() && !isSupportedPackageInstallablePath(include.Source) {
			continue
		}
		sourceDir := packageDir
		if !include.isMapping() && strings.HasPrefix(filepath.ToSlash(include.Source), constants.GithubDir) {
			sourceDir = packageRoot
		}
		absolutePath := filepath.Clean(filepath.Join(sourceDir, filepath.FromSlash(include.Source)))
		if include.isMapping() {
			if err := validateLocalPackageMappingSource(absolutePath, packageDir, include.Source); err != nil {
				return nil, err
			}
		}
		if _, exists := seen[absolutePath]; exists {
			continue
		}
		seen[absolutePath] = struct{}{}
		destination := include.Destination
		if destination == "" {
			destination = defaultPackageInstallDestination(absolutePath)
		}
		normalized = append(normalized, resolvedPackageInstallable{
			SourcePath:      absolutePath,
			DestinationPath: destination,
		})
	}
	return normalized, nil
}

func localPackageInstallablesFromScannedPaths(sourcePaths []string, packageDir string) []resolvedPackageInstallable {
	absolutePaths := make([]string, 0, len(sourcePaths))
	for _, sourcePath := range sourcePaths {
		absolutePaths = append(absolutePaths, filepath.Join(packageDir, filepath.FromSlash(sourcePath)))
	}
	return packageInstallablesFromSourcePaths(absolutePaths)
}

// validateLocalPackageMappingSource rejects mapping sources that resolve outside the
// package directory or that are symlinks.
func validateLocalPackageMappingSource(absolutePath, packageDir, source string) error {
	cleanedPackageDir := filepath.Clean(packageDir)
	if absolutePath != cleanedPackageDir && !strings.HasPrefix(absolutePath, cleanedPackageDir+string(os.PathSeparator)) {
		return fmt.Errorf("invalid Agentic Workflow manifest in %q: includes source %q resolves outside the package directory", packageDir, source)
	}
	info, err := os.Lstat(absolutePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("invalid Agentic Workflow manifest in %q: includes source %q does not exist", packageDir, source)
		}
		return fmt.Errorf("failed to inspect includes source %q: %w", source, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid Agentic Workflow manifest in %q: includes source %q is a symbolic link, which is not allowed", packageDir, source)
	}
	if info.IsDir() {
		return fmt.Errorf("invalid Agentic Workflow manifest in %q: includes source %q is a directory, but a file is required", packageDir, source)
	}
	return nil
}

// packageInstallableWorkflowName returns the workflow name used when installing a package
// entry. The name is derived from the install destination so that source-to-destination
// mappings install under their declared destination file name.
func packageInstallableWorkflowName(installable resolvedPackageInstallable) string {
	base := path.Base(filepath.ToSlash(installable.DestinationPath))
	return strings.TrimSuffix(base, path.Ext(base))
}

func appendLocalRepositoryPackageWorkflowSpecs(parsedSpecs []*WorkflowSpec, pkg *resolvedRepositoryPackage) []*WorkflowSpec {
	if pkg == nil {
		return parsedSpecs
	}
	for _, installable := range pkg.InstallationSource {
		parsedSpecs = append(parsedSpecs, &WorkflowSpec{
			WorkflowPath:           installable.SourcePath,
			WorkflowName:           packageInstallableWorkflowName(installable),
			DestinationPath:        installable.DestinationPath,
			FromRepositoryManifest: true,
		})
	}
	for _, resource := range pkg.ResourceFiles {
		parsedSpecs = append(parsedSpecs, &WorkflowSpec{
			WorkflowPath:           resource.SourcePath,
			WorkflowName:           packageResourceName(resource),
			DestinationPath:        resource.DestinationPath,
			FromRepositoryManifest: true,
			IsPackageResourceFile:  true,
		})
	}
	if pkg.ProjectFile != nil {
		parsedSpecs = append(parsedSpecs, &WorkflowSpec{
			WorkflowPath:           pkg.ProjectFile.SourcePath,
			WorkflowName:           "aw.json",
			DestinationPath:        pkg.ProjectFile.DestinationPath,
			FromRepositoryManifest: true,
			IsPackageResourceFile:  true,
		})
	}
	for _, skillFile := range pkg.SkillFiles {
		base := filepath.Base(skillFile.SourcePath)
		workflowName := filepath.Join(skillFile.SkillName, strings.TrimSuffix(base, filepath.Ext(base)))
		parsedSpecs = append(parsedSpecs, &WorkflowSpec{
			WorkflowPath:       skillFile.SourcePath,
			WorkflowName:       workflowName,
			IsPackageSkillFile: true,
			SkillName:          skillFile.SkillName,
		})
	}
	for _, agentFile := range pkg.AgentFiles {
		base := filepath.Base(agentFile)
		workflowName := strings.TrimSuffix(base, filepath.Ext(base))
		parsedSpecs = append(parsedSpecs, &WorkflowSpec{
			WorkflowPath:       agentFile,
			WorkflowName:       workflowName,
			IsPackageAgentFile: true,
		})
	}
	return parsedSpecs
}

func resolveLocalPackageSkillFiles(packageDir, packageRoot string, explicitSkillDirs []string) ([]resolvedPackageSkillFile, []string, error) {
	seenSkillDirs := make(map[string]struct{})
	var warnings []string

	var skillDirs []string
	appendIfNew := func(dir string) {
		cleaned := filepath.Clean(dir)
		if _, exists := seenSkillDirs[cleaned]; exists {
			return
		}
		seenSkillDirs[cleaned] = struct{}{}
		skillDirs = append(skillDirs, cleaned)
	}

	for _, dir := range explicitSkillDirs {
		baseDir := packageDir
		if strings.HasPrefix(filepath.ToSlash(dir), constants.GithubDir) {
			baseDir = packageRoot
		}
		appendIfNew(filepath.Join(baseDir, filepath.FromSlash(dir)))
	}
	autoScanned, err := scanLocalPackageSkillDirs(packageDir)
	if err != nil {
		if len(skillDirs) == 0 {
			return nil, nil, err
		}
		warnings = append(warnings, fmt.Sprintf("failed to auto-scan skills directory, proceeding with manifest skills only: %v", err))
	}
	for _, dir := range autoScanned {
		appendIfNew(dir)
	}

	manifestSkillDirSet := make(map[string]struct{}, len(explicitSkillDirs))
	for _, dir := range explicitSkillDirs {
		baseDir := packageDir
		if strings.HasPrefix(filepath.ToSlash(dir), constants.GithubDir) {
			baseDir = packageRoot
		}
		manifestSkillDirSet[filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(dir)))] = struct{}{}
	}

	var skillFiles []resolvedPackageSkillFile
	for _, skillDir := range skillDirs {
		if _, fromManifest := manifestSkillDirSet[skillDir]; fromManifest {
			markerPath := filepath.Join(skillDir, packageSkillMarkerFile)
			if _, err := os.Stat(markerPath); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					warnings = append(warnings, fmt.Sprintf("Skill directory %q is missing required %s marker file", skillDir, packageSkillMarkerFile))
					continue
				}
				return nil, nil, fmt.Errorf("failed to validate skill marker %q: %w", markerPath, err)
			}
		}
		files, err := collectLocalPackageSkillDirFiles(skillDir)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to list files in skill directory %q: %w", skillDir, err)
		}
		skillFiles = append(skillFiles, files...)
	}

	return skillFiles, warnings, nil
}

func collectLocalPackageSkillDirFiles(skillDir string) ([]resolvedPackageSkillFile, error) {
	var skillFiles []resolvedPackageSkillFile
	skillName := filepath.Base(skillDir)
	err := filepath.WalkDir(skillDir, func(currentPath string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		skillFiles = append(skillFiles, resolvedPackageSkillFile{SourcePath: currentPath, SkillName: skillName})
		return nil
	})
	return skillFiles, err
}

func resolveLocalPackageAgentFiles(packageDir, packageRoot string, explicitAgentFiles []string) ([]string, []string, error) {
	if len(explicitAgentFiles) > 0 {
		agentFiles := make([]string, 0, len(explicitAgentFiles))
		for _, sourcePath := range explicitAgentFiles {
			baseDir := packageDir
			if strings.HasPrefix(filepath.ToSlash(sourcePath), constants.GithubDir) {
				baseDir = packageRoot
			}
			agentFiles = append(agentFiles, filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(sourcePath))))
		}
		return agentFiles, nil, nil
	}

	var agentFiles []string
	for _, root := range []string{packageAgentsDirectory, constants.GithubDir + packageAgentsDirectory} {
		agentsDir := filepath.Join(packageDir, filepath.FromSlash(root))
		entries, err := os.ReadDir(agentsDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, nil, fmt.Errorf("failed to scan agents directory %q: %w", agentsDir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
				continue
			}
			agentFiles = append(agentFiles, filepath.Join(agentsDir, entry.Name()))
		}
	}
	return agentFiles, nil, nil
}

func scanLocalPackageSkillDirs(packageDir string) ([]string, error) {
	var skillDirs []string
	for _, root := range []string{packageSkillsDirectory, constants.GithubDir + packageSkillsDirectory} {
		skillsDir := filepath.Join(packageDir, filepath.FromSlash(root))
		entries, err := os.ReadDir(skillsDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("failed to scan skills directory %q: %w", skillsDir, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillDir := filepath.Join(skillsDir, entry.Name())
			if _, err := os.Stat(filepath.Join(skillDir, packageSkillMarkerFile)); err == nil {
				skillDirs = append(skillDirs, skillDir)
			}
		}
	}
	return skillDirs, nil
}

func appendRepositoryPackageWorkflowSpecs(parsedSpecs []*WorkflowSpec, repoSpec *RepoSpec, pkg *resolvedRepositoryPackage) []*WorkflowSpec {
	if pkg == nil {
		return parsedSpecs
	}
	host := explicitHostForRepo(repoSpec.RepoSlug)
	effectiveVersion := repositoryPackageEffectiveRef(repoSpec, pkg)
	for _, installable := range pkg.InstallationSource {
		// Each installable is guaranteed to be either a .md agentic workflow or a .yml
		// action workflow file; no other extensions can reach this point. The workflow
		// name is derived from the install destination so that source-to-destination
		// mappings install under their declared destination name.
		parsedSpecs = append(parsedSpecs, &WorkflowSpec{
			RepoSpec: RepoSpec{
				RepoSlug:    repoSpec.RepoSlug,
				Version:     effectiveVersion,
				PackagePath: repoSpec.PackagePath,
			},
			WorkflowPath:           installable.SourcePath,
			WorkflowName:           packageInstallableWorkflowName(installable),
			DestinationPath:        installable.DestinationPath,
			Host:                   host,
			FromRepositoryManifest: true,
		})
	}
	for _, resource := range pkg.ResourceFiles {
		parsedSpecs = append(parsedSpecs, &WorkflowSpec{
			RepoSpec: RepoSpec{
				RepoSlug:    repoSpec.RepoSlug,
				Version:     effectiveVersion,
				PackagePath: repoSpec.PackagePath,
			},
			WorkflowPath:           resource.SourcePath,
			WorkflowName:           packageResourceName(resource),
			DestinationPath:        resource.DestinationPath,
			Host:                   host,
			FromRepositoryManifest: true,
			IsPackageResourceFile:  true,
		})
	}
	if pkg.ProjectFile != nil {
		parsedSpecs = append(parsedSpecs, &WorkflowSpec{
			RepoSpec: RepoSpec{
				RepoSlug:    repoSpec.RepoSlug,
				Version:     effectiveVersion,
				PackagePath: repoSpec.PackagePath,
			},
			WorkflowPath:           pkg.ProjectFile.SourcePath,
			WorkflowName:           "aw.json",
			DestinationPath:        pkg.ProjectFile.DestinationPath,
			Host:                   host,
			FromRepositoryManifest: true,
			IsPackageResourceFile:  true,
		})
	}
	return appendRepositoryPackageExtensionSpecs(parsedSpecs, repoSpec, pkg, effectiveVersion, host)
}

func appendRepositoryPackageExtensionSpecs(parsedSpecs []*WorkflowSpec, repoSpec *RepoSpec, pkg *resolvedRepositoryPackage, effectiveVersion, host string) []*WorkflowSpec {
	for _, skillFile := range pkg.SkillFiles {
		base := filepath.Base(skillFile.SourcePath)
		workflowName := filepath.Join(skillFile.SkillName, strings.TrimSuffix(base, filepath.Ext(base)))
		parsedSpecs = append(parsedSpecs, &WorkflowSpec{
			RepoSpec: RepoSpec{
				RepoSlug:    repoSpec.RepoSlug,
				Version:     effectiveVersion,
				PackagePath: repoSpec.PackagePath,
			},
			WorkflowPath:       skillFile.SourcePath,
			WorkflowName:       workflowName,
			Host:               host,
			IsPackageSkillFile: true,
			SkillName:          skillFile.SkillName,
		})
	}
	for _, agentFile := range pkg.AgentFiles {
		base := filepath.Base(agentFile)
		workflowName := strings.TrimSuffix(base, filepath.Ext(base))
		parsedSpecs = append(parsedSpecs, &WorkflowSpec{
			RepoSpec: RepoSpec{
				RepoSlug:    repoSpec.RepoSlug,
				Version:     effectiveVersion,
				PackagePath: repoSpec.PackagePath,
			},
			WorkflowPath:       agentFile,
			WorkflowName:       workflowName,
			Host:               host,
			IsPackageAgentFile: true,
		})
	}
	return parsedSpecs
}

func resolveAddWorkflowSpecAndContent(ctx context.Context, initialSpec *WorkflowSpec, verbose bool) (*WorkflowSpec, *FetchedWorkflow, error) {
	currentSpec := *initialSpec
	visited := make(map[string]struct{})
	followedRedirect := false
	for range maxRedirectDepth {
		fetched, err := fetchWorkflowFromSourceWithContextFn(ctx, &currentSpec, verbose)
		if err != nil {
			return nil, nil, err
		}
		if fetched.IsLocal {
			return &currentSpec, fetched, nil
		}
		currentRef := currentSpec.Version
		if currentRef == "" {
			currentRef = "main"
		}
		locationKey := fmt.Sprintf("%s/%s@%s", currentSpec.RepoSlug, currentSpec.WorkflowPath, currentRef)
		if _, exists := visited[locationKey]; exists {
			return nil, nil, fmt.Errorf("redirect loop detected at %s", locationKey)
		}
		visited[locationKey] = struct{}{}
		redirect, err := extractRedirectFromContent(string(fetched.Content))
		if err != nil {
			return nil, nil, err
		}
		if redirect == "" {
			if followedRedirect {
				currentSpec.WorkflowName = initialSpec.WorkflowName
			}
			return &currentSpec, fetched, nil
		}
		redirectedSource, err := normalizeRedirectToSourceSpec(redirect)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid redirect %q in %s: %w", redirect, locationKey, err)
		}
		nextSpec := &WorkflowSpec{
			RepoSpec: RepoSpec{
				RepoSlug: redirectedSource.Repo,
				Version:  redirectedSource.Ref,
			},
			WorkflowPath: redirectedSource.Path,
			WorkflowName: normalizeWorkflowID(redirectedSource.Path),
			Host:         currentSpec.Host,
		}
		resolutionLog.Printf("Following redirect for add: from=%s to=%s", locationKey, nextSpec.String())
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Workflow redirect: %s -> %s", locationKey, nextSpec.String())))
		}
		followedRedirect = true
		currentSpec = *nextSpec
	}
	return nil, nil, fmt.Errorf("redirect chain exceeded maximum depth (%d) for workflow '%s'", maxRedirectDepth, initialSpec.String())
}

// expandLocalWildcardWorkflows expands wildcard workflow specifications for local workflows only.
func expandLocalWildcardWorkflows(specs []*WorkflowSpec, verbose bool) ([]*WorkflowSpec, error) {
	expandedWorkflows := []*WorkflowSpec{}

	for _, spec := range specs {
		if spec.IsWildcard && isLocalWorkflowPath(spec.WorkflowPath) {
			resolutionLog.Printf("Expanding local wildcard: %s", spec.WorkflowPath)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Discovering local workflows matching %s...", spec.WorkflowPath)))
			}

			// Expand local wildcard (e.g., ./*.md or ./workflows/*.md)
			discovered, err := expandLocalWildcard(spec)
			if err != nil {
				return nil, fmt.Errorf("failed to expand wildcard %s: %w", spec.WorkflowPath, err)
			}

			if len(discovered) == 0 {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No workflows found matching "+spec.WorkflowPath))
			} else {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Found %d workflow(s)", len(discovered))))
				}
				expandedWorkflows = append(expandedWorkflows, discovered...)
			}
		} else {
			expandedWorkflows = append(expandedWorkflows, spec)
		}
	}

	if len(expandedWorkflows) == 0 {
		return nil, errors.New("no workflows to add after expansion")
	}

	return expandedWorkflows, nil
}

// checkWorkflowHasDispatchFromContent checks if workflow content has a workflow_dispatch trigger
func checkWorkflowHasDispatchFromContent(content string) bool {
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		return false
	}

	onSection, exists := result.Frontmatter["on"]
	if !exists {
		return false
	}

	switch on := onSection.(type) {
	case map[string]any:
		_, hasDispatch := on["workflow_dispatch"]
		return hasDispatch
	case string:
		return strings.Contains(strings.ToLower(on), "workflow_dispatch")
	case []any:
		for _, item := range on {
			if str, ok := item.(string); ok && strings.EqualFold(str, "workflow_dispatch") {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// expandLocalWildcard expands a local wildcard path (e.g., ./*.md) into individual workflow specs
func expandLocalWildcard(spec *WorkflowSpec) ([]*WorkflowSpec, error) {
	pattern := spec.WorkflowPath

	// Use filepath.Glob to expand the pattern
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid wildcard pattern %s: %w", pattern, err)
	}

	if len(matches) == 0 {
		return nil, nil
	}

	mdMatches := sliceutil.Filter(matches, func(m string) bool {
		return strings.HasSuffix(m, ".md")
	})
	result := sliceutil.Map(mdMatches, func(match string) *WorkflowSpec {
		return &WorkflowSpec{
			RepoSpec: RepoSpec{
				RepoSlug: spec.RepoSlug,
				Version:  spec.Version,
			},
			WorkflowPath: match,
			WorkflowName: normalizeWorkflowID(match),
			IsWildcard:   false,
		}
	})

	return result, nil
}
