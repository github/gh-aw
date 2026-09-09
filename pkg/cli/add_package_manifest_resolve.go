// add_package_manifest_resolve.go: top-level orchestration of resolving a remote package
// (manifest + files) given a repo spec. See add_package_manifest.go for shared types.

package cli

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow"
)

type resolvedRepositoryPackageAssets struct {
	installationSources []resolvedPackageInstallable
	resourceFiles       []resolvedPackageResource
	extensionFiles      *repositoryPackageExtensionFiles
	warnings            []string
}

func resolveRepositoryPackage(ctx context.Context, repoSpec *RepoSpec, host string) (*resolvedRepositoryPackage, error) {
	addPackageManifestLog.Printf("Resolving repository package %q (packagePath=%q, host=%q)", repoSpec.RepoSlug, repoSpec.PackagePath, host)
	owner, repo, err := splitRepositoryPackageSlug(repoSpec.RepoSlug)
	if err != nil {
		return nil, err
	}
	ref := resolveRepositoryPackageRef(ctx, repoSpec, host)
	packagePath := strings.Trim(repoSpec.PackagePath, "/")

	manifestPath, manifestContent, err := loadRepositoryPackageManifestFile(ctx, owner, repo, packagePath, ref, host)
	if err != nil {
		return nil, err
	}

	manifest, warnings, err := parseRepositoryPackageManifest(manifestPath, manifestContent)
	if err != nil {
		return nil, err
	}
	manifestNodes, importWarnings, err := resolveRepositoryPackageManifestGraph(manifestPath, manifest, func(importPath string) ([]byte, error) {
		return downloadPackageFileFromGitHubForHost(ctx, owner, repo, importPath, ref, host)
	})
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, importWarnings...)

	assets, err := resolveRepositoryPackageManifestNodes(ctx, owner, repo, ref, host, repoSpec.RepoSlug, manifestNodes)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, assets.warnings...)
	projectFile, err := resolveRepositoryPackageProjectFile(ctx, owner, repo, packagePath, ref, host)
	if err != nil {
		return nil, err
	}
	if err := validateUniqueResolvedPackageFiles(assets.installationSources, assets.resourceFiles, assets.extensionFiles.skillFiles, assets.extensionFiles.agentFiles, manifestPath); err != nil {
		return nil, err
	}
	if err := validateUniqueManifestWorkflowFilenames(assets.installationSources, manifestPath); err != nil {
		return nil, err
	}

	docsPath, err := resolveRepositoryPackageDocsPath(ctx, owner, repo, packagePath, ref, host)
	if err != nil {
		return nil, err
	}
	if len(assets.installationSources) == 0 && len(assets.resourceFiles) == 0 && len(assets.extensionFiles.skillFiles) == 0 && len(assets.extensionFiles.agentFiles) == 0 && projectFile == nil {
		return nil, fmt.Errorf("repository %q does not contain any installable workflows, resources, skills, agents, or aw.json project settings (either explicitly declared or auto-discovered). Add workflows under 'workflows/', resources in aw.yml, skills under 'skills/', agents under 'agents/', or an aw.json file", repositoryPackageIdentifier(repoSpec.RepoSlug, packagePath))
	}
	pkg := newResolvedRepositoryPackage(manifestPath, ref, docsPath, manifest, assets.installationSources, assets.resourceFiles, assets.extensionFiles, warnings)
	pkg.ProjectFile = projectFile
	return pkg, nil
}

func resolveRepositoryPackageProjectFile(ctx context.Context, owner, repo, packagePath, ref, host string) (*resolvedPackageResource, error) {
	sourcePath := joinRepositoryPackagePath(packagePath, workflow.RepoConfigFileName)
	if _, err := downloadPackageFileFromGitHubForHost(ctx, owner, repo, sourcePath, ref, host); err != nil {
		if isRepositoryFileNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read package project file %q: %w", sourcePath, err)
	}
	return &resolvedPackageResource{
		SourcePath:      sourcePath,
		DestinationPath: workflow.RepoConfigFileName,
	}, nil
}

