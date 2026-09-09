// add_package_manifest_skills.go: discovering and resolving skill directories
// (SKILL.md) and agent files from a remote package.

package cli

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// resolvePackageSkillFiles returns the list of resolvedPackageSkillFile for a package.
// Manifest-specified skills (explicitSkillDirs) are resolved first. After that, the
// skills/ directory is always auto-scanned for any additional skill subdirectories that
// contain a SKILL.md file but are not already covered by the manifest. Each skill folder
// is traversed recursively so that all nested files are included.
func resolvePackageSkillFiles(ctx context.Context, owner, repo, packagePath, ref, host string, explicitSkillDirs []string) ([]resolvedPackageSkillFile, []string, error) {
	addPackageManifestLog.Printf("Resolving skill files for %s/%s (path=%q, ref=%s, %d explicit dirs)", owner, repo, packagePath, ref, len(explicitSkillDirs))

	// Step 1: resolve manifest skills first (explicit dirs).
	manifestSkillDirs := normalizeManifestSkillDirs(explicitSkillDirs, packagePath)
	skillDirs, warnings, err := resolvePackageSkillDirs(ctx, owner, repo, packagePath, ref, host, manifestSkillDirs)
	if err != nil {
		return nil, nil, err
	}

	// manifestSkillDirSet is used to know which dirs require a SKILL.md marker check.
	manifestSkillDirSet := make(map[string]struct{}, len(manifestSkillDirs))
	for _, d := range manifestSkillDirs {
		manifestSkillDirSet[d] = struct{}{}
	}

	var skillFiles []resolvedPackageSkillFile
	for _, skillDir := range skillDirs {
		files, fileWarnings, err := resolvePackageSkillDirFiles(ctx, owner, repo, ref, host, skillDir, manifestSkillDirSet)
		if err != nil {
			return nil, nil, err
		}
		warnings = append(warnings, fileWarnings...)
		skillFiles = append(skillFiles, files...)
	}
	return skillFiles, warnings, nil
}

func normalizeManifestSkillDirs(explicitSkillDirs []string, packagePath string) []string {
	var manifestSkillDirs []string
	for _, dir := range explicitSkillDirs {
		if strings.HasPrefix(filepath.ToSlash(dir), constants.GithubDir) {
			manifestSkillDirs = append(manifestSkillDirs, filepath.ToSlash(dir))
		} else {
			manifestSkillDirs = append(manifestSkillDirs, joinRepositoryPackagePath(packagePath, dir))
		}
	}
	return manifestSkillDirs
}

func resolvePackageSkillDirs(ctx context.Context, owner, repo, packagePath, ref, host string, manifestSkillDirs []string) ([]string, []string, error) {
	var warnings []string
	// Step 2: always auto-scan and append any skills not already in the manifest.
	autoScanned, err := scanPackageSkillDirs(ctx, owner, repo, packagePath, ref, host)
	if err != nil {
		// Auto-scan is supplementary for manifest-declared skills; preserve manifest
		// resolution even when scan fails transiently.
		if len(manifestSkillDirs) == 0 {
			return nil, nil, err
		}
		addPackageManifestLog.Printf("Skills auto-scan failed, proceeding with %d manifest-declared dirs only: %v", len(manifestSkillDirs), err)
		warnings = append(warnings, fmt.Sprintf("failed to auto-scan skills directory, proceeding with manifest skills only: %v", err))
	}

	// Build the final ordered list: manifest skills first, then auto-scanned extras.
	var skillDirs []string
	seenSkillDirs := make(map[string]struct{})
	for _, dir := range append(manifestSkillDirs, autoScanned...) {
		if _, exists := seenSkillDirs[dir]; exists {
			continue
		}
		seenSkillDirs[dir] = struct{}{}
		skillDirs = append(skillDirs, dir)
	}
	return skillDirs, warnings, nil
}

func resolvePackageSkillDirFiles(ctx context.Context, owner, repo, ref, host, skillDir string, manifestSkillDirSet map[string]struct{}) ([]resolvedPackageSkillFile, []string, error) {
	var warnings []string
	// For skills that came from the manifest, validate that the SKILL.md marker
	// exists so that typos in the manifest surface as clear warnings.
	if _, fromManifest := manifestSkillDirSet[skillDir]; fromManifest {
		markerPath := joinRepositoryPackagePath(skillDir, packageSkillMarkerFile)
		if _, err := downloadPackageFileFromGitHubForHost(ctx, owner, repo, markerPath, ref, host); err != nil {
			if isRepositoryFileNotFound(err) {
				return nil, []string{fmt.Sprintf("Skill directory %q is missing required %s marker file", skillDir, packageSkillMarkerFile)}, nil
			}
			return nil, nil, fmt.Errorf("failed to validate skill marker %q (check the repository, ref, and network connectivity): %w", markerPath, err)
		}
	}

	// Use recursive listing so that the entire skill folder (including any
	// subdirectories) is copied, not just the top-level files.
	files, err := listPackageDirFilesRecursivelyForHost(ctx, owner, repo, ref, skillDir, host)
	if err != nil {
		if isRepositoryFileNotFound(err) {
			warnings = append(warnings, fmt.Sprintf("Skill directory %q not found in package, skipping", skillDir))
			return nil, warnings, nil
		}
		return nil, nil, fmt.Errorf("failed to list files in skill directory %q (check the repository, ref, and network connectivity): %w", skillDir, err)
	}

	skillFiles := make([]resolvedPackageSkillFile, 0, len(files))
	skillName := filepath.Base(skillDir)
	for _, file := range files {
		skillFiles = append(skillFiles, resolvedPackageSkillFile{
			SourcePath: file,
			SkillName:  skillName,
		})
	}
	return skillFiles, warnings, nil
}

