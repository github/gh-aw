package cli

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

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