func resolveRepositoryPackageManifestNodes(ctx context.Context, owner, repo, ref, host, repoSlug string, nodes []repositoryPackageManifestNode) (*resolvedRepositoryPackageAssets, error) {
	assets := &resolvedRepositoryPackageAssets{extensionFiles: &repositoryPackageExtensionFiles{}}
	// Shared across every manifest node so that multiple imported manifests referencing the
	// same repository-root grader evaluator are deduplicated instead of being reported as
	// conflicting duplicate install destinations by validateUniqueResolvedPackageFiles.
	seenGraderEvaluators := make(map[string]string)
	for _, node := range nodes {
		visibilityWarnings, err := validateRepositoryPackageVisibility(node.Manifest, repositoryPackageIdentifier(repoSlug, node.PackagePath))
		if err != nil {
			return nil, err
		}
		assets.warnings = append(assets.warnings, visibilityWarnings...)

		nodeInstallables, includeSkillDirs, includeAgentFiles, err := resolveRepositoryPackageInstallablePaths(ctx, owner, repo, node.PackagePath, ref, host, node.Manifest, node.Path)
		if err != nil {
			return nil, err
		}
		assets.installationSources = append(assets.installationSources, nodeInstallables...)
		nodeResources := normalizePackageResourcePaths(node.Manifest.Resources, node.PackagePath)
		nodeResources, err = appendPackageGraderEvaluatorResources(ctx, owner, repo, ref, host, nodeResources, nodeInstallables, seenGraderEvaluators)
		if err != nil {
			return nil, err
		}
		assets.resourceFiles = append(assets.resourceFiles, nodeResources...)

		nodeExtensionFiles, err := resolveRepositoryPackageExtensionFiles(ctx, repositoryPackageExtensionFilesOptions{
			owner:             owner,
			repo:              repo,
			packagePath:       node.PackagePath,
			ref:               ref,
			host:              host,
			manifest:          node.Manifest,
			includeSkillDirs:  includeSkillDirs,
			includeAgentFiles: includeAgentFiles,
		})
		if err != nil {
			return nil, err
		}
		assets.extensionFiles.skillFiles = append(assets.extensionFiles.skillFiles, nodeExtensionFiles.skillFiles...)
		assets.extensionFiles.agentFiles = append(assets.extensionFiles.agentFiles, nodeExtensionFiles.agentFiles...)
		assets.warnings = append(assets.warnings, nodeExtensionFiles.warnings...)
	}
	return assets, nil
}

// appendPackageGraderEvaluatorResources discovers grader evaluator resources referenced by the
// given installable package workflows and appends them to resourceFiles. seen tracks previously
// resolved evaluator destinations across the entire manifest graph (i.e. across every call for
// every node), so that multiple manifest nodes referencing the identical repository-root
// evaluator are deduplicated instead of tripping the later duplicate-destination validation,
// while still rejecting genuinely conflicting sources for the same destination.
func appendPackageGraderEvaluatorResources(ctx context.Context, owner, repo, ref, host string, resourceFiles []resolvedPackageResource, installationSources []resolvedPackageInstallable, seen map[string]string) ([]resolvedPackageResource, error) {
	for _, resource := range resourceFiles {
		seen[packageResourceDestinationKey(resource.DestinationPath)] = resource.SourcePath
	}
	addPackageManifestLog.Printf("resolving grader evaluators from %d installable package source(s)", len(installationSources))
	for _, installable := range installationSources {
		if !strings.HasSuffix(strings.ToLower(installable.SourcePath), ".md") {
			continue
		}
		content, err := downloadPackageFileFromGitHubForHost(ctx, owner, repo, installable.SourcePath, ref, host)
		if err != nil {
			if isRepositoryFileNotFound(err) {
				addPackageManifestLog.Printf("skipping grader evaluator resource discovery for unavailable package workflow %q: %v", installable.SourcePath, err)
				continue
			}
			return nil, fmt.Errorf("failed to read package workflow %q while resolving grader evaluator resources: %w", installable.SourcePath, err)
		}
		entries, err := extractResourceEntries(string(content))
		if err != nil {
			return nil, fmt.Errorf("failed to parse package workflow %q grader resources: %w", installable.SourcePath, err)
		}
		for _, entry := range entries {
			if !entry.isGraderEvaluator {
				continue
			}
			resource := packageGraderEvaluatorResource(installable, entry.path)
			key := packageResourceDestinationKey(resource.DestinationPath)
			if previousSource, exists := seen[key]; exists {
				if previousSource != resource.SourcePath {
					return nil, fmt.Errorf("package workflows reference multiple grader evaluator resources for %q: %q and %q", resource.DestinationPath, previousSource, resource.SourcePath)
				}
				continue
			}
			seen[key] = resource.SourcePath
			resourceFiles = append(resourceFiles, resource)
		}
	}
	return resourceFiles, nil
}

func packageGraderEvaluatorResource(installable resolvedPackageInstallable, runPath string) resolvedPackageResource {
	if localPath, ok := strings.CutPrefix(runPath, "./"); ok {
		return resolvedPackageResource{
			SourcePath:      path.Join(path.Dir(installable.SourcePath), localPath),
			DestinationPath: path.Join(path.Dir(installable.DestinationPath), localPath),
		}
	}
	return resolvedPackageResource{
		SourcePath:      runPath,
		DestinationPath: runPath,
	}
}

