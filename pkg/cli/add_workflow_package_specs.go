package cli

import (
	"path/filepath"
	"strings"
)

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
	firstPackageSpec := len(parsedSpecs)
	host := explicitHostForRepo(repoSpec.RepoSlug)
	effectiveVersion := repositoryPackageEffectiveRef(repoSpec, pkg)
	for _, installable := range pkg.InstallationSource {
		// Each installable is guaranteed to be either a .md agentic workflow or a .yml
		// action workflow file; no other extensions can reach this point. The workflow
		// name is derived from the install destination so that source-to-destination
		// mappings install under their declared destination name.
		parsedSpecs = append(parsedSpecs, &WorkflowSpec{
			RepoSlug:               repoSpec.RepoSlug,
			Version:                effectiveVersion,
			PackagePath:            repoSpec.PackagePath,
			WorkflowPath:           installable.SourcePath,
			WorkflowName:           packageInstallableWorkflowName(installable),
			DestinationPath:        installable.DestinationPath,
			Host:                   host,
			FromRepositoryManifest: true,
		})
	}
	for _, resource := range pkg.ResourceFiles {
		parsedSpecs = append(parsedSpecs, &WorkflowSpec{
			RepoSlug:               repoSpec.RepoSlug,
			Version:                effectiveVersion,
			PackagePath:            repoSpec.PackagePath,
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
			RepoSlug:               repoSpec.RepoSlug,
			Version:                effectiveVersion,
			PackagePath:            repoSpec.PackagePath,
			WorkflowPath:           pkg.ProjectFile.SourcePath,
			WorkflowName:           "aw.json",
			DestinationPath:        pkg.ProjectFile.DestinationPath,
			Host:                   host,
			FromRepositoryManifest: true,
			IsPackageResourceFile:  true,
		})
	}
	parsedSpecs = appendRepositoryPackageExtensionSpecs(parsedSpecs, repoSpec, pkg, effectiveVersion, host)
	for _, spec := range parsedSpecs[firstPackageSpec:] {
		spec.VersionInferred = repoSpec.Version == ""
	}
	return parsedSpecs
}

func appendRepositoryPackageExtensionSpecs(parsedSpecs []*WorkflowSpec, repoSpec *RepoSpec, pkg *resolvedRepositoryPackage, effectiveVersion, host string) []*WorkflowSpec {
	for _, skillFile := range pkg.SkillFiles {
		base := filepath.Base(skillFile.SourcePath)
		workflowName := filepath.Join(skillFile.SkillName, strings.TrimSuffix(base, filepath.Ext(base)))
		parsedSpecs = append(parsedSpecs, &WorkflowSpec{
			RepoSlug:           repoSpec.RepoSlug,
			Version:            effectiveVersion,
			PackagePath:        repoSpec.PackagePath,
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
			RepoSlug:           repoSpec.RepoSlug,
			Version:            effectiveVersion,
			PackagePath:        repoSpec.PackagePath,
			WorkflowPath:       agentFile,
			WorkflowName:       workflowName,
			Host:               host,
			IsPackageAgentFile: true,
		})
	}
	return parsedSpecs
}