// resolvePackageAgentFiles returns the list of agent file source paths for a package.
// If explicitAgentFiles is non-empty it is used; otherwise the agents/ directory is
// auto-scanned for .md files.
func resolvePackageAgentFiles(ctx context.Context, owner, repo, packagePath, ref, host string, explicitAgentFiles []string) ([]string, []string, error) {
	if len(explicitAgentFiles) > 0 {
		var agentFiles []string
		for _, f := range explicitAgentFiles {
			if strings.HasPrefix(filepath.ToSlash(f), constants.GithubDir) {
				agentFiles = append(agentFiles, filepath.ToSlash(f))
			} else {
				agentFiles = append(agentFiles, joinRepositoryPackagePath(packagePath, f))
			}
		}
		return agentFiles, nil, nil
	}

	var agentFiles []string
	for _, root := range []string{packageAgentsDirectory, constants.GithubDir + packageAgentsDirectory} {
		agentsDir := joinRepositoryPackagePath(packagePath, root)
		files, err := listPackageDirFilesForHost(ctx, owner, repo, ref, agentsDir, host)
		if err != nil {
			if isRepositoryFileNotFound(err) {
				continue
			}
			return nil, nil, fmt.Errorf("failed to scan agents directory %q (check the repository, ref, and network connectivity): %w", agentsDir, err)
		}
		for _, f := range files {
			if strings.HasSuffix(strings.ToLower(f), ".md") {
				agentFiles = append(agentFiles, f)
			}
		}
	}
	return agentFiles, nil, nil
}

// scanPackageSkillDirs auto-scans the skills/ directory of a package and returns the paths
// of skill subdirectories (those that contain a SKILL.md file).
func scanPackageSkillDirs(ctx context.Context, owner, repo, packagePath, ref, host string) ([]string, error) {
	var skillDirs []string
	for _, root := range []string{packageSkillsDirectory, constants.GithubDir + packageSkillsDirectory} {
		skillsDir := joinRepositoryPackagePath(packagePath, root)
		subdirs, err := listPackageDirSubdirsForHost(ctx, owner, repo, ref, skillsDir, host)
		if err != nil {
			if isRepositoryFileNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("failed to scan skills directory %q (check the repository, ref, and network connectivity): %w", skillsDir, err)
		}
		for _, subdir := range subdirs {
			markerPath := joinRepositoryPackagePath(subdir, packageSkillMarkerFile)
			if _, err := downloadPackageFileFromGitHubForHost(ctx, owner, repo, markerPath, ref, host); err == nil {
				skillDirs = append(skillDirs, subdir)
			}
		}
	}
	addPackageManifestLog.Printf("Auto-scan found %d skill directories under %s/%s (path=%q)", len(skillDirs), owner, repo, packagePath)
	return skillDirs, nil
}

func scanRepositoryPackageInstallablePaths(ctx context.Context, owner, repo, packagePath, ref, host string) ([]string, error) {
	var collected []string
	seen := make(map[string]struct{})

	for _, sourceDir := range packageSourceDirectories {
		sourcePath := joinRepositoryPackagePath(packagePath, sourceDir)
		files, err := listPackageWorkflowFilesForHost(ctx, owner, repo, ref, sourcePath, host)
		if err != nil {
			if isRepositoryFileNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("failed to scan %q in %s/%s@%s (check the repository, ref, and network connectivity): %w", sourcePath, owner, repo, ref, err)
		}

		for _, file := range files {
			// listPackageWorkflowFilesForHost returns full repo-root-relative paths
			// (e.g. "folder/workflows/foo.md" when scanning "folder/workflows/").
			// isSupportedPackageInstallablePath expects package-relative paths, so
			// strip the package prefix before validation for nested bundles.
			pathToValidate := file
			if packagePath != "" {
				pathToValidate = strings.TrimPrefix(file, packagePath+"/")
			}
			if !isSupportedPackageInstallablePath(pathToValidate) {
				continue
			}
			if _, exists := seen[file]; exists {
				continue
			}
			seen[file] = struct{}{}
			collected = append(collected, file)
		}
	}

	return collected, nil
}

func resolveRepositoryPackageDocsPath(ctx context.Context, owner, repo, packagePath, ref, host string) (string, error) {
	readmePath := joinRepositoryPackagePath(packagePath, "README.md")
	repoSlug := path.Join(owner, repo)
	packageID := repositoryPackageIdentifier(repoSlug, packagePath)
	if _, err := downloadPackageFileFromGitHubForHost(ctx, owner, repo, readmePath, ref, host); err == nil {
		return readmePath, nil
	} else if isRepositoryFileNotFound(err) {
		return "", fmt.Errorf("repository %q is not a valid Agentic Workflow package: missing required README.md at %q. Add a README.md describing the package. Example:\n# My Package\n\nDescribe what this package does", packageID, readmePath)
	} else {
		return "", fmt.Errorf("failed to read package README %q from %s/%s@%s (check the repository, ref, and network connectivity): %w", readmePath, owner, repo, ref, err)
	}
}

func repositoryPackageIdentifier(repoSlug, packagePath string) string {
	if packagePath == "" {
		return repoSlug
	}
	return path.Join(repoSlug, packagePath)
}