func splitRepositoryPackageSlug(repoSlug string) (string, string, error) {
	parts := strings.SplitN(repoSlug, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("repository slug %q is not in 'owner/repo' format. Example: owner/repo", repoSlug)
	}
	return parts[0], parts[1], nil
}

func resolveRepositoryPackageRef(ctx context.Context, repoSpec *RepoSpec, host string) string {
	// At manifest-fetch time there is no resolved package metadata yet.
	ref := repositoryPackageEffectiveRef(repoSpec, nil)
	if ref != "" {
		return ref
	}
	if isGhAwRepository(repoSpec.RepoSlug) {
		if latestRelease, err := getRepositoryPackageLatestRelease(ctx, repoSpec.RepoSlug, host); err == nil {
			return latestRelease
		} else {
			addPackageManifestLog.Printf("failed to resolve latest release for %s (host=%q): %v", repoSpec.RepoSlug, host, err)
		}
	}
	ref = "main"
	if defaultBranch, err := getRepositoryPackageDefaultBranch(ctx, repoSpec.RepoSlug, host); err == nil {
		ref = defaultBranch
	} else {
		addPackageManifestLog.Printf("failed to resolve default branch for %s (host=%q), falling back to %q: %v", repoSpec.RepoSlug, host, ref, err)
	}
	return ref
}

