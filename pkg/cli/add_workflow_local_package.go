package cli

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow"
)

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
	importRoot := localPackageImportRoot(packageDir)
	manifestNodes, importWarnings, err := resolveRepositoryPackageManifestGraph(manifestPath, manifest, importRoot, func(importPath string) ([]byte, error) {
		return readLocalImportedManifest(importPath, importRoot)
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

	assets, err := resolveLocalRepositoryPackageManifestNodes(manifestNodes, importRoot)
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
