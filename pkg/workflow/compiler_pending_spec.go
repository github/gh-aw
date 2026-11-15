package workflow

import (
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
)

const pendingDriveInputsDir = "/tmp/gh-aw/pending-drive"

func (spec *pendingWriteSpec) addPendingDirectory(publication pendingWritePublication, artifact string) {
	spec.Bindings = append(spec.Bindings, pendingWriteTransport{ID: publication.BindingID, Kind: "directory", Path: publication.Path, Artifact: artifact, Job: publication.Job, Branch: publication.Branch, Repository: publication.Repository})
	spec.Publications = append(spec.Publications, publication)
}

func (spec *pendingWriteSpec) addPendingMemorySpec(data *WorkflowData, prefix string) {
	if data.RepoMemoryConfig == nil {
		return
	}
	for _, memory := range data.RepoMemoryConfig.Memories {
		repository := memory.TargetRepo
		if repository == "" {
			repository = "${{ github.repository }}"
		}
		if memory.Wiki {
			repository += ".wiki"
		}
		spec.addPendingDirectory(pendingWritePublication{ID: memory.ID, Job: pushRepoMemoryJobName, Type: "repo_memory", Kind: "directory", BindingID: "repo_memory:" + memory.ID, Path: constants.TmpRepoMemoryDir + memory.ID, Repository: repository, Branch: memory.BranchName}, prefix+"repo-memory-"+SanitizeWorkflowIDForCacheKey(memory.ID))
	}
}

func (spec *pendingWriteSpec) addPendingCacheSpec(data *WorkflowData, prefix string) {
	if data.CacheMemoryConfig == nil {
		return
	}
	var githubConfig *GitHubToolConfig
	if data.ParsedTools != nil {
		githubConfig = data.ParsedTools.GitHub
	}
	for _, cache := range data.CacheMemoryConfig.Caches {
		if cache.RestoreOnly {
			continue
		}
		artifact := prefix + "cache-memory"
		if cache.ID != "default" {
			artifact += "-" + cache.ID
		}
		key := computeIntegrityCacheKey(cache, githubConfig)
		if !strings.HasSuffix(key, "-${{ github.run_id }}") {
			key += "-${{ github.run_id }}"
		}
		id := "cache_memory:" + cache.ID
		spec.addPendingDirectory(pendingWritePublication{ID: id, Job: updateCacheMemoryJobName, Type: "cache_memory", Kind: "directory", BindingID: id, Path: cacheMemoryDirFor(cache.ID), StorageKey: key}, artifact)
	}
}

func (spec *pendingWriteSpec) addPendingDriveSpec(data *WorkflowData) {
	if data.DriveMemoryConfig == nil {
		return
	}
	for _, drive := range data.DriveMemoryConfig.Drives {
		if drive.RestoreOnly {
			continue
		}
		id := "drive_memory:" + drive.ID
		spec.addPendingDirectory(pendingWritePublication{ID: id, Job: updateDriveMemoryJobName, Type: "drive_memory", Kind: "directory", BindingID: id, Path: path.Join(pendingDriveInputsDir, drive.ID), DriveName: drive.DriveName}, generateDriveMemoryArtifactName(data, drive.ID))
	}
}

func (spec *pendingWriteSpec) addPendingSafeOutputSpec(data *WorkflowData, prefix string) {
	if data.SafeOutputs == nil {
		return
	}
	if data.SafeOutputs.UploadAssets != nil {
		spec.addPendingDirectory(pendingWritePublication{ID: "assets", Job: "upload_assets", Type: "upload_asset", Kind: "directory", BindingID: "assets", Path: constants.TmpGhAwAssetsDir, Branch: data.SafeOutputs.UploadAssets.BranchName}, prefix+"safe-outputs-assets")
	}
	if data.SafeOutputs.UploadArtifact != nil {
		spec.Bindings = append(spec.Bindings, pendingWriteTransport{ID: "upload_artifacts", Kind: "directory", Path: artifactStagingDirExpr, Artifact: prefix + SafeOutputsUploadArtifactStagingArtifactName})
	}
	if data.SafeOutputs.UploadCodeCoverage != nil {
		spec.addPendingDirectory(pendingWritePublication{ID: "coverage", Job: "upload_code_coverage", Type: "upload_code_coverage", Kind: "directory", BindingID: "coverage", Path: codeCoverageDownloadPath}, prefix+SafeOutputsUploadCodeCoverageStagingArtifactName)
	}
	if data.SafeOutputs.CreateCodeScanningAlerts != nil {
		spec.Publications = append(spec.Publications, pendingWritePublication{ID: "sarif", Job: "upload_code_scanning_sarif", Type: "create_code_scanning_alert", Kind: "sarif", Path: path.Join(constants.SarifArtifactDownloadPath.String(), constants.SarifFileName.String())})
	}
	if data.SafeOutputs.CallWorkflow != nil {
		for _, worker := range data.SafeOutputs.CallWorkflow.Workflows {
			spec.Publications = append(spec.Publications, pendingWritePublication{ID: worker, Job: "call-" + sanitizeJobName(worker), Type: "call_workflow", Kind: "call", Worker: worker})
		}
	}
	for name := range data.SafeOutputs.Jobs {
		normalized := stringutil.NormalizeSafeOutputIdentifier(name)
		spec.CustomTypes = append(spec.CustomTypes, normalized)
		spec.Publications = append(spec.Publications, pendingWritePublication{ID: normalized, Job: normalized, Type: "custom_job", Kind: "file", BindingID: "agent_output", Path: SafeJobsDownloadDirExpr + constants.AgentOutputFilename.String()})
	}
	for name := range data.SafeOutputs.Scripts {
		spec.CustomTypes = append(spec.CustomTypes, stringutil.NormalizeSafeOutputIdentifier(name))
	}
	for name := range data.SafeOutputs.Actions {
		spec.CustomTypes = append(spec.CustomTypes, stringutil.NormalizeSafeOutputIdentifier(name))
	}
}