func resolveRepositoryPackageInstallablePaths(ctx context.Context, owner, repo, packagePath, ref, host string, manifest *repositoryPackageManifest, manifestPath string) ([]resolvedPackageInstallable, []string, []string, error) {
	expandedIncludes, err := expandRepositoryPackageWildcardIncludes(ctx, owner, repo, packagePath, ref, host, manifest.Includes)
	if err != nil {
		return nil, nil, nil, err
	}
	includeInstallablePaths, includeSkillDirs, includeAgentFiles := splitManifestIncludePaths(expandedIncludes)
	includeInstallablePaths = append(includeInstallablePaths, manifestIncludesFromPaths(manifest.Files)...)
	hasExplicitWorkflowSelector := len(manifest.Files) > 0
	for _, include := range manifest.Includes {
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

	installationSources := normalizePackageInstallablePaths(includeInstallablePaths, packagePath)
	if len(installationSources) == 0 && !hasExplicitWorkflowSelector && len(manifest.Imports) == 0 {
		addPackageManifestLog.Print("No explicit installable paths in manifest, scanning repository for installables")
		scanned, err := scanRepositoryPackageInstallablePaths(ctx, owner, repo, packagePath, ref, host)
		if err != nil {
			return nil, nil, nil, err
		}
		installationSources = packageInstallablesFromSourcePaths(scanned)
	}
	addPackageManifestLog.Printf("Resolved %d installable source(s) for package", len(installationSources))
	if err := validateUniqueManifestWorkflowFilenames(installationSources, manifestPath); err != nil {
		return nil, nil, nil, err
	}
	if err := validateUniqueManifestInstallDestinations(installationSources, manifestPath); err != nil {
		return nil, nil, nil, err
	}
	return installationSources, includeSkillDirs, includeAgentFiles, nil
}

func expandRepositoryPackageWildcardIncludes(ctx context.Context, owner, repo, packagePath, ref, host string, includes []repositoryPackageInclude) ([]repositoryPackageInclude, error) {
	expanded := make([]repositoryPackageInclude, 0, len(includes))
	for _, include := range includes {
		parent, wildcard := manifestIncludeWildcardParent(include.Source)
		if !wildcard {
			expanded = append(expanded, include)
			continue
		}

		remoteParent := parent
		rootRelative := !include.isMapping() && strings.HasPrefix(parent, constants.GithubDir)
		if packagePath != "" && !rootRelative {
			remoteParent = joinRepositoryPackagePath(packagePath, parent)
		}
		files, err := listPackageDirFilesForHost(ctx, owner, repo, ref, remoteParent, host)
		if err != nil && !isRepositoryFileNotFound(err) {
			return nil, fmt.Errorf("failed to expand includes wildcard %q in %s/%s@%s: %w", include.Source, owner, repo, ref, err)
		}
		var dirs []string
		if !include.isMapping() {
			dirs, err = listPackageDirSubdirsForHost(ctx, owner, repo, ref, remoteParent, host)
			if err != nil && !isRepositoryFileNotFound(err) {
				return nil, fmt.Errorf("failed to expand includes wildcard %q in %s/%s@%s: %w", include.Source, owner, repo, ref, err)
			}
		}

		fileCandidates := make([]string, 0, len(files))
		for _, candidate := range files {
			candidate = filepath.ToSlash(candidate)
			if packagePath != "" && !rootRelative {
				candidate = strings.TrimPrefix(candidate, strings.TrimSuffix(packagePath, "/")+"/")
			}
			fileCandidates = append(fileCandidates, candidate)
		}
		if !include.isMapping() {
			for i, candidate := range dirs {
				candidate = filepath.ToSlash(candidate)
				if packagePath != "" && !rootRelative {
					candidate = strings.TrimPrefix(candidate, strings.TrimSuffix(packagePath, "/")+"/")
				}
				dirs[i] = candidate
			}
		}
		matches, err := expandManifestWildcardCandidates(include, parent, fileCandidates, dirs)
		if err != nil {
			return nil, fmt.Errorf("failed to expand includes wildcard %q in %s/%s@%s: %w", include.Source, owner, repo, ref, err)
		}
		expanded = append(expanded, matches...)
	}
	return deduplicateManifestIncludes(expanded), nil
}

type repositoryPackageExtensionFiles struct {
	skillFiles []resolvedPackageSkillFile
	agentFiles []string
	warnings   []string
}

type repositoryPackageExtensionFilesOptions struct {
	owner             string
	repo              string
	packagePath       string
	ref               string
	host              string
	manifest          *repositoryPackageManifest
	includeSkillDirs  []string
	includeAgentFiles []string
}

func resolveRepositoryPackageExtensionFiles(ctx context.Context, options repositoryPackageExtensionFilesOptions) (*repositoryPackageExtensionFiles, error) {
	// Resolve skill files: explicit from manifest or auto-scanned.
	explicitSkillDirs := append([]string{}, options.manifest.Skills...)
	explicitSkillDirs = append(explicitSkillDirs, options.includeSkillDirs...)
	skillFiles, skillWarnings, err := resolvePackageSkillFiles(ctx, options.owner, options.repo, options.packagePath, options.ref, options.host, explicitSkillDirs)
	if err != nil {
		return nil, err
	}

	// Resolve agent files: explicit from manifest or auto-scanned.
	explicitAgentFiles := append([]string{}, options.manifest.Agents...)
	explicitAgentFiles = append(explicitAgentFiles, options.includeAgentFiles...)
	agentFiles, agentWarnings, err := resolvePackageAgentFiles(ctx, options.owner, options.repo, options.packagePath, options.ref, options.host, explicitAgentFiles)
	if err != nil {
		return nil, err
	}

	warnings := append(skillWarnings, agentWarnings...)
	return &repositoryPackageExtensionFiles{
		skillFiles: skillFiles,
		agentFiles: agentFiles,
		warnings:   warnings,
	}, nil
}

func newResolvedRepositoryPackage(manifestPath, ref, docsPath string, manifest *repositoryPackageManifest, installationSources []resolvedPackageInstallable, resourceFiles []resolvedPackageResource, extensionFiles *repositoryPackageExtensionFiles, warnings []string) *resolvedRepositoryPackage {
	return &resolvedRepositoryPackage{
		ManifestPath:       manifestPath,
		ResolvedRef:        ref,
		Name:               manifest.Name,
		Emoji:              manifest.Emoji,
		Icon:               manifest.Icon,
		Description:        manifest.Description,
		License:            manifest.License,
		Private:            manifest.Private,
		Experimental:       manifest.Experimental,
		DocsPath:           docsPath,
		InstallationSource: installationSources,
		ResourceFiles:      resourceFiles,
		Bootstrap:          manifest.Bootstrap,
		SkillFiles:         extensionFiles.skillFiles,
		AgentFiles:         extensionFiles.agentFiles,
		Warnings:           warnings,
	}
}

func loadRepositoryPackageManifestFile(ctx context.Context, owner, repo, packagePath, ref, host string) (string, []byte, error) {
	manifestPath := joinRepositoryPackagePath(packagePath, repositoryPackageManifestFileName)
	repoSlug := fmt.Sprintf("%s/%s", owner, repo)
	packageID := repositoryPackageIdentifier(repoSlug, packagePath)
	content, err := downloadPackageFileFromGitHubForHost(ctx, owner, repo, manifestPath, ref, host)
	if err != nil {
		if !isRepositoryFileNotFound(err) {
			return "", nil, fmt.Errorf("failed to read manifest %q from %s/%s@%s (check the repository, ref, and network connectivity): %w", manifestPath, owner, repo, ref, err)
		}
		if packagePath != "" {
			return "", nil, fmt.Errorf("%w: repository %q is not a valid Agentic Workflow package: no aw.yml manifest found in %q. Add %s or use an explicit workflow path", errRepositoryPackageManifestNotFound, packageID, packagePath, manifestPath)
		}
		return "", nil, fmt.Errorf("%w: repository %q is not a valid Agentic Workflow package: no aw.yml manifest found at the repository root. Add aw.yml or use an explicit workflow path", errRepositoryPackageManifestNotFound, repoSlug)
	}

	return manifestPath, content, nil
}
